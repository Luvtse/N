package handlers

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// WSBridge connects the Kafka event stream to the WebSocket hub. Without it,
// the hub is an island: clients can connect and subscribe to topics such as
// "ride:<id>" / "driver:<id>", but nothing ever publishes into h.broadcast,
// so riders never see live ride status and drivers never receive new offers.
//
// It consumes the canonical bus topic ("nidaw.events" — note KafkaBus.Publish
// writes every event there regardless of the logical topic argument) and fans
// ride lifecycle events out to the per-ride room plus the driver's personal
// room, using the exact wire shape the Flutter apps parse:
//
//	{type:"ride_update", payload:{topic, data:{ride_id,status,...}}}
//
// The client-side subscription filter reads payload["topic"], so the topic
// field must be set for delivery (see websocket_handler.run()).
// HubIntf is the minimal surface WSBridge needs from the WebSocket hub. The
// concrete *WebSocketHandler satisfies it; tests substitute a recorder.
type HubIntf interface {
	BroadcastToRide(rideID uuid.UUID, message Message)
	BroadcastToDriver(driverID uuid.UUID, message Message)
	BroadcastToTopic(topic string, payload map[string]interface{}) error
}

type WSBridge struct {
	hub    HubIntf
	logger *zap.Logger

	// dedupMu guards seenRideIDs: the bounded LRU of recently fanned-out
	// idempotency keys (see fanoutKey). The outbox publisher guarantees
	// at-least-once delivery, so duplicate bus events with the same stable
	// event key are an expected condition, not a bug; the bridge suppresses
	// them before they reach client rooms.
	dedupMu     sync.Mutex
	seenRideIDs []string // FIFO order; oldest evicted at capacity
	seenIndex   map[string]struct{}
}

const (
	// maxDedupeKeys bounds memory: only ride-lifecycle events are keyed, and
	// a ride emits at most a handful of lifecycle transitions, so this window
	// covers realistic broker-replay bursts while staying small.
	maxDedupeKeys = 4096
)

// NewWSBridge creates the bridge for a hub. logger may be nil.
func NewWSBridge(hub HubIntf) *WSBridge {
	return &WSBridge{
		hub:       hub,
		seenIndex: make(map[string]struct{}),
	}
}

// fanoutKey returns the consumer-deduplication idempotency key for an event,
// or "" when the event is not eligible for dedupe. Ride lifecycle events are
// keyed on (event type, ride_id) because the transactional outbox uses
// event_id = ride_id (one row per ride transition): a redelivered duplicate
// carries both values unchanged, while legitimately distinct transitions of
// the same ride differ in type and therefore never collide here. Location
// updates are high-frequency ephemeral GPS samples with no business side
// effects, so they stay unkeyed.
func fanoutKey(ev eventbus.Event) string {
	switch ev.Type {
	case "ride.matched", "ride.started", "ride.completed", "ride.cancelled":
	default:
		return ""
	}
	rideID := extractUUID(ev.Payload, "ride_id")
	if rideID == uuid.Nil {
		return ""
	}
	return ev.Type + ":" + rideID.String()
}

// alreadyFannedOut reports whether key was seen before, recording it and
// evicting the oldest entry when the bounded window is full.
func (b *WSBridge) alreadyFannedOut(key string) bool {
	b.dedupMu.Lock()
	defer b.dedupMu.Unlock()
	if _, dup := b.seenIndex[key]; dup {
		return true
	}
	if len(b.seenRideIDs) >= maxDedupeKeys {
		oldest := b.seenRideIDs[0]
		b.seenRideIDs = b.seenRideIDs[1:]
		delete(b.seenIndex, oldest)
	}
	b.seenRideIDs = append(b.seenRideIDs, key)
	b.seenIndex[key] = struct{}{}
	return false
}

// SetLogger attaches an optional structured warn logger.
func (b *WSBridge) SetLogger(l *zap.Logger) {
	b.logger = l
}

// Subscribe registers the bridge on the bus so every event published to the
// canonical topic ("nidaw.events" — KafkaBus.Publish writes all events there
// regardless of the logical topic argument) is fanned out to the WS hub.
// This is the wiring that keeps the hub from being an island: without it,
// clients connect and subscribe to topics but receive nothing (§ live ride
// updates / driver offer feed). Call exactly once at boot, after the hub and
// bus exist. Errors from Handle are logged and swallowed by design: a dropped
// live update never affects money movement (REST + ledger stay authoritative).
func (b *WSBridge) Subscribe(ctx context.Context, bus eventbus.EventBus) error {
	if b == nil || b.hub == nil || bus == nil {
		return nil
	}
	return bus.Subscribe(ctx, "nidaw.events", func(ev eventbus.Event) {
		if err := b.Handle(ev); err != nil {
			if b.logger != nil {
				b.logger.Warn("ws bridge: broadcast failed",
					zap.String("event_type", ev.Type), zap.Error(err))
			} else {
				log.Printf("ws bridge: broadcast failed for %s: %v", ev.Type, err)
			}
		}
	})
}

// Handle processes one bus event. Unknown types are ignored. Returns nil
// always (broadcasting is best-effort; a dropped live update never affects
// money movement — REST + ledger settlement remain authoritative).
func (b *WSBridge) Handle(ev eventbus.Event) error {
	if b == nil || b.hub == nil {
		return nil
	}

	// Consumer idempotency: at-least-once delivery means a redelivered ride
	// lifecycle event must not fan out to client rooms twice. Keyed on
	// (type, ride_id), matching the outbox's event_id = ride_id contract.
	if key := fanoutKey(ev); key != "" && b.alreadyFannedOut(key) {
		if b.logger != nil {
			b.logger.Debug("ws bridge: suppressed duplicate event",
				zap.String("event_type", ev.Type), zap.String("dedupe_key", key))
		}
		return nil
	}

	rideID := extractUUID(ev.Payload, "ride_id")

	switch ev.Type {
	case "ride.requested":
		// Offer feed: notify all subscribed drivers that a new ride exists.
		_ = b.hub.BroadcastToTopic("driver:offers", map[string]interface{}{
			"topic":   "driver:offers",
			"ride_id": rideID.String(),
		})

	case "ride.matched", "ride.started", "ride.completed", "ride.cancelled":
		data := map[string]interface{}{
			"ride_id":  rideID.String(),
			"status":   strings_trim(ev.Type),
			"type":     ev.Type,
			"event_ts": ev.Timestamp,
		}
		for k, v := range ev.Payload {
			data[k] = v
		}
		if rideID != uuid.Nil {
			b.hub.BroadcastToRide(rideID, Message{
				Type: "ride_update",
				Payload: map[string]interface{}{
					"topic": "ride:" + rideID.String(),
					"data":  data,
				},
				Timestamp: time.Now().Unix(),
			})
		}
		// Mirror to the assigned driver's personal room (offer accepted state).
		if driverID := extractUUID(ev.Payload, "driver_id"); driverID != uuid.Nil {
			b.hub.BroadcastToDriver(driverID, Message{
				Type: "ride_update",
				Payload: map[string]interface{}{
					"topic": "driver:" + driverID.String(),
					"data":  data,
				},
				Timestamp: time.Now().Unix(),
			})
		}

	case "driver.location_updated":
		// Live GPS during tracking: fan out to the ride room when a ride_id is
		// present, else to the driver's own room (echo/self-check).
		if rideID != uuid.Nil {
			b.hub.BroadcastToRide(rideID, Message{
				Type: "driver_location",
				Payload: map[string]interface{}{
					"topic": "ride:" + rideID.String(),
					"data":  ev.Payload,
				},
				Timestamp: time.Now().Unix(),
			})
		} else if driverID := extractUUID(ev.Payload, "driver_id"); driverID != uuid.Nil {
			b.hub.BroadcastToDriver(driverID, Message{
				Type: "driver_location",
				Payload: map[string]interface{}{
					"topic": "driver:" + driverID.String(),
					"data":  ev.Payload,
				},
				Timestamp: time.Now().Unix(),
			})
		}
	}
	return nil
}

// strings_trim converts "ride.matched" -> "matched" (client status vocabulary).
func strings_trim(eventType string) string {
	for i := len(eventType) - 1; i >= 0; i-- {
		if eventType[i] == '.' {
			return eventType[i+1:]
		}
	}
	return eventType
}

// extractUUID pulls a UUID from a JSON-ish payload value tolerantly: the value
// may already be a string, or arrive as json.Number/map after re-marshalling.
func extractUUID(payload map[string]interface{}, key string) uuid.UUID {
	v, ok := payload[key]
	if !ok {
		return uuid.Nil
	}
	switch s := v.(type) {
	case string:
		if id, err := uuid.Parse(s); err == nil {
			return id
		}
	case []byte:
		if id, err := uuid.ParseBytes(s); err == nil {
			return id
		}
	default:
		// Re-marshal then parse (covers map/slice round-trips through Kafka).
		if b, err := json.Marshal(v); err == nil {
			var str string
			if json.Unmarshal(b, &str) == nil {
				if id, err := uuid.Parse(str); err == nil {
					return id
				}
			}
		}
	}
	return uuid.Nil
}
