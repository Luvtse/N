-- ============================================================================
-- Migration 009: Promo Codes & Coupons (§3.2)
-- ----------------------------------------------------------------------------
-- Creates the tables that backend/internal/modules/promotions references:
--   * promo_codes        — the code catalog (percent | fixed discounts with
--                         caps, minimums, validity windows, usage limits and
--                         optional ride-type applicability).
--   * promo_redemptions  — one row per applied discount, joined to rides so
--                         receipt-time application is auditable and per-user /
--                         global usage limits can be enforced transactionally.
--   * rides.promo_code / rides.promo_discount — the snapshot columns the ride
--                         DTO carries so receipts show the discount line item
--                         without joining redemptions.
-- All statements are IF NOT EXISTS so re-running is a no-op.
-- ============================================================================

BEGIN;

CREATE TABLE IF NOT EXISTS promo_codes (
    id                  UUID PRIMARY KEY,
    code                VARCHAR(64) NOT NULL,
    type                VARCHAR(10) NOT NULL CHECK (type IN ('percent', 'fixed')),
    value               NUMERIC(12,2) NOT NULL CHECK (value > 0),
    max_discount        NUMERIC(12,2),          -- cap for percent codes; NULL = uncapped
    min_fare            NUMERIC(12,2) NOT NULL DEFAULT 0,
    valid_from          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_to            TIMESTAMPTZ,            -- NULL = open-ended
    usage_limit         INT,                    -- global cap; NULL = unlimited
    per_user_limit      INT,                    -- per-user cap; NULL = unlimited
    applicable_ride_types TEXT[],               -- NULL/empty = all ride types
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Codes are matched case-insensitively everywhere (redeem, list, grant), so
-- uniqueness is defined on the lowercased form.
CREATE UNIQUE INDEX IF NOT EXISTS uq_promo_codes_code ON promo_codes (LOWER(code));

CREATE TABLE IF NOT EXISTS promo_redemptions (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code            VARCHAR(64) NOT NULL,
    ride_id         UUID REFERENCES rides(id) ON DELETE SET NULL,
    discount_applied NUMERIC(12,2) NOT NULL,
    currency        VARCHAR(8) NOT NULL,
    redeemed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_promo_redemptions_user_code
    ON promo_redemptions(user_id, LOWER(code));
CREATE UNIQUE INDEX IF NOT EXISTS uq_promo_redemptions_ride
    ON promo_redemptions(ride_id) WHERE ride_id IS NOT NULL;

ALTER TABLE rides
    ADD COLUMN IF NOT EXISTS promo_code     VARCHAR(64),
    ADD COLUMN IF NOT EXISTS promo_discount NUMERIC(12,2) NOT NULL DEFAULT 0;

COMMENT ON COLUMN rides.promo_code IS
    'Promo code applied at request time (§3.2). NULL = no discount.';
COMMENT ON COLUMN rides.promo_discount IS
    'Discount amount in ride currency computed server-side at request time and persisted at completion; the receipt renders it as a line item. Never trusted from client input.';

COMMIT;
