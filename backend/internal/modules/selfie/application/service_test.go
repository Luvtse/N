package application_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	selfieapp "nidaw-backend/internal/modules/selfie/application"
	selfiedomain "nidaw-backend/internal/modules/selfie/domain"
	selfieinfra "nidaw-backend/internal/modules/selfie/infrastructure"

	"github.com/google/uuid"
)

// fakeRides implements application.RideLookup over an in-memory map so tests
// stay deterministic and never touch Postgres.
type fakeRides struct {
	drivers map[uuid.UUID]uuid.UUID // rideID -> driverID
	riders  map[uuid.UUID]uuid.UUID // rideID -> riderID
}

func (f *fakeRides) RideDriver(rideID uuid.UUID) (uuid.UUID, error) {
	if d, ok := f.drivers[rideID]; ok && d != uuid.Nil {
		return d, nil
	}
	return uuid.Nil, selfieapp.ErrNoMatchedDriver
}

func (f *fakeRides) RideRider(rideID uuid.UUID) (uuid.UUID, error) {
	if r, ok := f.riders[rideID]; ok {
		return r, nil
	}
	return uuid.Nil, errors.New("ride not found")
}

type harness struct {
	svc     *selfieapp.Service
	repo    *selfieinfra.MemoryRepository
	storage *selfieinfra.LocalFileStorage
	rides   *fakeRides
	now     *time.Time
	rideID  uuid.UUID
	driver  uuid.UUID
	rider   uuid.UUID
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	storage, err := selfieinfra.NewLocalFileStorage(dir, "https://cdn.test/selfies", "0123456789abcdef0123456789abcdef", 15*time.Minute)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	repo := selfieinfra.NewMemoryRepository()
	now := time.Now().UTC().Truncate(time.Second)
	h := &harness{
		repo:    repo,
		storage: storage,
		rides: &fakeRides{
			drivers: map[uuid.UUID]uuid.UUID{},
			riders:  map[uuid.UUID]uuid.UUID{},
		},
		now:    &now,
		rideID: uuid.New(),
		driver: uuid.New(),
		rider:  uuid.New(),
	}
	h.rides.drivers[h.rideID] = h.driver
	h.rides.riders[h.rideID] = h.rider
	h.svc = selfieapp.NewService(repo, h.rides, storage, storage,
		selfieapp.WithClock(func() time.Time { return *h.now }),
	)
	return h
}

func jpegBytes(n int) []byte {
	b := make([]byte, n)
	copy(b, []byte("\xff\xd8\xff\xe0"))
	for i := 4; i < n; i++ {
		b[i] = byte(i % 251)
	}
	return b
}

func TestUploadHappyPathProducesRedeemableView(t *testing.T) {
	h := newHarness(t)
	selfie, err := h.svc.Upload(selfieapp.UploadInput{
		RideID:      h.rideID,
		DriverID:    h.driver,
		CapturedAt:  *h.now,
		ContentType: "image/jpeg",
		Data:        jpegBytes(1024),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if selfie.Status != selfiedomain.StatusUploaded {
		t.Fatalf("status = %q, want uploaded", selfie.Status)
	}
	view := h.svc.AttachToRiderView(h.rideID)
	if view.State != selfiedomain.RiderStateAvailable {
		t.Fatalf("state = %q, want available", view.State)
	}
	if view.URL == "" || view.ExpiresAt == nil {
		t.Fatalf("available view must carry URL+expiry, got %+v", view)
	}
}

func TestUploadRejectsRiderAndBadInputs(t *testing.T) {
	h := newHarness(t)

	// Rider calling upload gets ErrForbidden — never silent success.
	if _, err := h.svc.Upload(selfieapp.UploadInput{
		RideID: h.rideID, DriverID: h.rider, ContentType: "image/jpeg", Data: jpegBytes(512),
	}); !errors.Is(err, selfiedomain.ErrForbidden) {
		t.Fatalf("rider upload err = %v, want ErrForbidden", err)
	}

	// Oversize rejected before any storage write.
	if _, err := h.svc.Upload(selfieapp.UploadInput{
		RideID: h.rideID, DriverID: h.driver, ContentType: "image/jpeg", Data: jpegBytes(selfiedomain.MaxImageBytes + 1),
	}); !errors.Is(err, selfiedomain.ErrImageTooLarge) {
		t.Fatalf("oversize err = %v, want ErrImageTooLarge", err)
	}

	// Bad content type rejected.
	if _, err := h.svc.Upload(selfieapp.UploadInput{
		RideID: h.rideID, DriverID: h.driver, ContentType: "image/svg+xml", Data: jpegBytes(64),
	}); !errors.Is(err, selfiedomain.ErrBadContentType) {
		t.Fatalf("bad type err = %v, want ErrBadContentType", err)
	}

	// Empty body rejected.
	if _, err := h.svc.Upload(selfieapp.UploadInput{
		RideID: h.rideID, DriverID: h.driver, ContentType: "image/jpeg", Data: nil,
	}); !errors.Is(err, selfiedomain.ErrEmptyImage) {
		t.Fatalf("empty err = %v, want ErrEmptyImage", err)
	}
}

func TestReuploadIsIdempotentPerRideDriver(t *testing.T) {
	h := newHarness(t)
	first, err := h.svc.Upload(selfieapp.UploadInput{
		RideID: h.rideID, DriverID: h.driver, CapturedAt: *h.now, ContentType: "image/jpeg", Data: jpegBytes(512),
	})
	if err != nil {
		t.Fatalf("first upload: %v", err)
	}
	second, err := h.svc.Upload(selfieapp.UploadInput{
		RideID: h.rideID, DriverID: h.driver, CapturedAt: h.now.Add(time.Minute), ContentType: "image/png", Data: jpegBytes(700),
	})
	if err != nil {
		t.Fatalf("second upload: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("re-upload created a new row (%s vs %s); must replace", second.ID, first.ID)
	}
	view := h.svc.AttachToRiderView(h.rideID)
	if view.State != selfiedomain.RiderStateAvailable {
		t.Fatalf("state after re-upload = %q", view.State)
	}
}

func TestRiderViewStatesAreHonest(t *testing.T) {
	h := newHarness(t)

	// No selfie yet => not_sent (never blank, never fabricated).
	if v := h.svc.AttachToRiderView(h.rideID); v.State != selfiedomain.RiderStateNotSent {
		t.Fatalf("pre-upload state = %q, want not_sent", v.State)
	}

	if _, err := h.svc.Upload(selfieapp.UploadInput{
		RideID: h.rideID, DriverID: h.driver, CapturedAt: *h.now, ContentType: "image/jpeg", Data: jpegBytes(256),
	}); err != nil {
		t.Fatalf("upload: %v", err)
	}

	// Advance past the 15-minute window => expired.
	*h.now = h.now.Add(16 * time.Minute)
	v := h.svc.AttachToRiderView(h.rideID)
	if v.State != selfiedomain.RiderStateExpired {
		t.Fatalf("post-window state = %q, want expired", v.State)
	}
	if v.URL != "" {
		t.Fatalf("expired view must not expose a URL, got %q", v.URL)
	}
}

func TestRedeemEnforcesExpirySignatureAndParticipants(t *testing.T) {
	h := newHarness(t)
	selfie, err := h.svc.Upload(selfieapp.UploadInput{
		RideID: h.rideID, DriverID: h.driver, CapturedAt: *h.now, ContentType: "image/jpeg", Data: jpegBytes(900),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}

	// Derive the storage path and expiry exactly as the service did, then mint
	// the same signature an honest client would present. The service keys
	// objects at "pre-selfies/{rideID}/{selfieID}.jpg" (no leading slash);
	// Sign embeds them under the "/objects/" URL marker which the app layer's
	// objectKeyFromURL uses to recover the canonical key for verification.
	exp := selfie.ExpiresAt.UTC()
	path := "pre-selfies/" + selfie.RideID.String() + "/" + selfie.ID.String() + ".jpg"
	minted, err := h.storage.Sign(path, exp)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !strings.Contains(minted, "/objects/pre-selfies/") {
		t.Fatalf("minted URL missing /objects/ marker: %s", minted)
	}
	const sigMarker = "&sig="
	si := strings.LastIndex(minted, sigMarker)
	if si < 0 {
		t.Fatalf("minted URL missing signature: %s", minted)
	}
	signature := minted[si+len(sigMarker):]

	// Rider redeems successfully and gets real bytes back.
	res, err := h.svc.Redeem(selfie.ID, exp.UTC(), h.rider, signature)
	if err != nil {
		t.Fatalf("rider redeem: %v", err)
	}
	if len(res.Data) != 900 {
		t.Fatalf("served %d bytes, want 900", len(res.Data))
	}

	// Stranger is forbidden even with a valid signature.
	if _, err := h.svc.Redeem(selfie.ID, exp.UTC(), uuid.New(), signature); !errors.Is(err, selfiedomain.ErrForbidden) {
		t.Fatalf("stranger redeem err = %v, want ErrForbidden", err)
	}

	// Tampered signature fails closed.
	if _, err := h.svc.Redeem(selfie.ID, exp.UTC(), h.rider, "de"+signature[2:]); !errors.Is(err, selfieapp.ErrBadSignature) {
		t.Fatalf("tampered redeem err = %v, want ErrBadSignature", err)
	}

	// Expired link returns 410 semantics via ErrExpired.
	*h.now = h.now.Add(16 * time.Minute)
	if _, err := h.svc.Redeem(selfie.ID, exp.UTC(), h.rider, signature); !errors.Is(err, selfieapp.ErrExpired) {
		t.Fatalf("expired redeem err = %v, want ErrExpired", err)
	}
}
