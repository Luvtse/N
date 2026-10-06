package middleware

import (
	"net/http"

	"nidaw-backend/internal/shared/auth"

	"github.com/go-chi/chi/v5"
)

// AuthRequired validates the bearer access token via the shared auth service
// and injects the user identity claims into the request context. Requests
// without a valid token are rejected with 401 before reaching handlers.
func AuthRequired(authService *auth.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authService == nil {
				http.Error(w, `{"error":"auth service unavailable"}`, http.StatusInternalServerError)
				return
			}
			authService.Middleware()(next).ServeHTTP(w, r)
		})
	}
}

// ExtractUserToContext populates chi route-context URL parameters (userID,
// driverID) from the authenticated claims placed in the request context by
// AuthRequired. Handlers that resolve identity through the chi route context
// depend on this step; it must run after AuthRequired.
func ExtractUserToContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		userID, ok := auth.GetUserIDFromContext(ctx)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if chiCtx := chi.RouteContext(ctx); chiCtx != nil {
			// chi v5 has no SetURLParam; append to the route-param stack so
			// chi.URLParam(r, "userID") resolves for downstream handlers.
			chiCtx.URLParams.Add("userID", userID.String())
			if role, rok := auth.GetUserRoleFromContext(ctx); rok && role == "driver" {
				chiCtx.URLParams.Add("driverID", userID.String())
			}
		}
		next.ServeHTTP(w, r)
	})
}
