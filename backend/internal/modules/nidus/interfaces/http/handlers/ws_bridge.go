package handlers

import (
	"encoding/json"
	"time"

	"nidaw-backend/internal/shared/eventbus"

	"github.com/google/uuid"
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
type WSBridge struct {
	hub    *WebSocketHandler
	logger interface{ Warnf(format string, args ...interface{}) }
}

// NewWSBridge creates the bridge for a hub. logger may be nil.
func NewWSBridge(hub *WebSocketHandler) *WSBridge {
	return &WSBridge{hub: hub}
}

// SetLogger attaches an optional printf-style warn logger.
func (b *WSBridge) SetLogger(l interface{ Warnf(format string, args ...interface{}) }) {
	b.logger = l
}

// Handle processes one bus event. Unknown types are ignored. Returns nil
// always (broadcasting is best-effort; a dropped live update never affects
// money movement — REST + ledger settlement remain authoritative).
func (b *WSBridge) Handle(ev eventbus.Event) error {
	if b == nil || b.hub == nil {
		return nil
	}

	rideID := extractUUID(ev.Payload, "ride_id")

	switch ev.Type {
	case "ride.requested":
		// Offer feed: notify all subscribed drivers that a new ride exists.
		b.hub.broadcast <- Message{
			Type: "ride_offer_new",
			Payload: map[string]interface{}{
				"topic":   "driver:offers",
				"ride_id": rideID.String(),
			},
			Timestamp: time.Now().Unix(),
		}

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
		if !rideID.IsNil() {
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
		if driverID := extractUUID(ev.Payload, "driver_id"); !driverID.IsNil() {
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
		if !rideID.IsNil() {
			b.hub.BroadcastToRide(rideID, Message{
				Type: "driver_location",
				Payload: map[string]interface{}{
					"topic": "ride:" + rideID.String(),
					"data":  ev.Payload,
				},
				Timestamp: time.Now().Unix(),
			})
		} else if driverID := extractUUID(ev.Payload, "driver_id"); !driverID.IsNil() {
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
