package application

import (
	"errors"
	"fmt"
	"time"

	telephonydomain "nidaw-backend/internal/modules/telephony/domain"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ParticipantResolver supplies the ride's rider id, driver id and both real
// phone numbers. It is implemented over Postgres in infrastructure so this
// module never reaches into the nidus module's internals directly.
type ParticipantResolver interface {
	// ResolveRider returns the user id that owns the ride plus the rider's
	// E.164 phone number from the users table.
	ResolveRider(rideID uuid.UUID) (userID uuid.UUID, phone string, err error)
	// ResolveDriver returns the driver's user id (for authorization) plus the
	// driver's E.164 phone number. Returns ErrNoMatchedDriver when the ride
	// has not been matched yet — provisioning must wait for DriverMatched.
	ResolveDriver(rideID uuid.UUID) (userID uuid.UUID, phone string, err error)
}

// RideUserLookup maps a driver's stored phone back to the user-scoped id used
// in JWT subjects, so a driver can fetch the contact view of their own ride.
type RideUserLookup func(driverNumber string) (uuid.UUID, error)

var (
	ErrNoMatchedDriver = errors.New("ride has no matched driver yet")
	ErrUnresolvable    = errors.New("ride participants could not be resolved")
)

// Service orchestrates proxy-session lifecycle: provision on match, contact
// views for both parties, teardown on completion/cancellation, and expiry
// sweeping. All provider access goes through telephonydomain.Provider so the
// concrete stack (Twilio Proxy vs. self-hosted FreeSWITCH) is swappable.
type Service struct {
	repo       telephonydomain.Repository
	provider   telephonydomain.Provider
	pool       *ProxyPool
	resolver   ParticipantResolver
	userLookup RideUserLookup
	log        *zap.Logger // may be nil; logging is best-effort
	grace      time.Duration // added to ride duration estimate; plan says +30 min
	now        func() time.Time
}

// NewService assembles the telephony service. poolSize numbers are kept warm
// in ProxyPool; grace defaults to 30 minutes when zero.
func NewService(
	repo telephonydomain.Repository,
	provider telephonydomain.Provider,
	numbers []string,
	resolver ParticipantResolver,
	userLookup RideUserLookup,
	grace time.Duration,
	opts ...ServiceOption,
) (*Service, error) {
	pool, err := NewProxyPool(numbers)
	if err != nil {
		return nil, err
	}
	if grace <= 0 {
		grace = 30 * time.Minute
	}
	s := &Service{
		repo:       repo,
		provider:   provider,
		pool:       pool,
		resolver:   resolver,
		userLookup: userLookup,
		grace:      grace,
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// ServiceOption customises a Service at construction. Used by tests to inject
// a deterministic clock so TTL/expiry behaviour is not timing-dependent.
type ServiceOption func(*Service)

// WithClock overrides the service's time source. Production code never calls
// this; the default is time.Now.
func WithClock(now func() time.Time) ServiceOption {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// WithLogger attaches a structured logger to the service. Best-effort only —
// a nil logger disables output rather than panicking.
func WithLogger(l *zap.Logger) ServiceOption {
	return func(s *Service) { s.log = l }
}

func (s *Service) warn(msg string, err error) {
	if s.log != nil {
		s.log.Warn(msg, zap.Error(err))
	}
}

// Provision creates (or returns the existing) active session for a ride.
// Called from the ride.matched event consumer. Idempotent per ride: a second
// call while a session is active returns the same row without double-booking
// a proxy number. ttl is the expected ride duration; the session lives for
// ttl + grace (plan §2.2).
func (s *Service) Provision(rideID uuid.UUID, ttl time.Duration) (*telephonydomain.ProxySession, error) {
	if existing, err := s.repo.GetByRide(rideID); err == nil && existing.IsActive(s.now()) {
		return existing, nil
	}

	riderID, riderPhone, err := s.resolver.ResolveRider(rideID)
	if err != nil {
		return nil, fmt.Errorf("%w: rider: %v", ErrUnresolvable, err)
	}
	driverID, driverPhone, err := s.resolver.ResolveDriver(rideID)
	if err != nil {
		return nil, err // ErrNoMatchedDriver passes through untouched
	}
	_ = riderID
	_ = driverID

	riderN, err := telephonydomain.NormalizeE164(riderPhone)
	if err != nil {
		return nil, fmt.Errorf("rider phone: %w", err)
	}
	driverN, err := telephonydomain.NormalizeE164(driverPhone)
	if err != nil {
		return nil, fmt.Errorf("driver phone: %w", err)
	}

	proxy, err := s.pool.Acquire()
	if err != nil {
		return nil, err
	}

	now := s.now().UTC()
	session := &telephonydomain.ProxySession{
		ID:           uuid.New(),
		RideID:       rideID,
		RiderNumber:  riderN,
		DriverNumber: driverN,
		ProxyNumber:  proxy,
		Status:       telephonydomain.SessionActive,
		Provider:     s.provider.Name(),
		CreatedAt:    now,
		ExpiresAt:    now.Add(ttl + s.grace),
	}

	sid, err := s.provider.Provision(rideID, riderN, driverN, proxy, session.ExpiresAt)
	if err != nil {
		s.pool.Release(proxy) // return the number; nothing was booked
		return nil, fmt.Errorf("provider provision failed: %w", err)
	}
	session.ProviderSID = sid

	if err := s.repo.Save(session); err != nil {
		// Roll back the provider side too, or the pairing outlives its record.
		_ = s.provider.Teardown(sid)
		s.pool.Release(proxy)
		return nil, err
	}
	return session, nil
}

// ContactFor returns the masked-dial view for the calling party. Riders see
// "call your driver via X"; drivers see "call your rider via X". Neither
// party's real number ever leaves the database.
func (s *Service) ContactFor(rideID, callerUserID uuid.UUID) (*telephonydomain.ContactView, error) {
	session, err := s.repo.GetByRide(rideID)
	if err != nil {
		return nil, err
	}
	if !session.IsActive(s.now()) {
		return nil, telephonydomain.ErrSessionNotFound
	}

	riderID, _, err := s.resolver.ResolveRider(rideID)
	if err != nil {
		return nil, fmt.Errorf("%w: rider: %v", ErrUnresolvable, err)
	}
	if callerUserID == riderID {
		return &telephonydomain.ContactView{
			RideID:      session.RideID,
			ProxyNumber: session.ProxyNumber,
			CallableAs:  "driver",
			ExpiresAt:   session.ExpiresAt,
			Active:      true,
		}, nil
	}

	driverID, _, err := s.resolver.ResolveDriver(rideID)
	if err != nil {
		return nil, err
	}
	if callerUserID == driverID {
		return &telephonydomain.ContactView{
			RideID:      session.RideID,
			ProxyNumber: session.ProxyNumber,
			CallableAs:  "rider",
			ExpiresAt:   session.ExpiresAt,
			Active:      true,
		}, nil
	}
	return nil, telephonydomain.ErrForbidden
}

// Teardown ends a session early (ride completed or cancelled). Safe to call
// when no session exists — it is a no-op, because terminal events may arrive
// for rides that were never matched.
func (s *Service) Teardown(rideID uuid.UUID) error {
	session, err := s.repo.GetByRide(rideID)
	if err != nil {
		if errors.Is(err, telephonydomain.ErrSessionNotFound) {
			return nil
		}
		return err
	}
	if session.Status.IsTerminal() {
		return nil
	}
	at := s.now().UTC()
	session.Status = telephonydomain.SessionTornDown
	session.TorndownAt = &at
	if session.ProviderSID != "" {
		if err := s.provider.Teardown(session.ProviderSID); err != nil {
			session.FailureReason = "provider teardown failed: " + err.Error()
			s.warn("telephony: provider teardown failed; pairing may outlive its record", err)
			// Persist the failure reason but still mark the local row down;
			// leaving it "active" would keep patching calls we cannot trace.
		}
	}
	if err := s.repo.Save(session); err != nil {
		return err
	}
	s.pool.Release(session.ProxyNumber)
	return nil
}

// SweepExpired marks sessions past their TTL expired and frees their numbers.
// Returns how many rows were swept. Runs from a background ticker so proxy
// inventory recycles even if terminal events are lost.
func (s *Service) SweepExpired() (int, error) {
	expired, err := s.repo.ListExpired(s.now().UTC(), 100)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, session := range expired {
		if session.Status != telephonydomain.SessionActive {
			continue
		}
		session.Status = telephonydomain.SessionExpired
		if session.ProviderSID != "" {
			_ = s.provider.Teardown(session.ProviderSID)
		}
		if err := s.repo.Save(session); err != nil {
			continue
		}
		s.pool.Release(session.ProxyNumber)
		n++
	}
	return n, nil
}
