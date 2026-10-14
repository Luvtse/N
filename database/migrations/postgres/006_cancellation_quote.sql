-- ============================================================================
-- Migration 006: Authoritative Cancellation-Fee Accounting (§2.5)
-- ----------------------------------------------------------------------------
-- The rider-facing cancel sheet now shows a real quote computed by the shared
-- cancellation policy service (GET /rides/{id}/cancellation-quote), and the
-- cancel command persists the exact number that was charged at the instant of
-- cancellation. This migration adds the columns those code paths already
-- reference:
--   * rides.cancel_reason  — referenced by CancelRideHandler since the
--                            lifecycle work, but never actually created by
--                            001/init.sql. Added idempotently here.
--   * rides.cancel_fee     — authoritative fee charged to the RIDER only
--                            (driver-initiated cancels always persist 0).
--                            Computed server-side by
--                            services.ComputeCancellationQuote; never trusted
--                            from client input.
--   * rides.cancelled_by   — "rider" | "driver", derived server-side from the
--                            JWT subject; lets history/receipt reconcile who
--                            ended the ride.
-- All statements are IF NOT EXISTS so re-running is a no-op.
-- ============================================================================

BEGIN;

ALTER TABLE rides
    ADD COLUMN IF NOT EXISTS cancel_reason TEXT,
    ADD COLUMN IF NOT EXISTS cancel_fee    DECIMAL(10,2) NOT NULL DEFAULT 0.00,
    ADD COLUMN IF NOT EXISTS cancelled_by  VARCHAR(10);

COMMENT ON COLUMN rides.cancel_fee IS
    'Authoritative cancellation fee charged to the rider, computed by the shared cancellation policy at the instant of cancel (ETB). 0 for driver-initiated cancels and free-window cancels. Matches the quote returned by GET /rides/{id}/cancellation-quote unless ride state changed in between.';

COMMENT ON COLUMN rides.cancelled_by IS
    'Server-derived actor of the cancellation: "rider" or "driver". Never client input.';

COMMENT ON COLUMN rides.cancel_reason IS
    'Free-text reason supplied by the cancelling party (defaults to "no reason provided").';

COMMIT;
