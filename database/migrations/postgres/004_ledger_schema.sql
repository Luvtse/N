-- ============================================================================
-- Phase D / Step 1: Private Ledger System — Database Schema
-- ----------------------------------------------------------------------------
-- Implements the internal (private, permissioned) ledger tables:
--   * ledger_transactions  — immutable hash-chained transaction log (INSERT only)
--   * user_balances        — cached balance state per user
--   * topup_requests       — fiat on-ramp tracking (Telebirr / Chapa / M-Pesa)
--   * withdrawal_requests  — payout tracking
--   * escrow_holds         — 3-day (72h) ride payment holds for drivers
--   * ride_disputes        — dispute records against escrowed rides
--   * fraud_flags          — risk scores and holds
--   * audit_log            — admin actions (append-only)
--
-- Design notes:
--   * Amounts are stored in ETB cents (cents BIGINT). 1 ETB = 100 cents.
--     This avoids float rounding errors entirely; Money is a value object
--     in Go that wraps int64 cents.
--   * ledger_transactions is IMMUTABLE: a trigger blocks UPDATE/DELETE.
--   * Hash chain per user: tx_hash = SHA256(tx_id || prev_hash || user_id ||
--     amount_cents || balance_after_cents || timestamp_unix_nano), hex-encoded.
--     Genesis transactions use prev_hash = '0'.
-- ============================================================================

BEGIN;

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================================
-- LEDGER TRANSACTIONS (immutable, hash-chained per user)
-- ============================================================================

CREATE TABLE ledger_transactions (
    tx_id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Signed amount in ETB cents. Positive = credit, negative = debit.
    amount_cents       BIGINT NOT NULL,
    -- User's total available+held balance snapshot after this tx (ETB cents).
    balance_after_cents BIGINT NOT NULL,
    -- Previous tx hash in this user's chain ('0' for genesis).
    prev_hash          VARCHAR(64) NOT NULL DEFAULT '0',
    -- SHA-256 hex of (tx_id||prev_hash||user_id||amount||balance_after||timestamp)
    tx_hash            VARCHAR(64) NOT NULL,
    type               VARCHAR(40) NOT NULL CHECK (type IN (
        'topup',                 -- fiat on-ramp credit
        'ride_debit',            -- rider charged for completed ride
        'ride_credit_held',      -- driver credited into held (escrow)
        'escrow_release',        -- held -> available migration (driver leg)
        'refund',                -- back to rider (dispute/no-show)
        'withdrawal_debit',      -- payout initiated
        'withdrawal_reversal',   -- payout failed, funds returned
        'adjustment_credit',     -- audited admin credit
        'adjustment_debit',      -- audited admin debit / negative adjustment
        'chargeback'             -- provider chargeback claw-back
    )),
    currency           CHAR(3) NOT NULL DEFAULT 'ETB',
    -- Optional linkage to external entities (ride id, topup id, dispute id...)
    reference_id       UUID,
    reference_type     VARCHAR(40),
    idempotency_key    VARCHAR(255),
    description        TEXT,
    metadata           JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One row per user ordered by chain position: enforce uniqueness of
-- (user_id, prev_hash) so no two txs can claim the same predecessor.
CREATE UNIQUE INDEX uq_ledger_user_prev_hash ON ledger_transactions(user_id, prev_hash);
CREATE INDEX idx_ledger_user_created ON ledger_transactions(user_id, created_at DESC);
CREATE INDEX idx_ledger_reference ON ledger_transactions(reference_id, reference_type);
CREATE INDEX idx_ledger_type ON ledger_transactions(type);

-- Idempotency: at most one tx per key per user.
CREATE UNIQUE INDEX uq_ledger_idempotency ON ledger_transactions(user_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- Immutability guard: block any UPDATE or DELETE on the transaction log.
CREATE OR REPLACE FUNCTION ledger_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'ledger_transactions is append-only: % is forbidden', TG_OP;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_ledger_immutable
    BEFORE UPDATE OR DELETE ON ledger_transactions
    FOR EACH ROW EXECUTE FUNCTION ledger_block_mutation();

-- ============================================================================
-- USER BALANCES (cached state, single source of derived truth)
-- ============================================================================

CREATE TABLE user_balances (
    user_id              UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    available_cents      BIGINT NOT NULL DEFAULT 0 CHECK (available_cents >= 0
                          OR status = 'negative_lock'),  -- negative only under lock
    pending_cents        BIGINT NOT NULL DEFAULT 0 CHECK (pending_cents >= 0),
    held_cents           BIGINT NOT NULL DEFAULT 0 CHECK (held_cents >= 0),
    withdrawable_cents   BIGINT NOT NULL DEFAULT 0 CHECK (withdrawable_cents >= 0),
    lifetime_credited    BIGINT NOT NULL DEFAULT 0 CHECK (lifetime_credited >= 0),
    lifetime_debited     BIGINT NOT NULL DEFAULT 0 CHECK (lifetime_debited >= 0),
    -- Head of this user's hash chain ('0' before first tx). Cached for
    -- O(1) chain appends; also mirrored in Redis by the hash-chain service.
    latest_tx_hash       VARCHAR(64) NOT NULL DEFAULT '0',
    latest_tx_id         UUID,
    currency             CHAR(3) NOT NULL DEFAULT 'ETB',
    -- account | negative_lock | frozen_review | closed
    status               VARCHAR(30) NOT NULL DEFAULT 'account',
    version              BIGINT NOT NULL DEFAULT 0,  -- optimistic concurrency counter
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_balances_status ON user_balances(status);

-- ============================================================================
-- TOPUP REQUESTS (fiat on-ramp: Telebirr / Chapa / M-Pesa Ethiopia)
-- ============================================================================

CREATE TABLE topup_requests (
    topup_id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id              UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount_cents         BIGINT NOT NULL CHECK (amount_cents > 0),
    currency             CHAR(3) NOT NULL DEFAULT 'ETB' CHECK (currency = 'ETB'),
    provider             VARCHAR(20) NOT NULL CHECK (provider IN ('telebirr', 'chapa', 'mpesa')),
    provider_reference   VARCHAR(255),
    -- pending | processing | completed | failed | reversed
    status               VARCHAR(20) NOT NULL DEFAULT 'pending',
    -- the optimistic credit recorded when the request was created
    credit_tx_id         UUID REFERENCES ledger_transactions(tx_id),
    -- the compensating negative adjustment if the provider payment failed
    reversal_tx_id       UUID REFERENCES ledger_transactions(tx_id),
    failure_reason       TEXT,
    requested_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at         TIMESTAMPTZ,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata             JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX idx_topup_user ON topup_requests(user_id, requested_at DESC);
CREATE INDEX idx_topup_status ON topup_requests(status);
CREATE UNIQUE INDEX uq_topup_provider_ref ON topup_requests(provider, provider_reference)
    WHERE provider_reference IS NOT NULL;

-- ============================================================================
-- WITHDRAWAL REQUESTS (payouts: bank transfer / M-Pesa / Telebirr merchant)
-- ============================================================================

CREATE TABLE withdrawal_requests (
    withdrawal_id        UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id              UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount_cents         BIGINT NOT NULL CHECK (amount_cents > 0),
    fee_cents            BIGINT NOT NULL DEFAULT 0 CHECK (fee_cents >= 0),
    currency             CHAR(3) NOT NULL DEFAULT 'ETB' CHECK (currency = 'ETB'),
    destination_type     VARCHAR(30) NOT NULL CHECK (destination_type IN
                          ('bank_transfer', 'mpesa', 'telebirr_merchant')),
    destination_details  JSONB NOT NULL DEFAULT '{}'::jsonb,  -- encrypted at rest in prod
    -- pending | fraud_hold | approved | processing | completed | failed | reversed
    status               VARCHAR(20) NOT NULL DEFAULT 'pending',
    debit_tx_id          UUID REFERENCES ledger_transactions(tx_id),
    reversal_tx_id       UUID REFERENCES ledger_transactions(tx_id),
    provider_reference   VARCHAR(255),
    risk_score           NUMERIC(5,4),
    reviewed_by          UUID REFERENCES users(id),  -- admin sign-off (Phase H)
    reviewed_at          TIMESTAMPTZ,
    failure_reason       TEXT,
    requested_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at         TIMESTAMPTZ,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_withdrawal_user ON withdrawal_requests(user_id, requested_at DESC);
CREATE INDEX idx_withdrawal_status ON withdrawal_requests(status);

-- ============================================================================
-- ESCROW HOLDS (3-day ride payment safety window)
-- ============================================================================

CREATE TABLE escrow_holds (
    hold_id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    ride_id              UUID NOT NULL,
    rider_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    driver_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount_cents         BIGINT NOT NULL CHECK (amount_cents > 0),
    currency             CHAR(3) NOT NULL DEFAULT 'ETB',
    -- the ride_credit_held tx that funded this hold
    credit_tx_id         UUID REFERENCES ledger_transactions(tx_id),
    -- set when released (escrow_release tx) or refunded (refund tx)
    release_tx_id        UUID REFERENCES ledger_transactions(tx_id),
    dispute_id           UUID,  -- FK added below after ride_disputes exists
    -- held | disputed | released | refunded
    status               VARCHAR(20) NOT NULL DEFAULT 'held',
    release_after        TIMESTAMPTZ NOT NULL,  -- created + 72h, paused by disputes
    released_at          TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX uq_escrow_ride ON escrow_holds(ride_id);
CREATE INDEX idx_escrow_driver_status ON escrow_holds(driver_id, status);
-- Hot path for the hourly release job (Phase F):
CREATE INDEX idx_escrow_release_scan ON escrow_holds(release_after)
    WHERE status = 'held' AND dispute_id IS NULL;

-- ============================================================================
-- RIDE DISPUTES
-- ============================================================================

CREATE TABLE ride_disputes (
    dispute_id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    ride_id              UUID NOT NULL,
    hold_id              UUID NOT NULL REFERENCES escrow_holds(hold_id),
    filed_by_user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    against_user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason_code          VARCHAR(40) NOT NULL CHECK (reason_code IN
                          ('no_show', 'overcharge', 'route_deviation', 'vehicle_issue',
                           'unsafe_driving', 'lost_item', 'fare_split', 'other')),
    description          TEXT NOT NULL,
    evidence_urls        TEXT[] NOT NULL DEFAULT '{}',
    -- open | auto_resolved | admin_review | resolved_rider | resolved_driver | rejected
    status               VARCHAR(30) NOT NULL DEFAULT 'open',
    resolution_notes     TEXT,
    refund_tx_id         UUID REFERENCES ledger_transactions(tx_id),
    release_tx_id        UUID REFERENCES ledger_transactions(tx_id),
    assigned_admin_id    UUID REFERENCES users(id),
    resolved_at          TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE escrow_holds
    ADD CONSTRAINT fk_escrow_dispute FOREIGN KEY (dispute_id)
    REFERENCES ride_disputes(dispute_id);

CREATE INDEX idx_dispute_status ON ride_disputes(status);
CREATE INDEX idx_dispute_ride ON ride_disputes(ride_id);
CREATE UNIQUE INDEX uq_dispute_active_per_ride ON ride_disputes(ride_id)
    WHERE status IN ('open', 'admin_review');

-- ============================================================================
-- FRAUD FLAGS (risk scores and holds)
-- ============================================================================

CREATE TABLE fraud_flags (
    flag_id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id              UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- velocity | device_fingerprint | ip_reputation | ml_model | manual
    check_type           VARCHAR(30) NOT NULL,
    -- low | medium | high | critical
    severity             VARCHAR(10) NOT NULL,
    risk_score           NUMERIC(5,4) NOT NULL DEFAULT 0,  -- 0.0000 .. 1.0000
    entity_type          VARCHAR(30),   -- withdrawal_request | topup_request | user
    entity_id            UUID,
    details              JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- open | acknowledged | dismissed | actioned
    status               VARCHAR(20) NOT NULL DEFAULT 'open',
    reviewed_by          UUID REFERENCES users(id),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_fraud_user ON fraud_flags(user_id, created_at DESC);
CREATE INDEX idx_fraud_open ON fraud_flags(status) WHERE status = 'open';
CREATE INDEX idx_fraud_velocity ON fraud_flags(user_id, check_type, created_at DESC);

-- ============================================================================
-- AUDIT LOG (admin actions, append-only)
-- ============================================================================

CREATE TABLE audit_log (
    audit_id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    actor_user_id        UUID NOT NULL REFERENCES users(id),
    actor_role           VARCHAR(50) NOT NULL,
    action               VARCHAR(60) NOT NULL,  -- force_adjustment | approve_withdrawal | resolve_dispute | ...
    target_type          VARCHAR(40) NOT NULL,  -- user | withdrawal_request | ride_dispute | ...
    target_id            UUID,
    reason_code          VARCHAR(60),
    reason_text          TEXT NOT NULL,
    before_state         JSONB,
    after_state          JSONB,
    ip_address           INET,
    user_agent           TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_actor ON audit_log(actor_user_id, created_at DESC);
CREATE INDEX idx_audit_target ON audit_log(target_type, target_id);

CREATE OR REPLACE FUNCTION audit_block_mutation() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only: % is forbidden', TG_OP;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_audit_immutable
    BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_block_mutation();

COMMIT;
