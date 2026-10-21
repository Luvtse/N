-- ============================================================================
-- Migration 015 — Payment integrity (Phase 5: NIDUS-PAYMENT-001)
-- PREREQUISITE: 016_payment_integrity_base.sql creates the base `transactions`
-- table (no earlier migration ever did; verified against 001..014). The
-- ALTER/CREATE-INDEX statements below assume that table exists. Both files are
-- idempotent, so applying 015 after 016 is safe in either order on fresh and
-- existing databases.
-- ----------------------------------------------------------------------------
-- Closes the verified defects in internal/modules/payment:
--   D1a process_payment.go TOCTOU idempotency: check-then-insert with no
--       database constraint allowed two concurrent requests with the same
--       IdempotencyKey to both reach the gateway. This migration adds a
--       UNIQUE partial index on metadata->>'idempotency_key' so the claim is
--       enforced by the database; the handler now INSERTs first and treats a
--       unique violation as "someone else already claimed this key".
--   D1b provider-timeout ambiguity: transactions stuck in 'processing' whose
--       gateway call timed out must be RECONCILED, never reported failed or
--       succeeded without authoritative evidence. New columns persist the
--       provider reference + an audit trail of terminal transitions.
--   D2  refund over-issue: refunds were applied via load-modify-write with no
--       row lock and no per-refund record. A refunds ledger table plus a
--       conditional UPDATE (refunded_amount + $x <= amount checked in SQL)
--       makes concurrent refunds safe and auditable.
--   Legacy compatibility: the module queries reference card_last4/card_brand
--       which are absent from the initial schema; they are added here so the
--       payment module's SQL matches the physical schema exactly.
-- ============================================================================

BEGIN;

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ---------------------------------------------------------------------------
-- 0. Base table (self-sufficiency): `transactions` is created by migration
--    016_payment_integrity_base.sql, but no deployment may assume 016 has run
--    before this one, and no earlier migration (001..014) ever created it.
--    The CREATE TABLE IF NOT EXISTS below uses the identical definition so
--    either ordering converges to the same physical shape. Column list
--    verified against the payment module's own INSERT/SELECT statements
--    (process_payment.go saveTransaction, refund_payment.go loadTransaction).
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

-- ---------------------------------------------------------------------------
-- 1. transactions: legacy-module columns + authoritative references
-- ---------------------------------------------------------------------------
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS card_last4 VARCHAR(4);
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS card_brand VARCHAR(32);
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS refunded_at TIMESTAMPTZ;
-- Provider-side checkout reference we issued (idempotency key relayed to rail)
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS payment_intent_id VARCHAR(255);
-- Terminal-transition audit: when a tx reached its latest terminal state and
-- what authoritative evidence backed that transition.
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS settled_at TIMESTAMPTZ;
ALTER TABLE transactions ADD COLUMN IF NOT EXISTS settlement_evidence TEXT;

COMMENT ON COLUMN transactions.settled_at IS
    'Set when status last reached a terminal state (succeeded/failed/cancelled/refunded/partially_refunded). NULL for pending/processing.';
COMMENT ON COLUMN transactions.settlement_evidence IS
    'Authoritative evidence backing the terminal state, e.g. "verify:telebirr:<ref>:succeeded" or "gateway-error:<err>". Never set from webhook assertion alone (golden rule).';

-- ---------------------------------------------------------------------------
-- 2. Idempotent charge claim (D1a): DB-enforced uniqueness of the key.
--    Partial: rows without a key (system-generated) are unconstrained.
-- ---------------------------------------------------------------------------
CREATE UNIQUE INDEX IF NOT EXISTS uq_transactions_idempotency_key
    ON transactions ((metadata->>'idempotency_key'))
    WHERE metadata ? 'idempotency_key';

COMMENT ON INDEX uq_transactions_idempotency_key IS
    'Database-level guarantee that at most one transaction row exists per idempotency key. Concurrent duplicate ProcessPayment claims resolve to a single winner; loser replays the winner''s row.';

-- ---------------------------------------------------------------------------
-- 3. Refunds ledger (D2): one row per accepted refund request, written in the
--    same locked transaction as the refunded_amount increment. The partial
--    unique index allows at most ONE in-flight (pending) refund per
--    transaction, so a retried/concurrent refund either waits on the row lock
--    and replays, or is rejected — it can never double-issue.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS payment_refunds (
    id                   UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    transaction_id       UUID NOT NULL REFERENCES transactions(id),
    amount               DECIMAL(12,2) NOT NULL CHECK (amount > 0),
    reason               TEXT,
    requested_by         UUID,
    provider_reference   VARCHAR(255),          -- rail reference used for reversal
    provider_refund_id   VARCHAR(255),          -- rail-issued refund id, if returned
    status               VARCHAR(32) NOT NULL DEFAULT 'pending', -- pending|succeeded|failed
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_payment_refunds_transaction
    ON payment_refunds(transaction_id);

CREATE UNIQUE INDEX IF NOT EXISTS uq_payment_refunds_pending_per_tx
    ON payment_refunds(transaction_id)
    WHERE status = 'pending';

COMMENT ON TABLE payment_refunds IS
    'Audit ledger of refund operations against payment transactions. Combined with the conditional UPDATE ... WHERE refunded_amount + $new <= amount guard under SELECT FOR UPDATE in refund_payment.go, over-refund under concurrency is impossible.';

COMMIT;
