// Package application implements the Pre-Selfie Verification use cases
// (§2.3): driver upload, rider DTO attachment, and signed-URL redemption.
package application

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	selfiedomain "nidaw-backend/internal/modules/selfie/domain"

	"github.com/google/uuid"
)

// RideLookup resolves who is driving a ride. Implemented over Postgres in
// infrastructure so this module never reaches into the nidus module's
// internals directly.
type RideLookup interface {
	// RideDriver returns the matched driver id for a ride. Returns
	// ErrNoMatchedDriver when the ride has not been matched yet — the
	// pre-selfie step fires on DriverEnRoute, which always follows a match.
	RideDriver(rideID uuid.UUID) (driverID uuid.UUID, err error)
	// RideRider returns the rider user id that owns the ride, used to gate
	// signed-URL redemption to participants only.
	RideRider(rideID uuid.UUID) (riderUserID uuid.UUID, err error)
}

var ErrNoMatchedDriver = errors.New("ride has no matched driver yet")

// ImageStorage persists selfie bytes and returns an object key or public URL.
// The concrete backend (local disk now, S3/GCS later) is swappable without
// touching handlers or the service.
type ImageStorage interface {
	// Put stores the bytes under a deterministic key derived from the selfie
	// ID and returns the storage path/key. It must fail loudly (never return
	// a fake path) when the backend is unreachable.
	Put(key string, contentType string, data []byte) (storedPath string, err error)
}

// SignedURLIssuer mints short-lived, tamper-evident URLs for stored selfies.
type SignedURLIssuer interface {
	// Sign returns a URL that serves the object at storedPath and is only
	// valid until expiresAt. Rejected/mutated signatures must fail closed.
	Sign(storedPath string, expiresAt time.Time) (url string, err error)
	// Verify checks a supplied signature/expiry pair against storedPath.
	Verify(storedPath string, expiresAt time.Time, signature string) bool
}

// UploadInput carries one driver selfie upload.
type UploadInput struct {
	RideID      uuid.UUID
	DriverID    uuid.UUID // JWT subject of the authenticated driver account
	CapturedAt  time.Time // client-declared capture moment; server clamps it
	ContentType string
	Data        []byte
}

// RiderViewStateUnavailable is returned when the selfie subsystem itself is
// broken (repository outage). It is deliberately distinct from "not_sent":
// a storage failure must never be dressed up as driver non-compliance.
const RiderViewStateUnavailable = "unavailable"

// RiderView is what attaches to the rider's ride DTO. Every field is real:
// either a fresh signed URL, or an explicit state explaining why there isn't
// one. There is no case where State=="available" but URL=="".
type RiderView struct {
	State     string     `json:"state"` // available | not_sent | expired | unavailable
	URL       string     `json:"driver_pre_selfie_url,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Service orchestrates the pre-selfie lifecycle.
type Service struct {
	repo    selfiedomain.Repository
	rides   RideLookup
	storage ImageStorage
	signer  SignedURLIssuer
	fetcher ObjectFetcher
	now     func() time.Time
	ttl     time.Duration
}

// ObjectFetcher reads back stored bytes for the redemption path. LocalFileStorage
// implements it; S3/GCS adapters must as well.
type ObjectFetcher interface {
	Get(storedPath string) ([]byte, error)
}

// Option customizes the service (tests inject clocks).
type Option func(*Service)

// WithClock injects a deterministic clock.
func WithClock(now func() time.Time) Option {
	return func(s *Service) { s.now = now }
}

// WithTTL overrides the signed-URL / view window (default 15 min per plan).
func WithTTL(ttl time.Duration) Option {
	return func(s *Service) { s.ttl = ttl }
}

// WithFetcher overrides the byte-fetching port used by Redeem (tests and S3
// adapters where the storage writer and reader are separate objects).
func WithFetcher(f ObjectFetcher) Option {
	return func(s *Service) { s.fetcher = f }
}

// Redemption failure modes, typed so the HTTP layer maps them to distinct
// status codes instead of one opaque 500.
var (
	ErrExpired      = errors.New("signed URL expired or mismatched")
	ErrBadSignature = errors.New("signature verification failed")
)

// NewService wires the use cases over their ports.
func NewService(repo selfiedomain.Repository, rides RideLookup, storage ImageStorage, signer SignedURLIssuer, opts ...Option) *Service {
	s := &Service{
		repo:    repo,
		rides:   rides,
		storage: storage,
		signer:  signer,
		now:     time.Now,
		ttl:     selfiedomain.SelfieTTL,
	}
	if f, ok := storage.(ObjectFetcher); ok {
		s.fetcher = f
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Upload stores a driver's pre-selfie for a ride. Authorization model: the
// authenticated caller's user id must map to the ride's matched driver. A
// rider calling this endpoint gets selfiedomain.ErrForbidden (403), never a
// silent success. Re-upload replaces the prior row (idempotent PUT keyed by
// ride+driver) so a driver who blurs the first shot can retake it.
func (s *Service) Upload(in UploadInput) (*selfiedomain.PreSelfie, error) {
	if err := selfiedomain.ValidateUpload(in.RideID, in.DriverID, in.ContentType, int64(len(in.Data))); err != nil {
		return nil, err
	}

	matchedDriver, err := s.rides.RideDriver(in.RideID)
	if err != nil {
		if errors.Is(err, ErrNoMatchedDriver) {
			return nil, err
		}
		return nil, fmt.Errorf("resolve ride driver: %w", err)
	}
	if matchedDriver != in.DriverID {
		return nil, selfiedomain.ErrForbidden
	}

	now := s.now()
	captured := in.CapturedAt
	if captured.IsZero() || captured.After(now.Add(time.Minute)) {
		// Clamp implausible client clocks instead of trusting them.
		captured = now
	}

	selfie := selfiedomain.PreSelfie{
		ID:          uuid.New(),
		RideID:      in.RideID,
		DriverID:    in.DriverID,
		SizeBytes:   int64(len(in.Data)),
		ContentType: in.ContentType,
		Status:      selfiedomain.StatusUploaded,
		CapturedAt:  captured,
		ExpiresAt:   now.Add(s.ttl),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	key := fmt.Sprintf("pre-selfies/%s/%s.%s", in.RideID, selfie.ID, extFor(in.ContentType))
	storedPath, err := s.storage.Put(key, in.ContentType, in.Data)
	if err != nil {
		// Fail loudly: no row is written pointing at bytes that do not exist.
		return nil, fmt.Errorf("%w: %v", selfiedomain.ErrStorageFailed, err)
	}

	signedURL, err := s.signer.Sign(storedPath, selfie.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("%w: sign: %v", selfiedomain.ErrStorageFailed, err)
	}
	selfie.ImageURL = signedURL

	stored, err := s.repo.Upsert(selfie)
	if err != nil {
		return nil, fmt.Errorf("persist pre-selfie: %w", err)
	}
	return &stored, nil
}

// AttachToRiderView produces the honest rider-facing state for a ride DTO.
// Called by the ride GET handler when the feature flag is on. Never returns
// State=="available" with an empty URL, and never invents a URL for a driver
// who skipped the step.
func (s *Service) AttachToRiderView(rideID uuid.UUID) RiderView {
	now := s.now()
	selfie, err := s.repo.FindByRide(rideID)
	if err != nil {
		if errors.Is(err, selfiedomain.ErrNotFound) {
			return RiderView{State: selfiedomain.RiderStateNotSent}
		}
		// A repository outage is NOT the same as "driver did not send".
		// Report it honestly as unavailable so the UI can show a retry
		// instead of blaming the driver.
		return RiderView{State: RiderViewStateUnavailable}
	}
	if !selfie.IsRedeemable(now) {
		return RiderView{State: selfiedomain.RiderStateExpired}
	}
	// Re-mint the signed URL on every read so the rider always holds a URL
	// valid for the remainder of the window rather than a stale one saved at
	// upload time. If re-signing fails (storage misconfig/outage) we must NOT
	// claim the selfie expired — the bytes are fine; report "unavailable" with
	// a retry affordance instead of falsely blaming the clock or the driver.
	url, err := s.signer.Sign(objectKeyFromURL(selfie.ImageURL), selfie.ExpiresAt)
	if err != nil {
		return RiderView{State: RiderViewStateUnavailable}
	}
	exp := selfie.ExpiresAt
	return RiderView{State: selfiedomain.RiderStateAvailable, URL: url, ExpiresAt: &exp}
}

// RedeemPathForView converts the minted display URL from AttachToRiderView into
// the API redemption route the client should actually call:
//
//	GET /api/v1/selfies/{id}?exp={unix}&sig={hex}
//
// The minted URL embeds the selfie ID in its object key
// ("pre-selfies/{rideID}/{selfieID}.{ext}") and carries exp/sig query params;
// this helper extracts those three values so clients never parse URLs
// themselves. Returns ok=false if the view is not "available" or the URL does
// not match the expected shape — callers then surface the view's honest state
// instead of fabricating a path.
func (s *Service) RedeemPathForView(view RiderView) (path string, ok bool) {
	if view.State != selfiedomain.RiderStateAvailable || view.URL == "" || view.ExpiresAt == nil {
		return "", false
	}
	key := objectKeyFromURL(view.URL)
	base := key
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	ext := ""
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		ext = base[i:]
	}
	idStr := strings.TrimSuffix(base, ext)
	id, err := uuid.Parse(idStr)
	if err != nil {
		return "", false
	}
	qIdx := strings.IndexByte(view.URL, '?')
	if qIdx < 0 {
		return "", false
	}
	params, err := url.ParseQuery(view.URL[qIdx+1:])
	if err != nil {
		return "", false
	}
	sig := params.Get("sig")
	exp := params.Get("exp")
	if sig == "" || exp == "" {
		return "", false
	}
	// Cross-check the embedded expiry against the view's ExpiresAt so a
	// mismatched mint can never be redeemed under a longer window than issued.
	if parsed, err := strconv.ParseInt(exp, 10, 64); err != nil || parsed != view.ExpiresAt.Unix() {
		return "", false
	}
	return fmt.Sprintf("/api/v1/selfies/%s?exp=%s&sig=%s", id, exp, sig), true
}

// RedeemResult carries the selfie aggregate plus its bytes and the ride ID so
// the HTTP layer can serve image content after a successful redemption.
type RedeemResult struct {
	Selfie selfiedomain.PreSelfie
	RideID uuid.UUID
	Data   []byte
}

// Redeem validates a rider's access to a stored selfie given the signed-URL
// parameters, enforcing participant gating + expiry + signature in one path.
// Every failure mode returns a typed error; none of them fall open.
func (s *Service) Redeem(selfieID uuid.UUID, expiresAt time.Time, viewerID uuid.UUID, signature string) (*RedeemResult, error) {
	selfie, err := s.repo.FindByID(selfieID)
	if err != nil {
		return nil, err // ErrNotFound passes through
	}
	if !s.now().Before(selfie.ExpiresAt) || !expiresAt.Equal(selfie.ExpiresAt) {
		return nil, ErrExpired
	}
	// Participant gate: only the rider or the matched driver of this ride may
	// redeem. The lookup fails closed (ErrUnresolvable surfaces as an error).
	riderID, err := s.rides.RideRider(selfie.RideID)
	if err != nil {
		return nil, err
	}
	if viewerID != riderID && viewerID != selfie.DriverID {
		return nil, selfiedomain.ErrForbidden
	}
	path := objectKeyFromURL(selfie.ImageURL)
	if !s.signer.Verify(path, selfie.ExpiresAt, signature) {
		return nil, ErrBadSignature
	}
	data, err := s.fetcher.Get(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", selfiedomain.ErrStorageFailed, err)
	}
	return &RedeemResult{Selfie: *selfie, RideID: selfie.RideID, Data: data}, nil
}

func extFor(contentType string) string {
	switch contentType {
	case "image/png":
		return "png"
	case "image/webp":
		return "webp"
	default:
		return "jpg"
	}
}

// objectKeyFromURL extracts the storage path from a minted URL. LocalFile
// signs paths of the form "<base>/objects/<key>?..."; we store the raw key on
// the aggregate's URL only for display, so signing uses the key deterministically.
func objectKeyFromURL(u string) string {
	// Keys are embedded after "/objects/" up to the query string.
	const marker = "/objects/"
	start := 0
	for i := 0; i+len(marker) <= len(u); i++ {
		if u[i:i+len(marker)] == marker {
			start = i + len(marker)
			break
		}
	}
	if start == 0 {
		return u
	}
	end := len(u)
	for i := start; i < len(u); i++ {
		if u[i] == '?' {
			end = i
			break
		}
	}
	return u[start:end]
}
