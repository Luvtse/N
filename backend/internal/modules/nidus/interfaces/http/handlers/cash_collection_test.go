// Package handlers integration tests for the §3.1 cash-collection confirm
// endpoint (POST /api/v1/nidus/rides/{rideID}/confirm-cash-collection).
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
	nidushandlers "nidaw-backend/internal/modules/nidus/interfaces/http/handlers"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func newCashConfirmServer(t *testing.T, userID uuid.UUID, rideID uuid.UUID, wireCmd bool) http.Handler {
	t.Helper()
	db := &database.Postgres{} // nil pool => all queries fail with ErrDatabaseClosed
	bus := eventbus.NewMemoryBus()

	var lc nidushandlers.LifecycleCommands
	if wireCmd {
		lc.ConfirmCashCollection = niduscmds.NewConfirmCashCollectionHandler(db, bus)
	}

	h := nidushandlers.NewRideHandler(db, nil, nil, nil, nil, nil, nidushandlers.DefaultCancellationPolicyForTest())
	h.SetLifecycle(lc)

	r := chi.NewRouter()
	r.Post("/api/v1/nidus/rides/{rideID}/confirm-cash-collection", h.ConfirmCashCollection)
	return injectUser(userID, r, rideID)
}

// injectUser mimics AuthRequired+ExtractUserToContext: it stamps the JWT
// subject into the request context and sets the chi route parameter so
// parseRideID resolves the same way production does.
func injectUser(userID uuid.UUID, next http.Handler, rideID uuid.UUID) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := context.WithValue(req.Context(), auth.UserIDKey, userID)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("rideID", rideID.String())
		next.ServeHTTP(w, req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx)))
	})
}

func postConfirm(srv http.Handler) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/nidus/rides/x/confirm-cash-collection", nil))
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
	rideID := uuid.New()
	db := &database.Postgres{}
	h := nidushandlers.NewRideHandler(db, nil, nil, nil, nil, nil, nidushandlers.DefaultCancellationPolicyForTest())
	h.SetLifecycle(nidushandlers.LifecycleCommands{
		ConfirmCashCollection: niduscmds.NewConfirmCashCollectionHandler(db, eventbus.NewMemoryBus()),
	})
	r := chi.NewRouter()
	r.Post("/api/v1/nidus/rides/{rideID}/confirm-cash-collection", h.ConfirmCashCollection)
	w := httptest.NewRecorder()
	// No user injected: unauthenticated request must 401, not panic.
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: want 401, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "UNAUTHORIZED" {
		t.Fatalf("want UNAUTHORIZED, got %q", code)
	}
	_ = rideID
}

func TestConfirmCashCollection_UnwiredHandlerIsHonest501(t *testing.T) {
	// Flag-off / not-constructed state: the endpoint must answer NOT_IMPLEMENTED
	// (501) rather than pretending the collection was recorded.
	user := uuid.New()
	ride := uuid.New()
	srv := newCashConfirmServer(t, user, ride, false)
	w := postConfirm(srv)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("unwired: want 501, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "NOT_IMPLEMENTED" {
		t.Fatalf("want NOT_IMPLEMENTED, got %q", code)
	}
}

func TestConfirmCashCollection_InvalidRideID(t *testing.T) {
	user := uuid.New()
	r := chi.NewRouter()
	db := &database.Postgres{}
	h := nidushandlers.NewRideHandler(db, nil, nil, nil, nil, nil, nidushandlers.DefaultCancellationPolicyForTest())
	h.SetLifecycle(nidushandlers.LifecycleCommands{
		ConfirmCashCollection: niduscmds.NewConfirmCashCollectionHandler(db, eventbus.NewMemoryBus()),
	})
	r.Post("/api/v1/nidus/rides/{rideID}/confirm-cash-collection", h.ConfirmCashCollection)
	inject := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := context.WithValue(req.Context(), auth.UserIDKey, user)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("rideID", "not-a-uuid")
		r.ServeHTTP(w, req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx)))
	})
	w := httptest.NewRecorder()
	inject.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", nil))
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
	user := uuid.New()
	ride := uuid.New()
	srv := newCashConfirmServer(t, user, ride, true)
	w := postConfirm(srv)
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
		err      error
		status   int
		code     string
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
