-- 017_t5_fault_injection.sql
-- Phase 5 settlement-integrity gate support.
--
-- The commit-failure integration test (T5: TestIntegration_Payment_CommitFailure_NoEventPublished)
-- needs a deterministic way to fail the refund-ledger FINALIZE step AFTER the
-- provider rail call has already succeeded, in order to prove that:
--   1. the durable 'pending' row in payment_refunds survives (reconciler can
--      resolve it later), and
--   2. no payment.refunded event is published for an uncommitted transition.
--
-- The refund finalize transaction probes this table (read-only) before writing
-- the terminal status. A row armed for a transaction forces the finalize to
-- return an error and roll back; absence of the table (production databases
-- that never apply this migration) is treated as "no fault" via SQLSTATE
-- 42P01 handling in commands/scanners.go (isUndefinedTable).
--
-- This migration is intended for TEST/CI databases only. It is idempotent.

CREATE TABLE IF NOT EXISTS t5_fault_injection (
    transaction_id uuid PRIMARY KEY,
    fail_finalize  boolean NOT NULL DEFAULT true
);

COMMENT ON TABLE t5_fault_injection IS
    'Phase 5 T5 gate: deterministic finalize-failure injection. TEST/CI ONLY.';
