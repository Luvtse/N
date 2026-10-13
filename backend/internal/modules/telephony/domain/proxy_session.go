// Package domain holds the telephony module's core entities and ports.
package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SessionStatus enumerates the lifecycle of a proxy (masked) calling session.
type SessionStatus string

const (
	// SessionActive means calls between rider and driver are being patched.
	SessionActive SessionStatus = "active"
	// SessionExpired means the TTL elapsed; the proxy number no longer patches.
	SessionExpired SessionStatus = "expired"
	// SessionTornDown means an explicit teardown occurred (ride completed or
	// cancelled before the TTL). The number returns to the pool immediately.
	SessionTornDown SessionStatus = "torn_down"
)

// IsTerminal reports whether the status can still transition.
func (s SessionStatus) IsTerminal() bool {
	return s == SessionExpired || s == SessionTornDown
}

var (
	ErrSessionNotFound    = errors.New("proxy session not found")
	ErrNoProxyNumber      = errors.New("no proxy number available in the pool")
	ErrInvalidPhoneNumber = errors.New("phone number must be E.164 (+country code, 7-15 digits)")
	ErrForbidden          = errors.New("caller is not a participant of this ride")

	// e164Re accepts +<1-3 digit country code><rest>, total 8-16 chars.
	e164Re = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)
)

// NormalizeE164 trims whitespace and validates the E.164 shape required by
// every supported provider (Twilio Proxy, self-hosted FreeSWITCH). Numbers
// without an explicit "+" are rejected rather than guessed at, so silently
// wrong inputs never enter a call-patch table.
func NormalizeE164(raw string) (string, error) {
	n := strings.TrimSpace(raw)
	if !e164Re.MatchString(n) {
		return "", ErrInvalidPhoneNumber
	}
	return n, nil
}

// ProxySession binds one real rider↔driver phone pair to one shared proxy
// number for the duration of a ride plus a grace window. Both parties dial
// the same proxy number; the provider patches based on which side called.
type ProxySession struct {
	ID            uuid.UUID     `json:"id" db:"id"`
	RideID        uuid.UUID     `json:"ride_id" db:"ride_id"`
	RiderNumber   string        `json:"-" db:"rider_number"`  // never serialized outward
	DriverNumber  string        `json:"-" db:"driver_number"` // never serialized outward
	ProxyNumber   string        `json:"proxy_number" db:"proxy_number"`
	Status        SessionStatus `json:"status" db:"status"`
	Provider      string        `json:"provider" db:"provider"`
	ProviderSID   string        `json:"provider_sid,omitempty" db:"provider_sid"`
	CreatedAt     time.Time     `json:"created_at" db:"created_at"`
	ExpiresAt     time.Time     `json:"expires_at" db:"expires_at"`
	TorndownAt    *time.Time    `json:"torn_down_at,omitempty" db:"torn_down_at"`
	FailureReason string        `json:"failure_reason,omitempty" db:"failure_reason"`
}

// IsActive reports whether the session can currently patch calls. Expiry is
// evaluated on read as well as by the sweeper, so a session whose row has not
// yet been swept still refuses new lookups once ExpiresAt passes.
func (s *ProxySession) IsActive(now time.Time) bool {
	return s.Status == SessionActive && now.Before(s.ExpiresAt)
}

// ContactView is the ONLY projection of a session that leaves this module for
// a client: it exposes the proxy number and which role answers, never either
// party's real number (plan §2.2 hard requirement).
type ContactView struct {
	RideID      uuid.UUID `json:"ride_id"`
	ProxyNumber string    `json:"proxy_number"`
	CallableAs  string    `json:"callable_as"` // "driver" | "rider": who answers
	ExpiresAt   time.Time `json:"expires_at"`
	Active      bool      `json:"active"`
}

// Repository is the persistence port. A Postgres implementation ships in
// infrastructure; tests use the in-memory implementation.
type Repository interface {
	Save(session *ProxySession) error
	GetByRide(rideID uuid.UUID) (*ProxySession, error)
	ListExpired(now time.Time, limit int) ([]*ProxySession, error)
}

// Provider is the swappable adapter port named in plan §2.2 ("Adapter
// interface so the concrete provider (Twilio Proxy, or a self-hosted
// Asterisk/FreeSWITCH for cost control) can be swapped").
type Provider interface {
	// Name identifies the adapter in logs/metrics ("twilio_proxy", "freeswitch").
	Name() string
	// Provision creates the server-side pairing for one ride and returns the
	// provider-side session id stored on ProxySession.ProviderSID.
	Provision(rideID uuid.UUID, riderNumber, driverNumber, proxyNumber string, expiresAt time.Time) (providerSID string, err error)
	// Teardown removes the pairing so the proxy number stops patching even if
	// the local row lags behind.
	Teardown(providerSID string) error
}
