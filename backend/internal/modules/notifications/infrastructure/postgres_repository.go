// Package infrastructure provides the Postgres-backed notification repository.
// Schema is created idempotently at boot (EnsureSchema), matching the repo's
// existing convention for module-owned tables. The users table already carries
// email; locale lives in an optional profile column that we add defensively so
// localization (§4.3) has a real source instead of a hardcoded default.
package infrastructure

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	domain "nidaw-backend/internal/modules/notifications/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresRepository implements domain.Repository against pgxpool.
type PostgresRepository struct {
	pool *pgxpool.Pool
}

// NewPostgresRepository constructs the repository. pool must not be nil.
func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const schemaDDL = `
CREATE TABLE IF NOT EXISTS notifications (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL,
    channel         TEXT NOT NULL CHECK (channel IN ('in_app','fcm','email')),
    category        TEXT NOT NULL,
    title           TEXT NOT NULL,
    body            TEXT NOT NULL,
    deep_link       TEXT NOT NULL DEFAULT '',
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at         TIMESTAMPTZ,
    delivered_at    TIMESTAMPTZ,
    failed_at       TIMESTAMPTZ,
    failure_reason  TEXT NOT NULL DEFAULT '',
    attempt_count   INT NOT NULL DEFAULT 0,
    next_retry_at   TIMESTAMPTZ,
    status          TEXT NOT NULL DEFAULT 'pending'
);
CREATE INDEX IF NOT EXISTS idx_notifications_inbox
    ON notifications (user_id, channel, created_at DESC, id)
    WHERE channel = 'in_app';
CREATE INDEX IF NOT EXISTS idx_notifications_retry
    ON notifications (next_retry_at)
    WHERE status = 'pending' AND channel <> 'in_app';

CREATE TABLE IF NOT EXISTS notification_device_tokens (
    token        TEXT PRIMARY KEY,
    user_id      UUID NOT NULL,
    platform     TEXT NOT NULL DEFAULT '',
    app_version  TEXT NOT NULL DEFAULT '',
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_device_tokens_user
    ON notification_device_tokens (user_id) WHERE revoked_at IS NULL;

ALTER TABLE users ADD COLUMN IF NOT EXISTS locale TEXT NOT NULL DEFAULT 'en';
`

// EnsureSchema creates module tables and the users.locale column idempotently.
func (r *PostgresRepository) EnsureSchema() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := r.pool.Exec(ctx, schemaDDL)
	return err
}

func (r *PostgresRepository) Create(n *domain.Notification) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO notifications (id, user_id, channel, category, title, body, deep_link,
			payload, created_at, read_at, delivered_at, failed_at, failure_reason,
			attempt_count, next_retry_at, status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9,$10,$11,$12,$13,$14,$15,$16)`,
		n.ID, n.UserID, string(n.Channel), string(n.Category), n.Title, n.Body, n.DeepLink,
		string(n.PayloadJSON()), n.CreatedAt, n.ReadAt, n.DeliveredAt, n.FailedAt,
		n.FailureReason, n.AttemptCount, n.NextRetryAt, n.Status)
	if err != nil {
		return fmt.Errorf("notifications.Create: %w", err)
	}
	return nil
}

func (r *PostgresRepository) MarkDelivered(id uuid.UUID, at time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.pool.Exec(ctx, `
		UPDATE notifications SET status='delivered', delivered_at=$2, failure_reason=''
		WHERE id=$1`, id, at.UTC())
	return err
}

func (r *PostgresRepository) MarkAttemptFailed(id uuid.UUID, reason string, attempt int, nextRetryAt *time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var t *time.Time
	if nextRetryAt != nil {
		utc := nextRetryAt.UTC()
		t = &utc
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE notifications SET status='pending', failure_reason=$2, attempt_count=$3, next_retry_at=$4
		WHERE id=$1`, id, truncate(reason, 500), attempt, t)
	return err
}

func (r *PostgresRepository) MarkFinalFailed(id uuid.UUID, reason string, at time.Time) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := r.pool.Exec(ctx, `
		UPDATE notifications SET status='failed', failed_at=$2, failure_reason=$3, attempt_count=attempt_count+1
		WHERE id=$1`, id, at.UTC(), truncate(reason, 500))
	return err
}

func (r *PostgresRepository) MarkRead(id, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, err := r.pool.Exec(ctx, `
		UPDATE notifications SET read_at=now()
		WHERE id=$1 AND user_id=$2 AND channel='in_app' AND read_at IS NULL`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Distinguish "belongs to someone else / wrong channel" from
		// "already read": both are acceptable no-ops for the client, but a
		// missing row must surface as not-found.
		var exists bool
		err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM notifications WHERE id=$1 AND user_id=$2)`, id, userID).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return domain.ErrNotificationNotFound
		}
	}
	return nil
}

func scanNotification(row pgx.Row) (*domain.Notification, error) {
	var n domain.Notification
	var channel, category string
	var payload []byte
	err := row.Scan(&n.ID, &n.UserID, &channel, &category, &n.Title, &n.Body, &n.DeepLink,
		&payload, &n.CreatedAt, &n.ReadAt, &n.DeliveredAt, &n.FailedAt, &n.FailureReason,
		&n.AttemptCount, &n.NextRetryAt, &n.Status)
	if err != nil {
		return nil, err
	}
	n.Channel = domain.Channel(channel)
	n.Category = domain.Category(category)
	n.ParsePayload(payload)
	return &n, nil
}

const inboxColumns = `id, user_id, channel, category, title, body, deep_link, payload,
	created_at, read_at, delivered_at, failed_at, failure_reason, attempt_count, next_retry_at, status`

// encodeCursor packs created_at (RFC3339Nano UTC) + id into an opaque cursor.
func encodeCursor(t time.Time, id uuid.UUID) string {
	raw := t.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(cursor string) (time.Time, uuid.UUID, error) {
	var zeroT time.Time
	var zeroU uuid.UUID
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return zeroT, zeroU, fmt.Errorf("bad cursor encoding")
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return zeroT, zeroU, fmt.Errorf("bad cursor shape")
	}
	ts, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return zeroT, zeroU, fmt.Errorf("bad cursor timestamp")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return zeroT, zeroU, fmt.Errorf("bad cursor id")
	}
	return ts, id, nil
}

func (r *PostgresRepository) ListInbox(userID uuid.UUID, cursor string, limit int) ([]*domain.Notification, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := []interface{}{userID, limit + 1}
	where := `user_id=$1 AND channel='in_app'`
	if cursor != "" {
		ct, cid, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		where += ` AND (created_at, id) < ($3, $4)`
		args = append(args, ct.UTC(), cid)
	}
	q := fmt.Sprintf(`SELECT %s FROM notifications WHERE %s
		ORDER BY created_at DESC, id DESC LIMIT $2`, inboxColumns, where)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("notifications.ListInbox: %w", err)
	}
	defer rows.Close()

	var items []*domain.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, "", err
		}
		items = append(items, n)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(items) > limit {
		last := items[limit-1]
		next = encodeCursor(last.CreatedAt, last.ID)
		items = items[:limit]
	}
	return items, next, nil
}

func (r *PostgresRepository) UnreadCount(userID uuid.UUID) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var c int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM notifications
		WHERE user_id=$1 AND channel='in_app' AND read_at IS NULL`, userID).Scan(&c)
	return c, err
}

func (r *PostgresRepository) PendingRetries(now time.Time, limit int) ([]*domain.Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT %s FROM notifications
		WHERE status='pending' AND channel <> 'in_app'
		  AND (next_retry_at IS NULL OR next_retry_at <= $1)
		  AND attempt_count < 3
		ORDER BY created_at ASC LIMIT $2`, inboxColumns),
		now.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("notifications.PendingRetries: %w", err)
	}
	defer rows.Close()
	var out []*domain.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UpsertDeviceToken(t *domain.DeviceToken) error {
	if strings.TrimSpace(t.Token) == "" {
		return domain.ErrEmptyToken
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO notification_device_tokens (token, user_id, platform, app_version, last_seen_at, revoked_at)
		VALUES ($1,$2,$3,$4,$5,NULL)
		ON CONFLICT (token) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			platform = EXCLUDED.platform,
			app_version = EXCLUDED.app_version,
			last_seen_at = EXCLUDED.last_seen_at,
			revoked_at = NULL`,
		t.Token, t.UserID, t.Platform, t.AppVersion, now)
	if err != nil {
		return fmt.Errorf("notifications.UpsertDeviceToken: %w", err)
	}
	return nil
}

func (r *PostgresRepository) RevokeDeviceToken(token string, userID uuid.UUID) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Ownership enforced in the WHERE clause: a foreign token matches nothing
	// and is silently a no-op, so callers cannot revoke another user's device.
	_, err := r.pool.Exec(ctx, `
		UPDATE notification_device_tokens SET revoked_at=now() WHERE token=$1 AND user_id=$2 AND revoked_at IS NULL`, token, userID)
	return err
}

func (r *PostgresRepository) ActiveTokensForUser(userID uuid.UUID) ([]*domain.DeviceToken, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := r.pool.Query(ctx, `
		SELECT token, user_id, platform, app_version, last_seen_at
		FROM notification_device_tokens
		WHERE user_id=$1 AND revoked_at IS NULL
		ORDER BY last_seen_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("notifications.ActiveTokensForUser: %w", err)
	}
	defer rows.Close()
	var out []*domain.DeviceToken
	for rows.Next() {
		var t domain.DeviceToken
		if err := rows.Scan(&t.Token, &t.UserID, &t.Platform, &t.AppVersion, &t.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (r *PostgresRepository) UserLocale(userID uuid.UUID) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var locale string
	// users.locale is created by EnsureSchema; default 'en' guarantees a value.
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(locale,'en') FROM users WHERE id=$1`, userID).Scan(&locale)
	if err == pgx.ErrNoRows {
		return "en", nil
	}
	if err != nil {
		return "en", err
	}
	if strings.TrimSpace(locale) == "" {
		return "en", nil
	}
	return locale, nil
}

func (r *PostgresRepository) UserEmail(userID uuid.UUID) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var email string
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(email,'') FROM users WHERE id=$1`, userID).Scan(&email)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return email, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// Compile-time assertion that the Postgres repository satisfies the port.
var _ domain.Repository = (*PostgresRepository)(nil)
