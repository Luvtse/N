// Package application implements the payment-method registry service (§3.1):
// CRUD over per-user saved methods with a single-default invariant, backed by
// either Postgres (production) or an in-memory store (tests).
package application

import (
	"context"
	"sort"
	"sync"

	"nidaw-backend/internal/modules/payments/domain"

	"github.com/google/uuid"
)

// ============================================================================
// REPOSITORY PORT
// ============================================================================

// PaymentMethodRepository is the storage port for the registry. All methods
// are user-scoped: implementations must never leak another user's rows.
type PaymentMethodRepository interface {
	ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.PaymentMethod, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.PaymentMethod, error)
	Create(ctx context.Context, m *domain.PaymentMethod) error
	Update(ctx context.Context, m *domain.PaymentMethod) error
	Delete(ctx context.Context, id uuid.UUID) error
	// ClearDefaults demotes every default row of the user; used before
	// promoting a new default so the partial unique index holds.
	ClearDefaults(ctx context.Context, userID uuid.UUID) error
}

// ============================================================================
// IN-MEMORY IMPLEMENTATION (tests / dev fallback)
// ============================================================================

// InMemoryPaymentMethodRepository is a concurrency-safe map-backed store used
// by unit/integration tests. It enforces the same single-default invariant as
// the Postgres implementation.
type InMemoryPaymentMethodRepository struct {
	mu    sync.RWMutex
	byID  map[uuid.UUID]*domain.PaymentMethod
	calls int
}

func NewInMemoryPaymentMethodRepository() *InMemoryPaymentMethodRepository {
	return &InMemoryPaymentMethodRepository{byID: map[uuid.UUID]*domain.PaymentMethod{}}
}

func (r *InMemoryPaymentMethodRepository) ListByUser(_ context.Context, userID uuid.UUID) ([]domain.PaymentMethod, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []domain.PaymentMethod
	for _, m := range r.byID {
		if m.UserID == userID {
			out = append(out, *m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r *InMemoryPaymentMethodRepository) GetByID(_ context.Context, id uuid.UUID) (*domain.PaymentMethod, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	cp := *m
	return &cp, nil
}

func (r *InMemoryPaymentMethodRepository) Create(_ context.Context, m *domain.PaymentMethod) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *m
	r.byID[m.ID] = &cp
	return nil
}

func (r *InMemoryPaymentMethodRepository) Update(_ context.Context, m *domain.PaymentMethod) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[m.ID]; !ok {
		return domain.ErrNotFound
	}
	cp := *m
	r.byID[m.ID] = &cp
	return nil
}

func (r *InMemoryPaymentMethodRepository) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[id]; !ok {
		return domain.ErrNotFound
	}
	delete(r.byID, id)
	return nil
}

func (r *InMemoryPaymentMethodRepository) ClearDefaults(_ context.Context, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.byID {
		if m.UserID == userID && m.IsDefault {
			m.IsDefault = false
		}
	}
	return nil
}
