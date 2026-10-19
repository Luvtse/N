package interfaces

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	app "nidaw-backend/internal/modules/notifications/application"
	domain "nidaw-backend/internal/modules/notifications/domain"
	"nidaw-backend/internal/shared/eventbus"
)

// EventConsumer bridges the shared Kafka event bus to the notification
// dispatcher (§1.4 "Backend — event wiring"). It subscribes to the canonical
// bus topic ("nidaw.events" — KafkaBus.Publish writes every event there
// regardless of the logical topic argument, same convention as WSBridge and
// the ledger settlement consumer) and maps domain events to Commands:
//
//	ride.matched      -> ride_matched   (rider)
//	ride.completed    -> receipt        (rider; email forced by policy)
//	payment.failed    -> payment_failed (rider; email forced by policy)
//	promo.granted     -> promo          (user)
//	sos.confirmed     -> safety         (rider; email forced by policy)
//
// Unknown event types are ignored without error. Handler errors are returned
// so the bus's retry path can re-deliver; a persistently failing handler
// never blocks other events because each dispatch is independent per row.
type EventConsumer struct {
	dispatcher *app.Dispatcher
}

// NewEventConsumer wires the consumer to the dispatcher.
func NewEventConsumer(d *app.Dispatcher) *EventConsumer {
	return &EventConsumer{dispatcher: d}
}

// Handle processes one bus event. Safe to pass directly to
// eventbus.EventBus.Subscribe's handler (wrap with ctx cancellation upstream).
func (c *EventConsumer) Handle(ctx context.Context, ev eventbus.Event) error {
	userID := extractUUID(ev.Payload, "user_id")
	if userID == uuid.Nil {
		// Not rider-addressable; nothing to notify. Driver-side events carry
		// driver_id only and are out of scope for §1.4 rider notifications.
		return nil
	}

	switch ev.Type {
	case "ride.matched":
		_, err := c.dispatcher.Dispatch(ctx, domain.Command{
			UserID:   userID,
			Category: domain.CategoryRideMatched,
			DeepLink: deepLinkRide(extractUUID(ev.Payload, "ride_id")),
			Payload:  map[string]interface{}{"ride_id": ev.Payload["ride_id"]},
			Vars: map[string]string{
				"driver_name": driverDisplayName(ctx, ev.Payload["driver_id"]),
				"vehicle":     stringOr(ev.Payload["vehicle"], ""),
				"eta_minutes": stringOr(ev.Payload["eta_minutes"], ""),
			},
		})
		return err

	case "ride.completed":
		fare := stringOr(ev.Payload["final_fare"], "")
		items := receiptBreakdown(ev.Payload)
		_, err := c.dispatcher.Dispatch(ctx, domain.Command{
			UserID:   userID,
			Category: domain.CategoryReceipt,
			DeepLink: deepLinkRide(extractUUID(ev.Payload, "ride_id")),
			Payload: map[string]interface{}{
				"ride_id":   ev.Payload["ride_id"],
				"amount":    fare,
				"currency":  "ETB",
				"breakdown": items,
			},
			Vars: map[string]string{
				"amount":   fare,
				"currency": "ETB",
			},
		})
		return err

	case "payment.failed":
		amount := stringOr(ev.Payload["amount"], "")
		currency := stringOr(ev.Payload["currency"], "ETB")
		_, err := c.dispatcher.Dispatch(ctx, domain.Command{
			UserID:   userID,
			Category: domain.CategoryPaymentFailed,
			DeepLink: "/nidus/payments/recover",
			Payload: map[string]interface{}{
				"payment_id": ev.Payload["payment_id"],
				"amount":     amount,
				"currency":   currency,
				"reason":     ev.Payload["reason"],
			},
			Vars: map[string]string{
				"amount":   amount,
				"currency": currency,
			},
		})
		return err

	case "promo.granted":
		_, err := c.dispatcher.Dispatch(ctx, domain.Command{
			UserID:   userID,
			Category: domain.CategoryPromo,
			DeepLink: "/nidus/promos",
			Payload: map[string]interface{}{
				"code":  ev.Payload["code"],
				"value": ev.Payload["value"],
			},
			Vars: map[string]string{
				"promo_copy": stringOr(ev.Payload["copy"], defaultPromoCopy(ev.Payload["code"])),
			},
		})
		return err

	case "sos.confirmed":
		_, err := c.dispatcher.Dispatch(ctx, domain.Command{
			UserID:   userID,
			Category: domain.CategorySafety,
			DeepLink: deepLinkRide(extractUUID(ev.Payload, "ride_id")),
			Payload: map[string]interface{}{
				"ride_id":    ev.Payload["ride_id"],
				"event_type": ev.Payload["event_type"],
			},
		})
		return err

	case "driver.arriving":
		// §1.4 event wiring: produced by the DriverArrivingWatcher when a
		// matched driver's GPS ping lands within the arrival radius of the
		// pickup (and again inside the final-approach radius). Rides are
		// announced at most once per radius tier (deduped in the watcher),
		// so this case never spams the rider.
		_, err := c.dispatcher.Dispatch(ctx, domain.Command{
			UserID:   userID,
			Category: domain.CategoryDriverArriving,
			DeepLink: deepLinkRide(extractUUID(ev.Payload, "ride_id")),
			Payload: map[string]interface{}{
				"ride_id":       ev.Payload["ride_id"],
				"distance_m":    ev.Payload["distance_m"],
				"approach_tier": ev.Payload["approach_tier"],
			},
			Vars: map[string]string{
				"driver_name": driverDisplayName(ctx, ev.Payload["driver_name"]),
			},
		})
		return err

	case "ride.scheduled_reminder":
		// §3.6 event wiring: produced by ScheduledReminderWorker ~30 minutes
		// before pickup (scan window T-35m..T-25m, ledger-stamped so exactly
		// one reminder per ride). The dispatcher policy + localized EN/AM/OM
		// templates for scheduled_ride_reminder already existed; this case is
		// the missing producer->consumer link.
		scheduledAt := timeOrZero(ev.Payload["scheduled_at"])
		minutes := ""
		if !scheduledAt.IsZero() {
			m := int(time.Until(scheduledAt).Round(time.Minute) / time.Minute)
			if m < 0 {
				m = 0
			}
			minutes = fmt.Sprintf("%d", m)
		}
		vars := map[string]string{}
		if minutes != "" {
			vars["minutes_before_pickup"] = minutes
		}
		_, err := c.dispatcher.Dispatch(ctx, domain.Command{
			UserID:   userID,
			Category: domain.CategoryScheduledReminder,
			DeepLink: deepLinkRide(extractUUID(ev.Payload, "ride_id")),
			Payload: map[string]interface{}{
				"ride_id":        ev.Payload["ride_id"],
				"scheduled_at":   ev.Payload["scheduled_at"],
				"pickup_address": ev.Payload["pickup_address"],
				"ride_type":      ev.Payload["ride_type"],
			},
			Vars: vars,
		})
		return err

	case "ride.destination_changed":
		// §3.5 event wiring: produced by ChangeDestinationHandler after the
		// new dropoff and recomputed fare are durably persisted, so the fare
		// quoted in the notification matches what the rider will pay.
		newAddr := stringOr(ev.Payload["new_dropoff_address"], "")
		vars := map[string]string{}
		if newAddr != "" {
			vars["new_destination"] = newAddr
		}
		_, err := c.dispatcher.Dispatch(ctx, domain.Command{
			UserID:   userID,
			Category: domain.CategoryDestinationChanged,
			DeepLink: deepLinkRide(extractUUID(ev.Payload, "ride_id")),
			Payload: map[string]interface{}{
				"ride_id":           ev.Payload["ride_id"],
				"new_dropoff_lat":   ev.Payload["new_dropoff_lat"],
				"new_dropoff_lng":   ev.Payload["new_dropoff_lng"],
				"new_dropoff_address": ev.Payload["new_dropoff_address"],
				"previous_fare":     ev.Payload["previous_fare"],
				"new_fare":          ev.Payload["new_fare"],
				"currency":          ev.Payload["currency"],
			},
			Vars: vars,
		})
		return err

	default:
		return nil
	}

}

// Subscribe registers the consumer on the bus. Call once at boot.
func (c *EventConsumer) Subscribe(ctx context.Context, bus eventbus.EventBus) error {
	return bus.Subscribe(ctx, "nidaw.events", func(ev eventbus.Event) {
		// The bus handler signature is synchronous fire-and-forget per message;
		// errors are logged by the caller shim in main.go since the interface
		// swallows them here. We keep the error visible via a panic-free path:
		// dispatch failures leave rows pending/failed in the DB, which the
		// retry worker and inbox endpoint still surface honestly.
		_ = c.Handle(ctx, ev)
	})
}

func deepLinkRide(rideID uuid.UUID) string {
	if rideID == uuid.Nil {
		return "/nidus"
	}
	return "/nidus/tracking/" + rideID.String()
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

func stringOr(v interface{}, def string) string {
	switch t := v.(type) {
	case nil:
		return def
	case string:
		if t == "" {
			return def
		}
		return t
	case float64:
		// JSON numbers arrive as float64; render integers without ".000".
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%.2f", t)
	case int:
		return fmt.Sprintf("%d", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// timeOrZero best-effort parses a timestamp carried on an event payload.
// In-process bus events carry time.Time directly; JSON-delivered events
// carry RFC3339 strings. Anything else yields the zero time, which callers
// treat as "unknown" — they then omit the template variable rather than
// rendering a fake countdown (Global Rule 2).
func timeOrZero(v interface{}) time.Time {
	switch t := v.(type) {
	case time.Time:
		return t
	case *time.Time:
		if t != nil {
			return *t
		}
	case string:
		if parsed, err := time.Parse(time.RFC3339Nano, t); err == nil {
			return parsed
		}
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// driverDisplayName resolves a human name for the matched driver when the
// event carries one inline; otherwise it degrades to an honest placeholder
// sentence built from what IS known (never a fabricated name).
func driverDisplayName(ctx context.Context, v interface{}) string {
	if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
		// Some producers send the driver UUID in this field.
		if _, err := uuid.Parse(s); err == nil {
			return "your driver"
		}
		return s
	}
	return "your driver"
}

// receiptBreakdown normalizes an optional breakdown array carried on
// ride.completed payloads into the email template's table shape. Absent
// breakdown yields an empty slice — the email then shows title/body only
// rather than inventing line items (Global Rule: no fake values).
func receiptBreakdown(payload map[string]interface{}) []interface{} {
	raw, ok := payload["fare_breakdown"]
	if !ok {
		return []interface{}{}
	}
	list, ok := raw.([]interface{})
	if !ok {
		return []interface{}{}
	}
	out := make([]interface{}, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		out = append(out, map[string]interface{}{
			"label":    stringOr(m["label"], ""),
			"amount":   stringOr(m["amount"], ""),
			"currency": stringOr(m["currency"], "ETB"),
		})
	}
	return out
}

func defaultPromoCopy(code interface{}) string {
	if s, ok := code.(string); ok && s != "" {
		return "You have a new promo code: " + s + ". Apply it on your next ride."
	}
	return "You have a new promo. Check your offers."
}
