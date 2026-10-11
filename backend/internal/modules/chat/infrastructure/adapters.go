package infrastructure

import (
	"context"
	"encoding/json"
	"time"

	chatapp "nidaw-backend/internal/modules/chat/application"
	chatdomain "nidaw-backend/internal/modules/chat/domain"
	nidushandlers "nidaw-backend/internal/modules/nidus/interfaces/http/handlers"

	"nidaw-backend/internal/shared/database"

	"github.com/google/uuid"
)

// Compile-time port checks.
var (
	_ chatapp.RideLookup   = (*RideStoreLookup)(nil)
	_ chatapp.RealtimePort = (*WSRealtime)(nil)
)

// RideStoreLookup implements chat's RideLookup over the rides table. It reads
// only user_id/driver_id — the chat module never mutates ride state. When the
// DB is not ready it returns ErrRidesUnavailable so Send fails closed rather
// than posting into unverifiable conversations.
type RideStoreLookup struct {
	db *database.Postgres
}

// NewRideStoreLookup wires the lookup to the shared Postgres handle.
func NewRideStoreLookup(db *database.Postgres) *RideStoreLookup {
	return &RideStoreLookup{db: db}
}

// Participants resolves the ride's rider and (matched) driver ids.
func (l *RideStoreLookup) Participants(rideID uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	if l.db == nil || !l.db.IsReady() {
		return uuid.Nil, uuid.Nil, chatapp.ErrRidesUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var rider uuid.UUID
	var driver *uuid.UUID
	err := l.db.QueryRow(ctx,
		`SELECT user_id, driver_id FROM rides WHERE id = $1`, rideID).
		Scan(&rider, &driver)
	if err != nil {
		return uuid.Nil, uuid.Nil, chatapp.ErrRidesUnavailable
	}
	if driver == nil {
		return rider, uuid.Nil, nil
	}
	return rider, *driver, nil
}

// WSRealtime implements chatapp.RealtimePort over the shared nidus WebSocket
// hub, publishing every new message onto topic "chat.ride.{rideId}" — the
// exact topic name locked in plan §2.1. Delivery here is a live hint; the
// persisted row plus GET history are authoritative.
type WSRealtime struct {
	hub *nidushandlers.WebSocketHandler
}

// NewWSRealtime wraps the hub. A nil hub degrades to no-op publishes.
func NewWSRealtime(hub *nidushandlers.WebSocketHandler) *WSRealtime {
	return &WSRealtime{hub: hub}
}

// PublishChat fans one message out to the ride's chat topic.
func (r *WSRealtime) PublishChat(rideID uuid.UUID, msg *chatdomain.Message) {
	if r == nil || r.hub == nil || msg == nil {
		return
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}
	topic := "chat.ride." + rideID.String()
	payload["topic"] = topic
	_ = r.hub.BroadcastToTopic(topic, payload)
}
