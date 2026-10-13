package infrastructure

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Storage errors surfaced to callers. These are real errors; the service maps
// them to 503s and never fabricates a URL when storage is unavailable.
var (
	ErrNoPublicBaseURL = errors.New("selfie storage: public base URL not configured; signed URLs cannot be issued")
	ErrObjectNotFound  = errors.New("selfie storage: object not found")
)

// LocalFileStorage stores selfie images on the local filesystem and issues
// HMAC-SHA256-signed, time-limited public URLs for them. It implements both
// the application.ImageStorage and application.SignedURLIssuer ports.
//
// This is the dev/staging default. In production, swap in an S3/GCS-backed
// implementation of the same two interfaces without touching handlers or the
// service — the ports exist precisely so the concrete provider can change.
type LocalFileStorage struct {
	dir           string
	publicBaseURL string // e.g. https://cdn.nidus.example/selfies — no trailing slash
	key           []byte // HMAC signing key
	ttl           time.Duration
	mu            sync.RWMutex
}

// NewLocalFileStorage creates a storage rooted at dir. If dir is empty it
// defaults to ./data/selfies relative to the process working directory. The
// signing key should come from configuration (SELFIE_SIGNING_KEY); if it is
// empty a random per-process key is generated — that is fine for single-node
// dev but must be set explicitly in multi-node deployments because every node
// needs the same key to verify URLs minted by another node.
func NewLocalFileStorage(dir, publicBaseURL, signingKeyHex string, ttl time.Duration) (*LocalFileStorage, error) {
	if dir == "" {
		dir = filepath.Join("data", "selfies")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("selfie storage: create dir: %w", err)
	}
	publicBaseURL = strings.TrimRight(publicBaseURL, "/")

	var key []byte
	if signingKeyHex != "" {
		decoded, err := hex.DecodeString(signingKeyHex)
		if err != nil {
			return nil, fmt.Errorf("selfie storage: invalid SELFIE_SIGNING_KEY (expected hex): %w", err)
		}
		if len(decoded) < 16 {
			return nil, errors.New("selfie storage: SELFIE_SIGNING_KEY must decode to at least 16 bytes")
		}
		key = decoded
	} else {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("selfie storage: generate signing key: %w", err)
		}
	}
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &LocalFileStorage{
		dir:           dir,
		publicBaseURL: publicBaseURL,
		key:           key,
		ttl:           ttl,
	}, nil
}

// Put stores the bytes under the given key and returns the stored path. It
// implements application.ImageStorage.Put. The service layer has already
// enforced size and content-type limits before invoking Put; storage failures
// return real errors (never a fake path).
func (s *LocalFileStorage) Put(key string, contentType string, data []byte) (string, error) {
	_ = contentType // extension is chosen by the service via objectKey; kept in port for S3 metadata
	if key == "" {
		return "", errors.New("selfie storage: object key required")
	}
	// Defense against path traversal: keys are opaque tokens we mint, but never
	// let a crafted key escape the storage root.
	clean := filepath.Clean("/" + key)
	full := filepath.Join(s.dir, clean)
	root := filepath.Clean(s.dir)
	if !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", errors.New("selfie storage: object key escapes storage root")
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return "", err
	}
	tmp := full + ".tmp-" + uuid.NewString()
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, full); err != nil { // atomic publish
		return "", err
	}
	return clean, nil
}

// Sign mints a short-lived, tamper-evident URL for the stored object. Format:
//
//	{base}/objects/{storedPath}?exp={unix}&sig={hex(hmac)}
//
// where the HMAC payload is "storedPath|expUnix" — i.e. signed over the raw
// storage key, NOT the display URL. The "/objects/" segment in the URL is a
// marker the application layer's objectKeyFromURL uses to recover the exact
// key for verification; keeping the signature bound to the canonical key
// means minting and verifying can never disagree about what was signed.
// Returns ErrNoPublicBaseURL when the base URL is unconfigured — the service
// surfaces that as 503 rather than handing out a fake or unusable link.
// Implements SignedURLIssuer.
func (s *LocalFileStorage) Sign(storedPath string, expiresAt time.Time) (string, error) {
	s.mu.RLock()
	base := s.publicBaseURL
	s.mu.RUnlock()

	if base == "" {
		return "", ErrNoPublicBaseURL
	}
	key := strings.TrimLeft(filepath.Clean("/"+storedPath), "/")
	sig := s.computeSig(key, expiresAt)
	u := fmt.Sprintf("%s/objects/%s?exp=%d&sig=%s", base, key, expiresAt.Unix(), sig)
	return u, nil
}

// Verify checks a supplied signature/expiry pair against storedPath. It fails
// closed on any mismatch or expiry. Implements SignedURLIssuer.Verify (bool).
func (s *LocalFileStorage) Verify(storedPath string, expiresAt time.Time, signature string) bool {
	want := s.computeSig(storedPath, expiresAt)
	// Constant-time comparison — never short-circuit on prefix equality.
	if subtle.ConstantTimeCompare([]byte(want), []byte(strings.ToLower(signature))) != 1 {
		return false
	}
	if time.Now().After(expiresAt) {
		return false
	}
	return true
}

// SetTTL adjusts the signed-URL lifetime (used by tests and config reloads).
func (s *LocalFileStorage) SetTTL(ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ttl = ttl
}

// TTL returns the current signed-URL lifetime.
func (s *LocalFileStorage) TTL() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ttl
}

// Get reads back an object's bytes (participant-gated serving path).
func (s *LocalFileStorage) Get(storedPath string) ([]byte, error) {
	clean := filepath.Clean("/" + storedPath)
	full := filepath.Join(s.dir, clean)
	b, err := os.ReadFile(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrObjectNotFound
	}
	return b, err
}

func (s *LocalFileStorage) computeSig(storedPath string, expiresAt time.Time) string {
	mac := hmac.New(sha256.New, s.key)
	fmt.Fprintf(mac, "%s|%d", storedPath, expiresAt.Unix())
	return hex.EncodeToString(mac.Sum(nil))
}
