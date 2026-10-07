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
// REQUEST TOPUP (Phase D Step 5; optimistic-credit flow of Phase E Step 3)
// ============================================================================
//
// Flow:
//   1. RequestTopup — credit available instantly (topup tx), create a pending
//      topup_request linked to the credit tx, and emit ledger.topup_requested
//      so a background job can call the provider adapter asynchronously.
//   2. ConfirmTopup — provider reported success: mark request completed.
//   3. FailTopup    — provider reported failure: write a compensating
//      adjustment_debit ("claw-back") and place the account in negative_lock
//      (blocks new rides until repaid). If the user still has funds covering
//      the claw-back from other sources, the lock is cleared immediately.

var ErrTopupNotPending = errors.New("ledger: topup request is not in a pending/processing state")

// TopupInput describes one fiat on-ramp request from the API layer.
type TopupInput struct {
	UserID         uuid.UUID
	AmountCents    int64
	Provider       string // telebirr | chapa | mpesa
	IdempotencyKey string // client-generated; also keys the optimistic credit
	Metadata       map[string]interface{}
}

func (in TopupInput) validate() error {
	if in.UserID == uuid.Nil {
		return errors.New("ledger: topup requires a user id")
	}
	if in.AmountCents <= 0 {
		return errors.New("ledger: topup amount must be positive")
	}
	if !entities.ValidProvider(in.Provider) {
		return entities.ErrInvalidProvider
	}
	if in.IdempotencyKey == "" {
		return errors.New("ledger: topup requires an idempotency key")
	}
	return nil
}

// RequestTopup optimistically credits the user and records the pending
// topup_request. Returns the persisted request (status pending).
func RequestTopup(ctx context.Context, d *Deps, log *zap.Logger, in TopupInput) (*entities.TopupRequest, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	amount, err := valueobjects.NewMoney(in.AmountCents)
	if err != nil {
		return nil, err
	}

	var req *entities.TopupRequest
	err = d.Uow.WithTx(ctx, func(tx services.DBTx) error {
		// Optimistic credit, keyed by the caller's idempotency key so a retried
		// POST cannot double-credit. LedgerService.Credit replays the original
		// tx for duplicates; we detect replay via the request row below.
		_, existing, err := d.Txs.ExistsIdempotencyKey(ctx, tx, in.UserID, "topup:"+in.IdempotencyKey)
		if err != nil {
			return err
		}
		if existing != nil {
			// Replay: reload the already-created request for a stable response.
			if existing.ReferenceID != nil {
				r, err := d.Topups.GetByID(ctx, *existing.ReferenceID)
				if err != nil {
					return err
				}
				req = r
				return nil
			}
			return services.ErrDuplicateRequest
		}

		topupID := uuid.New()
		credit, err := d.Ledger.Credit(ctx, in.UserID, amount, valueobjects.TxTypeTopup, false, services.CreditOptions{
			IdempotencyKey: "topup:" + in.IdempotencyKey,
			ReferenceID:    &topupID,
			ReferenceType:  "topup",
			Description:    fmt.Sprintf("Top-up via %s (pending confirmation)", in.Provider),
			Metadata:       in.Metadata,
		})
		if err != nil {
			return fmt.Errorf("ledger: optimistic topup credit: %w", err)
		}

		r, err := entities.NewTopupRequest(topupID, in.UserID, amount, in.Provider)
		if err != nil {
			return err
		}
		r.CreditTxID = &credit.TxID
		r.Metadata = in.Metadata
		if err := d.Topups.Create(ctx, tx, r); err != nil {
			return fmt.Errorf("ledger: create topup request: %w", err)
		}
		req = r

		return publish(ctx, tx, d, Event{
			Type:  "ledger.topup_requested",
			Topic: "nidaw.ledger",
			Payload: map[string]interface{}{
				"topup_id":     topupID.String(),
				"user_id":      in.UserID.String(),
				"amount_cents": amount.Cents(),
				"provider":     in.Provider,
			},
			Timestamp: d.clock().Now().Unix(),
		})
	})
	if err != nil {
		return nil, err
	}
	log.Info("topup requested",
		zap.String("topup_id", req.TopupID.String()),
		zap.String("provider", string(req.Provider)),
		zap.Int64("amount_cents", req.Amount.Cents()))
	return req, nil
}

// VerifyAndSettleTopup polls the provider (via the verifier port) and drives
// the request to completion or failure. Called by the async topup job.
func VerifyAndSettleTopup(ctx context.Context, d *Deps, log *zap.Logger, topupID uuid.UUID) error {
	if d.Verifier == nil {
		return errors.New("ledger: no topup verifier wired (Phase E adapters pending)")
	}
	req, err := d.Topups.GetByID(ctx, topupID)
	if err != nil {
		return err
	}
	if req.Status != entities.TopupStatusPending && req.Status != entities.TopupStatusProcessing {
		return nil // terminal already
	}
	paid, providerRef, err := d.Verifier.VerifyTopup(ctx, req)
	if err != nil {
		return fmt.Errorf("ledger: verify topup with provider: %w", err)
	}
	if paid {
		return ConfirmTopup(ctx, d, log, topupID, providerRef)
	}
	return FailTopup(ctx, d, log, topupID, "provider reports unpaid/expired reference")
}

// ConfirmTopup marks the request completed. The optimistic credit stands.
func ConfirmTopup(ctx context.Context, d *Deps, log *zap.Logger, topupID uuid.UUID, providerRef string) error {
	now := d.clock().Now()
	return d.Uow.WithTx(ctx, func(tx services.DBTx) error {
		req, err := d.Topups.GetByID(ctx, topupID)
		if err != nil {
			return err
		}
		switch req.Status {
		case entities.TopupStatusCompleted:
			return nil // idempotent
		case entities.TopupStatusPending, entities.TopupStatusProcessing:
		default:
			return ErrTopupNotPending
		}
		if err := req.MarkCompleted(now); err != nil {
			return err
		}
		if providerRef != "" {
			req.ProviderReference = providerRef
		}
		if err := d.Topups.Update(ctx, tx, req); err != nil {
			return fmt.Errorf("ledger: confirm topup: %w", err)
		}
		// A successful repayment may restore solvency after a prior claw-back.
		if err := d.Ledger.ClearNegativeLock(ctx, req.UserID); err != nil {
			log.Warn("clear negative lock after topup failed", zap.Error(err))
		}
		return publish(ctx, tx, d, Event{
			Type:  "ledger.topup_completed",
			Topic: "nidaw.ledger",
			Payload: map[string]interface{}{
				"topup_id":     topupID.String(),
				"user_id":      req.UserID.String(),
				"provider":     string(req.Provider),
				"provider_ref": req.ProviderReference,
			},
			Timestamp: now.Unix(),
		})
	})
}

// FailTopup claws back the optimistic credit with a compensating
// adjustment_debit and engages negative_lock when the user has already spent
// part of the phantom funds (Phase E Step 3 failure path).
func FailTopup(ctx context.Context, d *Deps, log *zap.Logger, topupID uuid.UUID, reason string) error {
	return d.Uow.WithTx(ctx, func(tx services.DBTx) error {
		req, err := d.Topups.GetByID(ctx, topupID)
		if err != nil {
			return err
		}
		switch req.Status {
		case entities.TopupStatusFailed, entities.TopupStatusReversed:
			return nil // idempotent
		case entities.TopupStatusPending, entities.TopupStatusProcessing:
		default:
			return ErrTopupNotPending
		}
		if req.CreditTxID == nil {
			return errors.New("ledger: topup missing credit tx reference; manual intervention required")
		}

		// Compensating debit; allowNegativeLock drops the account into
		// negative_lock if the funds were already spent.
		claw, err := d.Ledger.Debit(ctx, req.UserID, req.Amount, valueobjects.TxTypeAdjustmentDebit, true, services.CreditOptions{
			IdempotencyKey: fmt.Sprintf("topup:clawback:%s", topupID),
			ReferenceID:    &req.TopupID,
			ReferenceType:  "topup",
			Description:    fmt.Sprintf("Claw-back of failed %s top-up: %s", req.Provider, reason),
		})
		if err != nil {
			return fmt.Errorf("ledger: topup claw-back debit: %w", err)
		}

		if err := req.MarkFailed(reason); err != nil {
			return err
		}
		req.ReversalTxID = &claw.TxID
		if err := d.Topups.Update(ctx, tx, req); err != nil {
			return fmt.Errorf("ledger: fail topup: %w", err)
		}

		// If the claw-back left the account solvent (user had other funds),
		// lift the lock immediately.
		bal, err := d.Balances.Get(ctx, req.UserID)
		if err == nil && !bal.Available.IsNegative() && bal.Status == entities.BalanceStatusNegativeLock {
			if err := d.Ledger.ClearNegativeLock(ctx, req.UserID); err != nil {
				log.Warn("clear negative lock after claw-back failed", zap.Error(err))
			}
		}

		return publish(ctx, tx, d, Event{
			Type:  "ledger.topup_failed",
			Topic: "nidaw.ledger",
			Payload: map[string]interface{}{
				"topup_id":     topupID.String(),
				"user_id":      req.UserID.String(),
				"amount_cents": req.Amount.Cents(),
				"reason":       reason,
			},
			Timestamp: d.clock().Now().Unix(),
		})
	})
}
