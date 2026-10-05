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
	ErrPayoutNotFound      = errors.New("payout not found")
	ErrInvalidPayoutState  = errors.New("invalid payout state transition")
	ErrInsufficientBalance = errors.New("insufficient driver balance")
)

// ============================================================================
// TYPES
// ============================================================================

// PayoutStatus represents the state of a driver payout
type PayoutStatus string

const (
	PayoutStatusPending   PayoutStatus = "pending"
	PayoutStatusProcessing PayoutStatus = "processing"
	PayoutStatusCompleted PayoutStatus = "completed"
	PayoutStatusFailed    PayoutStatus = "failed"
	PayoutStatusCancelled PayoutStatus = "cancelled"
)

// PayoutMethod represents the payout method
type PayoutMethod string

const (
	PayoutMethodBankTransfer PayoutMethod = "bank_transfer"
	PayoutMethodInstantPay   PayoutMethod = "instant_pay"
	PayoutMethodDebitCard    PayoutMethod = "debit_card"
	PayoutMethodWallet       PayoutMethod = "wallet"
)

// PayoutFrequency represents how often payouts are processed
type PayoutFrequency string

const (
	PayoutFrequencyDaily   PayoutFrequency = "daily"
	PayoutFrequencyWeekly  PayoutFrequency = "weekly"
	PayoutFrequencyMonthly PayoutFrequency = "monthly"
	PayoutFrequencyInstant PayoutFrequency = "instant"
)

// ============================================================================
// PAYOUT ENTITY
// ============================================================================

// Payout represents a payout to a driver
type Payout struct {
	// Identity
	ID       uuid.UUID `json:"id" db:"id"`
	DriverID uuid.UUID `json:"driver_id" db:"driver_id"`
	UserID   uuid.UUID `json:"user_id" db:"user_id"` // Driver's user account

	// Financial
	Amount         float64 `json:"amount" db:"amount"`
	Currency       string  `json:"currency" db:"currency"`
	Fee            float64 `json:"fee" db:"fee"`
	NetAmount      float64 `json:"net_amount" db:"net_amount"`

	// Status
	Status PayoutStatus `json:"status" db:"status"`

	// Payout Details
	Method       PayoutMethod `json:"method" db:"method"`
	ExternalID   string       `json:"external_id,omitempty" db:"external_id"` // External payout provider ID
	AccountLast4 string       `json:"account_last4,omitempty" db:"account_last4"`

	// Period
	PeriodStart time.Time  `json:"period_start" db:"period_start"`
	PeriodEnd   time.Time  `json:"period_end" db:"period_end"`

	// Breakdown
	TotalEarnings   float64 `json:"total_earnings" db:"total_earnings"`
	Tips            float64 `json:"tips" db:"tips"`
	Bonuses         float64 `json:"bonuses" db:"bonuses"`
	PlatformFees    float64 `json:"platform_fees" db:"platform_fees"`
	Adjustments     float64 `json:"adjustments" db:"adjustments"`
	TransactionCount int    `json:"transaction_count" db:"transaction_count"`

	// Metadata
	Description string            `json:"description,omitempty" db:"description"`
	Metadata    map[string]string `json:"metadata,omitempty" db:"metadata"`

	// Failure Info
	FailureReason string `json:"failure_reason,omitempty" db:"failure_reason"`

	// Timestamps
	ScheduledAt time.Time  `json:"scheduled_at" db:"scheduled_at"`
	ProcessedAt *time.Time `json:"processed_at,omitempty" db:"processed_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
}

// ============================================================================
// DOMAIN BEHAVIORS
// ============================================================================

// TransitionStatus attempts to transition to a new status
func (p *Payout) TransitionStatus(newStatus PayoutStatus) error {
	validTransitions := map[PayoutStatus][]PayoutStatus{
		PayoutStatusPending:    {PayoutStatusProcessing, PayoutStatusCancelled},
		PayoutStatusProcessing: {PayoutStatusCompleted, PayoutStatusFailed},
	}

	allowed, exists := validTransitions[p.Status]
	if !exists {
		return ErrInvalidPayoutState
	}

	for _, s := range allowed {
		if s == newStatus {
			p.Status = newStatus
			p.UpdatedAt = time.Now().UTC()

			if newStatus == PayoutStatusCompleted {
				now := time.Now().UTC()
				p.CompletedAt = &now
			}
			return nil
		}
	}

	return ErrInvalidPayoutState
}

// MarkAsFailed marks the payout as failed with a reason
func (p *Payout) MarkAsFailed(reason string) error {
	if err := p.TransitionStatus(PayoutStatusFailed); err != nil {
		return err
	}
	p.FailureReason = reason
	return nil
}

// MarkAsCompleted marks the payout as completed
func (p *Payout) MarkAsCompleted(externalID string) error {
	if err := p.TransitionStatus(PayoutStatusCompleted); err != nil {
		return err
	}
	p.ExternalID = externalID
	now := time.Now().UTC()
	p.ProcessedAt = &now
	p.CompletedAt = &now
	return nil
}

// CalculateNetAmount calculates the net payout amount
func (p *Payout) CalculateNetAmount() {
	p.NetAmount = p.TotalEarnings + p.Tips + p.Bonuses + p.Adjustments - p.PlatformFees - p.Fee
}

// IsProcessable returns true if payout can be processed
func (p *Payout) IsProcessable() bool {
	return p.Status == PayoutStatusPending &&
		time.Now().After(p.ScheduledAt) &&
		p.NetAmount > 0
}

// IsOverdue returns true if payout is overdue
func (p *Payout) IsOverdue() bool {
	if p.Status != PayoutStatusPending {
		return false
	}
	// Consider overdue if more than 24 hours past scheduled time
	return time.Now().After(p.ScheduledAt.Add(24 * time.Hour))
}

// ============================================================================
// DRIVER BALANCE ENTITY
// ============================================================================

// DriverBalance represents a driver's available balance
type DriverBalance struct {
	DriverID       uuid.UUID `json:"driver_id" db:"driver_id"`
	AvailableBalance float64 `json:"available_balance" db:"available_balance"`
	PendingBalance   float64 `json:"pending_balance" db:"pending_balance"`
	TotalEarnings    float64 `json:"total_earnings" db:"total_earnings"`
	TotalWithdrawn   float64 `json:"total_withdrawn" db:"total_withdrawn"`
	Currency         string  `json:"currency" db:"currency"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

// CanWithdraw checks if driver can withdraw the specified amount
func (b *DriverBalance) CanWithdraw(amount float64) bool {
	return b.AvailableBalance >= amount && amount > 0
}

// Withdraw deducts amount from available balance
func (b *DriverBalance) Withdraw(amount float64) error {
	if !b.CanWithdraw(amount) {
		return ErrInsufficientBalance
	}
	b.AvailableBalance -= amount
	b.TotalWithdrawn += amount
	b.UpdatedAt = time.Now().UTC()
	return nil
}

// Credit adds amount to available balance
func (b *DriverBalance) Credit(amount float64) {
	b.AvailableBalance += amount
	b.TotalEarnings += amount
	b.UpdatedAt = time.Now().UTC()
}