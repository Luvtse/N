package application

import (
	"context"
	"time"

	"nidaw-backend/internal/modules/payments/domain"

	"github.com/google/uuid"
)

// ============================================================================
// SERVICE
// ============================================================================

// PaymentMethodService implements the registry use-cases behind
// GET/POST/DELETE/PATCH /payment-methods. It owns the single-default
// invariant and the PCI-safe validation rules; HTTP handlers stay thin.
type PaymentMethodService struct {
	repo PaymentMethodRepository
}

func NewPaymentMethodService(repo PaymentMethodRepository) *PaymentMethodService {
	return &PaymentMethodService{repo: repo}
}

// List returns the user's methods in creation order. An empty slice is an
// honest "no methods yet" state — the client renders its add-method affordance
// rather than fabricating a default.
func (s *PaymentMethodService) List(ctx context.Context, userID uuid.UUID) ([]domain.PaymentMethod, error) {
	return s.repo.ListByUser(ctx, userID)
}

// CreateInput is the validated-add payload from the HTTP layer. Card fields
// are ignored for non-card types so a client bug can never persist stray card
// metadata on a cash row.
type CreateInput struct {
	Type     domain.MethodType
	Card     *domain.CardMetadata
	MakeDefault bool
}

// Create adds a method for the user. Rules (§3.1):
//   - type must be wallet|card|cash;
//   - card requires sanitized SDK metadata (last4/brand/expiry) — never a PAN;
//   - the first method of a user becomes the default automatically;
//   - MakeDefault demotes the previous default atomically-enough via
//     ClearDefaults + promote (same pattern as SetDefault).
func (s *PaymentMethodService) Create(ctx context.Context, userID uuid.UUID, in CreateInput) (*domain.PaymentMethod, error) {
	if !in.Type.Valid() {
		return nil, domain.ErrInvalidMethodType
	}
	if in.Type == domain.MethodTypeCard {
		if in.Card == nil {
			return nil, domain.ErrCardMetadataAbsent
		}
		if err := in.Card.Validate(); err != nil {
			return nil, err
		}
	} else {
		in.Card = nil // strip card metadata on wallet/cash rows
	}

	existing, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	isDefault := in.MakeDefault || len(existing) == 0

	now := time.Now().UTC()
	m := &domain.PaymentMethod{
		ID:        uuid.New(),
		UserID:    userID,
		Type:      in.Type,
		IsDefault: isDefault,
		Card:      in.Card,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if isDefault {
		if err := s.repo.ClearDefaults(ctx, userID); err != nil {
			return nil, err
		}
	}
	if err := s.repo.Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// Delete removes a method after ownership checks. The current default cannot
// be deleted outright (the rider would be left with no way to pay) — the
// client must promote another method first, matching the UI flow.
func (s *PaymentMethodService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if m.UserID != userID {
		return domain.ErrForbidden
	}
	if m.IsDefault {
		others, err := s.repo.ListByUser(ctx, userID)
		if err != nil {
			return err
		}
		hasAlternative := false
		for _, o := range others {
			if o.ID != id {
				hasAlternative = true
				break
			}
		}
		if hasAlternative {
			return domain.ErrCannotDeleteDefault
		}
		// Last method standing: deleting it is allowed and leaves the user
		// with the honest empty state.
	}
	return s.repo.Delete(ctx, id)
}

// SetDefault promotes id-to-default for userID, demoting the previous one.
func (s *PaymentMethodService) SetDefault(ctx context.Context, userID, id uuid.UUID) (*domain.PaymentMethod, error) {
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if m.UserID != userID {
		return nil, domain.ErrForbidden
	}
	if m.IsDefault {
		return m, nil // idempotent
	}
	if err := s.repo.ClearDefaults(ctx, userID); err != nil {
		return nil, err
	}
	m.IsDefault = true
	m.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

// ResolveForRide maps the payment_method_id a rider sent on POST /rides into
// the concrete method, enforcing ownership. Returns ErrNotFound/ErrForbidden
// for unknown or foreign ids — the ride handler then fails the request with
// INVALID_PAYMENT rather than silently defaulting to cash.
func (s *PaymentMethodService) ResolveForRide(ctx context.Context, userID uuid.UUID, methodID string) (*domain.PaymentMethod, error) {
	if methodID == "" {
		// No explicit choice: honour the saved default when present.
		list, err := s.repo.ListByUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		for i := range list {
			if list[i].IsDefault {
				return &list[i], nil
			}
		}
		return nil, domain.ErrNoPaymentMethods
	}
	id, err := uuid.Parse(methodID)
	if err != nil {
		return nil, domain.ErrNotFound
	}
	m, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if m.UserID != userID {
		return nil, domain.ErrForbidden
	}
	return m, nil
}
