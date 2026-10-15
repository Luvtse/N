// Package http_test — §3.3 Split Fare HTTP integration tests.
//
// Postgres is not available in unit-test environments, so these tests drive
// the mounted router with a nil-pool *database.Postgres: every DB call fails
// explicitly (the shared client's documented crash-safe degradation), which
// lets us cover the full wiring contract the apps depend on:
//   - route mounting is gated by SplitSvc availability (flag off => 404 absent)
//   - owner routes sit behind AuthRequired (missing JWT => 401)
//   - the public accept route is reachable WITHOUT auth and rejects bad
//     tokens as an indistinguishable 404 (no enumeration oracle)
//   - malformed bodies / path params produce stable wire codes, never panics
//     and never a fabricated success (Global Rule 2).
package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	nidusservices "nidaw-backend/internal/modules/nidus/application/services"
	nidushttp "nidaw-backend/internal/modules/nidus/interfaces/http"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"

	"github.com/google/uuid"
)

// testAuthService mirrors the production composition root's auth service with
// a fixed test secret; tokens minted here validate against the same
// middleware.AuthRequired chain the deployed router uses.
func testAuthService(t *testing.T) *auth.Service {
	t.Helper()
	svc, err := auth.NewService(auth.ServiceConfig{
		JWTSecret:  "split-test-secret",
		AccessTTL:  time.Hour,
		RefreshTTL: 2 * time.Hour,
		Issuer:     "nidus-test",
		Audience:   "nidus-test",
	}, nil)
	if err != nil {
		t.Fatalf("auth service: %v", err)
	}
	return svc
}

func riderToken(t *testing.T, svc *auth.Service) string {
	t.Helper()
	pair, err := svc.GenerateTokenPair(uuid.New(), "rider@test.local", "rider", "")
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	return pair.AccessToken
}

func newSplitRouter(splitWired bool, authSvc *auth.Service) http.Handler {
	deps := &nidushttp.Dependencies{
		DB:          &database.Postgres{}, // nil pool => every query errors honestly
		CORSOrigins: []string{"*"},
		AuthService: authSvc,
	}
	if splitWired {
		deps.SplitSvc = nidusservices.NewSplitService(deps.DB)
	}
	return nidushttp.NewRouter(deps)
}

func TestSplitRoutesUnmountedWhenServiceNil(t *testing.T) {
	svc := testAuthService(t)
	h := newSplitRouter(false, svc)
	authHeader := "Bearer " + riderToken(t, svc)

	// Owner routes must be absent entirely (404), not 405 or a fake empty
	// list. A valid token is supplied so the request reaches the mounting
	// decision rather than stopping at AuthRequired.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nidus/rides/"+uuid.NewString()+"/split", bytes.NewReader([]byte(`{"shares":[]}`)))
	req.Header.Set("Authorization", authHeader)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST split with SplitSvc nil: want 404, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/nidus/rides/"+uuid.NewString()+"/split", nil)
	req.Header.Set("Authorization", authHeader)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET split with SplitSvc nil: want 404, got %d", rec.Code)
	}

	// Public accept likewise unmounted when the service is unwired.
	req = httptest.NewRequest(http.MethodPost, "/api/v1/split/deadbeef/accept", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST accept with SplitSvc nil: want 404, got %d", rec.Code)
	}
}

func TestSplitOwnerRoutesRequireAuth(t *testing.T) {
	h := newSplitRouter(true, testAuthService(t))
	rideID := uuid.NewString()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nidus/rides/"+rideID+"/split", bytes.NewReader([]byte(`{"shares":[{"contact":"a@b.c","share_amount":5}]}`)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated POST split: want 401, got %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/nidus/rides/"+rideID+"/split", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET split: want 401, got %d", rec.Code)
	}
}

func TestSplitCreateRejectsBogusToken(t *testing.T) {
	h := newSplitRouter(true, testAuthService(t))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nidus/rides/"+uuid.NewString()+"/split", bytes.NewReader([]byte(`{"shares":`)))
	req.Header.Set("Authorization", "Bearer not-a-real-jwt")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// Auth middleware runs first; a bogus token must be rejected before body
	// parsing, and must be an honest 401 rather than a 500 panic path.
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bogus bearer token: want 401, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSplitCreateWithValidTokenFailsHonestlyWithoutDB(t *testing.T) {
	svc := testAuthService(t)
	h := newSplitRouter(true, svc)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nidus/rides/"+uuid.NewString()+"/split",
		bytes.NewReader([]byte(`{"shares":[{"contact":"friend@example.com","share_amount":5.0}]}`)))
	req.Header.Set("Authorization", "Bearer "+riderToken(t, svc))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// Nil pool => Create cannot load the ride. Per Global Rule 2 this must be
	// an explicit error envelope (SPLIT_NOT_FOUND for unknown/unreadable ride
	// or SPLIT_FAILED/INTERNAL for pool errors) — never a 200/201.
	if rec.Code == http.StatusOK || rec.Code == http.StatusCreated {
		t.Fatalf("nil-pool create must not fabricate success, got %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Code < 400 {
		t.Fatalf("want 4xx/5xx honest failure, got %d body=%s", rec.Code, rec.Body.String())
	}
	if code := wireCode(t, rec.Body.Bytes()); code == "" {
		t.Fatalf("error envelope missing stable code: %s", rec.Body.String())
	}
}

func TestSplitAcceptPublicRouteBadTokenIs404(t *testing.T) {
	h := newSplitRouter(true, testAuthService(t))

	// No Authorization header at all: the token IS the authorization, so this
	// must reach the handler and fail as SPLIT_NOT_FOUND (nil pool cannot find
	// any row) — never a 401, never a 200.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/split/"+longToken()+"/accept", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("public accept with unknown token: want 404, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := wireCode(t, rec.Body.Bytes()); got != "SPLIT_NOT_FOUND" {
		t.Fatalf("want SPLIT_NOT_FOUND wire code, got %v", got)
	}
}

func TestSplitAcceptEmptyTokenIs404(t *testing.T) {
	h := newSplitRouter(true, testAuthService(t))
	// chi will not match an empty segment; use a whitespace-only token to
	// exercise the service-side trim/length guard instead.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/split/%20/accept", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("whitespace token: want 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// wireCode unwraps the shared error envelope {"error":{"code":...,"message":...}}.
func wireCode(t *testing.T, raw []byte) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("error body not JSON: %v (%s)", err, string(raw))
	}
	return env.Error.Code
}

// longToken exceeds the service's 64-char length cap, proving the guard fires
// BEFORE any DB touch (cheap rejection, no oracle on token shape).
func longToken() string {
	return uuid.NewString() + uuid.NewString() // 72 chars > 64 cap
}
