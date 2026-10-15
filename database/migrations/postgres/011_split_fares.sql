-- ============================================================================
-- Migration 011: §3.3 Split Fare (Phase 2)
--
-- One row per invitee on a split ride. The inviter is NOT stored here; the
-- inviter's share is computed as fare minus the sum of all shares, so the
-- ledger can never double-count the rider who started the split.
--
-- Acceptance is tokenized: the invitee may be anonymous (no account), so the
-- public accept endpoint authenticates with split_token, not a session.
-- Tokens are high-entropy random strings generated server-side and are never
-- logged. UNIQUE enforcement happens in the application layer (a partial
-- unique index cannot exclude the '' sentinel used for unaccepted invites).
--
-- Idempotent: safe to re-run.
-- ============================================================================

BEGIN;

CREATE TABLE IF NOT EXISTS ride_split_invites (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ride_id         UUID NOT NULL REFERENCES rides(id) ON DELETE CASCADE,
    inviter_id      UUID NOT NULL,                 -- rider who created the split
    invitee_contact TEXT     NOT NULL,             -- email or phone, normalized lowercase
    invitee_user_id UUID,                          -- filled on accept when the invitee has an account
    split_token     TEXT     NOT NULL DEFAULT '',  -- '' until accepted; set at accept time
    share_amount    NUMERIC(10,2) NOT NULL CHECK (share_amount > 0),
    currency        TEXT     NOT NULL DEFAULT 'ETB',
    status          TEXT     NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'accepted', 'paid', 'declined', 'expired')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    accepted_at     TIMESTAMPTZ,
    paid_at         TIMESTAMPTZ
);

-- At most one live invite per contact per ride (prevents accidental double
-- invitations to the same person while the sheet is open/closed repeatedly).
CREATE UNIQUE INDEX IF NOT EXISTS uq_split_ride_contact
    ON ride_split_invites (ride_id, lower(invitee_contact));

-- Tokenized public accept path: O(1) lookup by token, only for issued tokens.
CREATE UNIQUE INDEX IF NOT EXISTS uq_split_token
    ON ride_split_invites (split_token)
    WHERE split_token <> '';

CREATE INDEX IF NOT EXISTS idx_split_ride
    ON ride_split_invites (ride_id, status);

COMMENT ON TABLE ride_split_invites IS
    '§3.3 Split-fare invites. The inviter''s share is fare_amount minus SUM(share_amount) over non-declined invites; each accepted invitee pays their share through the same payment-method CRUD as any other charge.';

COMMIT;
