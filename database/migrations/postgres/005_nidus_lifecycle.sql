-- ============================================================================
-- Migration 005: Nidus Ride Lifecycle & Settlement Durability
-- ----------------------------------------------------------------------------
-- Supports the full ride flow (request -> match -> accept -> start ->
-- complete -> settle -> rate/tip):
--   1. rides.currency default switched USD -> ETB (Ethiopian ledger rails;
--      all ledger money is ETB/santim — see 004_ledger_schema.sql).
--   2. rides.payment_method_id column (captured from the request payload).
--   3. Partial index for the driver offer feed + auto-matcher scan of
--      unassigned rides.
--   4. ride_completed_events: durable settlement outbox written atomically
--      with the terminal 'completed' UPDATE; drained by SettlementOutbox
--      when the synchronous broker publish fails (event_id = ride id makes
--      replays a no-op via ON CONFLICT DO NOTHING).
-- ============================================================================

BEGIN;

-- ----------------------------------------------------------------------------
-- 1. Currency default: Ethiopian Birr end-to-end
-- ----------------------------------------------------------------------------
ALTER TABLE rides
    ALTER COLUMN currency SET DEFAULT 'ETB';

-- ----------------------------------------------------------------------------
-- 2. Payment method reference from the ride request
-- ----------------------------------------------------------------------------
ALTER TABLE rides
    ADD COLUMN IF NOT EXISTS payment_method_id VARCHAR(64);

COMMENT ON COLUMN rides.payment_method_id IS
    'Client-selected payment method at request time (ledger_balance, telebirr, chapa, mpesa). Informational only — settlement always runs through the private ledger.';

-- ----------------------------------------------------------------------------
-- 3. Offer-feed / auto-matcher scan index
--    (rides WHERE status IN ('requested','searching') AND driver_id IS NULL)
-- ----------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_rides_unassigned
    ON rides (requested_at DESC)
    WHERE driver_id IS NULL AND status IN ('requested', 'searching');

-- ----------------------------------------------------------------------------
-- 4. Settlement outbox (ride lifecycle -> ledger)
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ride_completed_events (
    event_id         UUID PRIMARY KEY,               -- == ride_id (idempotent key)
    ride_id          UUID NOT NULL REFERENCES rides(id),
    driver_user_id   UUID NOT NULL,                  -- ledger identity (drivers.user_id)
    payload          JSONB NOT NULL,                 -- exact ride.completed event body
    direct_published BOOLEAN NOT NULL DEFAULT FALSE, -- true => sync broker publish succeeded
    published_at     TIMESTAMPTZ,                    -- set when outbox drain publishes it
    attempts         INTEGER NOT NULL DEFAULT 0,     -- drain retry counter (observability)
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Unpublished rows for the SettlementOutbox drain loop
-- (FOR UPDATE SKIP LOCKED ordering key).
CREATE INDEX IF NOT EXISTS idx_rce_unpublished
    ON ride_completed_events (created_at)
    WHERE published_at IS NULL;

COMMENT ON TABLE ride_completed_events IS
    'Durable outbox for the ride.completed settlement event. Written in the same transaction as the terminal ride UPDATE; drained every 30s until the event bus accepts it. Downstream ledger consumption is idempotent per ride ("ride:debit:<ride_id>"), so duplicate delivery is safe.';

COMMIT;
