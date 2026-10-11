// In-memory notification repository for integration tests (domain.Repository).
// It mirrors the Postgres semantics: keyset pagination over (created_at,id),
// unread counting, and retry eligibility. Not registered in production wiring.
package infrastructure

import (
	"sort"
	"strings"
	"sync"
	"time"

	domain "nidaw-backend/internal/modules/notifications/domain"

	"github.com/google/uuid"
)

// MemoryRepository is a concurrency-safe in-memory domain.Repository.
type MemoryRepository struct {
	mu     sync.Mutex
	rows   map[uuid.UUID]*domain.Notification
	tokens map[string]*domain.DeviceToken
	// Locale/Email fixtures for tests; default "en"/"" respectively.
	LocaleOverrides map[uuid.UUID]string
	EmailOverrides  map[uuid.UUID]string
}

// NewMemoryRepository returns an empty repository with schema ready.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		rows:            make(map[uuid.UUID]*domain.Notification),
		tokens:          make(map[string]*domain.DeviceToken),
		LocaleOverrides: make(map[uuid.UUID]string),
		EmailOverrides:  make(map[uuid.UUID]string),
	}
}

func (m *MemoryRepository) EnsureSchema() error { return nil }

func (m *MemoryRepository) Create(n *domain.Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *n
	m.rows[n.ID] = &cp
	return nil
}

func (m *MemoryRepository) MarkDelivered(id uuid.UUID, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.rows[id]
	if !ok {
		return domain.ErrNotificationNotFound
	}
	t := at
	n.DeliveredAt = &t
	n.Status = domain.StatusDelivered
	n.FailureReason = ""
	return nil
}

func (m *MemoryRepository) MarkAttemptFailed(id uuid.UUID, reason string, attempt int, nextRetryAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.rows[id]
	if !ok {
		return domain.ErrNotificationNotFound
	}
	n.FailureReason = reason
	n.AttemptCount = attempt
	n.NextRetryAt = nextRetryAt
	n.Status = domain.StatusPending
	return nil
}

func (m *MemoryRepository) MarkFinalFailed(id uuid.UUID, reason string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.rows[id]
	if !ok {
		return domain.ErrNotificationNotFound
	}
	t := at
	n.FailedAt = &t
	n.FailureReason = reason
	n.AttemptCount++
	n.Status = domain.StatusFailed
	return nil
}

func (m *MemoryRepository) MarkRead(id, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.rows[id]
	if !ok || n.UserID != userID {
		return domain.ErrNotificationNotFound
	}
	if n.Channel != domain.ChannelInApp {
		return nil // no-op like Postgres path
	}
	if n.ReadAt == nil {
		now := time.Now().UTC()
		n.ReadAt = &now
	}
	return nil
}

func (m *MemoryRepository) ListInbox(userID uuid.UUID, cursor string, limit int) ([]*domain.Notification, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	all := make([]*domain.Notification, 0)
	for _, n := range m.rows {
		if n.UserID == userID && n.Channel == domain.ChannelInApp {
			cp := *n
			all = append(all, &cp)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID.String() > all[j].ID.String()
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})

	start := 0
	if cursor != "" {
		ct, cid, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		for i, n := range all {
			if n.CreatedAt.Before(ct) || (n.CreatedAt.Equal(ct) && strings.Compare(n.ID.String(), cid.String()) < 0) {
				start = i
				break
			}
			start = len(all)
		}
	}
	page := all[start:]
	next := ""
	if len(page) > limit {
		last := page[limit-1]
		next = encodeCursor(last.CreatedAt, last.ID)
		page = page[:limit]
	}
	out := make([]*domain.Notification, len(page))
	copy(out, page)
	return out, next, nil
}

func (m *MemoryRepository) UnreadCount(userID uuid.UUID) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := 0
	for _, n := range m.rows {
		if n.UserID == userID && n.Channel == domain.ChannelInApp && n.ReadAt == nil {
			c++
		}
	}
	return c, nil
}

func (m *MemoryRepository) PendingRetries(now time.Time, limit int) ([]*domain.Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var due []*domain.Notification
	for _, n := range m.rows {
		if n.Status != domain.StatusPending || n.Channel == domain.ChannelInApp {
			continue
		}
		if n.AttemptCount >= 3 {
			continue
		}
		if n.NextRetryAt != nil && n.NextRetryAt.After(now) {
			continue
		}
		cp := *n
		due = append(due, &cp)
	}
	sort.Slice(due, func(i, j int) bool { return due[i].CreatedAt.Before(due[j].CreatedAt) })
	if len(due) > limit {
		due = due[:limit]
	}
	return due, nil
}

func (m *MemoryRepository) UpsertDeviceToken(t *domain.DeviceToken) error {
	if strings.TrimSpace(t.Token) == "" {
		return domain.ErrEmptyToken
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *t
	cp.RevokedAt = nil
	cp.LastSeenAt = time.Now().UTC()
	m.tokens[t.Token] = &cp
	return nil
}

func (m *MemoryRepository) RevokeDeviceToken(token string, userID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tokens[token]; ok && t.UserID == userID {
		now := time.Now().UTC()
		t.RevokedAt = &now
	}
	return nil
}

func (m *MemoryRepository) ActiveTokensForUser(userID uuid.UUID) ([]*domain.DeviceToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*domain.DeviceToken
	for _, t := range m.tokens {
		if t.UserID == userID && t.RevokedAt == nil {
			cp := *t
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	return out, nil
}

func (m *MemoryRepository) UserLocale(userID uuid.UUID) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.LocaleOverrides[userID]; ok && strings.TrimSpace(l) != "" {
		return l, nil
	}
	return "en", nil
}

func (m *MemoryRepository) UserEmail(userID uuid.UUID) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.EmailOverrides[userID], nil
}

// Rows exposes a snapshot for test assertions.
func (m *MemoryRepository) Rows() []*domain.Notification {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*domain.Notification, 0, len(m.rows))
	for _, n := range m.rows {
		cp := *n
		out = append(out, &cp)
	}
	return out
}

var _ domain.Repository = (*MemoryRepository)(nil)

// FindByIDForTest returns a copy of the stored row for test assertions.
func (m *MemoryRepository) FindByIDForTest(id uuid.UUID) (*domain.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n, ok := m.rows[id]
	if !ok {
		return nil, domain.ErrNotificationNotFound
	}
	cp := *n
	return &cp, nil
}
