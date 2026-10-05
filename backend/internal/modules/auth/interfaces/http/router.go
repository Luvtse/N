package http

import (
	"net/http"
	"time"

	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/middleware"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

// ============================================================================
// ROUTER CONSTRUCTOR
// ============================================================================

// NewRouter creates a new auth router with all authentication routes
func NewRouter(
	db *database.Postgres,
	authService *auth.Service,
	logger *zap.Logger,
) http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.Logger(logger))
	r.Use(middleware.Recoverer(logger))
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(middleware.RequestIDToContext)
	r.Use(middleware.ContentType("application/json"))
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.RateLimit(60, time.Minute)) // 60 requests per minute

	// Initialize handler
	handler := NewAuthHandler(db, authService)

	// ========================================================================
	// PUBLIC ROUTES (no auth required)
	// ========================================================================
	r.Post("/api/v1/auth/login", handler.Login)
	r.Post("/api/v1/auth/register", handler.Register)
	r.Post("/api/v1/auth/refresh", handler.Refresh)

	// ========================================================================
	// PROTECTED ROUTES (auth required)
	// ========================================================================
	r.Group(func(r chi.Router) {
		r.Use(authService.Middleware())

		r.Get("/api/v1/auth/me", handler.GetCurrentUser)
		r.Post("/api/v1/auth/logout", handler.Logout)
		r.Put("/api/v1/auth/profile", handler.UpdateProfile)
		r.Post("/api/v1/auth/change-password", handler.ChangePassword)
	})

	// ========================================================================
	// HEALTH CHECK
	// ========================================================================
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","service":"auth"}`))
	})

	return r
}