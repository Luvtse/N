package infrastructure

import (
	"fmt"
	"sync"
	"time"

	telephonydomain "nidaw-backend/internal/modules/telephony/domain"

	"github.com/google/uuid"
)

// MemoryRepository is a thread-safe in-memory telephonydomain.Repository used
// by tests and as the graceful-degradation store when Postgres is unavailable
// in dev environments.
type MemoryRepository struct {
	mu    sync.RWMutex
	byRide map[uuid.UUID]*telephonydomain.ProxySession
}

// NewMemoryRepository returns an empty in-memory proxy-session store.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{byRide: make(map[uuid.UUID]*telephonydomain.ProxySession)}
}

// Save upserts the session keyed by ride id — one session per ride at a time;
// re-provisioning after teardown replaces the terminal row deliberately.
func (r *MemoryRepository) Save(session *telephonydomain.ProxySession) error {
	if session == nil || session.RideID == uuid.Nil {
		return fmt.Errorf("telephony: cannot save session without ride id")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := *session
	r.byRide[session.RideID] = &stored
	return nil
}

// GetByRide returns the most recent session for a ride, active or terminal.
func (r *MemoryRepository) GetByRide(rideID uuid.UUID) (*telephonydomain.ProxySession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	session, ok := r.byRide[rideID]
	if !ok {
		return nil, telephonydomain.ErrSessionNotFound
	}
	out := *session
	return &out, nil
}

// ListExpired returns active sessions whose ExpiresAt is at or before now.
func (r *MemoryRepository) ListExpired(now time.Time, limit int) ([]*telephonydomain.ProxySession, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*telephonydomain.ProxySession
	for _, session := range r.byRide {
		if session.Status == telephonydomain.SessionActive && !now.Before(session.ExpiresAt) {
			out = append(out, session)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
