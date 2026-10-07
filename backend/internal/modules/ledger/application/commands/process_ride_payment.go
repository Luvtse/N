package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"nidaw-backend/internal/modules/ledger/application/services"
	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
)

// ============================================================================
// PROCESS RIDE PAYMENT (Phase D Step 5 / Phase F Step 1 trigger)
// ============================================================================
//
// Consumes the ride.completed event and settles the fare atomically:
//   1. Debit rider available balance (ride_debit).
//   2. Credit driver into HELD bucket — escrow, not spendable yet
//      (ride_credit_held) — and open an escrow_hold with
//      release_after = now + 72h.
// Both movements commit in one DB transaction; a failure rolls back the
// debit as well, so no fare can be half-charged. Idempotency is keyed on
// the ride id so Kafka redelivery cannot double-charge.

// ErrRideAlreadySettled signals a duplicate ride.completed delivery.
var ErrRideAlreadySettled = errors.New("ledger: ride already settled")

// RideCompletedEvent mirrors the payload of the "ride.completed" Kafka event.
type RideCompletedEvent struct {
	RideID          uuid.UUID
	RiderID         uuid.UUID
	DriverID        uuid.UUID
	FareCents       int64  // ETB cents charged to the rider
	FeeCents        int64  // platform fee taken out of the fare (driver credited net)
	Currency        string // must be ETB
	CompletedAtUnix int64
}

// ProcessRidePayment handles one ride.completed settlement.
func ProcessRidePayment(ctx context.Context, d *Deps, log *zap.Logger, ev RideCompletedEvent) (*entities.EscrowHold, error) {
	if ev.RideID == uuid.Nil || ev.RiderID == uuid.Nil || ev.DriverID == uuid.Nil {
		return nil, errors.New("ledger: ride payment requires ride, rider and driver ids")
	}
	if ev.FareCents <= 0 {
		return nil, errors.New("ledger: ride fare must be positive")
	}
	fare, err := valueobjects.NewMoney(ev.FareCents)
	if err != nil {
		return nil, err
	}
	fee, err := valueobjects.NewMoney(ev.FeeCents)
	if err != nil {
		return nil, err
	}
	if fee.Compare(fare) > 0 {
		return nil, errors.New("ledger: platform fee exceeds fare")
	}
	driverNet, err := fare.Sub(fee)
	if err != nil {
		return nil, err
	}

	rideStr := ev.RideID.String()
	debitKey := "ride:debit:" + rideStr
	creditKey := "ride:credit:" + rideStr

	var hold *entities.EscrowHold
	err = d.Uow.WithTx(ctx, func(tx services.DBTx) error {
		// Double-submit guard: has this ride already been debited from the rider?
		found, _, err := d.Txs.ExistsIdempotencyKey(ctx, tx, ev.RiderID, debitKey)
		if err != nil {
			return err
		}
		if found {
			return ErrRideAlreadySettled
		}

		// 1. Debit the rider (fails fast if funds are insufficient — the rider
		//    must top up before rides are auto-settled; negative_lock is NOT
		//    allowed here, riders settle from prepaid balance only).
		if _, err := d.Ledger.Debit(ctx, ev.RiderID, fare, valueobjects.TxTypeRideDebit, false, services.CreditOptions{
			IdempotencyKey: debitKey,
			ReferenceID:    &ev.RideID,
			ReferenceType:  "ride",
			Description:    fmt.Sprintf("Ride %s fare", rideStr),
		}); err != nil {
			return fmt.Errorf("ledger: rider debit: %w", err)
		}

		// 2. Credit the driver's HELD bucket (escrow).
		creditTx, err := d.Ledger.Credit(ctx, ev.DriverID, driverNet, valueobjects.TxTypeRideCreditHeld, true, services.CreditOptions{
			IdempotencyKey: creditKey,
			ReferenceID:    &ev.RideID,
			ReferenceType:  "ride",
			Description:    fmt.Sprintf("Ride %s earning (held 72h)", rideStr),
		})
		if err != nil {
			return fmt.Errorf("ledger: driver held credit: %w", err)
		}

		// 3. Open the 72h escrow hold (Phase F Step 1).
		now := d.clock().Now()
		h, err := entities.NewEscrowHold(uuid.New(), ev.RideID, ev.RiderID, ev.DriverID, driverNet, now)
		if err != nil {
			return err
		}
		h.CreditTxID = &creditTx.TxID
		if err := d.Escrows.Create(ctx, tx, h); err != nil {
			return fmt.Errorf("ledger: create escrow hold: %w", err)
		}
		hold = h

		return publish(ctx, tx, d, Event{
			Type:  "ledger.ride_settled",
			Topic: "nidaw.ledger",
			Payload: map[string]interface{}{
				"ride_id":       rideStr,
				"rider_id":      ev.RiderID.String(),
				"driver_id":     ev.DriverID.String(),
				"fare_cents":    ev.FareCents,
				"fee_cents":     ev.FeeCents,
				"held_cents":    driverNet.Cents(),
				"hold_id":       h.HoldID.String(),
				"release_after": h.ReleaseAfter.UTC().Format("2006-01-02T15:04:05Z"),
			},
			Timestamp: now.Unix(),
		})
	})
	if err != nil {
		return nil, err
	}
	log.Info("ride payment processed",
		zap.String("ride_id", rideStr),
		zap.Int64("fare_cents", ev.FareCents),
		zap.String("hold_id", hold.HoldID.String()))
	return hold, nil
}

// publish is a nil-safe helper for the outbox port.
func publish(ctx context.Context, tx services.DBTx, d *Deps, ev Event) error {
	if d.Events == nil {
		return nil
	}
	return d.Events.Publish(ctx, tx, ev)
}
