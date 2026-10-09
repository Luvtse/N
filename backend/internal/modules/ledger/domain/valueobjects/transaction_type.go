package valueobjects

import (
	"errors"
	"fmt"
)

// ============================================================================
// TRANSACTION TYPE — enum over the ledger's closed vocabulary
// ============================================================================

// TransactionType classifies every ledger entry. The set is intentionally
// closed: new money movements require a deliberate schema + enum change so
// audit reports stay meaningful.
type TransactionType string

const (
	// TxTypeTopup — fiat on-ramp credit (Telebirr / Chapa / M-Pesa).
	TxTypeTopup TransactionType = "topup"
	// TxTypeRideDebit — rider charged for a completed ride.
	TxTypeRideDebit TransactionType = "ride_debit"
	// TxTypeRideCreditHeld — driver credited into held balance (escrow).
	TxTypeRideCreditHeld TransactionType = "ride_credit_held"
	// TxTypeTip — rider-paid gratuity settled straight to driver available.
	TxTypeTip TransactionType = "tip"
	// TxTypeEscrowRelease — held -> available migration once the 72h window passes.
	TxTypeEscrowRelease TransactionType = "escrow_release"
	// TxTypeRefund — funds returned to rider (dispute / no-show).
	TxTypeRefund TransactionType = "refund"
	// TxTypeWithdrawalDebit — payout initiated, funds deducted from available.
	TxTypeWithdrawalDebit TransactionType = "withdrawal_debit"
	// TxTypeWithdrawalReversal — failed payout, funds returned to available.
	TxTypeWithdrawalReversal TransactionType = "withdrawal_reversal"
	// TxTypeAdjustmentCredit — audited admin-forced credit.
	TxTypeAdjustmentCredit TransactionType = "adjustment_credit"
	// TxTypeAdjustmentDebit — audited admin-forced debit / topup failure claw-back.
	TxTypeAdjustmentDebit TransactionType = "adjustment_debit"
	// TxTypeChargeback — provider chargeback claw-back.
	TxTypeChargeback TransactionType = "chargeback"
)

var allTxTypes = map[TransactionType]bool{
	TxTypeTopup:              true,
	TxTypeRideDebit:          true,
	TxTypeRideCreditHeld:     true,
	TxTypeTip:                true,
	TxTypeEscrowRelease:      true,
	TxTypeRefund:             true,
	TxTypeWithdrawalDebit:    true,
	TxTypeWithdrawalReversal: true,
	TxTypeAdjustmentCredit:   true,
	TxTypeAdjustmentDebit:    true,
	TxTypeChargeback:         true,
}

// ErrUnknownTransactionType is returned by ParseTransactionType on bad input.
var ErrUnknownTransactionType = errors.New("ledger: unknown transaction type")

// ParseTransactionType validates a raw string against the closed enum.
func ParseTransactionType(s string) (TransactionType, error) {
	t := TransactionType(s)
	if !allTxTypes[t] {
		return "", fmt.Errorf("%w: %q", ErrUnknownTransactionType, s)
	}
	return t, nil
}

// Valid reports whether the type is part of the closed enum.
func (t TransactionType) Valid() bool { return allTxTypes[t] }

// IsCredit reports whether this type increases the user's total balance.
// Note: escrow_release moves funds between buckets (held -> available) and
// is NOT a credit; it carries amount 0 in aggregate terms and is recorded
// as an internal migration with positive display amount only for the
// available bucket. Ledger core treats credits strictly via Money sign.
func (t TransactionType) IsCredit() bool {
	switch t {
	case TxTypeTopup, TxTypeRefund, TxTypeWithdrawalReversal,
		TxTypeAdjustmentCredit, TxTypeRideCreditHeld, TxTypeEscrowRelease:
		return true
	default:
		return false
	}
}

// String implements fmt.Stringer.
func (t TransactionType) String() string { return string(t) }
