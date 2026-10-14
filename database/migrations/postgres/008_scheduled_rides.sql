-- ============================================================================
-- Migration 008: Scheduled Rides column (§3.6)
-- ----------------------------------------------------------------------------
-- Adds rides.scheduled_at — the requested pickup time for a scheduled ride.
-- The request path (backend/internal/modules/nidus/application/commands/
-- request_ride.go) persists it at creation; NULL means an on-demand ride.
--
-- Validation window enforced application-side (RequestRideHandler.
-- validateCommand): scheduled_at must be >= now + 15 minutes and
-- <= now + 30 days. Storage-layer constraints are deliberately NOT added
-- here: they would compare against insert-time NOW() and break deterministic
-- test fixtures that seed rows with historical timestamps.
--
-- All statements are IF NOT EXISTS so re-running is a no-op.
-- ============================================================================

BEGIN;

ALTER TABLE rides
    ADD COLUMN IF NOT EXISTS scheduled_at TIMESTAMPTZ;

COMMENT ON COLUMN rides.scheduled_at IS
    'Requested pickup time for a scheduled ride (§3.6). NULL = on-demand. When set, the matcher pre-matches at T-15 min; clients render a "Scheduled" chip in History.';

CREATE INDEX IF NOT EXISTS idx_rides_scheduled_at
    ON rides(scheduled_at)
    WHERE scheduled_at IS NOT NULL;

COMMIT;
