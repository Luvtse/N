package commands

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"nidaw-backend/internal/modules/ledger/application/services"
	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
)

// ============================================================================
// REQUEST WITHDRAWAL (Phase D Step 5; payout flow of Phase E Step 4)
// ============================================================================
//
// Flow:
//   1. Fraud pre-checks: account age > 24h, completed rides > 5, and the
//      pluggable FraudEvaluator risk score (Phase G). A hold routes the
//      request into fraud_hold for admin sign-off WITHOUT moving money.
//   2. Immediate debit of available_balance (amount + fee) to prevent
//      double-spend while the payout is in flight.
//   3. Persist withdrawal_request (processing when a payout initiator is
//      wired, pending for the async job otherwise).
//   4. CompleteWithdrawal / FailWithdrawal drive the terminal states; on
//      failure the deduction is reversed with a withdrawal_reversal credit.

const (
	minAccountAge              = 24 * time.Hour
	minCompletedRides          = 5
	maxSinglePayoutCents int64 = 10_000_000 // 100,000.00 ETB hard cap per request
)

var ErrWithdrawalNotActionable = errors.New("ledger: withdrawal request is not actionable")

// WithdrawalInput describes one payout request from the API layer.
type WithdrawalInput struct {
	UserID             uuid.UUID
	AmountCents        int64
	FeeCents           int64
	DestinationType    string // bank_transfer | mpesa | telebirr_merchant
	DestinationDetails map[string]interface{}
	IdempotencyKey     string
}

func (in WithdrawalInput) validate() error {
	if in.UserID == uuid.Nil {
		return errors.New("ledger: withdrawal requires a user id")
	}
	if in.AmountCents <= 0 {
		return errors.New("ledger: withdrawal amount must be positive")
	}
	if in.AmountCents > maxSinglePayoutCents {
		return errors.New("ledger: withdrawal exceeds single-payout cap")
	}
	if in.FeeCents < 0 {
		return errors.New("ledger: withdrawal fee must not be negative")
	}
	switch entities.DestinationType(in.DestinationType) {
	case entities.DestBankTransfer, entities.DestMpesa, entities.DestTelebirrMerchant:
	default:
		return errors.New("ledger: invalid withdrawal destination type")
	}
	if len(in.DestinationDetails) == 0 {
		return errors.New("ledger: withdrawal requires destination details")
	}
	if in.IdempotencyKey == "" {
		return errors.New("ledger: withdrawal requires an idempotency key")
	}
	return nil
}

// RequestWithdrawal runs fraud gates then atomically debits and records the
// payout request.
func RequestWithdrawal(ctx context.Context, d *Deps, log *zap.Logger, in WithdrawalInput) (*entities.WithdrawalRequest, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	amount, err := valueobjects.NewMoney(in.AmountCents)
	if err != nil {
		return nil, err
	}
	fee, err := valueobjects.NewMoney(in.FeeCents)
	if err != nil {
		return nil, err
	}
	total, err := amount.Add(fee)
	if err != nil {
		return nil, err
	}

	// --- Fraud pre-checks (Phase E Step 4 / Phase G Step 1) -----------------
	riskScore := 0.0
	holdRequired := false
	if d.DriverStats != nil {
		age, rides, err := d.DriverStats.FetchDriverStats(ctx, in.UserID)
		if err != nil {
			log.Warn("driver stats unavailable; applying conservative hold", zap.Error(err))
			holdRequired = true
		} else if age < minAccountAge || rides <= minCompletedRides {
			return nil, fmt.Errorf("ledger: account not eligible for withdrawal (age %v, rides %d)", age, rides)
		}
	}
	if d.Fraud != nil {
		score, hold, err := d.Fraud.EvaluateWithdrawal(ctx, in.UserID, in.AmountCents)
		if err != nil {
			log.Warn("fraud evaluation failed; applying conservative hold", zap.Error(err))
			holdRequired = true
		} else {
			riskScore = score
			holdRequired = holdRequired || hold
		}
	}

	withdrawalID := uuid.New()
	debitKey := "withdrawal:" + in.IdempotencyKey

	var req *entities.WithdrawalRequest
	err = d.Uow.WithTx(ctx, func(tx services.DBTx) error {
		found, existing, err := d.Txs.ExistsIdempotencyKey(ctx, tx, in.UserID, debitKey)
		if err != nil {
			return err
		}
		if found {
			if existing.ReferenceID != nil {
				r, err := d.Withdrawals.GetByID(ctx, *existing.ReferenceID)
				if err != nil {
					return err
				}
				req = r
				return nil
			}
			return services.ErrDuplicateRequest
		}

		w, err := entities.NewWithdrawalRequest(withdrawalID, in.UserID, amount, fee, in.DestinationType)
		if err != nil {
			return err
		}
		w.DestinationDetails = in.DestinationDetails
		score := riskScore
		w.RiskScore = &score

		if holdRequired {
			// No money movement yet — park for admin review queue (Phase H).
			w.Status = entities.WithdrawalStatusFraudHold
			if err := d.Withdrawals.Create(ctx, tx, w); err != nil {
				return fmt.Errorf("ledger: create fraud-held withdrawal: %w", err)
			}
			req = w
			if err := d.Audit.Log(ctx, tx, &services.AuditEntry{
				ActorUserID: in.UserID,
				ActorRole:   "system",
				Action:      "withdrawal_fraud_hold",
				TargetType:  "withdrawal_request",
				TargetID:    &withdrawalID,
				ReasonCode:  "fraud_risk",
				ReasonText:  fmt.Sprintf("risk_score=%.3f", riskScore),
			}); err != nil {
				return err
			}
			return publish(ctx, tx, d, Event{
				Type:  "ledger.withdrawal_held",
				Topic: "nidaw.ledger",
				Payload: map[string]interface{}{
					"withdrawal_id": withdrawalID.String(),
					"user_id":       in.UserID.String(),
					"amount_cents":  in.AmountCents,
					"risk_score":    riskScore,
				},
				Timestamp: d.clock().Now().Unix(),
			})
		}

		// Immediate deduction prevents double-spend while funds are in flight.
		debit, err := d.Ledger.Debit(ctx, in.UserID, total, valueobjects.TxTypeWithdrawalDebit, false, services.CreditOptions{
			IdempotencyKey: debitKey,
			ReferenceID:    &withdrawalID,
			ReferenceType:  "withdrawal",
			Description:    fmt.Sprintf("Payout via %s (fee %s ETB)", in.DestinationType, fee),
		})
		if err != nil {
			return fmt.Errorf("ledger: withdrawal debit: %w", err)
		}
		w.DebitTxID = &debit.TxID
		w.Status = entities.WithdrawalStatusPending
		if d.Payouts != nil {
			ref, err := d.Payouts.InitiatePayout(ctx, w)
			if err != nil {
				// Provider rejected synchronously: roll back the whole tx
				// (debit included) so nothing is lost.
				return fmt.Errorf("ledger: payout initiation failed: %w", err)
			}
			w.ProviderReference = ref
			w.Status = entities.WithdrawalStatusProcessing
		}
		if err := d.Withdrawals.Create(ctx, tx, w); err != nil {
			return fmt.Errorf("ledger: create withdrawal request: %w", err)
		}
		req = w

		return publish(ctx, tx, d, Event{
			Type:  "ledger.withdrawal_requested",
			Topic: "nidaw.ledger",
			Payload: map[string]interface{}{
				"withdrawal_id": withdrawalID.String(),
				"user_id":       in.UserID.String(),
				"amount_cents":  in.AmountCents,
				"fee_cents":     in.FeeCents,
				"destination":   in.DestinationType,
				"status":        string(w.Status),
			},
			Timestamp: d.clock().Now().Unix(),
		})
	})
	if err != nil {
		return nil, err
	}
	log.Info("withdrawal requested",
		zap.String("withdrawal_id", req.WithdrawalID.String()),
		zap.String("status", string(req.Status)),
		zap.Int64("amount_cents", req.Amount.Cents()))
	return req, nil
}

// ApproveHeldWithdrawal releases a fraud-held request after admin sign-off
// (Phase H admin console) and performs the deferred debit + payout.
func ApproveHeldWithdrawal(ctx context.Context, d *Deps, log *zap.Logger, adminID uuid.UUID, withdrawalID uuid.UUID) error {
	now := d.clock().Now()
	return d.Uow.WithTx(ctx, func(tx services.DBTx) error {
		w, err := d.Withdrawals.GetByID(ctx, withdrawalID)
		if err != nil {
			return err
		}
		if w.Status != entities.WithdrawalStatusFraudHold {
			return ErrWithdrawalNotActionable
		}
		total, err := w.TotalDebited()
		if err != nil {
			return err
		}
		debit, err := d.Ledger.Debit(ctx, w.UserID, total, valueobjects.TxTypeWithdrawalDebit, false, services.CreditOptions{
			IdempotencyKey: fmt.Sprintf("withdrawal:approved:%s", withdrawalID),
			ReferenceID:    &w.WithdrawalID,
			ReferenceType:  "withdrawal",
			Description:    "Payout approved after fraud hold",
		})
		if err != nil {
			return fmt.Errorf("ledger: post-approval debit: %w", err)
		}
		w.DebitTxID = &debit.TxID
		w.Status = entities.WithdrawalStatusApproved
		reviewedAt := now
		w.ReviewedBy = &adminID
		w.ReviewedAt = &reviewedAt
		if d.Payouts != nil {
			ref, err := d.Payouts.InitiatePayout(ctx, w)
			if err != nil {
				return fmt.Errorf("ledger: payout initiation after approval: %w", err)
			}
			w.ProviderReference = ref
			w.Status = entities.WithdrawalStatusProcessing
		}
		if err := d.Withdrawals.Update(ctx, tx, w); err != nil {
			return err
		}
		if err := d.Audit.Log(ctx, tx, &services.AuditEntry{
			ActorUserID: adminID,
			ActorRole:   "admin",
			Action:      "withdrawal_approved",
			TargetType:  "withdrawal_request",
			TargetID:    &withdrawalID,
			ReasonCode:  "manual_approval",
		}); err != nil {
			return err
		}
		return publish(ctx, tx, d, Event{
			Type:  "ledger.withdrawal_approved",
			Topic: "nidaw.ledger",
			Payload: map[string]interface{}{
				"withdrawal_id": withdrawalID.String(),
				"user_id":       w.UserID.String(),
			},
			Timestamp: now.Unix(),
		})
	})
}

// RejectHeldWithdrawal closes a fraud-held request without moving money.
func RejectHeldWithdrawal(ctx context.Context, d *Deps, log *zap.Logger, adminID uuid.UUID, withdrawalID uuid.UUID, reason string) error {
	now := d.clock().Now()
	return d.Uow.WithTx(ctx, func(tx services.DBTx) error {
		w, err := d.Withdrawals.GetByID(ctx, withdrawalID)
		if err != nil {
			return err
		}
		if w.Status != entities.WithdrawalStatusFraudHold {
			return ErrWithdrawalNotActionable
		}
		w.Status = entities.WithdrawalStatusFailed
		w.FailureReason = reason
		reviewedAt := now
		w.ReviewedBy = &adminID
		w.ReviewedAt = &reviewedAt
		if err := d.Withdrawals.Update(ctx, tx, w); err != nil {
			return err
		}
		return d.Audit.Log(ctx, tx, &services.AuditEntry{
			ActorUserID: adminID,
			ActorRole:   "admin",
			Action:      "withdrawal_rejected",
			TargetType:  "withdrawal_request",
			TargetID:    &withdrawalID,
			ReasonCode:  "fraud_risk",
			ReasonText:  reason,
		})
	})
}

// CompleteWithdrawal marks a processing payout as paid out by the provider.
func CompleteWithdrawal(ctx context.Context, d *Deps, log *zap.Logger, withdrawalID uuid.UUID, providerRef string) error {
	now := d.clock().Now()
	return d.Uow.WithTx(ctx, func(tx services.DBTx) error {
		w, err := d.Withdrawals.GetByID(ctx, withdrawalID)
		if err != nil {
			return err
		}
		switch w.Status {
		case entities.WithdrawalStatusCompleted:
			return nil
		case entities.WithdrawalStatusProcessing, entities.WithdrawalStatusPending, entities.WithdrawalStatusApproved:
		default:
			return ErrWithdrawalNotActionable
		}
		w.Status = entities.WithdrawalStatusCompleted
		completedAt := now
		w.CompletedAt = &completedAt
		if providerRef != "" {
			w.ProviderReference = providerRef
		}
		if err := d.Withdrawals.Update(ctx, tx, w); err != nil {
			return err
		}
		return publish(ctx, tx, d, Event{
			Type:  "ledger.withdrawal_completed",
			Topic: "nidaw.ledger",
			Payload: map[string]interface{}{
				"withdrawal_id": withdrawalID.String(),
				"user_id":       w.UserID.String(),
				"provider_ref":  w.ProviderReference,
			},
			Timestamp: now.Unix(),
		})
	})
}

// FailWithdrawal reverses the upfront deduction when the payout rail reports
// failure (Phase E Step 4: "On Failure: Reverse deduction; notify admin").
func FailWithdrawal(ctx context.Context, d *Deps, log *zap.Logger, withdrawalID uuid.UUID, reason string) error {
	return d.Uow.WithTx(ctx, func(tx services.DBTx) error {
		w, err := d.Withdrawals.GetByID(ctx, withdrawalID)
		if err != nil {
			return err
		}
		switch w.Status {
		case entities.WithdrawalStatusFailed, entities.WithdrawalStatusReversed:
			return nil
		case entities.WithdrawalStatusPending, entities.WithdrawalStatusApproved, entities.WithdrawalStatusProcessing:
		default:
			return ErrWithdrawalNotActionable
		}
		if w.DebitTxID == nil {
			// Held-before-debit failure: no reversal needed, just close it.
			w.Status = entities.WithdrawalStatusFailed
			w.FailureReason = reason
			return d.Withdrawals.Update(ctx, tx, w)
		}
		total, err := w.TotalDebited()
		if err != nil {
			return err
		}
		reversal, err := d.Ledger.Credit(ctx, w.UserID, total, valueobjects.TxTypeWithdrawalReversal, false, services.CreditOptions{
			IdempotencyKey: fmt.Sprintf("withdrawal:reversal:%s", withdrawalID),
			ReferenceID:    &w.WithdrawalID,
			ReferenceType:  "withdrawal",
			Description:    fmt.Sprintf("Reversal of failed payout: %s", reason),
		})
		if err != nil {
			return fmt.Errorf("ledger: withdrawal reversal: %w", err)
		}
		w.ReversalTxID = &reversal.TxID
		w.Status = entities.WithdrawalStatusReversed
		w.FailureReason = reason
		if err := d.Withdrawals.Update(ctx, tx, w); err != nil {
			return err
		}
		// Notify admins (audit trail entry doubles as the alert feed).
		if err := d.Audit.Log(ctx, tx, &services.AuditEntry{
			ActorUserID: uuid.Nil,
			ActorRole:   "system",
			Action:      "withdrawal_failed_reversed",
			TargetType:  "withdrawal_request",
			TargetID:    &withdrawalID,
			ReasonCode:  "payout_failure",
			ReasonText:  reason,
		}); err != nil {
			return err
		}
		return publish(ctx, tx, d, Event{
			Type:  "ledger.withdrawal_failed",
			Topic: "nidaw.ledger",
			Payload: map[string]interface{}{
				"withdrawal_id": withdrawalID.String(),
				"user_id":       w.UserID.String(),
				"reason":        reason,
			},
			Timestamp: d.clock().Now().Unix(),
		})
	})
}
