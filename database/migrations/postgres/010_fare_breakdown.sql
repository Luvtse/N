-- ============================================================================
-- Migration 010: Itemized Receipt / fare breakdown (§3.4)
-- ----------------------------------------------------------------------------
-- Adds the persisted fare-component columns the pricing service already
-- computes at estimate time (base / distance / time / surge) plus the
-- platform's flat service fee, so a completed ride can render an itemized
-- receipt whose lines sum to the authoritative total WITHOUT re-pricing.
--
-- Honest-absence semantics: every column is nullable. A legacy ride (or one
-- completed before this migration) keeps NULLs and the API omits the
-- corresponding line items entirely - clients show "itemization unavailable"
-- rather than fabricated zeros, per CONTRIBUTING-RIDE-PARITY Global Rule 2.
--
-- Rides created after this migration snapshot the components at request time;
-- completion recomputes them against the actual fare through the SAME pricing
-- service, then persists the final values alongside fare_amount. The promo
-- discount line lives in rides.promo_discount (migration 009) and the tip
-- line in rides.tip_amount - this migration adds only what neither covers.
--
-- All statements are IF NOT EXISTS so re-running is a no-op.
-- ============================================================================

BEGIN;

ALTER TABLE rides ADD COLUMN IF NOT EXISTS base_fare_component     NUMERIC(10,2);
ALTER TABLE rides ADD COLUMN IF NOT EXISTS distance_fare_component NUMERIC(10,2);
ALTER TABLE rides ADD COLUMN IF NOT EXISTS time_fare_component     NUMERIC(10,2);
ALTER TABLE rides ADD COLUMN IF NOT EXISTS surge_fare_component    NUMERIC(10,2);
ALTER TABLE rides ADD COLUMN IF NOT EXISTS service_fee_component   NUMERIC(10,2);

COMMIT;
