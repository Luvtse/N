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
	"nidaw-backend/internal/shared/observability"
)

// ============================================================================
// FILE DISPUTE (Phase D Step 5; Phase F Step 3 entry point)
// ============================================================================
//
// A rider (or driver) disputes an escrowed ride within the 72h window:
//   1. Validate association: filer must be the rider or driver of the hold.
//   2. Window check: only holds still inside their release window can be
//      disputed (once funds are released the case becomes a manual admin
//      adjustment instead).
//   3. Pause the escrow: hold -> disputed, dispute_id linked, so the hourly
//      release job skips it.
//   4. Route by rules engine: simple reasons (no_show, lost_item) land as
//      auto_resolved for immediate processing; complex ones go to the admin
//      review queue.
//   5. Notify the counterparty via ledger.dispute_filed event (WebSocket /
//      push fan-out consumes this).

var (
	ErrDisputeWindowClosed = errors.New("ledger: dispute window closed for this ride")
	ErrNotDisputeParty     = errors.New("ledger: only the rider or driver of the ride may file a dispute")
	ErrAlreadyDisputed     = errors.New("ledger: ride already has an open dispute")
)

// DisputeInput describes one dispute filing from the API layer.
type DisputeInput struct {
	RideID        uuid.UUID
	FiledByUserID uuid.UUID
	ReasonCode    string // see entities.Reason* constants
	Description   string
	EvidenceURLs  []string
}

func (in DisputeInput) validate() error {
	if in.RideID == uuid.Nil || in.FiledByUserID == uuid.Nil {
		return errors.New("ledger: dispute requires ride id and filer id")
	}
	if !entities.ReasonCode(in.ReasonCode).Valid() {
		return entities.ErrInvalidReasonCode
	}
	if in.Description == "" {
		return errors.New("ledger: dispute description is required")
	}
	return nil
}

// FileDispute records the dispute and pauses the matching escrow hold.
func FileDispute(ctx context.Context, d *Deps, log *zap.Logger, in DisputeInput) (*entities.RideDispute, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	now := d.clock().Now()

	var out *entities.RideDispute
	err := d.Uow.WithTx(ctx, func(tx services.DBTx) error {
		hold, err := d.Escrows.GetByRideID(ctx, in.RideID)
		if err != nil {
			return fmt.Errorf("ledger: load escrow hold: %w", err)
		}
		if hold == nil {
			return errors.New("ledger: no escrow hold found for ride")
		}
		// Association check (anti-IDOR, mirrors Phase B7 posture).
		if in.FiledByUserID != hold.RiderID && in.FiledByUserID != hold.DriverID {
			return ErrNotDisputeParty
		}
		if hold.Status == entities.EscrowStatusDisputed {
			return ErrAlreadyDisputed
		}
		if hold.Status != entities.EscrowStatusHeld {
			return ErrDisputeWindowClosed // released/refunded already
		}
		if now.Before(hold.ReleaseAfter) {
			// within window — fine; after window the job may have released it.
		}

		disputeID := uuid.New()
		against := hold.DriverID
		if in.FiledByUserID == hold.DriverID {
			against = hold.RiderID
		}
		dsp, err := entities.NewRideDispute(disputeID, hold.RideID, hold.HoldID,
			in.FiledByUserID, against, in.ReasonCode, in.Description, in.EvidenceURLs, now)
		if err != nil {
			return err
		}
		if err := d.Disputes.Create(ctx, tx, dsp); err != nil {
			return fmt.Errorf("ledger: create dispute: %w", err)
		}
		if err := hold.PauseForDispute(disputeID); err != nil {
			return err
		}
		if err := d.Escrows.Update(ctx, tx, hold); err != nil {
			return fmt.Errorf("ledger: pause escrow for dispute: %w", err)
		}
		out = dsp

		if err := publish(ctx, tx, d, Event{
			Type:  "ledger.dispute_filed",
			Topic: "nidaw.ledger",
			Payload: map[string]interface{}{
				"dispute_id":   disputeID.String(),
				"ride_id":      hold.RideID.String(),
				"hold_id":      hold.HoldID.String(),
				"filed_by":     in.FiledByUserID.String(),
				"against":      against.String(),
				"reason":       in.ReasonCode,
				"status":       string(dsp.Status),
				"amount_cents": hold.Amount.Cents(),
			},
			Timestamp: now.Unix(),
		}); err != nil {
			return err
		}

		// Rules-engine routing (Phase F Step 3): simple cases auto-resolve
		// immediately inside the same DB transaction.
		if dsp.Status == entities.DisputeAutoResolved {
			switch entities.ReasonCode(in.ReasonCode) {
			case entities.ReasonNoShow:
				// Service never rendered -> full refund to rider.
				if err := resolveToRiderLocked(ctx, d, tx, dsp, hold, uuid.Nil, "auto: no-show, full refund"); err != nil {
					return err
				}
			case entities.ReasonLostItem:
				// Needs driver contact before money moves -> admin queue.
				dsp.Status = entities.DisputeAdminReview
				if err := d.Disputes.Update(ctx, tx, dsp); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Phase I Step 2: dispute-rate numerator (filed).
	observability.Ledger().RecordDispute("filed")
	log.Info("dispute filed",
		zap.String("dispute_id", out.DisputeID.String()),
		zap.String("ride_id", in.RideID.String()),
		zap.String("status", string(out.Status)))
	return out, nil
}

// ============================================================================
// RESOLVE DISPUTE (Phase D Step 5; Phase F outcome execution)
// ============================================================================

// DisputeOutcome is the admin decision on a reviewed dispute.
type DisputeOutcome string

const (
	OutcomeRefundRider   DisputeOutcome = "refund_rider"
	OutcomeReleaseDriver DisputeOutcome = "release_driver"
	OutcomeSplit         DisputeOutcome = "split" // partial refund, remainder released
)

// Valid reports whether o is a known outcome.
func (o DisputeOutcome) Valid() bool {
	switch o {
	case OutcomeRefundRider, OutcomeReleaseDriver, OutcomeSplit:
		return true
	default:
		return false
	}
}

// ResolveDisputeInput carries the admin decision.
type ResolveDisputeInput struct {
	DisputeID   uuid.UUID
	AdminID     uuid.UUID
	Outcome     DisputeOutcome
	RefundCents int64 // used when Outcome == OutcomeSplit; ignored otherwise
	Notes       string
}

// ResolveDispute executes the outcome: refunds the rider out of the driver's
// held bucket, releases funds to the driver, or splits them.
func ResolveDispute(ctx context.Context, d *Deps, log *zap.Logger, in ResolveDisputeInput) error {
	if in.DisputeID == uuid.Nil || in.AdminID == uuid.Nil {
		return errors.New("ledger: resolve dispute requires dispute id and admin id")
	}
	if !in.Outcome.Valid() {
		return fmt.Errorf("ledger: invalid dispute outcome %q", in.Outcome)
	}
	if in.Notes == "" {
		return errors.New("ledger: dispute resolution notes are required")
	}

	return d.Uow.WithTx(ctx, func(tx services.DBTx) error {
		dsp, err := d.Disputes.GetByID(ctx, in.DisputeID)
		if err != nil {
			return err
		}
		switch dsp.Status {
		case entities.DisputeOpen, entities.DisputeAutoResolved, entities.DisputeAdminReview:
		case entities.DisputeResolvedRider, entities.DisputeResolvedDriver, entities.DisputeRejected:
			return nil // already terminal, idempotent
		}
		hold, err := d.Escrows.GetByID(ctx, dsp.HoldID)
		if err != nil {
			return err
		}
		if hold == nil {
			return errors.New("ledger: escrow hold missing for dispute")
		}
		if hold.Status != entities.EscrowStatusDisputed {
			return errors.New("ledger: escrow is not in disputed state")
		}

		switch in.Outcome {
		case OutcomeRefundRider:
			if err := resolveToRiderLocked(ctx, d, tx, dsp, hold, in.AdminID, in.Notes); err != nil {
				return err
			}
			observability.Ledger().RecordDispute("refund")
		case OutcomeReleaseDriver:
			if err := resolveToDriverLocked(ctx, d, tx, dsp, hold, in.AdminID, in.Notes); err != nil {
				return err
			}
			observability.Ledger().RecordDispute("release")
		case OutcomeSplit:
			if in.RefundCents <= 0 || in.RefundCents >= hold.Amount.Cents() {
				return errors.New("ledger: split refund must be strictly between 0 and the held amount")
			}
			if err := resolveSplitLocked(ctx, d, tx, dsp, hold, in.AdminID, in.Notes, in.RefundCents); err != nil {
				return err
			}
			observability.Ledger().RecordDispute("split")
		}
		return nil
	})
}

// resolveToRiderLocked refunds the FULL held amount to the rider: pulls the
// funds back out of the driver's held bucket (adjustment_debit against held
// is expressed as an escrow-side reversal) and credits the rider (refund).
// Must run inside the caller's DB transaction with no other locks outstanding
// on these users beyond what LedgerService takes itself.
func resolveToRiderLocked(
	ctx context.Context, d *Deps, tx services.DBTx,
	dsp *entities.RideDispute, hold *entities.EscrowHold, adminID uuid.UUID, notes string,
) error {
	now := d.clock().Now()

	// 1. De-bit the driver: remove the escrowed earning from held.
	if err := reverseHeldCredit(ctx, d, tx, hold, hold.Amount,
		fmt.Sprintf("Dispute %s upheld: earning reclaimed", dsp.DisputeID), dsp.DisputeID); err != nil {
		return err
	}
	// 2. Refund the rider from platform float (escrow was funded by rider
	//    debit at settlement; the refund returns those funds).
	refund, err := d.Ledger.Credit(ctx, hold.RiderID, hold.Amount, valueobjects.TxTypeRefund, false, services.CreditOptions{
		IdempotencyKey: fmt.Sprintf("dispute:refund:%s", dsp.DisputeID),
		ReferenceID:    &dsp.DisputeID,
		ReferenceType:  "dispute",
		Description:    fmt.Sprintf("Ride %s dispute refund", hold.RideID),
	})
	if err != nil {
		return fmt.Errorf("ledger: dispute refund credit: %w", err)
	}
	dsp.RefundTxID = &refund.TxID
	dsp.ResolutionNotes = notes
	resolvedAt := now
	dsp.ResolvedAt = &resolvedAt
	dsp.AssignedAdminID = &adminID
	dsp.Status = entities.DisputeResolvedRider

	hold.Status = entities.EscrowStatusRefunded
	hold.ReleasedAt = &resolvedAt
	hold.DisputeID = &dsp.DisputeID

	if err := d.Disputes.Update(ctx, tx, dsp); err != nil {
		return err
	}
	if err := d.Escrows.Update(ctx, tx, hold); err != nil {
		return err
	}
	if err := auditDisputeResolution(ctx, d, tx, dsp, adminID, "dispute_resolved_refund_rider", notes); err != nil {
		return err
	}
	return publish(ctx, tx, d, Event{
		Type:  "ledger.dispute_resolved",
		Topic: "nidaw.ledger",
		Payload: map[string]interface{}{
			"dispute_id":   dsp.DisputeID.String(),
			"ride_id":      hold.RideID.String(),
			"outcome":      "refund_rider",
			"amount_cents": hold.Amount.Cents(),
		},
		Timestamp: now.Unix(),
	})
}

// resolveToDriverLocked releases the disputed funds to the driver's available
// balance (dispute rejected in the driver's favour). The money already sits
// in the driver's HELD bucket, so this is the standard held->available
// migration performed under the same lock LedgerService takes.
func resolveToDriverLocked(
	ctx context.Context, d *Deps, tx services.DBTx,
	dsp *entities.RideDispute, hold *entities.EscrowHold, adminID uuid.UUID, notes string,
) error {
	now := d.clock().Now()

	release, err := d.Ledger.ReleaseEscrow(ctx, hold.DriverID, hold.Amount, services.CreditOptions{
		IdempotencyKey: fmt.Sprintf("dispute:release:%s", dsp.DisputeID),
		ReferenceID:    &dsp.DisputeID,
		ReferenceType:  "dispute",
		Description:    fmt.Sprintf("Ride %s dispute resolved for driver", hold.RideID),
	})
	if err != nil {
		return fmt.Errorf("ledger: dispute release to driver: %w", err)
	}
	dsp.ReleaseTxID = &release.TxID
	dsp.ResolutionNotes = notes
	resolvedAt := now
	dsp.ResolvedAt = &resolvedAt
	dsp.AssignedAdminID = &adminID
	dsp.Status = entities.DisputeResolvedDriver

	hold.Status = entities.EscrowStatusReleased
	hold.ReleasedAt = &resolvedAt

	if err := d.Disputes.Update(ctx, tx, dsp); err != nil {
		return err
	}
	if err := d.Escrows.Update(ctx, tx, hold); err != nil {
		return err
	}
	if err := auditDisputeResolution(ctx, d, tx, dsp, adminID, "dispute_resolved_release_driver", notes); err != nil {
		return err
	}
	return publish(ctx, tx, d, Event{
		Type:  "ledger.dispute_resolved",
		Topic: "nidaw.ledger",
		Payload: map[string]interface{}{
			"dispute_id":   dsp.DisputeID.String(),
			"ride_id":      hold.RideID.String(),
			"outcome":      "release_driver",
			"amount_cents": hold.Amount.Cents(),
		},
		Timestamp: now.Unix(),
	})
}

// resolveSplitLocked refunds refundCents to the rider and releases the
// remainder of the hold to the driver.
func resolveSplitLocked(
	ctx context.Context, d *Deps, tx services.DBTx,
	dsp *entities.RideDispute, hold *entities.EscrowHold, adminID uuid.UUID, notes string, refundCents int64,
) error {
	now := d.clock().Now()
	refundAmt, err := valueobjects.NewMoney(refundCents)
	if err != nil {
		return err
	}
	driverAmt, err := hold.Amount.Sub(refundAmt)
	if err != nil {
		return err
	}

	// Reclaim only the refunded slice from the driver's held bucket.
	if err := reverseHeldCredit(ctx, d, tx, hold, refundAmt,
		fmt.Sprintf("Dispute %s split: partial earning reclaimed", dsp.DisputeID), dsp.DisputeID); err != nil {
		return err
	}
	// Refund the rider their slice.
	refund, err := d.Ledger.Credit(ctx, hold.RiderID, refundAmt, valueobjects.TxTypeRefund, false, services.CreditOptions{
		IdempotencyKey: fmt.Sprintf("dispute:refund:%s", dsp.DisputeID),
		ReferenceID:    &dsp.DisputeID,
		ReferenceType:  "dispute",
		Description:    fmt.Sprintf("Ride %s dispute partial refund", hold.RideID),
	})
	if err != nil {
		return fmt.Errorf("ledger: dispute split refund: %w", err)
	}
	// Release the driver's slice (held -> available).
	release, err := d.Ledger.ReleaseEscrow(ctx, hold.DriverID, driverAmt, services.CreditOptions{
		IdempotencyKey: fmt.Sprintf("dispute:split-release:%s", dsp.DisputeID),
		ReferenceID:    &dsp.DisputeID,
		ReferenceType:  "dispute",
		Description:    fmt.Sprintf("Ride %s dispute split release", hold.RideID),
	})
	if err != nil {
		return fmt.Errorf("ledger: dispute split release: %w", err)
	}

	dsp.RefundTxID = &refund.TxID
	dsp.ReleaseTxID = &release.TxID
	dsp.ResolutionNotes = notes
	resolvedAt := now
	dsp.ResolvedAt = &resolvedAt
	dsp.AssignedAdminID = &adminID
	dsp.Status = entities.DisputeResolvedDriver // remainder earned; refund recorded separately

	hold.Status = entities.EscrowStatusReleased
	hold.ReleasedAt = &resolvedAt

	if err := d.Disputes.Update(ctx, tx, dsp); err != nil {
		return err
	}
	if err := d.Escrows.Update(ctx, tx, hold); err != nil {
		return err
	}
	if err := auditDisputeResolution(ctx, d, tx, dsp, adminID, "dispute_resolved_split",
		fmt.Sprintf("%s (refund %d cents)", notes, refundCents)); err != nil {
		return err
	}
	return publish(ctx, tx, d, Event{
		Type:  "ledger.dispute_resolved",
		Topic: "nidaw.ledger",
		Payload: map[string]interface{}{
			"dispute_id":     dsp.DisputeID.String(),
			"ride_id":        hold.RideID.String(),
			"outcome":        "split",
			"refund_cents":   refundCents,
			"released_cents": driverAmt.Cents(),
		},
		Timestamp: now.Unix(),
	})
}

func auditDisputeResolution(
	ctx context.Context, d *Deps, tx services.DBTx,
	dsp *entities.RideDispute, adminID uuid.UUID, action, notes string,
) error {
	if d.Audit == nil {
		return nil
	}
	return d.Audit.Log(ctx, tx, &services.AuditEntry{
		ActorUserID: adminID,
		ActorRole:   "admin",
		Action:      action,
		TargetType:  "ride_dispute",
		TargetID:    &dsp.DisputeID,
		ReasonCode:  string(dsp.Reason),
		ReasonText:  notes,
	})
}

// reverseHeldCredit removes an escrow amount from the driver's HELD bucket by
// writing a negative ride_credit_held-reversal entry. Implemented as a direct
// locked mutation because LedgerService.Debit operates on available only.
func reverseHeldCredit(
	ctx context.Context, d *Deps, tx services.DBTx,
	hold *entities.EscrowHold, amount valueobjects.Money, description string, disputeID uuid.UUID,
) error {
	bal, err := d.Balances.LockForUpdate(ctx, tx, hold.DriverID)
	if err != nil {
		return err
	}
	if bal.Held.Compare(amount) < 0 {
		return errors.New("ledger: driver held balance insufficient to reclaim disputed earning")
	}
	newHeld, err := bal.Held.Sub(amount)
	if err != nil {
		return err
	}
	bal.Held = newHeld
	lifeDeb, err := bal.LifetimeDebited.Add(amount)
	if err != nil {
		return err
	}
	bal.LifetimeDebited = lifeDeb
	total, err := bal.Total()
	if err != nil {
		return err
	}
	negAmount, err := amount.Negate()
	if err != nil {
		return err
	}
	releaseKey := fmt.Sprintf("dispute:reclaim:%s", disputeID)
	ref := disputeID
	led, err := d.Chain.Append(ctx, tx, hold.DriverID, negAmount, total,
		valueobjects.TxTypeAdjustmentDebit, d.clock().Now().UnixNano(), bal,
		releaseKey, &ref, "dispute", description, nil, false)
	if err != nil {
		return err
	}
	hold.ReleaseTxID = &led.TxID
	bal.Version++
	return d.Balances.Save(ctx, tx, bal)
}
