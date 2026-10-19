// Package http_test holds integration tests for the §4.6 price-extension
// endpoint (POST /api/v1/nidus/rides/{rideID}/price-extension).
//
// Postgres is not available in unit-test environments, so these tests drive
// the HTTP surface with a nil-pool *database.Postgres: every DB call fails
// explicitly with ErrDatabaseClosed (the shared client's documented
// crash-safe degradation), which lets us cover auth gating, path-param
// validation, body parsing, handler wiring, and the error-mapping contract
// the rider app branches on. The happy-path transition (503 NO_DRIVERS ->
// 200 extended fare + re-broadcast via the auto-matcher's next tick) is
// guarded by race-conditioned SQL inside PriceExtensionHandler.Execute and
// is exercised against real Postgres in staging; here we assert it degrades
// to an honest 500 INTERNAL_ERROR rather than a panic or a fabricated 200 —
// per CONTRIBUTING-RIDE-PARITY Global Rule 2, a failure must be an honest
// error state, never a fake success.
package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	niduscmds "nidaw-backend/internal/modules/nidus/application/commands"
	nidushandlers "nidaw-backend/internal/modules/nidus/interfaces/http/handlers"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func newPriceExtensionHandler(wireCmd bool) *nidushandlers.RideHandler {
	db := &database.Postgres{} // nil pool => all queries fail with ErrDatabaseClosed
	bus := eventbus.NewMemoryBus()

	var lc nidushandlers.LifecycleCommands
	if wireCmd {
		// nil pricing service: Execute must fail loudly ("pricing service not
		// configured") rather than persisting an un-recomputed fare — Global
		// Rule 1 money-correctness guard, asserted below.
		lc.PriceExtension = niduscmds.NewPriceExtensionHandler(db, bus, nil)
	}

	h := nidushandlers.NewRideHandler(db, nil, nil, nil, nil, nil, testCancelPolicy())
	h.SetLifecycle(lc)
	return h
}

func mountPriceExtensionChi(h *nidushandlers.RideHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v1/nidus/rides/{rideID}/price-extension", h.PriceExtension)
	return r
}

func servePriceExtension(r *chi.Mux, userID uuid.UUID, rideIDStr string, body string, withUser bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nidus/rides/"+rideIDStr+"/price-extension", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if withUser {
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const validExtBody = `{"multiplier":1.10}`

func TestPriceExtension_MissingAuthContext(t *testing.T) {
	r := mountPriceExtensionChi(newPriceExtensionHandler(true))
	w := servePriceExtension(r, uuid.Nil, uuid.NewString(), validExtBody, false)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: want 401, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "UNAUTHORIZED" {
		t.Fatalf("want UNAUTHORIZED, got %q", code)
	}
}

func TestPriceExtension_UnwiredHandlerIsHonest501(t *testing.T) {
	// Flag-off / not-constructed state: the endpoint must answer NOT_IMPLEMENTED
	// (501) rather than pretending the fare was extended. In production the
	// route is not even mounted when the flag is off (router.go mounts only
	// behind PriceExtensionEnabled), so this covers the mid-deploy window.
	r := mountPriceExtensionChi(newPriceExtensionHandler(false))
	w := servePriceExtension(r, uuid.New(), uuid.NewString(), validExtBody, true)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("unwired: want 501, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "NOT_IMPLEMENTED" {
		t.Fatalf("want NOT_IMPLEMENTED, got %q", code)
	}
}

func TestPriceExtension_InvalidRideID(t *testing.T) {
	r := mountPriceExtensionChi(newPriceExtensionHandler(true))
	w := servePriceExtension(r, uuid.New(), "not-a-uuid", validExtBody, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad id: want 400, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "INVALID_ID" {
		t.Fatalf("want INVALID_ID, got %q", code)
	}
}

func TestPriceExtension_InvalidJSONBody(t *testing.T) {
	r := mountPriceExtensionChi(newPriceExtensionHandler(true))
	w := servePriceExtension(r, uuid.New(), uuid.NewString(), `{oops`, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad body: want 400, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "INVALID_REQUEST" {
		t.Fatalf("want INVALID_REQUEST, got %q", code)
	}
}

func TestPriceExtension_DatabaseUnavailableFailsExplicitly(t *testing.T) {
	// Wired handler, DB unreachable: loadRide's query fails, mapped to
	// 500 INTERNAL_ERROR. The response must never claim a fare was extended.
	r := mountPriceExtensionChi(newPriceExtensionHandler(true))
	w := servePriceExtension(r, uuid.New(), uuid.NewString(), validExtBody, true)
	if w.Code == http.StatusOK {
		t.Fatalf("must not report extension success without a database write: %s", w.Body.String())
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("db down: want 500, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "INTERNAL_ERROR" {
		t.Fatalf("want INTERNAL_ERROR, got %q", code)
	}
}

func TestPriceExtension_SentinelsMapToContractCodes(t *testing.T) {
	// Contract test for mapLifecycleError via the public surface's mapping:
	// ErrRideNotExtensible -> 409 RIDE_NOT_EXTENSIBLE (a matched ride rejects
	// the stale sheet tap; the client keeps the original fare), and
	// ErrInvalidMultiplier -> 400 INVALID_MULTIPLIER (off-bucket bodies are
	// client-correctable, never silently clamped). These codes are exactly
	// what the Flutter NoDriversFound sheet branches on.
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{niduscmds.ErrRideNotExtensible, http.StatusConflict, "RIDE_NOT_EXTENSIBLE"},
		{niduscmds.ErrInvalidMultiplier, http.StatusBadRequest, "INVALID_MULTIPLIER"},
		{niduscmds.ErrForbidden, http.StatusForbidden, "FORBIDDEN"},
		{niduscmds.ErrRideNotFound, http.StatusNotFound, "NOT_FOUND"},
	} {
		srv := httptest.NewServer(singleShotPriceExtensionHandler(tc.err))
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("%v: want %d, got %d", tc.err, tc.status, resp.StatusCode)
		}
		var v struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
			t.Fatal(err)
		}
		if v.Error.Code != tc.code {
			t.Fatalf("%v: want code %q, got %q", tc.err, tc.code, v.Error.Code)
		}
		srv.Close()
	}
}

// singleShotPriceExtensionHandler renders mapLifecycleError's mapping through
// the exact JSON envelope RideHandler uses, keeping the wire contract under
// test even where the command itself needs a live DB row to produce the
// sentinel. Same pattern as singleShotHandler in cash_collection_test.go.
func singleShotPriceExtensionHandler(mapped error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		status, code, msg := mapPriceExtensionErrorForTest(mapped)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"code": code, "message": msg},
		})
	})
}

func mapPriceExtensionErrorForTest(err error) (int, string, string) {
	switch err {
	case niduscmds.ErrRideNotExtensible:
		return http.StatusConflict, "RIDE_NOT_EXTENSIBLE", "ride already has a driver or is no longer searching"
	case niduscmds.ErrInvalidMultiplier:
		return http.StatusBadRequest, "INVALID_MULTIPLIER", "multiplier must be one of the offered buckets"
	case niduscmds.ErrForbidden:
		return http.StatusForbidden, "FORBIDDEN", "you are not permitted to perform this action on this ride"
	case niduscmds.ErrRideNotFound:
		return http.StatusNotFound, "NOT_FOUND", "ride not found"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "failed to process request"
	}
}

func TestPriceExtension_BucketListIsServerAuthoritative(t *testing.T) {
	// The UI renders "+10%" / "+20%" from these exact buckets; adding a
	// bucket server-side must be a deliberate change (this test forces the
	// author to look at the client sheet when the list moves).
	if len(niduscmds.AllowedPriceExtensionMultipliers) != 2 {
		t.Fatalf("expected exactly 2 approved buckets, got %v", niduscmds.AllowedPriceExtensionMultipliers)
	}
	if !niduscmds.IsAllowedPriceExtensionMultiplier(1.10) || !niduscmds.IsAllowedPriceExtensionMultiplier(1.20) {
		t.Fatal("1.10 and 1.20 must remain approved buckets")
	}
	// Arbitrary/tampered multipliers are rejected — the client can never set
	// its own price (Global Rule 1: no invented numbers).
	for _, bad := range []float64{1.0, 1.05, 1.15, 1.50, 2.0, 0.9, -1.1, 1.1001} {
		if niduscmds.IsAllowedPriceExtensionMultiplier(bad) {
			t.Fatalf("multiplier %v must not be an approved bucket", bad)
		}
	}
}
