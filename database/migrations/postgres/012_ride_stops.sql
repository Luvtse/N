-- ============================================================================
-- Migration 012: §3.5 Stopovers & Mid-Trip Destination Change (Phase 2)
-- ----------------------------------------------------------------------------
-- One row per intermediate stop on a ride, in visit order. The ride keeps its
-- pickup and final dropoff columns unchanged; stops[] sits between them:
--
--     pickup -> stops[0] -> ... -> stops[n-1] -> dropoff
--
-- The pricing service sums the legs over that ordered waypoint chain (see
-- nidus/application/services/pricing_service.go CalculateFareWithStops), so a
-- ride with zero stops prices exactly like before — this table is additive
-- and never rewrites history for legacy rows.
--
-- Domain limits enforced application-side (commands/request_ride.go +
-- commands/change_destination.go), NOT as storage CHECKs, so deterministic
-- test fixtures can seed rows directly:
--   * at most 3 stops per ride (MaxRideStops);
--   * a stop may not coincide with pickup or the next waypoint;
--   * stops are immutable once a ride enters 'in_progress' except through
--     the destination-change command, which only rewrites the FINAL dropoff
--     (mid-trip stop editing is deliberately out of scope for §3.5).
--
-- Idempotent: safe to re-run.
-- ============================================================================

BEGIN;

CREATE TABLE IF NOT EXISTS ride_stops (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ride_id         UUID NOT NULL REFERENCES rides(id) ON DELETE CASCADE,
    seq             SMALLINT NOT NULL CHECK (seq BETWEEN 1 AND 3),
    lat             DOUBLE PRECISION NOT NULL CHECK (lat BETWEEN -90 AND 90),
    lng             DOUBLE PRECISION NOT NULL CHECK (lng BETWEEN -180 AND 180),
    address         TEXT NOT NULL DEFAULT '',
    arrived_at      TIMESTAMPTZ,          -- stamped when the driver reaches the stop
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (ride_id, seq)
);

COMMENT ON TABLE ride_stops IS
    '§3.5 intermediate stops in visit order (pickup -> stops -> dropoff). At most 3 per ride, enforced application-side. Pricing sums the legs across the waypoint chain.';
COMMENT ON COLUMN ride_stops.arrived_at IS
    'Server-stamped arrival time at the stop; NULL until reached. Reserved for receipt "stop visited" annotations — never fabricated.';

CREATE INDEX IF NOT EXISTS idx_ride_stops_ride_seq
    ON ride_stops(ride_id, seq);

COMMIT;
