// Package adapters bridges the ledger command layer to the rest of the
// NIDAW platform: Kafka events (inbound ride.completed, outbound domain
// events) and shared infrastructure. Phase E/G will add provider adapters
// (Telebirr / Chapa / M-Pesa) and the fraud evaluator beside these.
package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"nidaw-backend/internal/modules/ledger/application/commands"
	"nidaw-backend/internal/modules/ledger/application/services"
	nidusevents "nidaw-backend/internal/modules/nidus/domain/events"
	"nidaw-backend/internal/shared/eventbus"
)

// ============================================================================
// OUTBOUND: commands.EventPublisher -> eventbus.EventBus
// ============================================================================

// EventBusPublisher implements commands.EventPublisher on top of the shared
// Kafka bus. Publishing failures are returned so callers can log + alert
// without rolling back committed money movements; Phase I hardening moves
// this to a transactional outbox table so publishes commit atomically with
// money movements.
type EventBusPublisher struct {
	bus   eventbus.EventBus
	topic string // default "nidaw.events" (matches KafkaBus writer default)
}

// NewEventBusPublisher wires the ledger to the platform event bus.
func NewEventBusPublisher(bus eventbus.EventBus) *EventBusPublisher {
	return &EventBusPublisher{bus: bus, topic: "nidaw.events"}
}

// SetTopic overrides the publish topic (tests / multi-tenant deployments).
func (p *EventBusPublisher) SetTopic(topic string) { p.topic = topic }

// Publish satisfies commands.EventPublisher. The services.DBTx parameter is
// intentionally unused in this transport (see outbox note above).
func (p *EventBusPublisher) Publish(ctx context.Context, _ services.DBTx, ev commands.Event) error {
	if p.bus == nil {
		return nil
	}
	return p.bus.Publish(ctx, p.topic, eventbus.Event{
		Type:      ev.Type,
		Payload:   ev.Payload,
		Timestamp: ev.Timestamp,
	})
}

var _ commands.EventPublisher = (*EventBusPublisher)(nil)

// ============================================================================
// INBOUND: ride.completed consumer -> ProcessRidePayment
// ============================================================================

// RideSettlementHandler consumes "ride.completed" events from the nidus rides
// topic and settles fares through the ledger (escrow credit included). It is
// safe for concurrent use; idempotency is enforced inside the ledger
// (ride-id key), so Kafka redelivery cannot double-charge.
type RideSettlementHandler struct {
	deps *commands.Deps
}

// NewRideSettlementHandler constructs the consumer callback.
func NewRideSettlementHandler(deps *commands.Deps) *RideSettlementHandler {
	return &RideSettlementHandler{deps: deps}
}

// Handle processes one eventbus.Event of type "ride.completed". Unknown types
// are ignored so it can be registered on a wildcard subscription.
func (h *RideSettlementHandler) Handle(ctx context.Context, ev eventbus.Event) error {
	if ev.Type != nidusevents.RideCompletedType {
		return nil
	}
	payload, err := decodePayload(ev.Payload)
	if err != nil {
		return fmt.Errorf("ledger adapter: ride.completed payload: %w", err)
	}
	settleCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	_, err = commands.ProcessRidePayment(settleCtx, h.deps, nil, payload)
	if err == commands.ErrRideAlreadySettled {
		// Duplicate delivery — acknowledge, nothing to do.
		return nil
	}
	return err
}

// decodePayload converts the loosely-typed Kafka payload into the strongly
// typed command DTO. A json round-trip tolerates uuid/string/number variance
// emitted by different producers.
func decodePayload(in map[string]interface{}) (commands.RideCompletedEvent, error) {
	var out commands.RideCompletedEvent
	raw, err := json.Marshal(in)
	if err != nil {
		return out, err
	}
	var wire struct {
		RideID      string  `json:"ride_id"`
		DriverID    string  `json:"driver_id"`
		UserID      string  `json:"user_id"`
		FinalFare   float64 `json:"final_fare"`
		PlatformFee float64 `json:"platform_fee"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return out, err
	}
	if out.RideID, err = uuid.Parse(wire.RideID); err != nil {
		return out, fmt.Errorf("ride_id: %w", err)
	}
	if out.DriverID, err = uuid.Parse(wire.DriverID); err != nil {
		return out, fmt.Errorf("driver_id: %w", err)
	}
	if out.RiderID, err = uuid.Parse(wire.UserID); err != nil {
		return out, fmt.Errorf("user_id: %w", err)
	}
	// ETB floats -> cents (round half away from zero).
	out.FareCents = etbToCents(wire.FinalFare)
	out.FeeCents = etbToCents(wire.PlatformFee)
	out.Currency = "ETB"
	out.CompletedAtUnix = time.Now().UTC().Unix()
	if out.FareCents <= 0 {
		return out, fmt.Errorf("invalid final_fare %s", strconv.FormatFloat(wire.FinalFare, 'f', 2, 64))
	}
	return out, nil
}

// etbToCents rounds an ETB amount to cents, half away from zero.
func etbToCents(etb float64) int64 {
	c := etb * 100
	if c >= 0 {
		return int64(c + 0.5)
	}
	return int64(c - 0.5)
}
