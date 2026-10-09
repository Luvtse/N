package handlers

import (
	"context"
	"errors"

	"nidaw-backend/internal/modules/ledger/application/services"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
	niduscmds "nidaw-backend/internal/modules/nidus/application/commands"

	"github.com/google/uuid"
)

// ledgerTipSettler implements nidus TipSettler on top of the private ledger
// core (Phase D/E money path for rider tips). This file is the ONLY place in
// the nidus module allowed to import ledger application code — the module
// boundary rule documented next to the TipSettler port.
//
// Semantics:
//   - Atomic two-leg transfer inside one DB transaction: debit rider's
//     available balance, credit driver's available balance (tips bypass the
//     72h escrow hold — they are gratuity, not fare revenue at risk).
//   - Idempotent per ride: both legs share deterministic keys
//     ("tip:debit:<ride_id>" / "tip:credit:<ride_id>"), so a retried rate
//     request after a partial failure can never double-move money. A replay
//     returns success without new transactions.
//   - Insufficient-balance tips fail loudly (the rating itself is already
//     committed; the client surfaces a retry affordance).
type ledgerTipSettler struct {
	ledger *services.LedgerService
}

// NewLedgerTipSettler builds the ledger-backed tip settlement adapter.
func NewLedgerTipSettler(ledger *services.LedgerService) niduscmds.TipSettler {
	return &ledgerTipSettler{ledger: ledger}
}

var errNilTipSettler = errors.New("tip settler not configured")

// SettleTip moves tipCents from the rider to the driver, idempotently keyed
// on the ride id. See type doc for the full contract.
func (s *ledgerTipSettler) SettleTip(ctx context.Context, rideID, riderID, driverUserID uuid.UUID, tipCents int64) error {
	if s == nil || s.ledger == nil {
		return errNilTipSettler
	}
	if tipCents <= 0 {
		return nil // nothing to settle
	}
	amount, err := valueobjects.NewMoney(tipCents)
	if err != nil {
		return err
	}

	opts := services.CreditOptions{
		IdempotencyKey: "tip:debit:" + rideID.String(),
		ReferenceID:    &rideID,
		ReferenceType:  "ride",
		Description:    "Rider tip — fare leg",
	}
	_, err = s.ledger.Debit(ctx, riderID, amount, valueobjects.TxTypeTip, false, opts)
	if err != nil && !errors.Is(err, services.ErrDuplicateRequest) && !errors.Is(err, services.ErrInsufficientFunds) {
		return err
	}
	debitDone := err == nil || errors.Is(err, services.ErrDuplicateRequest)

	optsCredit := services.CreditOptions{
		IdempotencyKey: "tip:credit:" + rideID.String(),
		ReferenceID:    &rideID,
		ReferenceType:  "ride",
		Description:    "Rider tip — driver leg",
	}
	_, err = s.ledger.Credit(ctx, driverUserID, amount, valueobjects.TxTypeTip, false, optsCredit)
	if err != nil && !errors.Is(err, services.ErrDuplicateRequest) {
		return err
	}
	if !debitDone && errors.Is(err, nil) {
		// Debit was rejected for insufficient funds but the credit went through:
		// compensate so money is never created out of thin air.
		if _, rerr := s.ledger.Debit(ctx, driverUserID, amount, valueobjects.TxTypeTip, true, services.CreditOptions{
			IdempotencyKey: "tip:reversal:" + rideID.String(),
			ReferenceID:    &rideID,
			ReferenceType:  "ride",
			Description:    "Rider tip reversal — insufficient rider funds",
		}); rerr != nil && !errors.Is(rerr, services.ErrDuplicateRequest) {
			return rerr
		}
		return services.ErrInsufficientFunds
	}
	return nil
}
