package http

import (
	"net/http"
	"time"

	"nidaw-backend/internal/modules/nidus/application/commands"
	"nidaw-backend/internal/modules/nidus/application/queries"
	"nidaw-backend/internal/modules/nidus/application/services"
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
	DB              *database.Postgres
	EventBus        eventbus.EventBus
	Logger          *zap.Logger
	AuthService     *auth.Service
	CacheService    CacheService
	MatchingEngine  *services.MatchingEngine
	ETAService      *services.ETAService
	PricingService  *services.PricingService
}

// CacheService interface for driver location caching
type CacheService interface {
	GetNearbyDrivers(ctx context.Context, lat, lng, radiusKm float64, status string) ([]*DriverLocation, error)
	UpdateDriverLocation(ctx context.Context, loc *DriverLocation) error
}

// DriverLocation represents a driver's current position
type DriverLocation struct {
	DriverID  string  `json:"driver_id"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timestamp int64   `json:"timestamp"`
	Heading   float64 `json:"heading"`
	Speed     float64 `json:"speed"`
	Status    string  `json:"status"`
}

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
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"}, // Configure per environment
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID", "X-RateLimit-Remaining"},
		AllowCredentials: true,
		MaxAge:           300,
	}))
	r.Use(middleware.RateLimit(100, time.Minute)) // 100 requests per minute per IP

	// ========================================================================
	// INITIALIZE HANDLERS
	// ========================================================================
	rideHandler := nidusHttp.NewRideHandler(
		commands.NewRequestRideHandler(deps.DB, deps.EventBus),
		queries.NewGetRideQuery(deps.DB),
		queries.NewListRidesQuery(deps.DB),
		deps.MatchingEngine,
		deps.ETAService,
		deps.PricingService,
	)

	driverHandler := nidusHttp.NewDriverHandler(
		queries.NewGetDriverQuery(deps.DB),
		queries.NewListDriversQuery(deps.DB),
		deps.MatchingEngine,
	)

	estimateHandler := nidusHttp.NewEstimateHandler(
		deps.ETAService,
		deps.PricingService,
	)

	locationHandler := nidusHttp.NewLocationHandler(
		deps.CacheService,
		deps.EventBus,
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
				r.Post("/{rideID}/cancel", rideHandler.CancelRide)
				r.Post("/{rideID}/rate", rideHandler.RateRide)
			})

			// ====================================================================
			// DRIVERS (rider-facing)
			// ====================================================================
			r.Route("/nidus/drivers", func(r chi.Router) {
				r.Get("/nearby", driverHandler.GetNearbyDrivers)
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