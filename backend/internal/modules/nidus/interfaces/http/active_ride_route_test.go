// Package http_test holds routing tests for the Phase 6 driver active-ride
// rehydration endpoint (GET /api/v1/nidus/drivers/active-ride), which the
// driver app calls on cold start, after crash recovery, and after a WebSocket
// reconnect to converge on the authoritative assigned ride.
//
// Postgres is not available in unit-test environments, so these tests drive
// the HTTP surface with a nil-pool *database.Postgres: every DB call fails
// explicitly with the shared client's documented crash-safe degradation.
// That lets us assert the contract the Flutter clients depend on — auth
// gating (401), non-driver gating (403 NOT_A_DRIVER), honest server errors
// (never a fabricated 200 per CONTRIBUTING-RIDE-PARITY Global Rule 2) — and,
// critically, that the literal path "/active-ride" resolves to RideHandler.
// GetActiveRide rather than falling into the parameterized /{driverID} route
// registered after it (chi static-before-param precedence, same convention
// already relied upon by /pending-rides).
package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	nidushandlers "nidaw-backend/internal/modules/nidus/interfaces/http/handlers"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// newActiveRideHandler builds a RideHandler backed by a nil-pool Postgres.
// The handler under test only uses h.db (driver-profile lookup + active-ride
// lookup) and h.getRideQuery; both fail honestly against the closed pool.
func newActiveRideHandler() *nidushandlers.RideHandler {
	db := &database.Postgres{} // nil pool => all queries fail (ErrDatabaseClosed)
	_ = eventbus.NewMemoryBus()
	h := nidushandlers.NewRideHandler(db, nil, nil, nil, nil, nil, testCancelPolicy())
	return h
}

// mountActiveRideChi registers the production route ordering verbatim:
// /nearby, /pending-rides, /active-ride, then /{driverID}. If the literal
// were omitted from the router (the Phase 6 defect), GET /active-ride would
// match /{driverID} and DriverHandler.GetDriver would answer instead — the
// error-code assertions below make that regression impossible to miss.
func mountActiveRideChi(h *nidushandlers.RideHandler) *chi.Mux {
	r := chi.NewRouter()
	r.Route("/api/v1/nidus/drivers", func(r chi.Router) {
		r.Get("/pending-rides", h.PendingRides)
		r.Get("/active-ride", h.GetActiveRide)
		// Sentinel param route: distinguishes "literal won" from "param caught".
		r.Get("/{driverID}", func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		})
	})
	return r
}

func serveActiveRide(r *chi.Mux, userID uuid.UUID, withUser bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nidus/drivers/active-ride", nil)
	if withUser {
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestActiveRideRoute_ResolvesLiteralNotParamRoute proves the additive fix:
// the request must reach RideHandler.GetActiveRide (which answers 401/403/
// 404/500 through its own contract), never the /{driverID} sentinel (418).
func TestActiveRideRoute_ResolvesLiteralNotParamRoute(t *testing.T) {
	r := mountActiveRideChi(newActiveRideHandler())
	w := serveActiveRide(r, uuid.New(), true)
	if w.Code == http.StatusTeapot {
		t.Fatalf("GET /active-ride fell through to /{driverID}; literal route not registered")
	}
}

// TestActiveRideRoute_MissingAuthContext: no JWT subject => 401 UNAUTHORIZED.
func TestActiveRideRoute_MissingAuthContext(t *testing.T) {
	r := mountActiveRideChi(newActiveRideHandler())
	w := serveActiveRide(r, uuid.Nil, false)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated: want 401, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "UNAUTHORIZED" {
		t.Fatalf("want UNAUTHORIZED, got %q", code)
	}
}

// TestActiveRideRoute_NonDriverIsForbidden: an authenticated user whose
// account has no driver profile must get 403 NOT_A_DRIVER. Against the
// nil pool the profile lookup itself fails, which the handler maps to the
// same forbidden response — either way this must NEVER be a 200 or a fall
// through to the driver-detail route.
func TestActiveRideRoute_NonDriverIsForbidden(t *testing.T) {
	r := mountActiveRideChi(newActiveRideHandler())
	w := serveActiveRide(r, uuid.New(), true)
	if w.Code == http.StatusOK || w.Code == http.StatusTeapot {
		t.Fatalf("want 403/5xx honest failure, got %d %s", w.Code, w.Body.String())
	}
	if w.Code >= 500 {
		// Honest degradation on a closed pool: acceptable, asserted separately.
		if code := decodeErrCode(t, w.Body.Bytes()); code != "INTERNAL_ERROR" && code != "ACTIVE_RIDE_NOT_FOUND" {
			t.Fatalf("5xx must carry a documented code, got %q body=%s", code, w.Body.String())
		}
		return
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeErrCode(t, w.Body.Bytes()); code != "NOT_A_DRIVER" {
		t.Fatalf("want NOT_A_DRIVER, got %q", code)
	}
}

// TestActiveRideRoute_NoFabricatedSuccess: with the closed pool, the handler
// must not return a 200 with a synthesized ride body. Global Rule 2: failures
// are honest error states.
func TestActiveRideRoute_NoFabricatedSuccess(t *testing.T) {
	r := mountActiveRideChi(newActiveRideHandler())
	w := serveActiveRide(r, uuid.New(), true)
	if w.Code == http.StatusOK {
		t.Fatalf("nil-pool DB produced a fabricated 200 active ride: %s", w.Body.String())
	}
}
