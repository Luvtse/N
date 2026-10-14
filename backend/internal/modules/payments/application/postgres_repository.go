package application

import (
	"context"
	"errors"

	"nidaw-backend/internal/modules/payments/domain"
	"nidaw-backend/internal/shared/database"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ============================================================================
// POSTGRES IMPLEMENTATION
// ============================================================================

// PostgresPaymentMethodRepository implements PaymentMethodRepository against
// the payment_methods table created by migration 007_cash_payments.sql.
type PostgresPaymentMethodRepository struct {
	db *database.Postgres
}

func NewPostgresPaymentMethodRepository(db *database.Postgres) *PostgresPaymentMethodRepository {
	return &PostgresPaymentMethodRepository{db: db}
}

const pmColumns = `id, user_id, type, is_default, last4, brand, exp_month, exp_year, created_at, updated_at`

// scanRow materialises a method from nullable card columns. Card metadata is
// only attached for type='card' rows that actually carry it; a corrupt row
// (card type, NULL metadata) surfaces as Card=nil and DisplayLabel()'s honest
// "details unavailable" fallback — never a fabricated number.
func scanRow(scan func(dest ...interface{}) error) (*domain.PaymentMethod, error) {
	var m domain.PaymentMethod
	var last4, brand *string
	var expMonth, expYear *int
	if err := scan(&m.ID, &m.UserID, &m.Type, &m.IsDefault, &last4, &brand, &expMonth, &expYear, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	if m.Type == domain.MethodTypeCard && last4 != nil && brand != nil && expMonth != nil && expYear != nil {
		m.Card = &domain.CardMetadata{Last4: *last4, Brand: *brand, ExpMonth: *expMonth, ExpYear: *expYear}
	}
	return &m, nil
}

func (r *PostgresPaymentMethodRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.PaymentMethod, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+pmColumns+` FROM payment_methods WHERE user_id = $1 ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.PaymentMethod
	for rows.Next() {
		m, err := scanRow(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

func (r *PostgresPaymentMethodRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.PaymentMethod, error) {
	row := r.db.QueryRow(ctx,
		`SELECT `+pmColumns+` FROM payment_methods WHERE id = $1`, id)
	m, err := scanRow(row.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	return m, err
}

func (r *PostgresPaymentMethodRepository) Create(ctx context.Context, m *domain.PaymentMethod) error {
	var last4, brand *string
	var expMonth, expYear *int
	if m.Card != nil {
		last4, brand = &m.Card.Last4, &m.Card.Brand
		expMonth, expYear = &m.Card.ExpMonth, &m.Card.ExpYear
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO payment_methods (id, user_id, type, is_default, last4, brand, exp_month, exp_year, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		m.ID, m.UserID, string(m.Type), m.IsDefault, last4, brand, expMonth, expYear, m.CreatedAt, m.UpdatedAt)
	return err
}

func (r *PostgresPaymentMethodRepository) Update(ctx context.Context, m *domain.PaymentMethod) error {
	var last4, brand *string
	var expMonth, expYear *int
	if m.Card != nil {
		last4, brand = &m.Card.Last4, &m.Card.Brand
		expMonth, expYear = &m.Card.ExpMonth, &m.Card.ExpYear
	}
	tag, err := r.db.Exec(ctx, `
		UPDATE payment_methods
		   SET type = $2, is_default = $3, last4 = $4, brand = $5, exp_month = $6, exp_year = $7, updated_at = $8
		 WHERE id = $1`,
		m.ID, string(m.Type), m.IsDefault, last4, brand, expMonth, expYear, m.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *PostgresPaymentMethodRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, `DELETE FROM payment_methods WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// ClearDefaults demotes all of the user's defaults inside one statement; the
// partial unique index uq_payment_methods_default_per_user keeps at most one
// default row even under concurrent promotions.
func (r *PostgresPaymentMethodRepository) ClearDefaults(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx,
		`UPDATE payment_methods SET is_default = FALSE, updated_at = NOW() WHERE user_id = $1 AND is_default = TRUE`,
		userID)
	return err
}
