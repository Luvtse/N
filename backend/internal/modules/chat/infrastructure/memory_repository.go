// Package infrastructure holds the chat module's persistence adapters: an
// in-memory repository for tests and a Postgres repository for production.
package infrastructure

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"nidaw-backend/internal/modules/chat/domain"

	"github.com/google/uuid"
)

// MemoryRepository is a thread-safe in-memory MessageRepository used by unit
// and integration tests, and as the graceful-degradation store when Postgres
// is unavailable in dev environments.
type MemoryRepository struct {
	mu       sync.RWMutex
	messages map[uuid.UUID]*domain.Message // by message id
	byRide   map[uuid.UUID][]uuid.UUID     // ride id -> message ids (insertion order)
}

// NewMemoryRepository returns an empty in-memory chat store.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		messages: make(map[uuid.UUID]*domain.Message),
		byRide:   make(map[uuid.UUID][]uuid.UUID),
	}
}

// Save persists a new message. It never overwrites an existing id.
func (r *MemoryRepository) Save(msg *domain.Message) error {
	if msg == nil || msg.ID == uuid.Nil {
		return fmt.Errorf("chat: cannot save message with nil id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.messages[msg.ID]; exists {
		return fmt.Errorf("chat: message %s already exists", msg.ID)
	}
	stored := *msg
	r.messages[msg.ID] = &stored
	r.byRide[msg.RideID] = append(r.byRide[msg.RideID], msg.ID)
	return nil
}

// Get fetches one message by id.
func (r *MemoryRepository) Get(id uuid.UUID) (*domain.Message, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.messages[id]
	if !ok {
		return nil, domain.ErrMessageNotFound
	}
	copied := *m
	return &copied, nil
}

// History returns a ride's messages oldest-first with keyset pagination on
// "RFC3339Nano:id" cursors. Mirrors the Postgres implementation semantics.
func (r *MemoryRepository) History(rideID uuid.UUID, cursor string, limit int) ([]*domain.Message, string, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make([]*domain.Message, 0, len(r.byRide[rideID]))
	for _, id := range r.byRide[rideID] {
		m := r.messages[id]
		copied := *m
		all = append(all, &copied)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return strings.Compare(all[i].ID.String(), all[j].ID.String()) < 0
		}
		return all[i].CreatedAt.Before(all[j].CreatedAt)
	})

	start := 0
	if cursor != "" {
		parsed, err := parseCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		curTime, curID := parsed.created, parsed.id
		for i, m := range all {
			if m.CreatedAt.After(curTime) ||
				(m.CreatedAt.Equal(curTime) && m.ID.String() > curID.String()) {
				start = i
				break
			}
			start = i + 1
		}
	}

	page := all[start:]
	next := ""
	if len(page) > limit {
		page = page[:limit]
		last := page[len(page)-1]
		next = base64Cursor(last.CreatedAt, last.ID)
	}
	return page, next, nil
}

// UnreadCount counts messages in the ride not sent by userID and not yet read.
func (r *MemoryRepository) UnreadCount(rideID, userID uuid.UUID) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, id := range r.byRide[rideID] {
		m := r.messages[id]
		if m.SenderID != userID && m.ReadAt == nil {
			count++
		}
	}
	return count, nil
}

// MarkAllRead marks unread messages from the other participant as read.
func (r *MemoryRepository) MarkAllRead(rideID, readerID uuid.UUID, at time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	updated := 0
	stamp := at
	for _, id := range r.byRide[rideID] {
		m := r.messages[id]
		if m.SenderID != readerID && m.ReadAt == nil {
			read := stamp
			m.ReadAt = &read
			updated++
		}
	}
	return updated, nil
}
