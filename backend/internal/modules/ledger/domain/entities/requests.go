package entities

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
)

// ============================================================================
// TOPUP REQUEST (fiat on-ramp)
// ============================================================================

// TopupStatus enumerates the on-ramp lifecycle.
type TopupStatus string

const (
	TopupStatusPending    TopupStatus = "pending"
	TopupStatusProcessing TopupStatus = "processing"
	TopupStatusCompleted  TopupStatus = "completed"
	TopupStatusFailed     TopupStatus = "failed"
	TopupStatusReversed   TopupStatus = "reversed"
)

// Provider identifies an Ethiopian payment rail adapter (Phase E).
type Provider string

const (
	ProviderTelebirr Provider = "telebirr"
	ProviderChapa    Provider = "chapa"
	ProviderMpesa    Provider = "mpesa"
)

// ValidProvider reports whether p is a supported rail.
func ValidProvider(p string) bool {
	switch Provider(p) {
	case ProviderTelebirr, ProviderChapa, ProviderMpesa:
		return true
	default:
		return false
	}
}

var ErrInvalidProvider = errors.New("ledger: invalid payment provider")

// TopupRequest tracks one fiat on-ramp attempt. Per Phase E Step 3 the
// optimistic credit (CreditTxID) is recorded at creation; on provider
// failure a compensating negative adjustment (ReversalTxID) claws it back
// and the user is placed in negative_lock.
type TopupRequest struct {
	TopupID           uuid.UUID
	UserID            uuid.UUID
	Amount            valueobjects.Money
	Provider          Provider
	ProviderReference string
	Status            TopupStatus
	CreditTxID        *uuid.UUID
	ReversalTxID      *uuid.UUID
	FailureReason     string
	RequestedAt       time.Time
	CompletedAt       *time.Time
	Metadata          map[string]interface{}
}

// NewTopupRequest validates inputs and returns a pending request.
func NewTopupRequest(topupID, userID uuid.UUID, amount valueobjects.Money, provider string) (*TopupRequest, error) {
	if userID == uuid.Nil {
		return nil, ErrTxMissingUser
	}
	if !amount.IsPositive() {
		return nil, errors.New("ledger: topup amount must be positive")
	}
	if err := amount.MustNonNegative(); err != nil {
		return nil, err
	}
	if !ValidProvider(provider) {
		return nil, ErrInvalidProvider
	}
	return &TopupRequest{
		TopupID:     topupID,
		UserID:      userID,
		Amount:      amount,
		Provider:    Provider(provider),
		Status:      TopupStatusPending,
		RequestedAt: time.Now().UTC(),
	}, nil
}

// MarkCompleted transitions pending/processing -> completed.
func (t *TopupRequest) MarkCompleted(at time.Time) error {
	switch t.Status {
	case TopupStatusPending, TopupStatusProcessing:
		t.Status = TopupStatusCompleted
		t.CompletedAt = &at
		return nil
	default:
		return errors.New("ledger: invalid topup transition to completed")
	}
}

// MarkFailed transitions pending/processing -> failed.
func (t *TopupRequest) MarkFailed(reason string) error {
	switch t.Status {
	case TopupStatusPending, TopupStatusProcessing:
		t.Status = TopupStatusFailed
		t.FailureReason = reason
		return nil
	default:
		return errors.New("ledger: invalid topup transition to failed")
	}
}

// ============================================================================
// WITHDRAWAL REQUEST (payout)
// ============================================================================

// WithdrawalStatus enumerates the payout lifecycle.
type WithdrawalStatus string

const (
	WithdrawalStatusPending    WithdrawalStatus = "pending"
	WithdrawalStatusFraudHold  WithdrawalStatus = "fraud_hold"
	WithdrawalStatusApproved   WithdrawalStatus = "approved"
	WithdrawalStatusProcessing WithdrawalStatus = "processing"
	WithdrawalStatusCompleted  WithdrawalStatus = "completed"
	WithdrawalStatusFailed     WithdrawalStatus = "failed"
	WithdrawalStatusReversed   WithdrawalStatus = "reversed"
)

// DestinationType enumerates payout rails.
type DestinationType string

const (
	DestBankTransfer     DestinationType = "bank_transfer"
	DestMpesa            DestinationType = "mpesa"
	DestTelebirrMerchant DestinationType = "telebirr_merchant"
)

// WithdrawalRequest tracks one payout. The debit from available_balance is
// immediate (double-spend prevention, Phase E Step 4); failures reverse it.
type WithdrawalRequest struct {
	WithdrawalID       uuid.UUID
	UserID             uuid.UUID
	Amount             valueobjects.Money
	Fee                valueobjects.Money
	DestinationType    DestinationType
	DestinationDetails map[string]interface{}
	Status             WithdrawalStatus
	DebitTxID          *uuid.UUID
	ReversalTxID       *uuid.UUID
	ProviderReference  string
	RiskScore          *float64
	ReviewedBy         *uuid.UUID
	ReviewedAt         *time.Time
	FailureReason      string
	RequestedAt        time.Time
	CompletedAt        *time.Time
}

// NewWithdrawalRequest validates inputs and returns a pending request.
func NewWithdrawalRequest(id, userID uuid.UUID, amount, fee valueobjects.Money, destType string) (*WithdrawalRequest, error) {
	if userID == uuid.Nil {
		return nil, ErrTxMissingUser
	}
	if !amount.IsPositive() {
		return nil, errors.New("ledger: withdrawal amount must be positive")
	}
	if err := fee.MustNonNegative(); err != nil {
		return nil, errors.New("ledger: withdrawal fee must be non-negative")
	}
	switch DestinationType(destType) {
	case DestBankTransfer, DestMpesa, DestTelebirrMerchant:
	default:
		return nil, errors.New("ledger: invalid withdrawal destination type")
	}
	return &WithdrawalRequest{
		WithdrawalID:    id,
		UserID:          userID,
		Amount:          amount,
		Fee:             fee,
		DestinationType: DestinationType(destType),
		Status:          WithdrawalStatusPending,
		RequestedAt:     time.Now().UTC(),
	}, nil
}

// TotalDebited is what leaves the available bucket (amount + fee).
func (w *WithdrawalRequest) TotalDebited() (valueobjects.Money, error) {
	return w.Amount.Add(w.Fee)
}

// ============================================================================
// ESCROW HOLD (3-day ride payment window)
// ============================================================================

// EscrowStatus enumerates hold lifecycle.
type EscrowStatus string

const (
	EscrowStatusHeld     EscrowStatus = "held"
	EscrowStatusDisputed EscrowStatus = "disputed"
	EscrowStatusReleased EscrowStatus = "released"
	EscrowStatusRefunded EscrowStatus = "refunded"
)

// EscrowHold records a driver earning held for the dispute window.
type EscrowHold struct {
	HoldID       uuid.UUID
	RideID       uuid.UUID
	RiderID      uuid.UUID
	DriverID     uuid.UUID
	Amount       valueobjects.Money
	CreditTxID   *uuid.UUID
	ReleaseTxID  *uuid.UUID
	DisputeID    *uuid.UUID
	Status       EscrowStatus
	ReleaseAfter time.Time
	ReleasedAt   *time.Time
	CreatedAt    time.Time
}

// EscrowWindow is the 72-hour safety window (Phase F).
const EscrowWindow = 72 * time.Hour

// NewEscrowHold creates a held record with release_after = now + 72h.
func NewEscrowHold(holdID, rideID, riderID, driverID uuid.UUID, amount valueobjects.Money, now time.Time) (*EscrowHold, error) {
	if rideID == uuid.Nil || riderID == uuid.Nil || driverID == uuid.Nil {
		return nil, errors.New("ledger: escrow requires ride, rider and driver ids")
	}
	if !amount.IsPositive() {
		return nil, errors.New("ledger: escrow amount must be positive")
	}
	release := now.Add(EscrowWindow)
	return &EscrowHold{
		HoldID:       holdID,
		RideID:       rideID,
		RiderID:      riderID,
		DriverID:     driverID,
		Amount:       amount,
		Status:       EscrowStatusHeld,
		ReleaseAfter: release,
		CreatedAt:    now,
	}, nil
}

// IsReleasable reports whether the hourly job may move funds now.
func (e *EscrowHold) IsReleasable(now time.Time) bool {
	return e.Status == EscrowStatusHeld && e.DisputeID == nil && !now.Before(e.ReleaseAfter)
}

// PauseForDispute flags the hold as disputed and stops the release timer.
func (e *EscrowHold) PauseForDispute(disputeID uuid.UUID) error {
	if e.Status != EscrowStatusHeld {
		return errors.New("ledger: only held escrows can be disputed")
	}
	e.Status = EscrowStatusDisputed
	e.DisputeID = &disputeID
	return nil
}

// ============================================================================
// RIDE DISPUTE
// ============================================================================

// DisputeStatus enumerates dispute workflow states.
type DisputeStatus string

const (
	DisputeOpen           DisputeStatus = "open"
	DisputeAutoResolved   DisputeStatus = "auto_resolved"
	DisputeAdminReview    DisputeStatus = "admin_review"
	DisputeResolvedRider  DisputeStatus = "resolved_rider"
	DisputeResolvedDriver DisputeStatus = "resolved_driver"
	DisputeRejected       DisputeStatus = "rejected"
)

// ReasonCode enumerates dispute reasons (mirrors DB CHECK constraint).
type ReasonCode string

const (
	ReasonNoShow        ReasonCode = "no_show"
	ReasonOvercharge    ReasonCode = "overcharge"
	ReasonRouteDev      ReasonCode = "route_deviation"
	ReasonVehicleIssue  ReasonCode = "vehicle_issue"
	ReasonUnsafeDriving ReasonCode = "unsafe_driving"
	ReasonLostItem      ReasonCode = "lost_item"
	ReasonFareSplit     ReasonCode = "fare_split"
	ReasonOther         ReasonCode = "other"
)

// Valid reports whether r is part of the closed reason-code vocabulary.
func (r ReasonCode) Valid() bool {
	switch r {
	case ReasonNoShow, ReasonOvercharge, ReasonRouteDev, ReasonVehicleIssue,
		ReasonUnsafeDriving, ReasonLostItem, ReasonFareSplit, ReasonOther:
		return true
	default:
		return false
	}
}

// AutoResolvableReasons are simple cases resolved by rules without admin
// review (Phase F Step 3).
func AutoResolvableReason(r ReasonCode) bool {
	switch r {
	case ReasonNoShow, ReasonLostItem:
		return true
	default:
		return false
	}
}

// RideDispute records a rider/driver complaint against an escrowed ride.
type RideDispute struct {
	DisputeID       uuid.UUID
	RideID          uuid.UUID
	HoldID          uuid.UUID
	FiledByUserID   uuid.UUID
	AgainstUserID   uuid.UUID
	Reason          ReasonCode
	Description     string
	EvidenceURLs    []string
	Status          DisputeStatus
	ResolutionNotes string
	RefundTxID      *uuid.UUID
	ReleaseTxID     *uuid.UUID
	AssignedAdminID *uuid.UUID
	ResolvedAt      *time.Time
	CreatedAt       time.Time
}

var ErrInvalidReasonCode = errors.New("ledger: invalid dispute reason code")

// NewRideDispute validates and constructs an open dispute.
func NewRideDispute(id, rideID, holdID, filedBy, against uuid.UUID, reason, description string, evidence []string, now time.Time) (*RideDispute, error) {
	switch ReasonCode(reason) {
	case ReasonNoShow, ReasonOvercharge, ReasonRouteDev, ReasonVehicleIssue,
		ReasonUnsafeDriving, ReasonLostItem, ReasonFareSplit, ReasonOther:
	default:
		return nil, ErrInvalidReasonCode
	}
	if description == "" {
		return nil, errors.New("ledger: dispute description is required")
	}
	status := DisputeOpen
	if AutoResolvableReason(ReasonCode(reason)) {
		status = DisputeAutoResolved
	} else {
		status = DisputeAdminReview
	}
	return &RideDispute{
		DisputeID:     id,
		RideID:        rideID,
		HoldID:        holdID,
		FiledByUserID: filedBy,
		AgainstUserID: against,
		Reason:        ReasonCode(reason),
		Description:   description,
		EvidenceURLs:  evidence,
		Status:        status,
		CreatedAt:     now,
	}, nil
}
