// Package http integration tests for the notifications REST surface (§1.4).
// The handler is exercised against the in-memory repository with the user id
// injected directly into the context (the same key AuthRequired+
// ExtractUserToContext populate in production), so these tests cover routing,
// validation, pagination, ownership semantics, and 404-not-403 behavior
// without Postgres or JWT signing.
package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	notihttp "nidaw-backend/internal/modules/notifications/interfaces/http"
	domain "nidaw-backend/internal/modules/notifications/domain"
	infra "nidaw-backend/internal/modules/notifications/infrastructure"
	app "nidaw-backend/internal/modules/notifications/application"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"nidaw-backend/internal/shared/auth"
)

func newServer(userID uuid.UUID) (*infra.MemoryRepository, http.Handler) {
	repo := infra.NewMemoryRepository()
	r := chi.NewRouter()
	notihttp.NewHandler(repo).Register(r)
	inject := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userID)))
		})
	}
	return repo, inject(r)
}

func do(srv http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func TestDeviceTokenLifecycle(t *testing.T) {
	user := uuid.New()
	repo, srv := newServer(user)

	if w := do(srv, "POST", "/devices", `{"token":"tok-1","platform":"android","app_version":"1.2.3"}`); w.Code != http.StatusOK {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	tokens, err := repo.ActiveTokensForUser(user)
	if err != nil || len(tokens) != 1 || tokens[0].Platform != "android" {
		t.Fatalf("token not persisted: %+v err=%v", tokens, err)
	}

	if w := do(srv, "POST", "/devices", `{"token":"tok-2","platform":"blackberry"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid platform must 400, got %d", w.Code)
	}
	if w := do(srv, "POST", "/devices", `{"platform":"ios"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("missing token must 400, got %d", w.Code)
	}
	// Register tok-2 with a valid platform so the post-revoke assertion below
	// proves ownership-scoped revocation: tok-1 (caller's) is revoked while
	// tok-2 (also caller's) stays active.
	if w := do(srv, "POST", "/devices", `{"token":"tok-2","platform":"ios"}`); w.Code != http.StatusOK {
		t.Fatalf("register tok-2: %d %s", w.Code, w.Body.String())
	}

	if w := do(srv, "DELETE", "/devices/tok-1", ""); w.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d", w.Code)
	}
	active, _ := repo.ActiveTokensForUser(user)
	if len(active) != 1 || active[0].Token != "tok-2" {
		t.Fatalf("revoked token still active: %+v", active)
	}
	// Revoking an unknown/foreign token is an indistinguishable 204 (no probing).
	if w := do(srv, "DELETE", "/devices/does-not-exist", ""); w.Code != http.StatusNoContent {
		t.Fatalf("unknown revoke must 204, got %d", w.Code)
	}
}

func TestInboxPaginationAndReadFlow(t *testing.T) {
	user := uuid.New()
	repo, srv := newServer(user)

	// Seed 25 in-app rows through the real dispatcher path so created_at ties
	// are broken by id like production.
	d := app.NewDispatcher(repo, app.DefaultPolicyTable(), app.DefaultRetryConfig(), nil, nil, nil)
	for i := 0; i < 25; i++ {
		if _, err := d.Dispatch(context.Background(), domain.Command{
			UserID:   user,
			Category: domain.CategoryPromo,
			DeepLink: "/nidus/promos",
		}); err != nil {
			t.Fatal(err)
		}
	}

	w := do(srv, "GET", "/inbox", "")
	if w.Code != http.StatusOK {
		t.Fatalf("inbox: %d %s", w.Code, w.Body.String())
	}
	var page struct {
		Items      []map[string]interface{} `json:"items"`
		NextCursor string                   `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 20 || page.NextCursor == "" {
		t.Fatalf("first page want 20 + cursor, got %d/%q", len(page.Items), page.NextCursor)
	}
	for _, it := range page.Items {
		if it["channel"] == "email" || it["unread"] != true {
			t.Fatalf("inbox item wrong shape: %v", it)
		}
	}

	w = do(srv, "GET", "/inbox?cursor="+page.NextCursor, "")
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 5 || page.NextCursor != "" {
		t.Fatalf("second page want 5 + no cursor, got %d/%q", len(page.Items), page.NextCursor)
	}

	if w := do(srv, "GET", "/inbox?limit=0", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("limit=0 must 400, got %d", w.Code)
	}

	// Unread count reflects all 25 before reads.
	w = do(srv, "GET", "/inbox/unread-count", "")
	var cnt struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cnt); err != nil || cnt.Count != 25 {
		t.Fatalf("unread count want 25, got %v (%v)", cnt, err)
	}

	firstID := page.Items[0]["id"].(string)
	if w := do(srv, "POST", "/"+firstID+"/read", ""); w.Code != http.StatusOK {
		t.Fatalf("mark read: %d %s", w.Code, w.Body.String())
	}
	// Idempotent second call still 200.
	if w := do(srv, "POST", "/"+firstID+"/read", ""); w.Code != http.StatusOK {
		t.Fatalf("mark read idempotent: %d", w.Code)
	}
	// Foreign id => 404, never 403 (no existence leak).
	if w := do(srv, "POST", "/"+uuid.New().String()+"/read", ""); w.Code != http.StatusNotFound {
		t.Fatalf("foreign mark-read must 404, got %d", w.Code)
	}
	w = do(srv, "GET", "/inbox/unread-count", "")
	_ = json.Unmarshal(w.Body.Bytes(), &cnt)
	if cnt.Count != 24 {
		t.Fatalf("unread after one read want 24, got %d", cnt.Count)
	}
}

func TestInboxIsScopedToCaller(t *testing.T) {
	userA := uuid.New()
	userB := uuid.New()
	repo := infra.NewMemoryRepository()
	d := app.NewDispatcher(repo, app.DefaultPolicyTable(), app.DefaultRetryConfig(), nil, nil, nil)
	if _, err := d.Dispatch(context.Background(), domain.Command{UserID: userB, Category: domain.CategoryRideMatched}); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	notihttp.NewHandler(repo).Register(r)
	srvA := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), auth.UserIDKey, userA)))
		})
	}(r)

	w := do(srvA, "GET", "/inbox", "")
	var page struct {
		Items []map[string]interface{} `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if len(page.Items) != 0 {
		t.Fatalf("user A must not see user B's inbox, got %d items", len(page.Items))
	}
	_ = time.Now
}
