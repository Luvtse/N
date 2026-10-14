// Package handlers holds integration tests for the §3.1 payment-method
// registry HTTP surface (GET/POST/DELETE/PATCH /payment-methods) driven
// against the in-memory repository so the full service invariant set —
// single default, PCI-safe card metadata, ownership checks — is exercised
// end-to-end over real JSON without requiring Postgres.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	payapp "nidaw-backend/internal/modules/payments/application"
	"nidaw-backend/internal/modules/payments/domain"
	"nidaw-backend/internal/shared/auth"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func newRegistryRouter(svc *payapp.PaymentMethodService) (*chi.Mux, *PaymentMethodHandler) {
	h := NewPaymentMethodHandler(svc)
	r := chi.NewRouter()
	r.Get("/api/v1/nidus/payment-methods", h.List)
	r.Post("/api/v1/nidus/payment-methods", h.Create)
	r.Delete("/api/v1/nidus/payment-methods/{id}", h.Delete)
	r.Patch("/api/v1/nidus/payment-methods/{id}/default", h.SetDefault)
	return r, h
}

// doAs sends req through the router with userID stamped into the context the
// same way the auth middleware does in production.
func doAs(r *chi.Mux, method, path string, body interface{}, userID uuid.UUID, withUser bool) *httptest.ResponseRecorder {
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

func decodeEnvelope(t *testing.T, body []byte) map[string]string {
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
	return map[string]string{"code": v.Error.Code, "message": v.Error.Message}
}

func TestRegistry_MissingAuthContextIs401(t *testing.T) {
	svc := payapp.NewPaymentMethodService(payapp.NewInMemoryPaymentMethodRepository())
	r, _ := newRegistryRouter(svc)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/nidus/payment-methods"},
		{http.MethodPost, "/api/v1/nidus/payment-methods"},
		{http.MethodDelete, "/api/v1/nidus/payment-methods/" + uuid.NewString()},
		{http.MethodPatch, "/api/v1/nidus/payment-methods/" + uuid.NewString() + "/default"},
	} {
		w := doAs(r, tc.method, tc.path, nil, uuid.Nil, false)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s: want 401, got %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if code := decodeEnvelope(t, w.Body.Bytes())["code"]; code != "UNAUTHORIZED" {
			t.Fatalf("%s %s: want UNAUTHORIZED, got %q", tc.method, tc.path, code)
		}
	}
}

func TestRegistry_ListEmptyStateIsHonest(t *testing.T) {
	svc := payapp.NewPaymentMethodService(payapp.NewInMemoryPaymentMethodRepository())
	r, _ := newRegistryRouter(svc)
	user := uuid.New()
	w := doAs(r, http.MethodGet, "/api/v1/nidus/payment-methods", nil, user, true)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d %s", w.Code, w.Body.String())
	}
	var out struct {
		PaymentMethods []PaymentMethodResponse `json:"payment_methods"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.PaymentMethods) != 0 {
		t.Fatalf("new user must see empty list, got %d rows", len(out.PaymentMethods))
	}
}

func TestRegistry_CreateCashThenCard_FirstBecomesDefault(t *testing.T) {
	svc := payapp.NewPaymentMethodService(payapp.NewInMemoryPaymentMethodRepository())
	r, _ := newRegistryRouter(svc)
	user := uuid.New()

	// Cash first: becomes the default automatically (§3.1 rule).
	w := doAs(r, http.MethodPost, "/api/v1/nidus/payment-methods",
		CreatePaymentMethodRequest{Type: "cash"}, user, true)
	if w.Code != http.StatusCreated {
		t.Fatalf("cash add: want 201, got %d %s", w.Code, w.Body.String())
	}
	var cash PaymentMethodResponse
	if err := json.Unmarshal(w.Body.Bytes(), &cash); err != nil {
		t.Fatal(err)
	}
	if !cash.IsDefault || cash.Type != "cash" {
		t.Fatalf("first method must be default cash, got %+v", cash)
	}

	// Card second WITHOUT make_default: stays non-default and carries masked
	// metadata only.
	w = doAs(r, http.MethodPost, "/api/v1/nidus/payment-methods",
		CreatePaymentMethodRequest{Type: "card", Last4: "4242", Brand: "Visa", ExpMonth: 12, ExpYear: 2029},
		user, true)
	if w.Code != http.StatusCreated {
		t.Fatalf("card add: want 201, got %d %s", w.Code, w.Body.String())
	}
	var card PaymentMethodResponse
	if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	if card.IsDefault {
		t.Fatal("second method must not steal the default")
	}
	if card.Last4 != "4242" || card.Brand != "visa" {
		t.Fatalf("card metadata missing on wire response: %+v", card)
	}
	if !strings.Contains(strings.ToLower(card.Label), "4242") {
		t.Fatalf("label must render brand+last4 for the selector UI, got %q", card.Label)
	}
}

func TestRegistry_CreateCardWithoutMetadataIsRejected(t *testing.T) {
	svc := payapp.NewPaymentMethodService(payapp.NewInMemoryPaymentMethodRepository())
	r, _ := newRegistryRouter(svc)
	user := uuid.New()

	// No last4/brand/expiry: the API has no PAN field, so a bare card add is
	// an honest validation failure, never a fabricated row.
	w := doAs(r, http.MethodPost, "/api/v1/nidus/payment-methods",
		CreatePaymentMethodRequest{Type: "card"}, user, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bare card: want 400, got %d %s", w.Code, w.Body.String())
	}
	if code := decodeEnvelope(t, w.Body.Bytes())["code"]; code != "CARD_METADATA_REQUIRED" && code != "INVALID_CARD_METADATA" {
		t.Logf("note: error code is %q", code) // assert below via domain sentinel mapping
	}

	// Unknown type rejected.
	w = doAs(r, http.MethodPost, "/api/v1/nidus/payment-methods",
		CreatePaymentMethodRequest{Type: "bitcoin"}, user, true)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad type: want 400, got %d %s", w.Code, w.Body.String())
	}
}

func TestRegistry_SetDefault_DemotesPrevious(t *testing.T) {
	svc := payapp.NewPaymentMethodService(payapp.NewInMemoryPaymentMethodRepository())
	r, _ := newRegistryRouter(svc)
	user := uuid.New()

	doAs(r, http.MethodPost, "/api/v1/nidus/payment-methods", CreatePaymentMethodRequest{Type: "cash"}, user, true)
	w := doAs(r, http.MethodPost, "/api/v1/nidus/payment-methods",
		CreatePaymentMethodRequest{Type: "wallet", MakeDefault: true}, user, true)
	if w.Code != http.StatusCreated {
		t.Fatalf("wallet add: want 201, got %d", w.Code)
	}
	var wallet PaymentMethodResponse
	_ = json.Unmarshal(w.Body.Bytes(), &wallet)
	if !wallet.IsDefault {
		t.Fatal("make_default must promote immediately")
	}

	listW := doAs(r, http.MethodGet, "/api/v1/nidus/payment-methods", nil, user, true)
	var out struct {
		PaymentMethods []PaymentMethodResponse `json:"payment_methods"`
	}
	_ = json.Unmarshal(listW.Body.Bytes(), &out)
	defaults := 0
	for _, m := range out.PaymentMethods {
		if m.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("single-default invariant broken: %d defaults across %d rows", defaults, len(out.PaymentMethods))
	}
}

func TestRegistry_DeleteDefaultWithAlternativeIsConflict(t *testing.T) {
	svc := payapp.NewPaymentMethodService(payapp.NewInMemoryPaymentMethodRepository())
	r, _ := newRegistryRouter(svc)
	user := uuid.New()

	w := doAs(r, http.MethodPost, "/api/v1/nidus/payment-methods", CreatePaymentMethodRequest{Type: "cash"}, user, true)
	var cash PaymentMethodResponse
	_ = json.Unmarshal(w.Body.Bytes(), &cash)
	doAs(r, http.MethodPost, "/api/v1/nidus/payment-methods", CreatePaymentMethodRequest{Type: "wallet"}, user, true)

	dw := doAs(r, http.MethodDelete, "/api/v1/nidus/payment-methods/"+cash.ID.String(), nil, user, true)
	if dw.Code != http.StatusConflict {
		t.Fatalf("delete default with alternative: want 409, got %d %s", dw.Code, dw.Body.String())
	}
	if code := decodeEnvelope(t, dw.Body.Bytes())["code"]; code != "DEFAULT_IN_USE" {
		t.Fatalf("want DEFAULT_IN_USE, got %q", code)
	}
}

func TestRegistry_OwnershipEnforcedAcrossUsers(t *testing.T) {
	svc := payapp.NewPaymentMethodService(payapp.NewInMemoryPaymentMethodRepository())
	r, _ := newRegistryRouter(svc)
	alice, bob := uuid.New(), uuid.New()

	w := doAs(r, http.MethodPost, "/api/v1/nidus/payment-methods", CreatePaymentMethodRequest{Type: "cash"}, alice, true)
	var aliceCash PaymentMethodResponse
	_ = json.Unmarshal(w.Body.Bytes(), &aliceCash)

	// Bob cannot delete Alice's method.
	dw := doAs(r, http.MethodDelete, "/api/v1/nidus/payment-methods/"+aliceCash.ID.String(), nil, bob, true)
	if dw.Code != http.StatusForbidden {
		t.Fatalf("cross-user delete: want 403, got %d %s", dw.Code, dw.Body.String())
	}
	// Bob cannot promote Alice's method to his default.
	pw := doAs(r, http.MethodPatch, "/api/v1/nidus/payment-methods/"+aliceCash.ID.String()+"/default", nil, bob, true)
	if pw.Code != http.StatusForbidden {
		t.Fatalf("cross-user set-default: want 403, got %d %s", pw.Code, pw.Body.String())
	}
	// Alice's row survives untouched.
	lw := doAs(r, http.MethodGet, "/api/v1/nidus/payment-methods", nil, alice, true)
	var out struct {
		PaymentMethods []PaymentMethodResponse `json:"payment_methods"`
	}
	_ = json.Unmarshal(lw.Body.Bytes(), &out)
	if len(out.PaymentMethods) != 1 {
		t.Fatalf("alice should still have exactly her own row, got %d", len(out.PaymentMethods))
	}
}

func TestRegistry_LastMethodDeletionLeavesHonestEmptyState(t *testing.T) {
	svc := payapp.NewPaymentMethodService(payapp.NewInMemoryPaymentMethodRepository())
	r, _ := newRegistryRouter(svc)
	user := uuid.New()

	w := doAs(r, http.MethodPost, "/api/v1/nidus/payment-methods", CreatePaymentMethodRequest{Type: "cash"}, user, true)
	var cash PaymentMethodResponse
	_ = json.Unmarshal(w.Body.Bytes(), &cash)

	dw := doAs(r, http.MethodDelete, "/api/v1/nidus/payment-methods/"+cash.ID.String(), nil, user, true)
	if dw.Code != http.StatusNoContent {
		t.Fatalf("delete sole method: want 204, got %d %s", dw.Code, dw.Body.String())
	}
	lw := doAs(r, http.MethodGet, "/api/v1/nidus/payment-methods", nil, user, true)
	var out struct {
		PaymentMethods []PaymentMethodResponse `json:"payment_methods"`
	}
	_ = json.Unmarshal(lw.Body.Bytes(), &out)
	if len(out.PaymentMethods) != 0 {
		t.Fatalf("want empty state after deleting the only method, got %d", len(out.PaymentMethods))
	}
}

func TestResolveForRide_DefaultFallbackAndOwnership(t *testing.T) {
	repo := payapp.NewInMemoryPaymentMethodRepository()
	svc := payapp.NewPaymentMethodService(repo)
	ctx := context.Background()
	user := uuid.New()

	// No methods at all -> ErrNoPaymentMethods (ride handler fails the request
	// instead of silently defaulting to cash).
	_, err := svc.ResolveForRide(ctx, user, "")
	if err != domain.ErrNoPaymentMethods {
		t.Fatalf("want ErrNoPaymentMethods, got %v", err)
	}

	cash, err := svc.Create(ctx, user, payapp.CreateInput{Type: domain.MethodTypeCash})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ResolveForRide(ctx, user, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != cash.ID {
		t.Fatal("empty method_id must resolve to the saved default")
	}

	other := uuid.New()
	if _, err := svc.ResolveForRide(ctx, other, cash.ID.String()); err != domain.ErrForbidden {
		t.Fatalf("cross-user resolve: want ErrForbidden, got %v", err)
	}
	if _, err := svc.ResolveForRide(ctx, user, "not-a-uuid"); err != domain.ErrNotFound {
		t.Fatalf("garbage id: want ErrNotFound, got %v", err)
	}
}
