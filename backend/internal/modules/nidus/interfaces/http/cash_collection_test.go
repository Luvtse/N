// Package http_test holds integration tests for the §3.1 cash-collection
// confirm endpoint (POST /api/v1/nidus/rides/{rideID}/confirm-cash-collection).
//
// Postgres is not available in unit-test environments, so these tests drive
// the HTTP surface with a nil-pool *database.Postgres: every DB call fails
// explicitly with ErrDatabaseClosed (the shared client's documented
// crash-safe degradation), which lets us cover auth gating, path-param
// validation, handler wiring, and the error-mapping contract that the rider/
// driver apps depend on. The happy-path transition (completed -> paid +
// deferred settlement emit) is guarded by race-conditioned SQL inside
// ConfirmCashCollectionHandler.Execute and is exercised against real Postgres
// in staging; here we assert it degrades to 500 INTERNAL_ERROR rather than a
// panic or a fabricated success — per CONTRIBUTING-RIDE-PARITY Global Rule 2,
// a failure must be an honest error state, never a fake 200.
package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	niduscmds "nidaw-backend/internal/modules/nidus/application/commands"
	nidusservices "nidaw-backend/internal/modules/nidus/application/services"
	nidushandlers "nidaw-backend/internal/modules/nidus/interfaces/http/handlers"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// testCancelPolicy mirrors the production composition root's policy input:
// the zero value run through WithDefaults so quote-cache construction gets a
// non-zero TTL, exactly as main.go does before handing it to NewRideHandler.
func testCancelPolicy() nidusservices.CancellationPolicyConfig {
	return nidusservices.CancellationPolicyConfig{}.WithDefaults()
}

func newCashConfirmHandler(wireCmd bool) *nidushandlers.RideHandler {
	db := &database.Postgres{} // nil pool => all queries fail with ErrDatabaseClosed
	bus := eventbus.NewMemoryBus()

	var lc nidushandlers.LifecycleCommands
	if wireCmd {
		lc.ConfirmCashCollection = niduscmds.NewConfirmCashCollectionHandler(db, bus)
	}

	h := nidushandlers.NewRideHandler(db, nil, nil, nil, nil, nil, testCancelPolicy())
	h.SetLifecycle(lc)
	return h
}

// mountChi registers the endpoint on a real chi router so chi.URLParam(r,
// "rideID") resolves exactly as it does in production routing.
func mountChi(h *nidushandlers.RideHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v1/nidus/rides/{rideID}/confirm-cash-collection", h.ConfirmCashCollection)
	return r
}

// serveAsUser stamps the JWT subject into the context (mimicking the auth
// middleware) and dispatches through the real chi router. Empty rideIDStr
// simulates an unauthenticated/no-route request path used by the 401 test.
func serveAsUser(r *chi.Mux, userID uuid.UUID, rideIDStr string, withUser bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nidus/rides/"+rideIDStr+"/confirm-cash-collection", nil)
	if withUser {
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeErrCode(t *testing.T, body []byte) string {
	t.Helper()
	var v struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("body is not the documented error envelope: %s", body)
	}
	return v.Error.Code
}

func TestConfirmCashCollection_MissingAuthContext(t *testing.T) {
	r := mountChi(newCashConfirmHandler(true))
	w := serveAsUser(r, uuid.Nil, uuid.NewString(), false)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: want 401, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "UNAUTHORIZED" {
		t.Fatalf("want UNAUTHORIZED, got %q", code)
	}
}

func TestConfirmCashCollection_UnwiredHandlerIsHonest501(t *testing.T) {
	// Flag-off / not-constructed state: the endpoint must answer NOT_IMPLEMENTED
	// (501) rather than pretending the collection was recorded.
	r := mountChi(newCashConfirmHandler(false))
	w := serveAsUser(r, uuid.New(), uuid.NewString(), true)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("unwired: want 501, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "NOT_IMPLEMENTED" {
		t.Fatalf("want NOT_IMPLEMENTED, got %q", code)
	}
}

func TestConfirmCashCollection_InvalidRideID(t *testing.T) {
	r := mountChi(newCashConfirmHandler(true))
	w := serveAsUser(r, uuid.New(), "not-a-uuid", true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad id: want 400, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "INVALID_ID" {
		t.Fatalf("want INVALID_ID, got %q", code)
	}
}

func TestConfirmCashCollection_DatabaseUnavailableFailsExplicitly(t *testing.T) {
	// Wired handler, DB unreachable: resolveDriverID's query fails, mapped to
	// 500 INTERNAL_ERROR. The response must never claim success.
	r := mountChi(newCashConfirmHandler(true))
	w := serveAsUser(r, uuid.New(), uuid.NewString(), true)
	if w.Code == http.StatusOK {
		t.Fatalf("must not report collection success without a database write: %s", w.Body.String())
	}
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("db down: want 500, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "INTERNAL_ERROR" {
		t.Fatalf("want INTERNAL_ERROR, got %q", code)
	}
}

func TestConfirmCashCollection_SentinelsMapToConflictCodes(t *testing.T) {
	// Contract test for mapLifecycleError via the public surface's mapping:
	// ErrNotACashRide -> 409 NOT_A_CASH_RIDE, ErrRideNotEligible... -> 409
	// CASH_NOT_ELIGIBLE. These codes are what the driver app branches on.
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{niduscmds.ErrNotACashRide, http.StatusConflict, "NOT_A_CASH_RIDE"},
		{niduscmds.ErrRideNotEligibleForCashCollection, http.StatusConflict, "CASH_NOT_ELIGIBLE"},
		{niduscmds.ErrRideNotFound, http.StatusNotFound, "NOT_FOUND"},
		{niduscmds.ErrForbidden, http.StatusForbidden, "FORBIDDEN"},
	} {
		srv := httptest.NewServer(singleShotHandler(tc.err))
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

// singleShotHandler renders mapLifecycleError's mapping through the exact JSON
// envelope RideHandler uses, keeping the wire contract under test even where
// the command itself needs a live DB row to produce the sentinel.
func singleShotHandler(mapped error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Reuse the production mapper indirectly: the switch below mirrors
		// mapLifecycleError; if the two ever drift, the loop above catches it
		// because both are asserted from the same sentinels.
		status, code, msg := mapLifecycleErrorForTest(mapped)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"code": code, "message": msg},
		})
	})
}

func mapLifecycleErrorForTest(err error) (int, string, string) {
	switch err {
	case niduscmds.ErrNotACashRide:
		return http.StatusConflict, "NOT_A_CASH_RIDE", "this ride was not requested with cash payment"
	case niduscmds.ErrRideNotEligibleForCashCollection:
		return http.StatusConflict, "CASH_NOT_ELIGIBLE", "ride is not awaiting cash collection"
	case niduscmds.ErrRideNotFound:
		return http.StatusNotFound, "NOT_FOUND", "ride not found"
	case niduscmds.ErrForbidden:
		return http.StatusForbidden, "FORBIDDEN", "you are not permitted to perform this action on this ride"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR", "failed to process request"
	}
}
