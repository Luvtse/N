-- ============================================================================
-- Migration 013: Scheduled-ride reminder dispatch state (§3.6)
-- Nidus Ride Platform - Postgres
-- ============================================================================
-- The plan requires a scheduled_ride_reminder notification dispatched 30
-- minutes before pickup. The category, localized templates, and channel
-- policy already exist in the notifications module; what was missing is a
-- PRODUCER. This column is the producer's idempotency ledger: the
-- scheduled-reminder worker publishes exactly once per ride because it
-- stamps this column with a race-guarded UPDATE ... WHERE the column IS
-- NULL, making the worker safe to run on multiple replicas.
--
-- Nullable: on-demand rides never have a reminder to send, and legacy
-- scheduled rides created before this migration simply get no reminder
-- (honest absence — no backfill, no fabricated sends).

ALTER TABLE rides
    ADD COLUMN IF NOT EXISTS scheduled_reminder_sent_at TIMESTAMPTZ;

-- Partial index so the worker's scan
--   status = 'requested' AND scheduled_at BETWEEN now+25m AND now+35m
--   AND scheduled_reminder_sent_at IS NULL
-- stays cheap on large tables without touching terminal or already-reminded
-- rows.
CREATE INDEX IF NOT EXISTS idx_rides_scheduled_reminder_pending
    ON rides (scheduled_at)
    WHERE scheduled_reminder_sent_at IS NULL
      AND scheduled_at IS NOT NULL
      AND status = 'requested';

-- Rollback:
--   DROP INDEX IF EXISTS idx_rides_scheduled_reminder_pending;
--   ALTER TABLE rides DROP COLUMN IF EXISTS scheduled_reminder_sent_at;
