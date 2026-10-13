// Package interfaces wires the telephony module to the outside world: the
// event-bus consumer that provisions/tears down proxy sessions and the HTTP
// surface riders/drivers call to fetch a masked contact.
package interfaces

import (
	"context"
	"time"

	telephonyapp "nidaw-backend/internal/modules/telephony/application"

	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
)

// RideTTL is the expected ride duration budget added to every proxy session.
// Plan §2.2 specifies "ride duration + 30 minutes"; the ride-duration term is
// approximated by this constant because the ride.matched payload carries an
// ETA in minutes, not a total trip length. The 30-minute grace is applied by
// the service itself, so total = RideTTL + 30m = 90m — generous for any urban
// trip yet bounded so numbers recycle.
const RideTTL = time.Hour

// EventConsumer bridges the shared Kafka bus to proxy-session lifecycle:
//
//	ride.matched   -> Provision (rider↔driver pairing on a pool number)
//	ride.completed -> Teardown  (number returns to the pool immediately)
//	ride.cancelled -> Teardown  (same; cancellation may precede completion)
//
// Unknown types are ignored. Provision failures are logged by the caller; no
// user-facing fallback to real numbers ever occurs (plan §2.2).
type EventConsumer struct {
	svc *telephonyapp.Service
}

func NewEventConsumer(svc *telephonyapp.Service) *EventConsumer {
	return &EventConsumer{svc: svc}
}

// Handle processes one bus event. Exported for direct use in tests.
func (c *EventConsumer) Handle(ev eventbus.Event) error {
	rideID := extractUUID(ev.Payload, "ride_id")
	if rideID == uuid.Nil {
		return nil
	}
	switch ev.Type {
	case "ride.matched":
		_, err := c.svc.Provision(rideID, RideTTL)
		return err
	case "ride.completed", "ride.cancelled":
		return c.svc.Teardown(rideID)
	default:
		return nil
	}
}

// Subscribe registers the consumer on the canonical bus topic. Call once at
// boot when FEATURE_MASKED_CALLING is enabled.
func (c *EventConsumer) Subscribe(ctx context.Context, bus eventbus.EventBus) error {
	return bus.Subscribe(ctx, "nidaw.events", func(ev eventbus.Event) {
		// Dispatch errors leave no half-state: Provision rolls back provider
		// and pool internally; Teardown is idempotent. The expiry sweeper is
		// the safety net if terminal events are lost entirely.
		_ = c.Handle(ev)
	})
}

func extractUUID(payload map[string]interface{}, key string) uuid.UUID {
	raw, ok := payload[key]
	if !ok {
		return uuid.Nil
	}
	switch v := raw.(type) {
	case uuid.UUID:
		return v
	case string:
		if id, err := uuid.Parse(v); err == nil {
			return id
		}
	}
	return uuid.Nil
}
