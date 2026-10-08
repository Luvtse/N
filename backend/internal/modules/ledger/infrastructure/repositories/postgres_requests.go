package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nidaw-backend/internal/modules/ledger/application/services"
	"nidaw-backend/internal/modules/ledger/domain/entities"
	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
)

// ============================================================================
// TOPUP REPOSITORY
// ============================================================================

type TopupRepo struct{ pool *pgxpool.Pool }

func NewTopupRepo(pool *pgxpool.Pool) *TopupRepo { return &TopupRepo{pool: pool} }

func nullableUUID(id *uuid.UUID) interface{} {
	if id == nil {
		return nil
	}
	return *id
}

func nullableTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return *t
}

func (r *TopupRepo) Create(ctx context.Context, tx services.DBTx, t *entities.TopupRequest) error {
	meta := marshalMeta(t.Metadata)
	ref := interface{}(nil)
	if t.ProviderReference != "" {
		ref = t.ProviderReference
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO topup_requests (topup_id, user_id, amount_cents, currency, provider,
			provider_reference, status, credit_tx_id, requested_at, metadata)
		VALUES ($1,$2,$3,'ETB',$4,$5,$6,$7,$8,$9)`,
		t.TopupID, t.UserID, t.Amount.Cents(), string(t.Provider), ref,
		string(t.Status), nullableUUID(t.CreditTxID), t.RequestedAt, meta)
	return err
}

func (r *TopupRepo) Update(ctx context.Context, tx services.DBTx, t *entities.TopupRequest) error {
	reason := interface{}(nil)
	if t.FailureReason != "" {
		reason = t.FailureReason
	}
	res, err := tx.Exec(ctx, `
		UPDATE topup_requests SET status=$2, provider_reference=COALESCE($3, provider_reference),
			credit_tx_id=COALESCE($4, credit_tx_id), reversal_tx_id=COALESCE($5, reversal_tx_id),
			failure_reason=$6, completed_at=$7, updated_at=NOW()
		WHERE topup_id=$1`,
		t.TopupID, string(t.Status),
		func() interface{} {
			if t.ProviderReference != "" {
				return t.ProviderReference
			}
			return nil
		}(),
		nullableUUID(t.CreditTxID), nullableUUID(t.ReversalTxID), reason, nullableTime(t.CompletedAt))
	if err != nil {
		return err
	}
	if res == 0 {
		return fmt.Errorf("ledger: topup %s not found for update", t.TopupID)
	}
	return nil
}

const topupColumns = `topup_id, user_id, amount_cents, provider, COALESCE(provider_reference,''),
	status, COALESCE(credit_tx_id::text,''), COALESCE(reversal_tx_id::text,''),
	COALESCE(failure_reason,''), requested_at, completed_at, metadata`

func scanTopup(row interface {
	Scan(dest ...interface{}) error
}) (*entities.TopupRequest, error) {
	var (
		id, userID  uuid.UUID
		cents       int64
		provider    string
		provRef     string
		status      string
		creditID    string
		reversalID  string
		failure     string
		requestedAt time.Time
		completedAt *time.Time
		metaRaw     []byte
	)
	if err := row.Scan(&id, &userID, &cents, &provider, &provRef, &status,
		&creditID, &reversalID, &failure, &requestedAt, &completedAt, &metaRaw); err != nil {
		return nil, err
	}
	amount, err := valueobjects.NewMoney(cents)
	if err != nil {
		return nil, err
	}
	t := &entities.TopupRequest{
		TopupID: id, UserID: userID, Amount: amount,
		Provider: entities.Provider(provider), ProviderReference: provRef,
		Status: entities.TopupStatus(status), FailureReason: failure,
		RequestedAt: requestedAt, CompletedAt: completedAt,
	}
	for _, pair := range []struct {
		s string
		o **uuid.UUID
	}{{creditID, &t.CreditTxID}, {reversalID, &t.ReversalTxID}} {
		if pair.s != "" {
			v := uuid.MustParse(pair.s)
			*pair.o = &v
		}
	}
	var meta map[string]interface{}
	_ = json.Unmarshal(metaRaw, &meta)
	t.Metadata = meta
	return t, nil
}

func (r *TopupRepo) GetByID(ctx context.Context, id uuid.UUID) (*entities.TopupRequest, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+topupColumns+` FROM topup_requests WHERE topup_id=$1`, id)
	t, err := scanTopup(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("ledger: topup %s not found", id)
	}
	return t, err
}

// LookupByProviderReference resolves a webhook reference back to the request.
// It matches the provider's own transaction id first, then falls back to our
// topup UUID (we send tx_ref = topup_id to every rail as the checkout
// reference). Returns (nil, nil) when nothing matches so callers can treat
// unknown references as no-ops rather than errors.
func (r *TopupRepo) LookupByProviderReference(ctx context.Context, ref string) (*entities.TopupRequest, error) {
	if ref == "" {
		return nil, nil
	}
	row := r.pool.QueryRow(ctx,
		`SELECT `+topupColumns+` FROM topup_requests
		  WHERE provider_reference = $1
		     OR ($1 ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
		         AND topup_id = $1::uuid)
		  LIMIT 1`, ref)
	t, err := scanTopup(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: lookup topup by provider ref: %w", err)
	}
	return t, nil
}

// ListStuck returns non-terminal topups requested before `before` (unix
// seconds), oldest first — the polling safety net for missed webhooks.
func (r *TopupRepo) ListStuck(ctx context.Context, beforeUnix int64, limit int) ([]*entities.TopupRequest, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+topupColumns+` FROM topup_requests
		  WHERE status IN ('pending','processing') AND requested_at < to_timestamp($1)
		  ORDER BY requested_at ASC LIMIT $2`, beforeUnix, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger: list stuck topups: %w", err)
	}
	defer rows.Close()
	var out []*entities.TopupRequest
	for rows.Next() {
		t, err := scanTopup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ============================================================================
// WITHDRAWAL REPOSITORY
// ============================================================================

type WithdrawalRepo struct{ pool *pgxpool.Pool }

func NewWithdrawalRepo(pool *pgxpool.Pool) *WithdrawalRepo { return &WithdrawalRepo{pool: pool} }

func (r *WithdrawalRepo) Create(ctx context.Context, tx services.DBTx, w *entities.WithdrawalRequest) error {
	dest, err := json.Marshal(w.DestinationDetails)
	if err != nil || len(dest) == 0 {
		dest = []byte("{}")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO withdrawal_requests (withdrawal_id, user_id, amount_cents, fee_cents,
			currency, destination_type, destination_details, status, requested_at)
		VALUES ($1,$2,$3,$4,'ETB',$5,$6,$7,$8)`,
		w.WithdrawalID, w.UserID, w.Amount.Cents(), w.Fee.Cents(),
		string(w.DestinationType), dest, string(w.Status), w.RequestedAt)
	return err
}

func (r *WithdrawalRepo) Update(ctx context.Context, tx services.DBTx, w *entities.WithdrawalRequest) error {
	reason := interface{}(nil)
	if w.FailureReason != "" {
		reason = w.FailureReason
	}
	provRef := interface{}(nil)
	if w.ProviderReference != "" {
		provRef = w.ProviderReference
	}
	var risk interface{}
	if w.RiskScore != nil {
		risk = *w.RiskScore
	}
	res, err := tx.Exec(ctx, `
		UPDATE withdrawal_requests SET status=$2,
			debit_tx_id=COALESCE($3, debit_tx_id), reversal_tx_id=COALESCE($4, reversal_tx_id),
			provider_reference=COALESCE($5, provider_reference), risk_score=COALESCE($6, risk_score),
			reviewed_by=COALESCE($7, reviewed_by), reviewed_at=COALESCE($8, reviewed_at),
			failure_reason=$9, completed_at=COALESCE($10, completed_at), updated_at=NOW()
		WHERE withdrawal_id=$1`,
		w.WithdrawalID, string(w.Status),
		nullableUUID(w.DebitTxID), nullableUUID(w.ReversalTxID), provRef, risk,
		nullableUUID(w.ReviewedBy), nullableTime(w.ReviewedAt), reason, nullableTime(w.CompletedAt))
	if err != nil {
		return err
	}
	if res == 0 {
		return fmt.Errorf("ledger: withdrawal %s not found for update", w.WithdrawalID)
	}
	return nil
}

const withdrawalColumns = `withdrawal_id, user_id, amount_cents, fee_cents, destination_type,
	status, COALESCE(debit_tx_id::text,''), COALESCE(reversal_tx_id::text,''),
	COALESCE(provider_reference,''), destination_details, COALESCE(failure_reason,''), requested_at, completed_at`

func (r *WithdrawalRepo) GetByID(ctx context.Context, id uuid.UUID) (*entities.WithdrawalRequest, error) {
	var (
		wID, userID    uuid.UUID
		amountC, feeC  int64
		destType       string
		status         string
		debitID, revID string
		provRef        string
		destRaw        []byte
		failure        string
		requestedAt    time.Time
		completedAt    *time.Time
	)
	err := r.pool.QueryRow(ctx,
		`SELECT `+withdrawalColumns+` FROM withdrawal_requests WHERE withdrawal_id=$1`, id).
		Scan(&wID, &userID, &amountC, &feeC, &destType, &status, &debitID, &revID,
			&provRef, &destRaw, &failure, &requestedAt, &completedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("ledger: withdrawal %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	return scanWithdrawal(wID, userID, amountC, feeC, destType, status, debitID, revID,
		provRef, destRaw, failure, requestedAt, completedAt)
}

// LookupByProviderReference resolves a payout callback back to the request.
// Matches provider_reference first, then our own withdrawal UUID (we send
// reference = withdrawal_id to the rail). Unknown refs yield (nil, nil).
func (r *WithdrawalRepo) LookupByProviderReference(ctx context.Context, ref string) (*entities.WithdrawalRequest, error) {
	if ref == "" {
		return nil, nil
	}
	var (
		wID, userID    uuid.UUID
		amountC, feeC  int64
		destType       string
		status         string
		debitID, revID string
		provRef        string
		destRaw        []byte
		failure        string
		requestedAt    time.Time
		completedAt    *time.Time
	)
	err := r.pool.QueryRow(ctx,
		`SELECT `+withdrawalColumns+` FROM withdrawal_requests
		  WHERE provider_reference = $1
		     OR ($1 ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
		         AND withdrawal_id = $1::uuid)
		  LIMIT 1`, ref).
		Scan(&wID, &userID, &amountC, &feeC, &destType, &status, &debitID, &revID,
			&provRef, &destRaw, &failure, &requestedAt, &completedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ledger: lookup withdrawal by provider ref: %w", err)
	}
	return scanWithdrawal(wID, userID, amountC, feeC, destType, status, debitID, revID,
		provRef, destRaw, failure, requestedAt, completedAt)
}

// ListActionable returns withdrawals still awaiting work: pending (payout not
// yet submitted — e.g. no initiator wired at request time) and processing
// (submitted, awaiting terminal confirmation via poll/webhook).
func (r *WithdrawalRepo) ListActionable(ctx context.Context, limit int) ([]*entities.WithdrawalRequest, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+withdrawalColumns+` FROM withdrawal_requests
		  WHERE status IN ('pending','processing')
		  ORDER BY requested_at ASC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger: list actionable withdrawals: %w", err)
	}
	defer rows.Close()
	var out []*entities.WithdrawalRequest
	for rows.Next() {
		var (
			wID, userID    uuid.UUID
			amountC, feeC  int64
			destType       string
			status         string
			debitID, revID string
			provRef        string
			destRaw        []byte
			failure        string
			requestedAt    time.Time
			completedAt    *time.Time
		)
		if err := rows.Scan(&wID, &userID, &amountC, &feeC, &destType, &status, &debitID, &revID,
			&provRef, &destRaw, &failure, &requestedAt, &completedAt); err != nil {
			return nil, err
		}
		w, err := scanWithdrawal(wID, userID, amountC, feeC, destType, status, debitID, revID,
			provRef, destRaw, failure, requestedAt, completedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListByStatus returns all withdrawals currently in the given status, oldest
// first (used by the payout job to pick up admin-approved requests).
func (r *WithdrawalRepo) ListByStatus(ctx context.Context, status entities.WithdrawalStatus, limit int) ([]*entities.WithdrawalRequest, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+withdrawalColumns+` FROM withdrawal_requests
		  WHERE status = $1
		  ORDER BY requested_at ASC LIMIT $2`, string(status), limit)
	if err != nil {
		return nil, fmt.Errorf("ledger: list withdrawals by status %q: %w", status, err)
	}
	defer rows.Close()
	var out []*entities.WithdrawalRequest
	for rows.Next() {
		var (
			wID, userID    uuid.UUID
			amountC, feeC  int64
			destType       string
			st             string
			debitID, revID string
			provRef        string
			destRaw        []byte
			failure        string
			requestedAt    time.Time
			completedAt    *time.Time
		)
		if err := rows.Scan(&wID, &userID, &amountC, &feeC, &destType, &st, &debitID, &revID,
			&provRef, &destRaw, &failure, &requestedAt, &completedAt); err != nil {
			return nil, err
		}
		w, err := scanWithdrawal(wID, userID, amountC, feeC, destType, st, debitID, revID,
			provRef, destRaw, failure, requestedAt, completedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func scanWithdrawal(wID, userID uuid.UUID, amountC, feeC int64, destType, status, debitID, revID,
	provRef string, destRaw []byte, failure string, requestedAt time.Time, completedAt *time.Time,
) (*entities.WithdrawalRequest, error) {
	amount, err := valueobjects.NewMoney(amountC)
	if err != nil {
		return nil, err
	}
	fee, err := valueobjects.NewMoney(feeC)
	if err != nil {
		return nil, err
	}
	w := &entities.WithdrawalRequest{
		WithdrawalID: wID, UserID: userID, Amount: amount, Fee: fee,
		DestinationType:   entities.DestinationType(destType),
		Status:            entities.WithdrawalStatus(status),
		ProviderReference: provRef, FailureReason: failure,
		RequestedAt: requestedAt, CompletedAt: completedAt,
	}
	if debitID != "" {
		v := uuid.MustParse(debitID)
		w.DebitTxID = &v
	}
	if revID != "" {
		v := uuid.MustParse(revID)
		w.ReversalTxID = &v
	}
	var dest map[string]interface{}
	_ = json.Unmarshal(destRaw, &dest)
	w.DestinationDetails = dest
	return w, nil
}

// ============================================================================
// ESCROW REPOSITORY
// ============================================================================

type EscrowRepo struct{ pool *pgxpool.Pool }

func NewEscrowRepo(pool *pgxpool.Pool) *EscrowRepo { return &EscrowRepo{pool: pool} }

func (r *EscrowRepo) Create(ctx context.Context, tx services.DBTx, e *entities.EscrowHold) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO escrow_holds (hold_id, ride_id, rider_id, driver_id, amount_cents,
			currency, credit_tx_id, dispute_id, status, release_after, created_at)
		VALUES ($1,$2,$3,$4,$5,'ETB',$6,$7,$8,$9,$10)`,
		e.HoldID, e.RideID, e.RiderID, e.DriverID, e.Amount.Cents(),
		nullableUUID(e.CreditTxID), nullableUUID(e.DisputeID),
		string(e.Status), e.ReleaseAfter, e.CreatedAt)
	return err
}

func (r *EscrowRepo) Update(ctx context.Context, tx services.DBTx, e *entities.EscrowHold) error {
	res, err := tx.Exec(ctx, `
		UPDATE escrow_holds SET status=$2,
			credit_tx_id=COALESCE($3, credit_tx_id),
			release_tx_id=COALESCE($4, release_tx_id),
			dispute_id=COALESCE($5, dispute_id),
			release_after=$6, released_at=COALESCE($7, released_at), updated_at=NOW()
		WHERE hold_id=$1`,
		e.HoldID, string(e.Status), nullableUUID(e.CreditTxID),
		nullableUUID(e.ReleaseTxID), nullableUUID(e.DisputeID),
		e.ReleaseAfter, nullableTime(e.ReleasedAt))
	if err != nil {
		return err
	}
	if res == 0 {
		return fmt.Errorf("ledger: escrow hold %s not found", e.HoldID)
	}
	return nil
}

const escrowColumns = `hold_id, ride_id, rider_id, driver_id, amount_cents, status,
	COALESCE(credit_tx_id::text,''), COALESCE(release_tx_id::text,''),
	COALESCE(dispute_id::text,''), release_after, released_at, created_at`

func scanEscrow(row interface {
	Scan(dest ...interface{}) error
}) (*entities.EscrowHold, error) {
	var (
		holdID, rideID, riderID, driverID uuid.UUID
		cents                             int64
		status                            string
		creditID, releaseID, disputeID    string
		releaseAfter                      time.Time
		releasedAt                        *time.Time
		createdAt                         time.Time
	)
	if err := row.Scan(&holdID, &rideID, &riderID, &driverID, &cents, &status,
		&creditID, &releaseID, &disputeID, &releaseAfter, &releasedAt, &createdAt); err != nil {
		return nil, err
	}
	amount, _ := valueobjects.NewMoney(cents)
	e := &entities.EscrowHold{
		HoldID: holdID, RideID: rideID, RiderID: riderID, DriverID: driverID,
		Amount: amount, Status: entities.EscrowStatus(status),
		ReleaseAfter: releaseAfter, ReleasedAt: releasedAt, CreatedAt: createdAt,
	}
	for _, p := range []struct {
		s string
		o **uuid.UUID
	}{{creditID, &e.CreditTxID}, {releaseID, &e.ReleaseTxID}, {disputeID, &e.DisputeID}} {
		if p.s != "" {
			v := uuid.MustParse(p.s)
			*p.o = &v
		}
	}
	return e, nil
}

func (r *EscrowRepo) GetByRideID(ctx context.Context, rideID uuid.UUID) (*entities.EscrowHold, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+escrowColumns+` FROM escrow_holds WHERE ride_id=$1`, rideID)
	e, err := scanEscrow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // no escrow for this ride yet
	}
	return e, err
}

func (r *EscrowRepo) GetByID(ctx context.Context, holdID uuid.UUID) (*entities.EscrowHold, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+escrowColumns+` FROM escrow_holds WHERE hold_id=$1`, holdID)
	e, err := scanEscrow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("ledger: escrow hold %s not found", holdID)
	}
	return e, err
}

func (r *EscrowRepo) FindReleasable(ctx context.Context, nowUnix int64, limit int) ([]*entities.EscrowHold, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+escrowColumns+` FROM escrow_holds
		WHERE status='held' AND dispute_id IS NULL AND release_after <= to_timestamp($1)
		ORDER BY release_after LIMIT $2`, nowUnix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*entities.EscrowHold
	for rows.Next() {
		e, err := scanEscrow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ============================================================================
// DISPUTE REPOSITORY
// ============================================================================

type DisputeRepo struct{ pool *pgxpool.Pool }

func NewDisputeRepo(pool *pgxpool.Pool) *DisputeRepo { return &DisputeRepo{pool: pool} }

func (r *DisputeRepo) Create(ctx context.Context, tx services.DBTx, d *entities.RideDispute) error {
	evidence := d.EvidenceURLs
	if evidence == nil {
		evidence = []string{}
	}
	notes := interface{}(nil)
	if d.ResolutionNotes != "" {
		notes = d.ResolutionNotes
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO ride_disputes (dispute_id, ride_id, hold_id, filed_by_user_id, against_user_id,
			reason_code, description, evidence_urls, status, resolution_notes, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		d.DisputeID, d.RideID, d.HoldID, d.FiledByUserID, d.AgainstUserID,
		string(d.Reason), d.Description, evidence, string(d.Status), notes, d.CreatedAt)
	return err
}

func (r *DisputeRepo) Update(ctx context.Context, tx services.DBTx, d *entities.RideDispute) error {
	res, err := tx.Exec(ctx, `
		UPDATE ride_disputes SET status=$2,
			resolution_notes=COALESCE($3, resolution_notes),
			refund_tx_id=COALESCE($4, refund_tx_id),
			release_tx_id=COALESCE($5, release_tx_id),
			assigned_admin_id=COALESCE($6, assigned_admin_id),
			resolved_at=COALESCE($7, resolved_at), updated_at=NOW()
		WHERE dispute_id=$1`,
		d.DisputeID, string(d.Status), func() interface{} {
			if d.ResolutionNotes != "" {
				return d.ResolutionNotes
			}
			return nil
		}(),
		nullableUUID(d.RefundTxID), nullableUUID(d.ReleaseTxID),
		nullableUUID(d.AssignedAdminID), nullableTime(d.ResolvedAt))
	if err != nil {
		return err
	}
	if res == 0 {
		return fmt.Errorf("ledger: dispute %s not found", d.DisputeID)
	}
	return nil
}

const disputeColumns = `dispute_id, ride_id, hold_id, filed_by_user_id, against_user_id,
	reason_code, description, evidence_urls, status, COALESCE(resolution_notes,''),
	COALESCE(refund_tx_id::text,''), COALESCE(release_tx_id::text,''),
	COALESCE(assigned_admin_id::text,''), resolved_at, created_at`

func (r *DisputeRepo) GetByID(ctx context.Context, id uuid.UUID) (*entities.RideDispute, error) {
	var (
		dID, rideID, holdID, byID, againstID uuid.UUID
		reason, description                  string
		evidence                             []string
		status, notes                        string
		refundID, releaseID, adminID         string
		resolvedAt                           *time.Time
		createdAt                            time.Time
	)
	err := r.pool.QueryRow(ctx,
		`SELECT `+disputeColumns+` FROM ride_disputes WHERE dispute_id=$1`, id).
		Scan(&dID, &rideID, &holdID, &byID, &againstID, &reason, &description, &evidence,
			&status, &notes, &refundID, &releaseID, &adminID, &resolvedAt, &createdAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("ledger: dispute %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	return scanDisputeRow(dID, rideID, holdID, byID, againstID, reason, description,
		evidence, status, notes, refundID, releaseID, adminID, resolvedAt, createdAt), nil
}

func scanDisputeRow(
	dID, rideID, holdID, byID, againstID uuid.UUID, reason, description string,
	evidence []string, status, notes, refundID, releaseID, adminID string,
	resolvedAt *time.Time, createdAt time.Time,
) *entities.RideDispute {
	d := &entities.RideDispute{
		DisputeID: dID, RideID: rideID, HoldID: holdID,
		FiledByUserID: byID, AgainstUserID: againstID,
		Reason: entities.ReasonCode(reason), Description: description,
		EvidenceURLs: evidence, Status: entities.DisputeStatus(status),
		ResolutionNotes: notes, ResolvedAt: resolvedAt, CreatedAt: createdAt,
	}
	for _, p := range []struct {
		s string
		o **uuid.UUID
	}{{refundID, &d.RefundTxID}, {releaseID, &d.ReleaseTxID}, {adminID, &d.AssignedAdminID}} {
		if p.s != "" {
			v := uuid.MustParse(p.s)
			*p.o = &v
		}
	}
	return d
}

// ListQueue powers the Phase F Step 3 admin review queue (and the Phase H
// console): pendingOnly restricts to open/admin_review rows; otherwise every
// state is returned. Oldest unresolved work surfaces first.
func (r *DisputeRepo) ListQueue(ctx context.Context, pendingOnly bool, limit int) ([]*entities.RideDispute, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT ` + disputeColumns + ` FROM ride_disputes`
	if pendingOnly {
		q += ` WHERE status IN ('open','admin_review')`
	}
	q += ` ORDER BY created_at ASC LIMIT $1`
	rows, err := r.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*entities.RideDispute
	for rows.Next() {
		var (
			dID, rideID, holdID, byID, againstID uuid.UUID
			reason, description                  string
			evidence                             []string
			status, notes                        string
			refundID, releaseID, adminID         string
			resolvedAt                           *time.Time
			createdAt                            time.Time
		)
		if err := rows.Scan(&dID, &rideID, &holdID, &byID, &againstID, &reason, &description,
			&evidence, &status, &notes, &refundID, &releaseID, &adminID, &resolvedAt, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, scanDisputeRow(dID, rideID, holdID, byID, againstID, reason, description,
			evidence, status, notes, refundID, releaseID, adminID, resolvedAt, createdAt))
	}
	return out, rows.Err()
}

// ============================================================================
// AUDIT REPOSITORY
// ============================================================================

type AuditRepo struct{ pool *pgxpool.Pool }

func NewAuditRepo(pool *pgxpool.Pool) *AuditRepo { return &AuditRepo{pool: pool} }

func jsonCol(v map[string]interface{}) interface{} {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// Log appends one audit record inside the caller's DB transaction so that
// admin mutations and their audit trail commit atomically.
func (r *AuditRepo) Log(ctx context.Context, tx services.DBTx, e *services.AuditEntry) error {
	ip := interface{}(nil)
	if e.IPAddress != "" {
		ip = e.IPAddress
	}
	ua := interface{}(nil)
	if e.UserAgent != "" {
		ua = e.UserAgent
	}
	rc := interface{}(nil)
	if e.ReasonCode != "" {
		rc = e.ReasonCode
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_log (actor_user_id, actor_role, action, target_type, target_id,
			reason_code, reason_text, before_state, after_state, ip_address, user_agent)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		e.ActorUserID, e.ActorRole, e.Action, e.TargetType, nullableUUID(e.TargetID),
		rc, e.ReasonText, jsonCol(e.BeforeState), jsonCol(e.AfterState), ip, ua)
	return err
}

// ListRecent enumerates audit_log rows newest-first for the admin console
// Activity/Audit view (Phase H Step 3). limit <= 0 defaults to 100; a
// non-empty actionFilter restricts results to that exact action.
func (r *AuditRepo) ListRecent(ctx context.Context, limit int, actionFilter string) ([]*services.AuditRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT audit_id, actor_user_id, actor_role, action, target_type, target_id,
		       COALESCE(reason_code,''), reason_text, COALESCE(host(ip_address),''), created_at
		FROM audit_log
		WHERE ($1 = '' OR action = $1)
		ORDER BY created_at DESC
		LIMIT $2`, actionFilter, limit)
	if err != nil {
		return nil, fmt.Errorf("ledger/repo: list audit log: %w", err)
	}
	defer rows.Close()

	var out []*services.AuditRecord
	for rows.Next() {
		var (
			rec        services.AuditRecord
			targetID   *uuid.UUID
			occurredAt time.Time
		)
		if err := rows.Scan(&rec.ID, &rec.ActorUserID, &rec.ActorRole, &rec.Action,
			&rec.TargetType, &targetID, &rec.ReasonCode, &rec.ReasonText, &rec.IPAddress, &occurredAt); err != nil {
			return nil, fmt.Errorf("ledger/repo: scan audit record: %w", err)
		}
		rec.TargetID = targetID
		rec.OccurredAt = occurredAt.UTC()
		out = append(out, &rec)
	}
	return out, rows.Err()
}
