package commands

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	nidusevents "nidaw-backend/internal/modules/nidus/domain/events"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"nidaw-backend/internal/shared/observability"
)

// ============================================================================
// SCHEDULED-RIDE REMINDER WORKER (§3.6)
// ============================================================================
//
// The plan requires a scheduled_ride_reminder notification dispatched 30
// minutes before pickup. Before this worker, the category, the localized
// dispatcher templates (EN/AM/OM), and the Flutter notification-router
// mapping all existed — but nothing ever emitted the event, so the whole
// path was dead traffic waiting for a producer. This file is that producer.
//
// Design mirrors AutoMatcher (same zap-logger worker style):
//   - Run blocks on a ticker; Tick is exported for deterministic testing.
//   - Each tick scans rides still awaiting matching whose pickup falls
//     inside a +/-5 minute window around T-30:
//       status = 'requested'
//       AND scheduled_at >  NOW() + INTERVAL '25 minutes'
//       AND scheduled_at <= NOW() + INTERVAL '35 minutes'
//       AND scheduled_reminder_sent_at IS NULL
//     The window (rather than an exact timestamp) tolerates tick cadence up
//     to 10 minutes without skipping or duplicating reminders.
//   - Multi-replica safety: the ledger stamp is a race-guarded UPDATE
//       UPDATE rides SET scheduled_reminder_sent_at = NOW()
//        WHERE id = $1 AND scheduled_reminder_sent_at IS NULL
//     RowsAffected == 0 means another replica won the stamp; this replica
//     skips publishing so exactly one reminder event exists per ride.
//   - Publish happens only AFTER the durable stamp, so a crash between the
//     two can at worst drop a reminder (never duplicate). Dropping is the
//     honest failure mode; fabricating a resend would risk double-notifying.
//
// Kill-switch: the worker is wired in cmd/server/main.go only when
// notifications are enabled — producing events no consumer will dispatch is
// dead traffic, which Global Rule 5 forbids.

// ScheduledReminderConfig tunes the reminder worker.
type ScheduledReminderConfig struct {
	Interval time.Duration // poll cadence (default 60s; the scan window is +/-5 min around T-30)
	Batch    int           // rides per tick (default 50)
}

func (c ScheduledReminderConfig) withDefaults() ScheduledReminderConfig {
	if c.Interval <= 0 {
		c.Interval = 60 * time.Second
	}
	if c.Batch <= 0 {
		c.Batch = 50
	}
	return c
}

// ScheduledReminderWorker periodically emits ride.scheduled_reminder events
// ~30 minutes before each scheduled pickup.
type ScheduledReminderWorker struct {
	db       *database.Postgres
	eventBus eventbus.EventBus
	cfg      ScheduledReminderConfig
	log      *zap.Logger
}

// NewScheduledReminderWorker wires the worker. eventBus may be nil (events
// then skipped, matching AutoMatcher's tolerance for test harnesses).
func NewScheduledReminderWorker(db *database.Postgres, eventBus eventbus.EventBus, cfg ScheduledReminderConfig, log *zap.Logger) *ScheduledReminderWorker {
	if log == nil {
		log = zap.NewNop()
	}
	return &ScheduledReminderWorker{db: db, eventBus: eventBus, cfg: cfg.withDefaults(), log: log}
}

// Run blocks until ctx is cancelled.
func (w *ScheduledReminderWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.Tick(ctx)
		}
	}
}

// dueRide is the minimal projection the worker needs per ride.
type dueRide struct {
	id          uuid.UUID
	userID      uuid.UUID
	scheduledAt time.Time
	pickupAddr  string
	rideType    string
}

// Tick performs one reminder pass (exported for deterministic testing).
// Returns the number of reminders published (0 when nothing is due).
func (w *ScheduledReminderWorker) Tick(ctx context.Context) int {
	rows, err := w.db.Query(ctx, `
		SELECT id, user_id, scheduled_at,
		       COALESCE(pickup_address, ''), ride_type
		  FROM rides
		 WHERE status = 'requested'
		   AND scheduled_at IS NOT NULL
		   AND scheduled_reminder_sent_at IS NULL
		   AND scheduled_at >  NOW() + INTERVAL '25 minutes'
		   AND scheduled_at <= NOW() + INTERVAL '35 minutes'
		 ORDER BY scheduled_at ASC
		 LIMIT $1`, w.cfg.Batch)
	if err != nil {
		w.log.Warn("scheduled-reminder: failed to scan due rides", zap.Error(err))
		return 0
	}
	var due []dueRide
	for rows.Next() {
		var r dueRide
		if err := rows.Scan(&r.id, &r.userID, &r.scheduledAt, &r.pickupAddr, &r.rideType); err != nil {
			continue
		}
		due = append(due, r)
	}
	rows.Close()

	published := 0
	for _, r := range due {
		if ctx.Err() != nil {
			return published
		}
		// Race-guarded ledger stamp BEFORE publish: exactly one replica wins
		// the NULL -> timestamp transition per ride.
		tag, err := w.db.Exec(ctx, `
			UPDATE rides
			   SET scheduled_reminder_sent_at = NOW(), updated_at = NOW()
			 WHERE id = $1
			   AND scheduled_reminder_sent_at IS NULL`, r.id)
		if err != nil {
			w.log.Warn("scheduled-reminder: stamp failed, will retry next tick",
				zap.Stringer("ride_id", r.id), zap.Error(err))
			continue
		}
		if tag.RowsAffected() == 0 {
			// Another replica already stamped (and will publish / has
			// published) this ride. Skip — never double-notify.
			continue
		}
		ev := &nidusevents.ScheduledRideReminder{
			RideID:      r.id,
			UserID:      r.userID,
			ScheduledAt: r.scheduledAt,
			PickupAddr:  r.pickupAddr,
			RideType:    r.rideType,
		}
		if w.eventBus != nil {
			if err := w.eventBus.Publish(ctx, "nidus.rides", eventbus.Event{
				Type:      ev.ToEvent().Type,
				Payload:   ev.ToEvent().Payload,
				Timestamp: ev.ToEvent().Timestamp.Unix(),
			}); err != nil {
				// The stamp is durable; the event is not. We deliberately do
				// NOT roll back the stamp: a dropped reminder is honest, a
				// rollback racing against other replicas is worse. Ops can
				// see the gap via the counter below.
				w.log.Warn("scheduled-reminder: publish failed after stamp",
					zap.Stringer("ride_id", r.id), zap.Error(err))
				continue
			}
		}
		published++
		if obs := observability.Nidus(); obs != nil {
			obs.RecordTick("reminder")
		}
		w.log.Info("scheduled-reminder: emitted reminder",
			zap.Stringer("ride_id", r.id),
			zap.Time("scheduled_at", r.scheduledAt))
	}
	return published
}
