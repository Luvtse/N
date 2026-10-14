-- ============================================================================
-- Migration 007: Cash Payment Option (§3.1)
-- ----------------------------------------------------------------------------
-- Adds the payment-method registry and cash collection-state columns that
-- backend/internal/modules/payments and the ride lifecycle code reference:
--   * payment_methods       — per-user saved methods (wallet / card metadata /
--                             cash). Card rows store ONLY last4/brand/expiry;
--                             a PAN must never be persisted anywhere.
--   * rides.payment_method_type     — snapshot of the chosen method type at
--                             request time ("wallet" | "card" | "cash").
--   * rides.cash_collected_at       — set by the driver-app collection-
--                             confirmation endpoint; NULL + completed status
--                             means pending_cash_collection.
-- All statements are IF NOT EXISTS so re-running is a no-op.
-- ============================================================================

BEGIN;

CREATE TABLE IF NOT EXISTS payment_methods (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type            VARCHAR(20) NOT NULL CHECK (type IN ('wallet', 'card', 'cash')),
    is_default      BOOLEAN NOT NULL DEFAULT FALSE,
    -- Card metadata only. NEVER store a PAN, CVV, or full expiry-bearing token.
    -- Populated exclusively for type='card' from the PCI-compliant SDK's
    -- sanitized response (last4 digits, brand, expiry month/year).
    last4           VARCHAR(4),
    brand           VARCHAR(32),
    exp_month       SMALLINT,
    exp_year        SMALLINT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_payment_methods_user_id ON payment_methods(user_id);

-- Partial unique index: at most one default method per user.
CREATE UNIQUE INDEX IF NOT EXISTS uq_payment_methods_default_per_user
    ON payment_methods(user_id) WHERE is_default = TRUE;

ALTER TABLE rides
    ADD COLUMN IF NOT EXISTS payment_method_type VARCHAR(20),
    ADD COLUMN IF NOT EXISTS cash_collected_at   TIMESTAMPTZ;

COMMENT ON COLUMN rides.payment_method_type IS
    'Payment method type snapshot taken at ride-request time: wallet | card | cash. Drives the §3.1 collect-on-completion flow for cash rides.';

COMMENT ON COLUMN rides.cash_collected_at IS
    'Instant the driver confirmed physical cash collection via POST /driver/rides/{id}/confirm-cash-collection. A completed ride with payment_method_type=cash and NULL cash_collected_at is in pending_cash_collection state.';

COMMIT;
