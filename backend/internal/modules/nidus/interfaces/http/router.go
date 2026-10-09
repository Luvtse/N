package http

import (
	"net/http"
	"time"

	"nidaw-backend/internal/modules/nidus/application/commands"
	niduscmds "nidaw-backend/internal/modules/nidus/application/commands"
	"nidaw-backend/internal/modules/nidus/application/queries"
	"nidaw-backend/internal/modules/nidus/application/services"
	niduscache "nidaw-backend/internal/modules/nidus/infrastructure/cache"
	nidusHttp "nidaw-backend/internal/modules/nidus/interfaces/http/handlers"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"nidaw-backend/internal/shared/middleware"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"go.uber.org/zap"
)

// ============================================================================
// ROUTER DEPENDENCIES
// ============================================================================

// Dependencies holds all services required by the router
type Dependencies struct {
	DB             *database.Postgres
	EventBus       eventbus.EventBus
	Logger         *zap.Logger
	AuthService    *auth.Service
	CacheService   CacheService
	MatchingEngine *services.MatchingEngine
	ETAService     *services.ETAService
	PricingService *services.PricingService
	TipSettler     niduscmds.TipSettler // ledger-backed tip settlement (nil => tips rejected)
	CORSOrigins    []string             // Phase B/B4: env-driven allowlist (CORS_ORIGINS)
}

// CacheService interface for driver location caching. DriverLocation is an
// alias to the canonical type in nidus/infrastructure/cache, so both packages
// see identical method signatures and *DriverLocationCache satisfies this
// interface directly.
type CacheService = nidusHttp.CacheService

// DriverLocation represents a driver's current position (alias of the
// canonical infrastructure type).
type DriverLocation = niduscache.DriverLocation

// ============================================================================
// ROUTER CONSTRUCTOR
// ============================================================================

// NewRouter creates a new chi router with all Nidus routes configured
func NewRouter(deps *Dependencies) http.Handler {
	r := chi.NewRouter()

	// ========================================================================
	// GLOBAL MIDDLEWARE (applied to all routes)
	// ========================================================================
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.Logger(deps.Logger))
	r.Use(middleware.Recoverer(deps.Logger))
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(middleware.RequestIDToContext)
	// Phase B/B4: wildcard origin is forbidden when credentials are enabled.
	// Origins come from CORS_ORIGINS env (config.CORSOrigins). Empty list =>
	// deny all cross-origin; explicit "*" => wildcard WITHOUT credentials.
	corsOpts := cors.Options{
		AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders: []string{"X-Request-ID", "X-RateLimit-Remaining"},
		MaxAge:         300,
	}
	if len(deps.CORSOrigins) == 0 {
		corsOpts.AllowOriginFunc = func(r *http.Request, origin string) bool { return false }
	} else if containsStr(deps.CORSOrigins, "*") {
		corsOpts.AllowedOrigins = []string{"*"}
		corsOpts.AllowCredentials = false
	} else {
		corsOpts.AllowedOrigins = deps.CORSOrigins
		corsOpts.AllowCredentials = true
	}
	r.Use(cors.Handler(corsOpts))
	r.Use(middleware.RateLimit(100, time.Minute)) // 100 requests per minute per IP

	// ========================================================================
	// INITIALIZE HANDLERS
	// ========================================================================
	// Ride lifecycle commands (state machine: requested -> ... -> completed).
	rateRideCmd := commands.NewRateRideHandler(deps.DB, deps.EventBus)
	if deps.TipSettler != nil {
		rateRideCmd.SetTipSettler(deps.TipSettler)
	}

	rideHandler := nidusHttp.NewRideHandler(
		deps.DB,
		commands.NewRequestRideHandler(deps.DB, deps.EventBus, deps.PricingService),
		queries.NewGetRideQuery(deps.DB),
		queries.NewListRidesQuery(deps.DB),
		deps.MatchingEngine,
		deps.PricingService,
	)
	rideHandler.SetLifecycle(nidusHttp.LifecycleCommands{
		Accept:   commands.NewAcceptRideHandler(deps.DB, deps.EventBus, deps.MatchingEngine, deps.PricingService),
		Start:    commands.NewStartRideHandler(deps.DB, deps.EventBus),
		Complete: commands.NewCompleteRideHandler(deps.DB, deps.EventBus, deps.PricingService),
		Cancel:   commands.NewCancelRideHandler(deps.DB, deps.EventBus),
		Rate:     rateRideCmd,
	})

	driverHandler := nidusHttp.NewDriverHandler(
		deps.MatchingEngine,
		deps.CacheService,
	)

	estimateHandler := nidusHttp.NewEstimateHandler(
		deps.PricingService,
	)

	locationHandler := nidusHttp.NewLocationHandler(
		deps.CacheService,
		deps.EventBus,
		deps.DB, // Phase B/B7: active-ride association check for driver GPS
	)

	// ========================================================================
	// HEALTH & READINESS ENDPOINTS (no auth)
	// ========================================================================
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := deps.DB.Ping(ctx); err != nil {
			http.Error(w, "database unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})

	r.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})

	// ========================================================================
	// API v1 ROUTES
	// ========================================================================
	r.Route("/api/v1", func(r chi.Router) {
		// Public endpoints (no auth required)
		r.Route("/nidus", func(r chi.Router) {
			// Ride estimates are public (used before login)
			r.Post("/rides/estimate", estimateHandler.EstimateFare)
		})

		// Protected endpoints (require authentication)
		r.Group(func(r chi.Router) {
			r.Use(middleware.AuthRequired(deps.AuthService))
			r.Use(middleware.ExtractUserToContext)

			// ====================================================================
			// RIDES
			// ====================================================================
			r.Route("/nidus/rides", func(r chi.Router) {
				r.Post("/", rideHandler.RequestRide)
				r.Get("/", rideHandler.ListRides)
				r.Get("/{rideID}", rideHandler.GetRide)
				// Driver-side lifecycle transitions (state machine guarded;
				// identity derived from the JWT subject, never the request body).
				r.Post("/{rideID}/accept", rideHandler.AcceptRide)
				r.Post("/{rideID}/start", rideHandler.StartRide)
				r.Post("/{rideID}/complete", rideHandler.CompleteRide)
				// Rider-side terminal actions.
				r.Post("/{rideID}/cancel", rideHandler.CancelRide)
				r.Post("/{rideID}/rate", rideHandler.RateRide)
			})

			// ====================================================================
			// DRIVERS (rider-facing)
			// ====================================================================
			r.Route("/nidus/drivers", func(r chi.Router) {
				r.Get("/nearby", driverHandler.GetNearbyDrivers)
				// Driver offer feed: unassigned rides available for acceptance.
				// Registered before /{driverID} so the literal path wins.
				r.Get("/pending-rides", rideHandler.PendingRides)
				r.Get("/{driverID}", driverHandler.GetDriver)
			})

			// ====================================================================
			// LOCATION TRACKING (real-time)
			// ====================================================================
			r.Route("/nidus/location", func(r chi.Router) {
				r.Post("/update", locationHandler.UpdateLocation)
				r.Get("/driver/{driverID}", locationHandler.GetDriverLocation)
			})
		})
	})

	// ========================================================================
	// 404 HANDLER
	// ========================================================================
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found","message":"endpoint not found"}`))
	})

	// ========================================================================
	// METHOD NOT ALLOWED HANDLER
	// ========================================================================
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"error":"method_not_allowed","message":"method not allowed"}`))
	})

	return r
}

// containsStr reports whether xs contains s (Go 1.19-compatible replacement
// for the stdlib "slices" package, which requires Go >= 1.21).
func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
