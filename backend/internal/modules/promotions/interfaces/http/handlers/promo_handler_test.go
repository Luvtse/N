// Package handlers_test holds integration tests for the §3.2 promotions HTTP
// surface (POST /promotions/redeem, GET /promotions/mine) driven against the
// in-memory repository over a real chi router with the auth-context stamping
// the production middleware performs. Postgres is unavailable in unit-test
// environments; per CONTRIBUTING-RIDE-PARITY Global Rule 2 every failure path
// must be an honest wire error (stable code + non-empty message), never a
// fabricated success or a silent full-fare fallback.
package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	promoapp "nidaw-backend/internal/modules/promotions/application"
	promodomain "nidaw-backend/internal/modules/promotions/domain"
	promohttp "nidaw-backend/internal/modules/promotions/interfaces/http/handlers"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/eventbus"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func newPromoRouter() (*chi.Mux, *promoapp.Service, *promoapp.InMemoryRepository) {
	repo := promoapp.NewInMemoryRepository()
	svc := promoapp.NewService(repo, eventbus.NewMemoryBus())
	h := promohttp.NewHandler(svc)
	r := chi.NewRouter()
	r.Post("/api/v1/promotions/redeem", h.Redeem)
	r.Get("/api/v1/promotions/mine", h.Mine)
	return r, svc, repo
}

func doAsUser(r *chi.Mux, method, path string, body interface{}, userID uuid.UUID, withUser bool) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if withUser {
		req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodePromoErr(t *testing.T, body []byte) (int, string, string) {
	t.Helper()
	var v struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("body is not the documented error envelope: %s", body)
	}
	return 0, v.Error.Code, v.Error.Message
}

func validCode() promodomain.PromoCode {
	return promodomain.PromoCode{
		Code:      "RIDE20",
		Type:      promodomain.CodeTypePercent,
		Value:     20,
		MinFare:   50,
		ValidFrom: time.Now().UTC().Add(-time.Hour),
	}
}

func TestPromoRedeem_MissingAuthIs401(t *testing.T) {
	r, _, _ := newPromoRouter()
	w := doAsUser(r, http.MethodPost, "/api/v1/promotions/redeem", map[string]interface{}{"code": "RIDE20", "fare": 100}, uuid.Nil, false)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d %s", w.Code, w.Body.String())
	}
	if _, code, _ := decodePromoErr(t, w.Body.Bytes()); code != "UNAUTHORIZED" {
		t.Fatalf("want UNAUTHORIZED, got %q", code)
	}
	w = doAsUser(r, http.MethodGet, "/api/v1/promotions/mine", nil, uuid.Nil, false)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("mine unauthenticated: want 401, got %d", w.Code)
	}
}

func TestPromoRedeem_BadRequestsAreHonestErrors(t *testing.T) {
	r, svc, _ := newPromoRouter()
	ctx := context.Background()
	userID := uuid.New()
	if _, err := svc.Grant(ctx, validCode()); err != nil {
		t.Fatalf("grant: %v", err)
	}

	// Malformed JSON.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/promotions/redeem", bytes.NewBufferString("{not-json"))
	req = req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON: want 400, got %d %s", w.Code, w.Body.String())
	}

	// Missing code.
	w = doAsUser(r, http.MethodPost, "/api/v1/promotions/redeem", map[string]interface{}{"fare": 100}, userID, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing code: want 400, got %d %s", w.Code, w.Body.String())
	}

	// Non-positive fare cannot project a discount.
	w = doAsUser(r, http.MethodPost, "/api/v1/promotions/redeem", map[string]interface{}{"code": "RIDE20", "fare": 0}, userID, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("zero fare: want 400, got %d %s", w.Code, w.Body.String())
	}
}

func TestPromoRedeem_DomainErrorsMapToStableWireCodes(t *testing.T) {
	r, svc, _ := newPromoRouter()
	ctx := context.Background()
	userID := uuid.New()
	if _, err := svc.Grant(ctx, validCode()); err != nil {
		t.Fatalf("grant: %v", err)
	}
	expired := validCode()
	expired.Code = "OLD"
	to := time.Now().UTC().Add(-time.Hour)
	expired.ValidTo = &to
	if _, err := svc.Grant(ctx, expired); err != nil {
		t.Fatalf("grant expired: %v", err)
	}

	cases := []struct {
		name       string
		code       string
		fare       float64
		wantStatus int
		wantCode   string
	}{
		{"unknown code", "NOPE", 100, http.StatusNotFound, "PROMO_NOT_FOUND"},
		{"expired code", "OLD", 100, http.StatusConflict, "PROMO_EXPIRED"},
		{"below min fare", "RIDE20", 40, http.StatusUnprocessableEntity, "PROMO_MIN_FARE"},
	}
	for _, tc := range cases {
		w := doAsUser(r, http.MethodPost, "/api/v1/promotions/redeem",
			map[string]interface{}{"code": tc.code, "fare": tc.fare, "currency": "ETB"}, userID, true)
		if w.Code != tc.wantStatus {
			t.Errorf("%s: want %d, got %d %s", tc.name, tc.wantStatus, w.Code, w.Body.String())
			continue
		}
		if _, code, msg := decodePromoErr(t, w.Body.Bytes()); code != tc.wantCode || msg == "" {
			t.Errorf("%s: want wire code %q with message, got %q/%q", tc.name, tc.wantCode, code, msg)
		}
	}
}

func TestPromoRedeem_HappyPathProjectsAndPersists(t *testing.T) {
	r, svc, _ := newPromoRouter()
	ctx := context.Background()
	userID := uuid.New()
	if _, err := svc.Grant(ctx, validCode()); err != nil {
		t.Fatalf("grant: %v", err)
	}

	w := doAsUser(r, http.MethodPost, "/api/v1/promotions/redeem",
		map[string]interface{}{"code": "ride20", "fare": 200, "currency": "etb", "ride_type": "standard"}, userID, true)
	if w.Code != http.StatusOK {
		t.Fatalf("redeem: want 200, got %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		Code              string  `json:"code"`
		Type              string  `json:"type"`
		ProjectedDiscount float64 `json:"projected_discount"`
		Currency          string  `json:"currency"`
		AppliesAt         string  `json:"applies_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body.String())
	}
	if resp.Code != "RIDE20" || resp.ProjectedDiscount != 40 || resp.AppliesAt != "request_time" {
		t.Fatalf("unexpected redeem response %+v", resp)
	}

	// Redeem wrote the open projection: Mine still lists the code and a second
	// browse-through redeem does not consume anything (no per-user limit here,
	// but the projection row count stays at one open row via CloseOpenProjections).
	w = doAsUser(r, http.MethodGet, "/api/v1/promotions/mine", nil, userID, true)
	if w.Code != http.StatusOK {
		t.Fatalf("mine: want 200, got %d", w.Code)
	}
	var mine struct {
		Promotions []struct {
			Code    string  `json:"code"`
			Type    string  `json:"type"`
			Value   float64 `json:"value"`
			MinFare float64 `json:"min_fare"`
		} `json:"promotions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &mine); err != nil {
		t.Fatalf("decode mine: %v", err)
	}
	if len(mine.Promotions) != 1 || mine.Promotions[0].Code != "RIDE20" {
		t.Fatalf("mine should list exactly the granted code, got %+v", mine.Promotions)
	}
}

func TestPromoMine_EmptyListSerializesAsArray(t *testing.T) {
	r, _, _ := newPromoRouter()
	w := doAsUser(r, http.MethodGet, "/api/v1/promotions/mine", nil, uuid.New(), true)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !bytes.Contains(w.Body.Bytes(), []byte(`"promotions":[]`)) {
		t.Fatalf("empty mine must serialize as [], got %s", body)
	}
}
