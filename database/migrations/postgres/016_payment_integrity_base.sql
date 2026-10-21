-- ============================================================================
-- Migration 016 — Payment integrity: base tables (Phase 5, NIDUS-PAYMENT-001)
-- ----------------------------------------------------------------------------
-- Companion to 015_payment_integrity.sql. The legacy payment module
-- (internal/modules/payment) reads and writes a `transactions` table that no
-- earlier migration ever created (verified: migrations 001..014 contain no
-- CREATE TABLE transactions; only ledger_transactions from 004). It also
-- persists reconciliation reports and driver payouts into tables that do not
-- exist. This migration creates the missing base objects so the schema matches
-- the module's own INSERT/SELECT column lists exactly.
--
-- Column list verified against the module's SQL:
--   process_payment.go saveTransaction/findTransactionByIdempotencyKey,
--   refund_payment.go loadTransaction/updateTransaction,
--   reconciliation_service.go getTransactionsInPeriod/saveReport/savePayout.
-- All statements are idempotent (IF NOT EXISTS / guarded ALTER) so applying
-- this after 015 on an existing database is a no-op.
-- ============================================================================

BEGIN;

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ---------------------------------------------------------------------------
-- 1. transactions — authoritative payment transaction ledger for the legacy
--    payment module. Money is DECIMAL(12,2) major units at the boundary; the
--    gateway contract uses int64 santim (see gateway_mapping.go toCents).
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS transactions (
    id                      UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id                 UUID NOT NULL,
    order_id                UUID NOT NULL,
    order_type              VARCHAR(50) NOT NULL,
    amount                  DECIMAL(12,2) NOT NULL CHECK (amount > 0),
    currency                CHAR(3) NOT NULL DEFAULT 'ETB',
    fee                     DECIMAL(12,2) NOT NULL DEFAULT 0,
    net_amount              DECIMAL(12,2) NOT NULL DEFAULT 0,
    refunded_amount         DECIMAL(12,2) NOT NULL DEFAULT 0,
    status                  VARCHAR(50) NOT NULL DEFAULT 'pending',
    payment_method          VARCHAR(50),
    external_transaction_id VARCHAR(255),
    description             TEXT,
    metadata                JSONB NOT NULL DEFAULT '{}'::jsonb,
    refund_reason           TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Over-refund canary at the schema level (defense in depth behind the
-- conditional UPDATE in refund_payment.go): persisted refunds never exceed
-- the original charge. GREATEST(...,0) tolerates the 2-decimal rounding of
-- float64 inputs at the boundary.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'chk_transactions_refunded_within_amount'
          AND conrelid = 'transactions'::regclass
    ) THEN
        ALTER TABLE transactions
            ADD CONSTRAINT chk_transactions_refunded_within_amount
            CHECK (refunded_amount <= GREATEST(amount, 0));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_transactions_user_id ON transactions(user_id);
CREATE INDEX IF NOT EXISTS idx_transactions_order_id ON transactions(order_id);
CREATE INDEX IF NOT EXISTS idx_transactions_status ON transactions(status);
CREATE INDEX IF NOT EXISTS idx_transactions_external_id ON transactions(external_transaction_id);
CREATE INDEX IF NOT EXISTS idx_transactions_created_at ON transactions(created_at);

COMMENT ON TABLE transactions IS
    'Payment transactions of the legacy payment module (charge/refund lifecycle). Idempotency claims enforced by uq_transactions_idempotency_key (migration 015).';

-- Columns added by 015 (kept here as guarded no-ops so 016 alone produces the
-- full physical shape the module expects; 015 remains the canonical source).
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS card_last4 VARCHAR(4);
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS card_brand VARCHAR(32);
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS refunded_at TIMESTAMPTZ;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS payment_intent_id VARCHAR(255);
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS settled_at TIMESTAMPTZ;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS settlement_evidence TEXT;

-- ---------------------------------------------------------------------------
-- 2. reconciliation_reports — audit trail of RunReconciliation executions
--    (reconciliation_service.go saveReport).
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS reconciliation_reports (
    id                     UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    period_start           TIMESTAMPTZ NOT NULL,
    period_end             TIMESTAMPTZ NOT NULL,
    total_transactions     INTEGER NOT NULL DEFAULT 0,
    total_amount           DECIMAL(14,2) NOT NULL DEFAULT 0,
    matched_transactions   INTEGER NOT NULL DEFAULT 0,
    matched_amount         DECIMAL(14,2) NOT NULL DEFAULT 0,
    unmatched_transactions INTEGER NOT NULL DEFAULT 0,
    unmatched_amount       DECIMAL(14,2) NOT NULL DEFAULT 0,
    status                 VARCHAR(32) NOT NULL, -- success | partial | failed
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_reconciliation_reports_period
    ON reconciliation_reports (period_start, period_end);

-- ---------------------------------------------------------------------------
-- 3. payouts — driver payout batches generated per accounting period
--    (reconciliation_service.go savePayout). The UNIQUE constraint below is
--    the DB-level guard against double payout generation when the job is
--    re-run or executed concurrently for the same (driver, period) window.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS payouts (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    driver_id         UUID NOT NULL,
    user_id           UUID,
    amount            DECIMAL(12,2) NOT NULL DEFAULT 0,
    currency          CHAR(3) NOT NULL DEFAULT 'ETB',
    fee               DECIMAL(12,2) NOT NULL DEFAULT 0,
    net_amount        DECIMAL(12,2) NOT NULL DEFAULT 0,
    status            VARCHAR(50) NOT NULL DEFAULT 'pending',
    method            VARCHAR(50),
    external_id       VARCHAR(255),
    account_last4     VARCHAR(4),
    period_start      TIMESTAMPTZ NOT NULL,
    period_end        TIMESTAMPTZ NOT NULL,
    total_earnings    DECIMAL(12,2) NOT NULL DEFAULT 0,
    tips              DECIMAL(12,2) NOT NULL DEFAULT 0,
    bonuses           DECIMAL(12,2) NOT NULL DEFAULT 0,
    platform_fees     DECIMAL(12,2) NOT NULL DEFAULT 0,
    adjustments       DECIMAL(12,2) NOT NULL DEFAULT 0,
    transaction_count INTEGER NOT NULL DEFAULT 0,
    description       TEXT,
    metadata          JSONB NOT NULL DEFAULT '{}'::jsonb,
    failure_reason    TEXT,
    scheduled_at      TIMESTAMPTZ,
    processed_at      TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_payouts_driver_period UNIQUE (driver_id, period_start, period_end)
);

CREATE INDEX IF NOT EXISTS idx_payouts_driver ON payouts(driver_id);
CREATE INDEX IF NOT EXISTS idx_payouts_status ON payouts(status);

COMMENT ON CONSTRAINT uq_payouts_driver_period ON payouts IS
    'At most one payout row per (driver, accounting period). GeneratePayouts replays conflict-safe against this: a rerun for the same period returns the existing payout instead of issuing a duplicate.';

COMMIT;
