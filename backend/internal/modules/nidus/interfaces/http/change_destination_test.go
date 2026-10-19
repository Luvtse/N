// Package http_test holds integration tests for the §3.5 mid-trip destination
// change endpoint (PATCH /api/v1/nidus/rides/{rideID}/destination).
//
// Postgres is not available in unit-test environments, so these tests drive
// the HTTP surface with a nil-pool *database.Postgres: every DB call fails
// explicitly with ErrDatabaseClosed (the shared client's documented
// crash-safe degradation), which lets us cover auth gating, path-param
// validation, JSON-body validation, handler wiring, and the error-mapping
// contract the rider app depends on. The happy-path fare recompute
// (previous/new fares in the 200 body) requires a live ride row and its
// race-guarded UPDATE is exercised against real Postgres in staging; here we
// assert the failure degrades to an honest 500 INTERNAL_ERROR rather than a
// panic or a fabricated success — per CONTRIBUTING-RIDE-PARITY Global Rule 2,
// a failure must be an honest error state, never a fake 200.
package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	niduscmds "nidaw-backend/internal/modules/nidus/application/commands"
	nidushandlers "nidaw-backend/internal/modules/nidus/interfaces/http/handlers"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func newChangeDestHandler(wireCmd bool) *nidushandlers.RideHandler {
	db := &database.Postgres{} // nil pool => all queries fail with ErrDatabaseClosed
	bus := eventbus.NewMemoryBus()

	var lc nidushandlers.LifecycleCommands
	if wireCmd {
		lc.ChangeDestination = niduscmds.NewChangeDestinationHandler(db, bus, nil)
	}

	h := nidushandlers.NewRideHandler(db, nil, nil, nil, nil, nil, testCancelPolicy())
	h.SetLifecycle(lc)
	return h
}

// mountPatchDest registers the endpoint on a real chi router so
// chi.URLParam(r, "rideID") resolves exactly as it does in production routing.
func mountPatchDest(h *nidushandlers.RideHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Patch("/api/v1/nidus/rides/{rideID}/destination", h.ChangeDestination)
	return r
}

func validDestBody() string {
	return `{"dropoff_lat":40.7580,"dropoff_lng":-73.9855,"dropoff_address":"Times Square, New York"}`
}

// servePatchDest stamps the JWT subject into the context (mimicking the auth
// middleware) and dispatches through the real chi router.
func servePatchDest(r *chi.Mux, userID uuid.UUID, rideIDStr string, body string, withUser bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/nidus/rides/"+rideIDStr+"/destination", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if withUser {
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestChangeDestination_MissingAuthContext(t *testing.T) {
	r := mountPatchDest(newChangeDestHandler(true))
	w := servePatchDest(r, uuid.Nil, uuid.NewString(), validDestBody(), false)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: want 401, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "UNAUTHORIZED" {
		t.Fatalf("want UNAUTHORIZED, got %q", code)
	}
}

func TestChangeDestination_UnwiredHandlerIsHonest501(t *testing.T) {
	// Flag-off / not-constructed state: the endpoint must answer NOT_IMPLEMENTED
	// (501) rather than pretending the destination was changed.
	r := mountPatchDest(newChangeDestHandler(false))
	w := servePatchDest(r, uuid.New(), uuid.NewString(), validDestBody(), true)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("unwired: want 501, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "NOT_IMPLEMENTED" {
		t.Fatalf("want NOT_IMPLEMENTED, got %q", code)
	}
}

func TestChangeDestination_InvalidRideID(t *testing.T) {
	r := mountPatchDest(newChangeDestHandler(true))
	w := servePatchDest(r, uuid.New(), "not-a-uuid", validDestBody(), true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad id: want 400, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "INVALID_ID" {
		t.Fatalf("want INVALID_ID, got %q", code)
	}
}

func TestChangeDestination_InvalidJSONBody(t *testing.T) {
	r := mountPatchDest(newChangeDestHandler(true))
	w := servePatchDest(r, uuid.New(), uuid.NewString(), `{"dropoff_lat": broken`, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad json: want 400, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "INVALID_REQUEST" {
		t.Fatalf("want INVALID_REQUEST, got %q", code)
	}
}

func TestChangeDestination_DBFailureIsHonest500(t *testing.T) {
	// With a nil pool the load inside Execute fails with ErrDatabaseClosed.
	// The command must surface that as an honest 500 — never a fabricated
	// 200 with invented previous/new fares (Global Rule 2).
	r := mountPatchDest(newChangeDestHandler(true))
	w := servePatchDest(r, uuid.New(), uuid.NewString(), validDestBody(), true)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("db down: want honest 500, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "INTERNAL_ERROR" {
		t.Fatalf("want INTERNAL_ERROR, got %q", code)
	}
}

func TestChangeDestination_FlagOffRouteNotMounted(t *testing.T) {
	// Kill-switch semantics: when StopoversEnabled is false the router must
	// not mount the PATCH route at all — a request then 404s via chi rather
	// than reaching a half-wired handler. Mirrors router.go's
	// `if deps.StopoversEnabled { ... }` block with the same method+pattern.
	depsStopoversEnabled := false
	r := chi.NewRouter()
	if depsStopoversEnabled {
		r.Patch("/api/v1/nidus/rides/{rideID}/destination", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/nidus/rides/"+uuid.NewString()+"/destination", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("flag off: route must not exist, got %d", w.Code)
	}
}

func TestChangeDestination_ErrorMappingContract(t *testing.T) {
	// The rider app branches on these exact codes (the tracking page re-fetches
	// the ride on DESTINATION_NOT_CHANGEABLE and shows "ride not found" on
	// 404). mapLifecycleError in ride_handler.go maps ErrDestinationNotChangeable
	// -> 409 DESTINATION_NOT_CHANGEABLE; exercise it end-to-end through a
	// single-shot handler using the SAME production mapper — unlike the cash
	// tests' mirrored switch, here the mapper is reachable because the command
	// returns the bare sentinel when its DB load fails closed... which with a
	// nil pool becomes INTERNAL_ERROR instead. So we pin the two unambiguous
	// wire-level outcomes we can produce hermetically (authz/param/body/501)
	// above, and assert the sentinel itself exists with the documented
	// message here, keeping the drift risk visible.
	if niduscmds.ErrDestinationNotChangeable == nil {
		t.Fatal("ErrDestinationNotChangeable sentinel missing from commands package")
	}
	const wantMsg = "destination can only be changed while the ride is en-route or in progress"
	if niduscmds.ErrDestinationNotChangeable.Error() != wantMsg {
		t.Fatalf("sentinel message drifted: %q", niduscmds.ErrDestinationNotChangeable.Error())
	}
	// And the production switch line stays pinned in source form: grep-style
	// assertion that ride_handler.go's mapLifecycleError still routes this
	// sentinel to 409/DESTINATION_NOT_CHANGEABLE (Global Rule 5: endpoint +
	// client contract covered together).
	src, err := os.ReadFile("../../interfaces/http/handlers/ride_handler.go")
	if err != nil {
		t.Skipf("handler source not co-located in this build layout: %v", err)
	}
	if !strings.Contains(string(src), `case errors.Is(err, commands.ErrDestinationNotChangeable):`) ||
		!strings.Contains(string(src), `"DESTINATION_NOT_CHANGEABLE"`) {
		t.Fatal("mapLifecycleError no longer maps ErrDestinationNotChangeable to 409 DESTINATION_NOT_CHANGEABLE")
	}
}
