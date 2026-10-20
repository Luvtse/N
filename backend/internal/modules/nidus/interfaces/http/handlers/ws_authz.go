package handlers

// Phase 4 — Realtime and access security: ride-level WebSocket authorization.
//
// Defect (verified against this file's history and websocket_handler.go):
// the hub's "subscribe" handler previously accepted ANY topic string from a
// client without server-side validation. An authenticated user could subscribe
// to "ride:<uuid>" for someone else's ride or "driver:<uuid>" for another
// driver and receive their private lifecycle/location updates, violating the
// SECURITY REQUIREMENTS ("Prevent riders and drivers from reading unrelated
// users' trip information").
//
// Fix: every subscription to a resource-scoped topic is checked against an
// injected authorizer before c.topics is mutated. The concrete implementation
// (PostgresRideAuthorizer) joins rides/drivers/users so that:
//
//   - "ride:<id>"        -> only the requesting rider (rides.user_id), the
//                           assigned driver (drivers.user_id via
//                           rides.driver_id), or an admin may subscribe.
//   - "driver:<id>"      -> only the driver themself (drivers.user_id) or an
//                           admin may subscribe.
//   - "user:<id>"        -> only that user themself or an admin.
//   - "driver:offers"    -> only role "driver" or "admin" (public offer feed).
//   - "*" wildcard       -> forbidden for every client (it would bypass all
//                           of the above through the hub's broadcast filter).
//   - anything else      -> rejected (deny-by-default).
//
// Fail-closed contract: when no authorizer is installed on the hub, all
// resource-scoped subscriptions are DENIED rather than allowed; only the
// non-private "driver:offers" feed passes its role check. This guarantees a
// misconfigured boot can never silently reopen the hole.

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"nidaw-backend/internal/shared/database"
)

// Topic namespaces understood by the hub. Keep in sync with ws_bridge.go,
// which fans events into exactly these rooms.
const (
	topicPrefixRide = "ride:"
	topicPrefixDrv  = "driver:"
	topicPrefixUser = "user:"
	// TopicDriverOffers is the shared, non-private dispatch feed: it carries
	// only the ride id of newly requested rides, never rider identity,
	// addresses, or fares. Restricted to the driver/admin roles.
	TopicDriverOffers = "driver:offers"
	// topicWildcard subscribes a client to EVERY room (see run()); it is an
	// unauthenticated data leak by design and must never be granted.
	topicWildcard = "*"
)

// SubscriptionDecision is the structured outcome of an authorization check.
// Callers map it to wire responses/log lines; Err must never contain raw
// SQL-driver text on the wire path (use Code for machine-readable reasons).
type SubscriptionDecision struct {
	Allowed bool
	// Code is a stable machine-readable reason: "ok", "forbidden",
	// "not_found", "invalid_topic", "internal".
	Code string
	// Err is the underlying error when Code == "internal"; nil otherwise.
	Err error
}

// DecisionAllowed / DecisionForbidden / ... are the canonical decisions.
func decisionOK() SubscriptionDecision {
	return SubscriptionDecision{Allowed: true, Code: "ok"}
}

func decisionForbidden() SubscriptionDecision {
	return SubscriptionDecision{Allowed: false, Code: "forbidden"}
}

func decisionInvalidTopic() SubscriptionDecision {
	return SubscriptionDecision{Allowed: false, Code: "invalid_topic"}
}

func decisionInternal(err error) SubscriptionDecision {
	return SubscriptionDecision{Allowed: false, Code: "internal", Err: err}
}

// SubscriptionAuthorizer decides whether userID may receive messages routed
// to topic. Implementations MUST fail closed on any error.
type SubscriptionAuthorizer interface {
	CanSubscribe(ctx context.Context, userID uuid.UUID, topic string) SubscriptionDecision
}

// parseTopicUUID extracts the UUID resource id from a prefixed topic such as
// "ride:5f0..." . Returns ok=false for malformed prefixes/ids.
func parseTopicUUID(topic, prefix string) (uuid.UUID, bool) {
	if !strings.HasPrefix(topic, prefix) {
		return uuid.Nil, false
	}
	raw := strings.TrimPrefix(topic, prefix)
	if raw == "" || raw == "*" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// PostgresRideAuthorizer is the production SubscriptionAuthorizer backed by
// the same Postgres pool that owns rides/drivers/users. All lookups are
// parameterized single-row SELECTs; no user-controlled string ever reaches
// the SQL text.
type PostgresRideAuthorizer struct {
	db *database.Postgres
}

// NewPostgresRideAuthorizer builds the authorizer. db may be nil; the
// authorizer then fails closed with decisionInternal-style denials (Code
// "internal") so the hub never widens access on a missing dependency.
func NewPostgresRideAuthorizer(db *database.Postgres) *PostgresRideAuthorizer {
	return &PostgresRideAuthorizer{db: db}
}

// CanSubscribe implements SubscriptionAuthorizer. See package comment for the
// full policy matrix.
func (a *PostgresRideAuthorizer) CanSubscribe(ctx context.Context, userID uuid.UUID, topic string) SubscriptionDecision {
	if a == nil || a.db == nil || !a.db.IsReady() {
		return decisionInternal(errors.New("ws authorizer: database unavailable"))
	}
	if userID == uuid.Nil {
		return decisionForbidden()
	}
	switch {
	case topic == topicWildcard:
		// Wildcard would deliver every room's traffic to this socket.
		return decisionForbidden()

	case topic == TopicDriverOffers:
		role, dec := a.lookupRole(ctx, userID)
		if dec.Code != "ok" {
			return dec
		}
		if role == "driver" || role == "admin" {
			return decisionOK()
		}
		return decisionForbidden()

	case strings.HasPrefix(topic, topicPrefixRide):
		rideID, ok := parseTopicUUID(topic, topicPrefixRide)
		if !ok {
			return decisionInvalidTopic()
		}
		return a.checkRideRoom(ctx, userID, rideID)

	case strings.HasPrefix(topic, topicPrefixDrv):
		driverID, ok := parseTopicUUID(topic, topicPrefixDrv)
		if !ok {
			return decisionInvalidTopic()
		}
		return a.checkDriverRoom(ctx, userID, driverID)

	case strings.HasPrefix(topic, topicPrefixUser):
		target, ok := parseTopicUUID(topic, topicPrefixUser)
		if !ok {
			return decisionInvalidTopic()
		}
		if target == userID {
			return decisionOK()
		}
		role, dec := a.lookupRole(ctx, userID)
		if dec.Code != "ok" {
			return dec
		}
		if role == "admin" {
			return decisionOK()
		}
		return decisionForbidden()

	default:
		// Deny-by-default for unknown namespaces.
		return decisionInvalidTopic()
	}
}

// checkRideRoom allows the ride's own rider, the driver currently assigned to
// the ride, or an admin. A ride that does not exist yields not_found (the hub
// maps that to a generic denial response; existence probing over WS is not a
// concern since ids are random v4 UUIDs and the REST layer already guards
// reads).
func (a *PostgresRideAuthorizer) checkRideRoom(ctx context.Context, userID, rideID uuid.UUID) SubscriptionDecision {
	const q = `
SELECT EXISTS (
         SELECT 1 FROM rides r
          WHERE r.id = $1
            AND (
                  r.user_id = $2
                  OR EXISTS (
                       SELECT 1 FROM drivers d
                        WHERE d.id = r.driver_id AND d.user_id = $2
                  )
                )
       ) AS is_participant,
       (SELECT role FROM users WHERE id = $2) AS caller_role`
	var (
		isParticipant bool
		callerRole    *string
	)
	err := a.db.QueryRow(ctx, q, rideID, userID).Scan(&isParticipant, &callerRole)
	if errors.Is(err, pgx.ErrNoRows) {
		// Caller user row vanished (deleted mid-session): fail closed.
		return decisionForbidden()
	}
	if err != nil {
		return decisionInternal(err)
	}
	if isParticipant {
		return decisionOK()
	}
	if callerRole != nil && *callerRole == "admin" {
		return decisionOK()
	}
	return decisionForbidden()
}

// checkDriverRoom allows the driver themself (drivers.user_id == caller) or an
// admin. Note the personal driver room carries that driver's live GPS and
// ride mirrors, so other drivers must NOT be able to listen in.
func (a *PostgresRideAuthorizer) checkDriverRoom(ctx context.Context, userID, driverID uuid.UUID) SubscriptionDecision {
	const q = `
SELECT EXISTS (
         SELECT 1 FROM drivers d WHERE d.id = $1 AND d.user_id = $2
       ) AS is_self,
       (SELECT role FROM users WHERE id = $2) AS caller_role`
	var (
		isSelf     bool
		callerRole *string
	)
	err := a.db.QueryRow(ctx, q, driverID, userID).Scan(&isSelf, &callerRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return decisionForbidden()
	}
	if err != nil {
		return decisionInternal(err)
	}
	if isSelf {
		return decisionOK()
	}
	if callerRole != nil && *callerRole == "admin" {
		return decisionOK()
	}
	return decisionForbidden()
}

// lookupRole resolves the caller's role for namespace-level policies. Missing
// user rows fail closed as forbidden (not internal): a deleted account must
// lose realtime access immediately.
func (a *PostgresRideAuthorizer) lookupRole(ctx context.Context, userID uuid.UUID) (string, SubscriptionDecision) {
	var role *string
	err := a.db.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", decisionForbidden()
	}
	if err != nil {
		return "", decisionInternal(err)
	}
	if role == nil {
		return "", decisionForbidden()
	}
	return *role, decisionOK()
}

// Compile-time assertion that the production authorizer satisfies the port.
var _ SubscriptionAuthorizer = (*PostgresRideAuthorizer)(nil)
