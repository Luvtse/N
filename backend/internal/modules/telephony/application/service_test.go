package application_test

import (
	"errors"
	"testing"
	"time"

	telephonyapp "nidaw-backend/internal/modules/telephony/application"
	telephonydomain "nidaw-backend/internal/modules/telephony/domain"
	telephonyinfra "nidaw-backend/internal/modules/telephony/infrastructure"

	"github.com/google/uuid"
)

// --- test doubles -----------------------------------------------------------

type fakeResolver struct {
	riderID  uuid.UUID
	riderPh  string
	driverID uuid.UUID
	driverPh string
}

func (f *fakeResolver) ResolveRider(uuid.UUID) (uuid.UUID, string, error) {
	return f.riderID, f.riderPh, nil
}

func (f *fakeResolver) ResolveDriver(uuid.UUID) (uuid.UUID, string, error) {
	if f.driverID == uuid.Nil {
		return uuid.Nil, "", telephonyapp.ErrNoMatchedDriver
	}
	return f.driverID, f.driverPh, nil
}

type fakeProvider struct {
	provisioned []uuid.UUID
	tornDown    []string
	failOn      int // fail the Nth provision call (1-based); 0 = never
	calls       int
}

func (p *fakeProvider) Name() string { return "fake_proxy" }

func (p *fakeProvider) Provision(rideID uuid.UUID, riderNumber, driverNumber, proxyNumber string, expiresAt time.Time) (string, error) {
	p.calls++
	if p.failOn != 0 && p.calls == p.failOn {
		return "", errors.New("provider exploded")
	}
	p.provisioned = append(p.provisioned, rideID)
	return "SID-" + rideID.String(), nil
}

func (p *fakeProvider) Teardown(providerSID string) error {
	p.tornDown = append(p.tornDown, providerSID)
	return nil
}

func newTestService(t *testing.T) (*telephonyapp.Service, *fakeProvider, *fakeResolver) {
	t.Helper()
	resolver := &fakeResolver{
		riderID:  uuid.New(),
		riderPh:  "+251911111111",
		driverID: uuid.New(),
		driverPh: "+251922222222",
	}
	provider := &fakeProvider{}
	repo := telephonyinfra.NewMemoryRepository()
	svc, err := telephonyapp.NewService(repo, provider, []string{"+251900000001", "+251900000002"}, resolver, nil, 30*time.Minute)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, provider, resolver
}

// newTestServiceWithClock builds the same service but with a controllable
// clock so TTL/expiry tests are deterministic instead of sleeping.
func newTestServiceWithClock(t *testing.T, now *time.Time) (*telephonyapp.Service, *fakeProvider, *fakeResolver) {
	t.Helper()
	resolver := &fakeResolver{
		riderID:  uuid.New(),
		riderPh:  "+251911111111",
		driverID: uuid.New(),
		driverPh: "+251922222222",
	}
	provider := &fakeProvider{}
	repo := telephonyinfra.NewMemoryRepository()
	svc, err := telephonyapp.NewService(
		repo, provider,
		[]string{"+251900000001", "+251900000002"},
		resolver, nil, time.Second, // tiny grace so tests can exercise short TTLs deterministically
		telephonyapp.WithClock(func() time.Time { return *now }),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, provider, resolver
}

// --- tests -------------------------------------------------------------------

func TestProvisionBooksUniqueNumbersAndIsIdempotent(t *testing.T) {
	svc, provider, _ := newTestService(t)
	rideID := uuid.New()

	first, err := svc.Provision(rideID, time.Hour)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if first.ProxyNumber != "+251900000001" {
		t.Fatalf("unexpected proxy number %q", first.ProxyNumber)
	}

	second, err := svc.Provision(rideID, time.Hour)
	if err != nil {
		t.Fatalf("idempotent re-Provision: %v", err)
	}
	if second.ID != first.ID || second.ProxyNumber != first.ProxyNumber {
		t.Fatal("re-provisioning an active ride must reuse the same session")
	}
	if len(provider.provisioned) != 1 {
		t.Fatalf("provider called %d times for one ride; want 1", len(provider.provisioned))
	}

	other, err := svc.Provision(uuid.New(), time.Hour)
	if err != nil {
		t.Fatalf("Provision second ride: %v", err)
	}
	if other.ProxyNumber == first.ProxyNumber {
		t.Fatal("two rides must never share a proxy number simultaneously")
	}
}

func TestProvisionRollsBackPoolOnProviderFailure(t *testing.T) {
	svc, provider, _ := newTestService(t)
	provider.failOn = 1

	if _, err := svc.Provision(uuid.New(), time.Hour); err == nil {
		t.Fatal("expected provision error to surface")
	}
	// The failed booking must not leak its number.
	ok, err := svc.Provision(uuid.New(), time.Hour)
	if err != nil {
		t.Fatalf("second Provision after rollback: %v", err)
	}
	if ok.ProxyNumber != "+251900000001" {
		t.Fatalf("failed provision leaked pool number; got %q", ok.ProxyNumber)
	}
}

func TestContactForNeverLeaksRealNumbers(t *testing.T) {
	svc, _, resolver := newTestService(t)
	rideID := uuid.New()
	session, err := svc.Provision(rideID, time.Hour)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	view, err := svc.ContactFor(rideID, resolver.riderID)
	if err != nil {
		t.Fatalf("ContactFor rider: %v", err)
	}
	if view.CallableAs != "driver" || view.ProxyNumber != session.ProxyNumber {
		t.Fatalf("bad rider view: %+v", view)
	}

	dview, err := svc.ContactFor(rideID, resolver.driverID)
	if err != nil {
		t.Fatalf("ContactFor driver: %v", err)
	}
	if dview.CallableAs != "rider" {
		t.Fatalf("bad driver view: %+v", dview)
	}

	if _, err := svc.ContactFor(rideID, uuid.New()); !errors.Is(err, telephonydomain.ErrForbidden) {
		t.Fatalf("stranger contact: want ErrForbidden, got %v", err)
	}
}

func TestTeardownReleasesNumberAndIsIdempotent(t *testing.T) {
	svc, provider, _ := newTestService(t)
	rideID := uuid.New()
	session, err := svc.Provision(rideID, time.Hour)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	if err := svc.Teardown(rideID); err != nil {
		t.Fatalf("Teardown: %v", err)
	}
	if len(provider.tornDown) != 1 || provider.tornDown[0] != session.ProviderSID {
		t.Fatalf("provider teardown mismatch: %v", provider.tornDown)
	}
	if err := svc.Teardown(rideID); err != nil {
		t.Fatalf("double Teardown must be a no-op: %v", err)
	}
	if _, err := svc.ContactFor(rideID, uuid.MustParse("00000000-0000-0000-0000-000000000000")); !errors.Is(err, telephonydomain.ErrSessionNotFound) {
		// stranger id on a torn-down ride: either forbidden or not-found is fine,
		// but ContactFor checks session liveness first — assert that path.
		if !errors.Is(err, telephonydomain.ErrForbidden) {
			t.Fatalf("torn-down contact: want session-not-found, got %v", err)
		}
	}

	// Number returned to the pool: next ride reuses it.
	next, err := svc.Provision(uuid.New(), time.Hour)
	if err != nil {
		t.Fatalf("Provision after teardown: %v", err)
	}
	if next.ProxyNumber != session.ProxyNumber {
		t.Fatalf("released number not recycled: %q vs %q", next.ProxyNumber, session.ProxyNumber)
	}
}

func TestSweepExpiresSessionsWithoutTerminalEvents(t *testing.T) {
	now := time.Now().UTC()
	svc, provider, _ := newTestServiceWithClock(t, &now)
	rideID := uuid.New()
	if _, err := svc.Provision(rideID, time.Millisecond); err != nil {
		t.Fatalf("Provision: %v", err)
	}

	// Advance the deterministic clock past expiry instead of sleeping — no
	// timing flakiness. The service captured &now at construction, so this
	// mutation is observed by s.now().
	now = now.Add(2 * time.Second)

	n, err := svc.SweepExpired()
	if err != nil {
		t.Fatalf("SweepExpired: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d sessions; want 1", n)
	}
	if len(provider.tornDown) != 1 {
		t.Fatalf("sweep must tear down provider pairing; got %v", provider.tornDown)
	}
	if _, err := svc.ContactFor(rideID, uuid.New()); !errors.Is(err, telephonydomain.ErrSessionNotFound) {
		t.Fatalf("expired session must refuse contact lookups, got %v", err)
	}
}

func TestEmptyPoolFailsLoudly(t *testing.T) {
	repo := telephonyinfra.NewMemoryRepository()
	provider := &fakeProvider{}
	if _, err := telephonyapp.NewService(repo, provider, nil, &fakeResolver{}, nil, 0); !errors.Is(err, telephonydomain.ErrNoProxyNumber) {
		t.Fatalf("empty pool must refuse to start masked calling, got %v", err)
	}
}

func TestInvalidPhoneNumberRejectedNotGuessed(t *testing.T) {
	svc, _, resolver := newTestService(t)
	resolver.riderPh = "0911111111" // local format without country code — must NOT be guessed
	if _, err := svc.Provision(uuid.New(), time.Hour); !errors.Is(err, telephonydomain.ErrInvalidPhoneNumber) {
		t.Fatalf("want E.164 rejection, got %v", err)
	}
}
