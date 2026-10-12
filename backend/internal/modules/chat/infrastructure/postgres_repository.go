package infrastructure

import (
	"context"
	"errors"
	"fmt"
	"time"

	"nidaw-backend/internal/modules/chat/domain"
	"nidaw-backend/internal/shared/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PostgresRepository is the production MessageRepository backed by the shared
// Postgres pool. Table: chat_messages, created idempotently at boot.
type PostgresRepository struct {
	db *database.Postgres
}

// NewPostgresRepository wires the chat store to the shared DB handle.
func NewPostgresRepository(db *database.Postgres) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// EnsureSchema creates the chat_messages table and indexes if absent.
func (r *PostgresRepository) EnsureSchema(ctx context.Context) error {
	ddl := `
CREATE TABLE IF NOT EXISTS chat_messages (
id              UUID PRIMARY KEY,
ride_id         UUID NOT NULL,
sender_id       UUID NOT NULL,
sender_role     TEXT NOT NULL CHECK (sender_role IN ('rider','driver')),
body            TEXT NOT NULL,
canned_id       TEXT NOT NULL DEFAULT '',
translated_from TEXT NOT NULL DEFAULT '',
created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
delivered_at    TIMESTAMPTZ,
read_at         TIMESTAMPTZ,
status          TEXT NOT NULL DEFAULT 'pending'
);
CREATE INDEX IF NOT EXISTS idx_chat_messages_ride_created
ON chat_messages (ride_id, created_at, id);
CREATE INDEX IF NOT EXISTS idx_chat_messages_unread
ON chat_messages (ride_id, sender_id) WHERE read_at IS NULL;`
	_, err := r.db.Exec(ctx, ddl)
	return err
}

const chatCols = `id, ride_id, sender_id, sender_role, body, canned_id, translated_from, created_at, delivered_at, read_at, status`

func scanChatMessage(row pgx.Row) (*domain.Message, error) {
	m := &domain.Message{}
	var role string
	var deliveredAt, readAt *time.Time
	err := row.Scan(&m.ID, &m.RideID, &m.SenderID, &role, &m.Body, &m.CannedID,
		&m.TranslatedFrom, &m.CreatedAt, &deliveredAt, &readAt, &m.Status)
	if err != nil {
		return nil, err
	}
	m.SenderRole = domain.SenderRole(role)
	m.DeliveredAt = deliveredAt
	m.ReadAt = readAt
	return m, nil
}

// Save inserts a message row.
func (r *PostgresRepository) Save(msg *domain.Message) error {
	ctx := context.Background()
	_, err := r.db.Exec(ctx, `
INSERT INTO chat_messages
(id, ride_id, sender_id, sender_role, body, canned_id, translated_from, created_at, delivered_at, read_at, status)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		msg.ID, msg.RideID, msg.SenderID, string(msg.SenderRole), msg.Body,
		msg.CannedID, msg.TranslatedFrom, msg.CreatedAt, msg.DeliveredAt, msg.ReadAt, msg.Status)
	if err != nil {
		return fmt.Errorf("chat: save message: %w", err)
	}
	return nil
}

// Get fetches one message by id.
func (r *PostgresRepository) Get(id uuid.UUID) (*domain.Message, error) {
	ctx := context.Background()
	row := r.db.QueryRow(ctx, `SELECT `+chatCols+` FROM chat_messages WHERE id = $1`, id)
	m, err := scanChatMessage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMessageNotFound
	}
	return m, err
}

// History returns oldest-first pages keyed on (created_at,id). Cursor format:
// "RFC3339Nano:id" — same contract as the in-memory implementation.
func (r *PostgresRepository) History(rideID uuid.UUID, cursor string, limit int) ([]*domain.Message, string, error) {
	ctx := context.Background()
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	args := []interface{}{rideID}
	where := "ride_id = $1"
	if cursor != "" {
		parsed, err := parseCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		curTime, curID := parsed.created, parsed.id
		where += " AND (created_at, id) > ($2, $3)"
		args = append(args, curTime, curID)
	}
	args = append(args, limit+1) // fetch one extra to detect next page

	rows, err := r.db.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM chat_messages WHERE %s ORDER BY created_at ASC, id ASC LIMIT $%d`,
			chatCols, where, len(args)), args...)
	if err != nil {
		return nil, "", fmt.Errorf("chat: history: %w", err)
	}
	defer rows.Close()

	var items []*domain.Message
	for rows.Next() {
		m, err := scanChatMessage(rows)
		if err != nil {
			return nil, "", err
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		next = base64Cursor(last.CreatedAt, last.ID)
	}
	if items == nil {
		items = []*domain.Message{}
	}
	return items, next, nil
}

// UnreadCount counts messages from the other participant that this user has
// not read.
func (r *PostgresRepository) UnreadCount(rideID, userID uuid.UUID) (int, error) {
	ctx := context.Background()
	row := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM chat_messages WHERE ride_id = $1 AND sender_id <> $2 AND read_at IS NULL`,
		rideID, userID)
	var count int
	err := row.Scan(&count)
	return count, err
}

// MarkAllRead stamps read_at for the other participant's unread messages.
func (r *PostgresRepository) MarkAllRead(rideID, readerID uuid.UUID, at time.Time) (int, error) {
	ctx := context.Background()
	tag, err := r.db.Exec(ctx,
		`UPDATE chat_messages SET read_at = $3
 WHERE ride_id = $1 AND sender_id <> $2 AND read_at IS NULL`,
		rideID, readerID, at)
	if err != nil {
		return 0, fmt.Errorf("chat: mark all read: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
