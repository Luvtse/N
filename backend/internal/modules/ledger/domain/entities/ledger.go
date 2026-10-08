// Package entities holds the ledger domain entities: LedgerTransaction,
// UserBalance, TopupRequest, WithdrawalRequest, EscrowHold and RideDispute.
package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
)

// ============================================================================
// LEDGER TRANSACTION (immutable, hash-chained)
// ============================================================================

// LedgerTransaction is a single append-only entry in a user's hash chain.
// Instances are created through NewLedgerTransaction which computes the
// chain hash; once persisted they must never be modified (the DB trigger
// enforces this as defence-in-depth).
type LedgerTransaction struct {
	TxID           uuid.UUID
	UserID         uuid.UUID
	Amount         valueobjects.Money // signed: credit > 0, debit < 0
	BalanceAfter   valueobjects.Money // total balance snapshot after this tx
	PrevHash       valueobjects.TransactionHash
	TxHash         valueobjects.TransactionHash
	Type           valueobjects.TransactionType
	Currency       string
	ReferenceID    *uuid.UUID // ride / topup / withdrawal / dispute linkage
	ReferenceType  string
	IdempotencyKey string
	Description    string
	Metadata       map[string]interface{}
	CreatedAt      time.Time
}

var (
	ErrTxMissingID      = errors.New("ledger: transaction id is required")
	ErrTxMissingUser    = errors.New("ledger: user id is required")
	ErrTxZeroAmount     = errors.New("ledger: transaction amount must not be zero")
	ErrTxCurrencyMix    = errors.New("ledger: transaction currency must match amount currency")
	ErrTxInvalidBalance = errors.New("ledger: balance_after must be non-negative unless negative_lock applies at service level")
)

// NewLedgerTransaction builds a transaction and computes its chain hash.
// The caller supplies prevHash (head of the user's chain, "0" for genesis).
// allowNegativeBalance permits a negative balance_after snapshot, which is
// only legal on the failed-topup claw-back path (negative_lock, Phase E
// Step 3): the account was optimistically credited and partially spent, so
// removing the full credit drives the total below zero until repaid.
func NewLedgerTransaction(
	txID, userID uuid.UUID,
	amount, balanceAfter valueobjects.Money,
	prevHash valueobjects.TransactionHash,
	txType valueobjects.TransactionType,
	timestamp time.Time,
	allowNegativeBalance bool,
) (*LedgerTransaction, error) {
	if txID == uuid.Nil {
		return nil, ErrTxMissingID
	}
	if userID == uuid.Nil {
		return nil, ErrTxMissingUser
	}
	if amount.IsZero() {
		return nil, ErrTxZeroAmount
	}
	if !txType.Valid() {
		return nil, valueobjects.ErrUnknownTransactionType
	}
	if amount.Currency() != valueobjects.CurrencyETB ||
		balanceAfter.Currency() != valueobjects.CurrencyETB {
		return nil, valueobjects.ErrUnsupportedCurrency
	}
	if balanceAfter.IsNegative() && !allowNegativeBalance {
		return nil, ErrTxInvalidBalance
	}

	tx := &LedgerTransaction{
		TxID:         txID,
		UserID:       userID,
		Amount:       amount,
		BalanceAfter: balanceAfter,
		PrevHash:     prevHash,
		Type:         txType,
		Currency:     valueobjects.CurrencyETB,
		CreatedAt:    timestamp,
	}
	tx.TxHash = valueobjects.ComputeTxHash(
		txID.String(), userID.String(), prevHash,
		amount.Cents(), balanceAfter.Cents(), timestamp.UnixNano(),
	)
	return tx, nil
}

// RecomputeHash re-derives the chain hash from stored fields — used by the
// chain verifier when walking history.
func (t *LedgerTransaction) RecomputeHash() valueobjects.TransactionHash {
	return valueobjects.ComputeTxHash(
		t.TxID.String(), t.UserID.String(), t.PrevHash,
		t.Amount.Cents(), t.BalanceAfter.Cents(), t.CreatedAt.UnixNano(),
	)
}

// VerifySelf reports whether the stored hash matches recomputation.
func (t *LedgerTransaction) VerifySelf() bool { return t.TxHash.Equal(t.RecomputeHash()) }

// ============================================================================
// USER BALANCE (cached state)
// ============================================================================

// BalanceStatus enumerates account-level ledger states.
type BalanceStatus string

const (
	BalanceStatusAccount      BalanceStatus = "account"       // normal
	BalanceStatusNegativeLock BalanceStatus = "negative_lock" // failed topup claw-back; blocks new rides until repaid
	BalanceStatusFrozenReview BalanceStatus = "frozen_review" // fraud freeze pending admin review
	BalanceStatusClosed       BalanceStatus = "closed"
)

// UserBalance is the cached, lockable balance row per user. Buckets:
// available (spendable/withdrawable now), pending (in-flight), held
// (escrowed earnings), withdrawable (available minus any reserve policy).
type UserBalance struct {
	UserID           uuid.UUID
	Available        valueobjects.Money
	Pending          valueobjects.Money
	Held             valueobjects.Money
	Withdrawable     valueobjects.Money
	LifetimeCredited valueobjects.Money
	LifetimeDebited  valueobjects.Money
	LatestTxHash     valueobjects.TransactionHash // chain head cache (mirrored in Redis)
	LatestTxID       *uuid.UUID
	Currency         string
	Status           BalanceStatus
	Version          int64
	UpdatedAt        time.Time
}

// Total returns available + held (the figure mirrored into balance_after on
// every chained transaction).
func (b *UserBalance) Total() (valueobjects.Money, error) {
	return b.Available.Add(b.Held)
}

// CanDebit checks spendable funds without mutating state.
func (b *UserBalance) CanDebit(amount valueobjects.Money) bool {
	if b.Status == BalanceStatusFrozenReview || b.Status == BalanceStatusClosed {
		return false
	}
	return b.Available.Compare(amount) >= 0 && amount.IsPositive()
}

// ApplyCredit mutates the cached buckets for a credit-type movement.
// bucket selects which sub-balance receives the funds.
func (b *UserBalance) ApplyCredit(amount valueobjects.Money, toHeld bool) error {
	if err := amount.MustNonNegative(); err != nil {
		return err
	}
	var err error
	if toHeld {
		b.Held, err = b.Held.Add(amount)
	} else {
		b.Available, err = b.Available.Add(amount)
	}
	if err != nil {
		return err
	}
	b.LifetimeCredited, err = b.LifetimeCredited.Add(amount)
	return err
}

// ApplyDebit mutates the cached buckets for a debit from available.
func (b *UserBalance) ApplyDebit(amount valueobjects.Money) error {
	if err := amount.MustNonNegative(); err != nil {
		return err
	}
	if b.Available.Compare(amount) < 0 && b.Status != BalanceStatusNegativeLock {
		return errors.New("ledger: insufficient available balance")
	}
	var err error
	b.Available, err = b.Available.Sub(amount)
	if err != nil {
		return err
	}
	b.LifetimeDebited, err = b.LifetimeDebited.Add(amount)
	return err
}

// ReleaseEscrow moves `amount` from held to available (Phase F escrow release).
func (b *UserBalance) ReleaseEscrow(amount valueobjects.Money) error {
	if b.Held.Compare(amount) < 0 {
		return errors.New("ledger: held balance insufficient for escrow release")
	}
	var err error
	b.Held, err = b.Held.Sub(amount)
	if err != nil {
		return err
	}
	b.Available, err = b.Available.Add(amount)
	return err
}
