package infrastructure

import (
	"errors"
	"sync"

	selfiedomain "nidaw-backend/internal/modules/selfie/domain"

	"github.com/google/uuid"
)

// MemoryRepository is the thread-safe in-memory implementation of
// selfiedomain.Repository used by tests and as a dev fallback when Postgres
// is unavailable. Keyed by (ride, driver) so Upsert replaces prior rows per
// the idempotent-PUT semantics documented on the port.
type MemoryRepository struct {
	mu     sync.RWMutex
	byRide map[uuid.UUID]selfiedomain.PreSelfie // one active row per ride
}

// NewMemoryRepository constructs an empty store.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{byRide: make(map[uuid.UUID]selfiedomain.PreSelfie)}
}

// EnsureSchema satisfies the boot convention; the memory store has no schema.
func (m *MemoryRepository) EnsureSchema() error { return nil }

// Upsert stores the selfie keyed by RideID, replacing any prior row for the
// same ride+driver pair. Returns a copy so callers cannot mutate stored state.
func (m *MemoryRepository) Upsert(selfie selfiedomain.PreSelfie) (selfiedomain.PreSelfie, error) {
	if selfie.RideID == uuid.Nil || selfie.DriverID == uuid.Nil {
		return selfiedomain.PreSelfie{}, errors.New("memory repo: ride and driver ids are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Idempotent PUT: reuse the prior row's primary key so re-uploads replace
	// rather than mint a new identity (mirrors the Postgres upsert ON CONFLICT).
	if prev, ok := m.byRide[selfie.RideID]; ok && prev.DriverID == selfie.DriverID {
		selfie.ID = prev.ID
		selfie.CreatedAt = prev.CreatedAt
	}
	stored := selfie
	m.byRide[selfie.RideID] = stored
	return stored, nil
}

// FindByRide returns the most recent selfie for a ride or ErrNotFound.
func (m *MemoryRepository) FindByRide(rideID uuid.UUID) (*selfiedomain.PreSelfie, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.byRide[rideID]; ok {
		got := s
		return &got, nil
	}
	return nil, selfiedomain.ErrNotFound
}

// FindByID scans by primary key (fine at test scale).
func (m *MemoryRepository) FindByID(id uuid.UUID) (*selfiedomain.PreSelfie, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.byRide {
		if s.ID == id {
			got := s
			return &got, nil
		}
	}
	return nil, selfiedomain.ErrNotFound
}

var _ selfiedomain.Repository = (*MemoryRepository)(nil)
