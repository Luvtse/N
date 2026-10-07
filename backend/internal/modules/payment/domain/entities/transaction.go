package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrTransactionNotFound     = errors.New("transaction not found")
	ErrInvalidTransactionState = errors.New("invalid transaction state transition")
	ErrAmountMismatch          = errors.New("amount mismatch")
	ErrAlreadyRefunded         = errors.New("transaction already refunded")
)

// ============================================================================
// TYPES
// ============================================================================

// TransactionStatus represents the state of a payment transaction
type TransactionStatus string

const (
	TransactionStatusPending    TransactionStatus = "pending"
	TransactionStatusProcessing TransactionStatus = "processing"
	TransactionStatusSucceeded  TransactionStatus = "succeeded"
	TransactionStatusFailed     TransactionStatus = "failed"
	TransactionStatusCancelled  TransactionStatus = "cancelled"
	TransactionStatusRefunded   TransactionStatus = "refunded"
	TransactionStatusPartiallyRefunded TransactionStatus = "partially_refunded"
)

// OrderType represents the type of order being paid for
type OrderType string

const (
	OrderTypeRide    OrderType = "ride"
	OrderTypeHotel   OrderType = "hotel"
	OrderTypeFood    OrderType = "food"
	OrderTypeFreight OrderType = "freight"
)

// PaymentMethod represents the payment method used
type PaymentMethod string

const (
	PaymentMethodCreditCard   PaymentMethod = "credit_card"
	PaymentMethodDebitCard    PaymentMethod = "debit_card"
	PaymentMethodDigitalWallet PaymentMethod = "digital_wallet"
	PaymentMethodBankTransfer PaymentMethod = "bank_transfer"
	PaymentMethodCrypto       PaymentMethod = "crypto"
	PaymentMethodCorporateAccount PaymentMethod = "corporate_account"
)

// ============================================================================
// TRANSACTION ENTITY
// ============================================================================

// Transaction represents a payment transaction in the system
type Transaction struct {
	// Identity
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	OrderID   uuid.UUID `json:"order_id" db:"order_id"`
	OrderType OrderType `json:"order_type" db:"order_type"`

	// Financial
	Amount          float64 `json:"amount" db:"amount"`
	Currency        string  `json:"currency" db:"currency"`
	Fee             float64 `json:"fee" db:"fee"`
	NetAmount       float64 `json:"net_amount" db:"net_amount"`
	RefundedAmount  float64 `json:"refunded_amount" db:"refunded_amount"`

	// Status
	Status TransactionStatus `json:"status" db:"status"`

	// Payment Details
	PaymentMethod         PaymentMethod `json:"payment_method" db:"payment_method"`
	ExternalTransactionID string        `json:"external_transaction_id" db:"external_transaction_id"` // Provider reference (Telebirr/Chapa/M-Pesa)
	PaymentIntentID       string        `json:"payment_intent_id" db:"payment_intent_id"`
	CardLast4             string        `json:"card_last4,omitempty" db:"card_last4"`
	CardBrand             string        `json:"card_brand,omitempty" db:"card_brand"`

	// Metadata
	Description string            `json:"description,omitempty" db:"description"`
	Metadata    map[string]string `json:"metadata,omitempty" db:"metadata"`

	// Refund Info
	RefundReason string    `json:"refund_reason,omitempty" db:"refund_reason"`
	RefundedAt   *time.Time `json:"refunded_at,omitempty" db:"refunded_at"`

	// Timestamps
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt time.Time  `json:"updated_at" db:"updated_at"`
}

// ============================================================================
// DOMAIN BEHAVIORS
// ============================================================================

// CanBeRefunded checks if transaction can be refunded
func (t *Transaction) CanBeRefunded() bool {
	return t.Status == TransactionStatusSucceeded &&
		t.RefundedAmount < t.Amount
}

// CanBeFullyRefunded checks if transaction can be fully refunded
func (t *Transaction) CanBeFullyRefunded() bool {
	return t.Status == TransactionStatusSucceeded &&
		t.RefundedAmount == 0
}

// RemainingRefundable returns the amount that can still be refunded
func (t *Transaction) RemainingRefundable() float64 {
	return t.Amount - t.RefundedAmount
}

// TransitionStatus attempts to transition to a new status
func (t *Transaction) TransitionStatus(newStatus TransactionStatus) error {
	validTransitions := map[TransactionStatus][]TransactionStatus{
		TransactionStatusPending:   {TransactionStatusProcessing, TransactionStatusSucceeded, TransactionStatusFailed, TransactionStatusCancelled},
		TransactionStatusProcessing: {TransactionStatusSucceeded, TransactionStatusFailed},
		TransactionStatusSucceeded: {TransactionStatusRefunded, TransactionStatusPartiallyRefunded},
		TransactionStatusPartiallyRefunded: {TransactionStatusRefunded, TransactionStatusPartiallyRefunded},
	}

	allowed, exists := validTransitions[t.Status]
	if !exists {
		return ErrInvalidTransactionState
	}

	for _, s := range allowed {
		if s == newStatus {
			t.Status = newStatus
			t.UpdatedAt = time.Now().UTC()
			return nil
		}
	}

	return ErrInvalidTransactionState
}

// ApplyRefund applies a refund amount to the transaction
func (t *Transaction) ApplyRefund(amount float64, reason string) error {
	if !t.CanBeRefunded() {
		return ErrAlreadyRefunded
	}

	if amount > t.RemainingRefundable() {
		return ErrAmountMismatch
	}

	t.RefundedAmount += amount
	t.RefundReason = reason
	now := time.Now().UTC()
	t.RefundedAt = &now

	if t.RefundedAmount >= t.Amount {
		t.Status = TransactionStatusRefunded
	} else {
		t.Status = TransactionStatusPartiallyRefunded
	}

	t.UpdatedAt = now
	return nil
}

// IsSuccessful returns true if transaction succeeded
func (t *Transaction) IsSuccessful() bool {
	return t.Status == TransactionStatusSucceeded ||
		t.Status == TransactionStatusPartiallyRefunded ||
		t.Status == TransactionStatusRefunded
}

// IsPending returns true if transaction is pending
func (t *Transaction) IsPending() bool {
	return t.Status == TransactionStatusPending ||
		t.Status == TransactionStatusProcessing
}

// PlatformFee calculates the platform's cut
func (t *Transaction) PlatformFee(platformFeeRate float64) float64 {
	return t.Amount * platformFeeRate
}

// DriverPayout calculates the driver's payout
func (t *Transaction) DriverPayout(platformFeeRate float64) float64 {
	return t.Amount - t.PlatformFee(platformFeeRate)
}