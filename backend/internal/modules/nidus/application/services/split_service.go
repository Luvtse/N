// Package services — §3.3 Split Fare.
//
// A rider (the inviter) splits a ride's fare with one or more contacts. Each
// invitee gets an invite row (migration 011) carrying their share and a
// tokenized accept path so an invitee without an account can still pay via a
// public link. The inviter's own share is always computed as
// fare - SUM(non-declined shares); it is never stored, so the ledger cannot
// double-count the inviter.
//
// Invariants enforced here (never trusted from client input):
//   - Only the ride's owner may create or read a split.
//   - Splits are allowed on completed rides that still have an outstanding
//     balance for the inviter (i.e. sum of shares < fare).
//   - Every share must be > 0; the sum of all shares must be <= the fare
//     (rounded to cents); at most 4 invitees per ride (5-way split max).
//   - Duplicate contacts per ride are rejected (matches the partial unique
//     index in migration 011).
//   - Accept requires the exact split token; a wrong/absent token is 404,
//     indistinguishable from a missing invite (no enumeration oracle).
package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"nidaw-backend/internal/shared/database"
)

// ============================================================================
// Domain types
// ============================================================================

// SplitInviteStatus enumerates the lifecycle of one invitee's share.
type SplitInviteStatus string

const (
	SplitPending  SplitInviteStatus = "pending"
	SplitAccepted SplitInviteStatus = "accepted"
	SplitPaid     SplitInviteStatus = "paid"
	SplitDeclined SplitInviteStatus = "declined"
	SplitExpired  SplitInviteStatus = "expired"
)

// SplitInvite is one invitee row of ride_split_invites (migration 011).
type SplitInvite struct {
	ID             uuid.UUID         `json:"id"`
	RideID         uuid.UUID         `json:"ride_id"`
	InviterID      uuid.UUID         `json:"-"` // never serialized to clients
	InviteeContact string            `json:"invitee_contact"`
	InviteeUserID  *uuid.UUID        `json:"invitee_user_id,omitempty"`
	ShareAmount    float64           `json:"share_amount"`
	Currency       string            `json:"currency"`
	Status         SplitInviteStatus `json:"status"`
	CreatedAt      time.Time         `json:"created_at"`
	AcceptedAt     *time.Time        `json:"accepted_at,omitempty"`
	PaidAt         *time.Time        `json:"paid_at,omitempty"`
}

// SplitView is the authoritative split state for one ride, as returned by
// GET /rides/{rideId}/split. InviterShare is server-computed:
// fare - SUM(shares over non-declined invites). It is never accepted from a
// client. If no split exists, Exists=false and the UI shows the "start a
// split" affordance rather than zeros.
type SplitView struct {
	Exists         bool          `json:"exists"`
	RideID         uuid.UUID     `json:"ride_id"`
	Fare           float64       `json:"fare"`
	Currency       string        `json:"currency"`
	InviterShare   float64       `json:"inviter_share"`
	TotalSplit     float64       `json:"total_split"`
	RemainingToPay float64       `json:"remaining_to_pay"` // accepted-but-unpaid shares
	Invitees       []SplitInvite `json:"invitees"`
}

// CreateSplitInput is one requested share inside CreateSplitCommand.
type CreateSplitInput struct {
	Contact     string  `json:"contact"`
	ShareAmount float64 `json:"share_amount"`
}

// CreateSplitCommand creates (or replaces) the invite set for a ride.
type CreateSplitCommand struct {
	RideID   uuid.UUID
	UserID   uuid.UUID // must be the ride owner
	Currency string
	Shares   []CreateSplitInput
}

// AcceptSplitCommand carries the tokenized public-accept payload. InviteeUserID
// is optional: an anonymous web acceptor pays without an account.
type AcceptSplitCommand struct {
	Token         string
	InviteeUserID *uuid.UUID
}

// Sentinel errors mapped to stable wire codes by the HTTP layer.
var (
	ErrSplitNotOwner      = errors.New("only the ride owner can manage this split")
	ErrSplitRideNotEligib = errors.New("ride is not eligible for fare splitting")
	ErrSplitEmptyShares   = errors.New("at least one share is required")
	ErrSplitTooMany       = errors.New("a ride can be split with at most 4 other people")
	ErrSplitBadShare      = errors.New("every share must be greater than zero")
	ErrSplitOverFare      = errors.New("the sum of shares exceeds the fare")
	ErrSplitDupContact    = errors.New("this contact was already invited")
	ErrSplitNotFound      = errors.New("split invite not found")
	ErrSplitBadState      = errors.New("invite is not in a state that accepts this action")
)

// MaxSplitInvitees is the hard cap on invitees per ride (5-way split total).
const MaxSplitInvitees = 4

// ============================================================================
// Service
// ============================================================================

// SplitService implements §3.3 on top of the shared Postgres pool. All money
// comparisons use cent-rounded arithmetic so NUMERIC(10,2) round-trips cannot
// drift by sub-cent floats.
type SplitService struct {
	db *database.Postgres
}

func NewSplitService(db *database.Postgres) *SplitService {
	return &SplitService{db: db}
}

// splitRowFare loads the ride's terminal fare + currency + status + owner.
// Splitting operates on the COMPLETED fare (the receipt page action), so we
// require status IN ('completed','paid') — a ride still in progress has no
// authoritative number to split yet (Global Rule 2: never split a guess).
func (s *SplitService) loadRideForSplit(ctx context.Context, rideID uuid.UUID) (owner uuid.UUID, fare float64, currency, status string, err error) {
	err = s.db.QueryRow(ctx, `
		SELECT user_id, fare_amount, currency, status::text
		  FROM rides WHERE id = $1`, rideID,
	).Scan(&owner, &fare, &currency, &status)
	return
}

// Create validates the share set and persists the invites. Re-creating a split
// before anyone has accepted replaces the pending set atomically; once any
// invite is accepted/paid the set is frozen (ErrSplitBadState) because the
// inviter's share is already committed against those numbers.
func (s *SplitService) Create(ctx context.Context, cmd *CreateSplitCommand) (*SplitView, error) {
	if len(cmd.Shares) == 0 {
		return nil, ErrSplitEmptyShares
	}
	if len(cmd.Shares) > MaxSplitInvitees {
		return nil, ErrSplitTooMany
	}

	owner, fare, currency, status, err := s.loadRideForSplit(ctx, cmd.RideID)
	if err != nil {
		return nil, ErrSplitNotFound
	}
	if owner != cmd.UserID {
		return nil, ErrSplitNotOwner
	}
	if status != "completed" && status != "paid" {
		return nil, ErrSplitRideNotEligib
	}
	if currency == "" {
		currency = cmd.Currency
	}
	if currency == "" {
		currency = "ETB"
	}

	// Validate + normalize shares before touching the DB.
	type norm struct {
		contact string
		cents   int64
	}
	shares := make([]norm, 0, len(cmd.Shares))
	totalCents := int64(0)
	seen := map[string]bool{}
	for _, sh := range cmd.Shares {
		contact := strings.ToLower(strings.TrimSpace(sh.Contact))
		if contact == "" {
			return nil, fmt.Errorf("%w: empty contact", ErrSplitBadShare)
		}
		if seen[contact] {
			return nil, ErrSplitDupContact
		}
		seen[contact] = true
		cents := roundCents(sh.ShareAmount)
		if cents <= 0 {
			return nil, ErrSplitBadShare
		}
		totalCents += cents
		shares = append(shares, norm{contact: contact, cents: cents})
	}
	fareCents := roundCents(fare)
	if totalCents > fareCents {
		return nil, ErrSplitOverFare
	}

	// Freeze check: any non-pending invite means the split is committed.
	var lockedCount int
	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM ride_split_invites
		 WHERE ride_id = $1 AND status <> 'pending'`, cmd.RideID,
	).Scan(&lockedCount); err != nil {
		return nil, err
	}
	if lockedCount > 0 {
		return nil, ErrSplitBadState
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Replace semantics: drop the old pending set, insert the new one.
	if _, err := tx.Exec(ctx, `
		DELETE FROM ride_split_invites WHERE ride_id = $1 AND status = 'pending'`,
		cmd.RideID,
	); err != nil {
		return nil, err
	}
	for _, sh := range shares {
		if _, err := tx.Exec(ctx, `
			INSERT INTO ride_split_invites
				(ride_id, inviter_id, invitee_contact, share_amount, currency, status)
			VALUES ($1, $2, $3, $4, $5, 'pending')`,
			cmd.RideID, cmd.UserID, sh.contact, float64(sh.cents)/100.0, currency,
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.Get(ctx, cmd.RideID, cmd.UserID)
}

// Get returns the authoritative split view. Readable by the ride owner only;
// invitees see their own slice through the tokenized accept flow instead.
func (s *SplitService) Get(ctx context.Context, rideID, requesterID uuid.UUID) (*SplitView, error) {
	owner, fare, currency, _, err := s.loadRideForSplit(ctx, rideID)
	if err != nil {
		return nil, ErrSplitNotFound
	}
	if owner != requesterID {
		return nil, ErrSplitNotOwner
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, ride_id, invitee_contact, invitee_user_id, share_amount,
		       currency, status::text, created_at, accepted_at, paid_at
		  FROM ride_split_invites
		 WHERE ride_id = $1
		 ORDER BY created_at ASC`, rideID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	view := &SplitView{
		RideID:   rideID,
		Fare:     fare,
		Currency: currency,
		Invitees: []SplitInvite{},
	}
	splitCents := int64(0)
	for rows.Next() {
		var inv SplitInvite
		var st string
		if err := rows.Scan(&inv.ID, &inv.RideID, &inv.InviteeContact, &inv.InviteeUserID,
			&inv.ShareAmount, &inv.Currency, &st, &inv.CreatedAt, &inv.AcceptedAt, &inv.PaidAt); err != nil {
			return nil, err
		}
		inv.Status = SplitInviteStatus(st)
		view.Invitees = append(view.Invitees, inv)
		if inv.Status != SplitDeclined && inv.Status != SplitExpired {
			splitCents += roundCents(inv.ShareAmount)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	if len(view.Invitees) > 0 {
		view.Exists = true
	}
	view.TotalSplit = float64(splitCents) / 100.0
	view.InviterShare = roundTo2(fare - view.TotalSplit)
	for _, inv := range view.Invitees {
		if inv.Status == SplitAccepted {
			view.RemainingToPay = roundTo2(view.RemainingToPay + inv.ShareAmount)
		}
	}
	return view, nil
}

// Accept redeems a tokenized invite. The token is generated when the invite is
// created and delivered out-of-band (SMS/email deep link); possession of the
// token IS the authorization, so this path is intentionally unauthenticated.
// An acceptor WITH an account binds invitee_user_id so their payment can be
// attributed; an anonymous acceptor leaves it NULL and pays via the hosted
// checkout later. Idempotent: re-accepting an accepted invite returns it.
func (s *SplitService) Accept(ctx context.Context, cmd *AcceptSplitCommand) (*SplitInvite, error) {
	token := strings.TrimSpace(cmd.Token)
	if token == "" || len(token) > 64 {
		return nil, ErrSplitNotFound
	}
	var inv SplitInvite
	var st string
	err := s.db.QueryRow(ctx, `
		SELECT id, ride_id, inviter_id, invitee_contact, invitee_user_id, share_amount,
		       currency, status::text, created_at, accepted_at, paid_at
		  FROM ride_split_invites
		 WHERE split_token = $1`, token,
	).Scan(&inv.ID, &inv.RideID, &inv.InviterID, &inv.InviteeContact, &inv.InviteeUserID,
		&inv.ShareAmount, &inv.Currency, &st, &inv.CreatedAt, &inv.AcceptedAt, &inv.PaidAt)
	if err != nil {
		return nil, ErrSplitNotFound // wrong token indistinguishable from missing (no oracle)
	}
	inv.Status = SplitInviteStatus(st)

	switch inv.Status {
	case SplitAccepted, SplitPaid:
		return &inv, nil // idempotent replay
	case SplitPending:
		// proceed
	default:
		return nil, ErrSplitBadState
	}

	now := time.Now().UTC()
	tag := "00000000-0000-0000-0000-000000000000"
	if cmd.InviteeUserID != nil {
		tag = cmd.InviteeUserID.String()
	}
	res, err := s.db.Exec(ctx, `
		UPDATE ride_split_invites
		   SET status = 'accepted', accepted_at = $2, invitee_user_id = COALESCE($3::uuid, invitee_user_id)
		 WHERE id = $1 AND status = 'pending'`,
		inv.ID, now, tagIfValid(tag))
	if err != nil {
		return nil, err
	}
	if res.RowsAffected() == 0 {
		// Lost a race with a concurrent accept/decline — re-read and return truth.
		return s.acceptReplay(ctx, token)
	}
	inv.Status = SplitAccepted
	inv.AcceptedAt = &now
	if cmd.InviteeUserID != nil {
		inv.InviteeUserID = cmd.InviteeUserID
	}
	return &inv, nil
}

func (s *SplitService) acceptReplay(ctx context.Context, token string) (*SplitInvite, error) {
	var inv SplitInvite
	var st string
	err := s.db.QueryRow(ctx, `
		SELECT id, ride_id, inviter_id, invitee_contact, invitee_user_id, share_amount,
		       currency, status::text, created_at, accepted_at, paid_at
		  FROM ride_split_invites WHERE split_token = $1`, token,
	).Scan(&inv.ID, &inv.RideID, &inv.InviterID, &inv.InviteeContact, &inv.InviteeUserID,
		&inv.ShareAmount, &inv.Currency, &st, &inv.CreatedAt, &inv.AcceptedAt, &inv.PaidAt)
	if err != nil {
		return nil, ErrSplitNotFound
	}
	inv.Status = SplitInviteStatus(st)
	return &inv, nil
}

// MarkPaid flips an accepted invite to paid once the invitee's share settles.
// Called by the payment settlement path (same CRUD as any other charge);
// idempotent.
func (s *SplitService) MarkPaid(ctx context.Context, inviteID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `
		UPDATE ride_split_invites
		   SET status = 'paid', paid_at = COALESCE(paid_at, now())
		 WHERE id = $1 AND status IN ('accepted','paid')`, inviteID)
	return err
}

// Token exposes the invite's accept token for notification dispatch
// (email/SMS deep links). Returns ErrSplitNotFound for unknown ids.
func (s *SplitService) Token(ctx context.Context, inviteID uuid.UUID) (string, error) {
	var tok string
	err := s.db.QueryRow(ctx,
		`SELECT split_token FROM ride_split_invites WHERE id = $1`, inviteID,
	).Scan(&tok)
	if err != nil || tok == "" {
		return "", ErrSplitNotFound
	}
	return tok, nil
}

// MintTokens generates and persists tokens for every pending invite of a ride
// right after Create, so the dispatcher can embed them in the email/SMS links.
// Tokens are 24 random bytes hex-encoded (48 chars, ~192 bits of entropy).
func (s *SplitService) MintTokens(ctx context.Context, rideID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id FROM ride_split_invites WHERE ride_id = $1 AND status = 'pending' AND split_token = ''`,
		rideID)
	if err != nil {
		return nil, err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		tok, err := randomToken()
		if err != nil {
			return nil, err
		}
		if _, err := s.db.Exec(ctx,
			`UPDATE ride_split_invites SET split_token = $2 WHERE id = $1 AND status = 'pending'`,
			id, tok); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// ============================================================================
// helpers
// ============================================================================

func roundCents(v float64) int64 {
	n, _ := big.NewRat(0, 1).SetString(fmt.Sprintf("%.2f", v))
	_ = n
	return int64(mathRound(v * 100))
}

func mathRound(f float64) float64 {
	if f >= 0 {
		return float64(int64(f + 0.5))
	}
	return float64(int64(f - 0.5))
}

func roundTo2(v float64) float64 {
	return float64(roundCents(v)) / 100.0
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// tagIfValid passes a UUID string through or nil so COALESCE keeps the
// existing binding when the acceptor is anonymous.
func tagIfValid(s string) *string {
	if u, err := uuid.Parse(s); err == nil && u.String() == s {
		return &s
	}
	return nil
}
