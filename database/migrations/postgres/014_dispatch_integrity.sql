-- ============================================================================
-- Migration 014: Dispatch Integrity (NIDUS-DISPATCH-001)
-- ----------------------------------------------------------------------------
-- Database-enforced invariants for atomic driver reservation + ride
-- assignment, the authoritative matched-ETA column, and the durable
-- ride.matched outbox (same pattern as ride_completed_events from 005).
--
--   1. uq_rides_active_driver: a partial UNIQUE index guaranteeing that no
--      two ACTIVE rides (requested/searching/matched/driver_en_route/
--      in_progress) can reference the same driver_id. This is the hard
--      backstop behind the transactional reservation protocol: even if an
--      application bug or a mis-routed replica attempts a double assign,
--      the second INSERT/UPDATE fails with a unique-violation instead of
--      silently putting one driver on two trips.
--   2. rides.matched_eta_minutes: the selected candidate's actual computed
--      ETA (heuristic routing estimate from MatchingEngine.calculateETA),
--      persisted at assignment time so clients never receive a fabricated
--      constant. Nullable: rides matched before this migration keep NULL
--      and clients fall back to their existing "not provided" handling.
--   3. ride_matched_events: transactional outbox written in the SAME
--      database transaction that flips the ride to 'matched' and reserves
--      the driver. Drained by MatchedEventsOutbox (application layer) with
--      bounded retries; event_id = ride_id makes replays idempotent via
--      ON CONFLICT DO NOTHING, exactly like the settlement outbox.
--
-- Rollback implications: DROP INDEX / DROP COLUMN / DROP TABLE reverse this
-- migration cleanly; no data is rewritten. The unique index build can fail
-- on a production table that already contains duplicate active assignments
-- — detect them first with:
--   SELECT driver_id, count(*) FROM rides
--    WHERE driver_id IS NOT NULL
--      AND status IN ('requested','searching','matched','driver_en_route','in_progress')
--    GROUP BY driver_id HAVING count(*) > 1;
-- Remediate (cancel/reassign one side) before applying.
-- ============================================================================

BEGIN;

-- ----------------------------------------------------------------------------
-- 1. One driver cannot hold two active rides (database-enforced invariant)
-- ----------------------------------------------------------------------------
CREATE UNIQUE INDEX IF NOT EXISTS uq_rides_active_driver
    ON rides (driver_id)
    WHERE driver_id IS NOT NULL
      AND status IN ('requested', 'searching', 'matched', 'driver_en_route', 'in_progress');

COMMENT ON INDEX uq_rides_active_driver IS
    'Dispatch invariant: at most one ACTIVE ride may reference a driver. Terminal/cancelled rows drop out of the index scope automatically.';

-- ----------------------------------------------------------------------------
-- 2. Authoritative matched ETA (minutes) persisted at assignment commit
-- ----------------------------------------------------------------------------
ALTER TABLE rides
    ADD COLUMN IF NOT EXISTS matched_eta_minutes INTEGER;

COMMENT ON COLUMN rides.matched_eta_minutes IS
    'Estimated pickup minutes computed by the matching engine (straight-line haversine heuristic at 30 km/h average city speed — NOT trained-model inference) and committed atomically with the assignment. NULL for pre-014 rides or when the scorer had no distance input.';

-- ----------------------------------------------------------------------------
-- 3. ride.matched transactional outbox (mirrors ride_completed_events, 005)
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS ride_matched_events (
    event_id         UUID PRIMARY KEY,               -- == ride_id (idempotent key)
    ride_id          UUID NOT NULL REFERENCES rides(id),
    driver_id        UUID NOT NULL,                  -- assigned driver (drivers.id)
    payload          JSONB NOT NULL,                 -- exact ride.matched event body
    direct_published BOOLEAN NOT NULL DEFAULT FALSE, -- true => sync broker publish succeeded
    published_at     TIMESTAMPTZ,                    -- set when outbox drain publishes it
    attempts         INTEGER NOT NULL DEFAULT 0,     -- drain retry counter (observability)
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Unpublished rows for the MatchedEventsOutbox drain loop
-- (FOR UPDATE SKIP LOCKED ordering key).
CREATE INDEX IF NOT EXISTS idx_rme_unpublished
    ON ride_matched_events (created_at)
    WHERE published_at IS NULL;

COMMENT ON TABLE ride_matched_events IS
    'Durable outbox for the ride.matched lifecycle event. Written in the same transaction as the atomic driver reservation + ride assignment; drained until the event bus accepts it. Consumers key on ride_id, so duplicate delivery is safe.';

COMMIT;
