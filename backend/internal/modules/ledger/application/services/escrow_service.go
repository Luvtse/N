// escrow_service.go — Phase F Step 1: the 3-day ride-payment safety window.
//
// EscrowService is the single authority on escrow lifecycle rules:
//   - how long a driver earning stays held (EscrowWindow = 72h),
//   - which disputes auto-resolve without an agent (simple cases such as
//     no-show / fare-split) versus which ones enter the admin review queue,
//   - whether a hold is eligible for automated release.
//
// ProcessRidePayment (settlement) and FileDispute (Phase F Step 3) consult
// this service instead of hard-coding windows/rules, so ops can retune the
// product policy in exactly one place. The DB remains the source of truth;
// this type holds no mutable state and is safe to share across goroutines.
package services

import (
	"time"

	"nidaw-backend/internal/modules/ledger/domain/entities"
)

// DefaultEscrowWindow is the 3-day hold applied to driver earnings after a
// completed ride (roadmap Phase F: "Credit driver held_balance ... release
// date = now + 72 hours").
const DefaultEscrowWindow = 72 * time.Hour

// EscrowService centralises escrow/dispute policy.
type EscrowService struct {
	window time.Duration
}

// NewEscrowService builds the service with the default 72h window.
func NewEscrowService() *EscrowService {
	return &EscrowService{window: DefaultEscrowWindow}
}

// NewEscrowServiceWithWindow allows overriding the window (tests, staged
// rollouts). Non-positive durations fall back to the default.
func NewEscrowServiceWithWindow(window time.Duration) *EscrowService {
	if window <= 0 {
		window = DefaultEscrowWindow
	}
	return &EscrowService{window: window}
}

// Window returns the configured hold duration.
func (s *EscrowService) Window() time.Duration { return s.window }

// ReleaseAfter computes the release timestamp for a hold created at `now`.
func (s *EscrowService) ReleaseAfter(now time.Time) time.Time {
	return now.UTC().Add(s.window)
}

// IsReleasable reports whether the hourly job may move funds from the
// driver's held bucket to available: status must be 'held', no dispute may
// be attached, and the window must have fully elapsed.
func (s *EscrowService) IsReleasable(h *entities.EscrowHold, now time.Time) bool {
	if h == nil {
		return false
	}
	return h.Status == entities.EscrowStatusHeld && h.DisputeID == nil && !now.Before(h.ReleaseAfter)
}

// AutoResolves reports whether a dispute reason is handled by the rules
// engine immediately (no human in the loop). Simple, objectively verifiable
// cases qualify; anything requiring judgement or counterparty contact goes
// to the admin queue. See entities.AutoResolvableReason for the catalogue.
func (s *EscrowService) AutoResolves(reason entities.ReasonCode) bool {
	return entities.AutoResolvableReason(reason)
}

// DisputeDeadline returns the last instant at which a ride may still be
// disputed — i.e. the escrow release moment. After this the funds are
// released and complaints become manual admin adjustments.
func (s *EscrowService) DisputeDeadline(completedAt time.Time) time.Time {
	return s.ReleaseAfter(completedAt)
}
