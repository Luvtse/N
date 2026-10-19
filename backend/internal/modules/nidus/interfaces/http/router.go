package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"nidaw-backend/internal/modules/nidus/application/commands"
	niduscmds "nidaw-backend/internal/modules/nidus/application/commands"
	"nidaw-backend/internal/modules/nidus/application/queries"
	"nidaw-backend/internal/modules/nidus/application/services"
	niduscache "nidaw-backend/internal/modules/nidus/infrastructure/cache"
	nidusHttp "nidaw-backend/internal/modules/nidus/interfaces/http/handlers"
	paymentsapp "nidaw-backend/internal/modules/payments/application"
	payhttp "nidaw-backend/internal/modules/payments/interfaces/http/handlers"
	promoapp "nidaw-backend/internal/modules/promotions/application"
	promodomain "nidaw-backend/internal/modules/promotions/domain"
	promohttp "nidaw-backend/internal/modules/promotions/interfaces/http/handlers"
	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"nidaw-backend/internal/shared/middleware"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
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
	// §3.1 Cash Payment Option. PaymentMethods is the payments-module port that
	// validates and snapshots the rider's chosen method at request time; nil
	// (flag off / module unwired) preserves legacy behaviour — rides are created
	// without a payment-method resolution step. CashEnabled mirrors the
	// cash_payment flag: when false, rides resolving to cash are rejected with
	// PAYMENT_UNAVAILABLE rather than silently created.
	PaymentMethods   niduscmds.PaymentMethodResolver
	CashEnabled      bool
	PaymentMethodSvc *paymentsapp.PaymentMethodService // nil => registry routes not mounted
	// §3.2 Promo Codes & Coupons. Promotions is the port RequestRideHandler
	// consumes at request time (consume the rider's projection, bind it to the
	// freshly minted ride id); nil or PromosEnabled==false means a supplied
	// promo code is rejected with PROMOTIONS_DISABLED rather than silently
	// ignored — an honest "promos unavailable" state, never a fake discount.
	// PromoFinalizer adjusts the discount against the final fare at receipt
	// time; nil leaves the request-time snapshot untouched. PromoSvc backs
	// POST /promotions/redeem and GET /promotions/mine via the promotions
	// HTTP handler; if nil those routes are not mounted at all. main.go wires
	// everything from the promotions module; the command-side ports are
	// declared structurally in commands so nidus only borrows types here at
	// composition, mirroring PaymentMethodSvc above.
	Promotions     niduscmds.PromoApplier
	PromosEnabled  bool
	PromoFinalizer niduscmds.PromoFinalizer
	PromoSvc       *promoapp.Service // nil => redeem/mine routes not mounted
	// WSHub backs GET /ws (§1.5 router cleanup: the hub existed but was never
	// mounted). main.go constructs one shared hub here and reuses it as the
	// notifications module's InAppBroadcaster (§1.4). nil => no /ws route.
	WSHub *nidusHttp.WebSocketHandler
	// PreSelfie attaches the driver's pre-selfie verification state to rider
	// ride DTOs (§2.3, plan: "attach it to the rider's ride DTO as
	// driver_pre_selfie_url"). Implemented by NewPreSelfieAttacher below over an
	// injected structural interface so this module never imports selfie
	// internals. nil (or flag off) => the DTO omits the selfie fields entirely;
	// the client then shows its honest "verification unavailable" state rather
	// than a fabricated one.
	PreSelfie nidusHttp.PreSelfieAttacher
	// §3.3 Split Fare. SplitSvc backs POST/GET /nidus/rides/{rideID}/split and
	// the public tokenized POST /split/{token}/accept routes. nil => the routes
	// are not mounted at all, so clients get a clean 404 and the receipt page
	// renders its honest "split unavailable" state — never a fake invite list.
	SplitSvc *services.SplitService // nil => split routes not mounted
	// §3.4 Email Receipt. ReceiptMailer backs POST /nidus/rides/{rideID}/email-receipt.
	// Injected into RideHandler via SetReceiptMailer at mount time; nil (or the
	// notifications flag off) means the route still mounts but honestly returns
	// 503 RECEIPT_EMAIL_UNAVAILABLE — never a fake "sent" confirmation. main.go
	// adapts the notifications Dispatcher to this structural port so nidus
	// never imports the notifications application package.
	ReceiptMailer nidusHttp.ReceiptMailer
	// §3.5 Stopovers & Mid-Trip Destination Change. StopoversEnabled mirrors
	// the ride_stopovers feature flag: when false, requests carrying stops are
	// rejected with STOPOVERS_DISABLED (never silently priced as direct trips)
	// and PATCH /nidus/rides/{rideID}/destination is not mounted at all — a
	// clean 404 beats a half-wired endpoint (Global Rule 5).
	StopoversEnabled bool
}

// SelfieRiderView is the structural shape of selfie/application.RiderView. It
// is declared as an anonymous-struct *alias* so any named type with identical
// underlying field layout (name, type, tag) satisfies assignments — Go permits
// assigning a bound method `func(uuid.UUID) selfieapp.RiderView` to a variable
// of type `func(uuid.UUID) SelfieRiderView` because RiderView and this alias
// have identical underlying types. That is how main.go injects
// *selfieapp.Service without the nidus module importing it.
type SelfieRiderView = struct {
	State     string     `json:"state"`
	URL       string     `json:"driver_pre_selfie_url,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// NewPreSelfieAttacher adapts the selfie service's two methods to the
// handlers-package port (nidusHttp.PreSelfieAttacher). Pass the bound method
// values from *selfieapp.Service at composition time (main.go):
//
//	nidusDeps.PreSelfie = nidushttp.NewPreSelfieAttacher(
//	    selfieSvc.AttachToRiderView, selfieSvc.RedeemPathForView)
//
// Both parameters are nil-tolerant: if either is nil the attacher degrades
// every ride to State=="unavailable" (an honest retry affordance) instead of
// panicking or fabricating a URL. The downgrade also fires when the minted
// display URL fails redemption-path validation.
func NewPreSelfieAttacher(
	attach func(uuid.UUID) SelfieRiderView,
	redeemPath func(SelfieRiderView) (string, bool),
) nidusHttp.PreSelfieAttacher {
	return nidusHttp.PreSelfieAttacherFunc(func(rideID uuid.UUID) nidusHttp.PreSelfieInfo {
		if attach == nil || redeemPath == nil {
			return nidusHttp.PreSelfieInfo{State: "unavailable"}
		}
		view := attach(rideID)
		out := nidusHttp.PreSelfieInfo{State: view.State, ExpiresAt: view.ExpiresAt}
		if view.State == "available" {
			if path, ok := redeemPath(view); ok && view.URL != "" {
				out.DriverPreSelfieURL = view.URL
				out.RedeemPath = path
			} else {
				// Minted URL failed validation — do not pretend it is usable.
				out.State = "unavailable"
				out.DriverPreSelfieURL = ""
				out.RedeemPath = ""
			}
		}
		return out
	})
}

// ============================================================================
// COMPOSITION ADAPTERS
// ============================================================================

// ============================================================================
// COMPOSITION ADAPTERS (§3.2 Promotions)
//
// The nidus command layer must never import the promotions module (dependency
// direction rule). But Go interface satisfaction is invariant in *named*
// return types: promotions/app.Service returns *promoapp.AppliedPromo while
// the niduscmds.PromoApplier port demands *niduscmds.PromoApplication — same
// fields, different names, NOT assignable through an interface method set.
// Worse, (*Service).Method as a bound method value has a *named* result type
// that is not convertible to an anonymous-struct func alias either. So these
// adapters take plain function literals instead: main.go writes the one-line
// conversion at composition time and this package never mentions any
// promotions named type in a signature position that would fail assignability.
//
// Error mapping (single source of truth for both adapters):
//   - promodomain.ErrCodeNotFound       -> niduscmds.ErrPromoNotFound
//   - promoapp.ErrRedemptionNotFound    -> niduscmds.ErrRedemptionNotFound
//   - anything else                     -> passed through unchanged (the
//     command layer already downgrades unknown errors to "ride proceeds
//     without a discount"; we invent nothing here)
//
// A nil inner degrades to ErrPromoNotFound / (0, nil): an honest "code
// unavailable" error, never a silent zero discount and never a panic.
// ============================================================================

// ApplyAtRequestFunc is the shape main.go provides at composition: given the
// rider, code, freshly minted ride id and projected fare, consume the open
// projection and return the authoritative discount snapshot.
type ApplyAtRequestFunc = func(ctx context.Context, userID uuid.UUID, code string, rideID uuid.UUID, finalFare float64) (string, float64, error)

type promoApplierAdapter struct {
	apply ApplyAtRequestFunc
}

// NewPromoApplier adapts an ApplyAtRequestFunc to the niduscmds.PromoApplier
// port. Wire only when FEATURE_PROMOTIONS is on; passing nil keeps rides
// promo-free (RequestRide then rejects any supplied code with PROMO_DISABLED —
// see request_ride.go).
func NewPromoApplier(apply ApplyAtRequestFunc) niduscmds.PromoApplier {
	return &promoApplierAdapter{apply: apply}
}

func (a *promoApplierAdapter) ApplyAtRequest(ctx context.Context, userID uuid.UUID, code string, rideID uuid.UUID, finalFare float64) (*niduscmds.PromoApplication, error) {
	if a.apply == nil {
		return nil, niduscmds.ErrPromoNotFound
	}
	appliedCode, discount, err := a.apply(ctx, userID, code, rideID, finalFare)
	if err != nil {
		return nil, mapPromoErr(err)
	}
	if appliedCode == "" {
		return nil, niduscmds.ErrPromoNotFound
	}
	return &niduscmds.PromoApplication{Code: appliedCode, Discount: discount}, nil
}

// FinalizeAtCompletionFunc is the shape main.go provides: recompute the
// discount for a completed ride against its final fare. Returns
// ErrRedemptionNotFound when the ride carries no redemption row (the command
// layer treats 0 + that error as "no promo on this ride").
type FinalizeAtCompletionFunc = func(ctx context.Context, rideID uuid.UUID, finalFare float64, code string) (float64, error)

type promoFinalizerAdapter struct {
	finalize FinalizeAtCompletionFunc
}

// NewPromoFinalizer adapts a FinalizeAtCompletionFunc to the
// niduscmds.PromoFinalizer port. Passing nil yields a nil port => receipt-time
// adjustment disabled; the request-time snapshot stands.
func NewPromoFinalizer(finalize FinalizeAtCompletionFunc) niduscmds.PromoFinalizer {
	if finalize == nil {
		return nil
	}
	return &promoFinalizerAdapter{finalize: finalize}
}

func (a *promoFinalizerAdapter) FinalizeAtCompletion(ctx context.Context, rideID uuid.UUID, finalFare float64, code string) (float64, error) {
	if a.finalize == nil {
		return 0, nil
	}
	discount, err := a.finalize(ctx, rideID, finalFare, code)
	if err != nil {
		return 0, mapPromoErr(err)
	}
	return discount, nil
}

// mapPromoErr translates promotions-module sentinel errors into the nidus
// command-boundary vocabulary via errors.Is identity against the exported
// vars from the promotions packages.
func mapPromoErr(err error) error {
	switch {
	case errors.Is(err, promodomain.ErrCodeNotFound):
		return niduscmds.ErrPromoNotFound
	case errors.Is(err, promoapp.ErrRedemptionNotFound):
		return niduscmds.ErrRedemptionNotFound
	default:
		return err
	}
}

// ============================================================================
// CACHE PORT
// ============================================================================

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
		commands.NewRequestRideHandler(deps.DB, deps.EventBus, deps.PricingService, deps.PaymentMethods, deps.CashEnabled, deps.Promotions, deps.PromosEnabled),
		queries.NewGetRideQuery(deps.DB),
		queries.NewListRidesQuery(deps.DB),
		deps.MatchingEngine,
		deps.PricingService,
		services.DefaultCancellationPolicy(),
	)
	rideHandler.SetLifecycle(nidusHttp.LifecycleCommands{
		Accept:   commands.NewAcceptRideHandler(deps.DB, deps.EventBus, deps.MatchingEngine, deps.PricingService),
		Start:    commands.NewStartRideHandler(deps.DB, deps.EventBus),
		Complete: commands.NewCompleteRideHandler(deps.DB, deps.EventBus, deps.PricingService, deps.PromoFinalizer),
		Cancel:   commands.NewCancelRideHandler(deps.DB, deps.EventBus),
		Rate:     rateRideCmd,
		// §3.1: driver-side cash-collection confirm (completed -> paid + deferred
		// settlement event). Mounted on the router only when cash is enabled.
		ConfirmCashCollection: commands.NewConfirmCashCollectionHandler(deps.DB, deps.EventBus),
	})
	// §2.3: embed the honest driver pre-selfie state on rider ride DTOs when
	// the selfie module is wired (FEATURE_PRE_SELFIE). nil => field omitted;
	// the client shows its "verification unavailable" state, never a fake one.
	if deps.PreSelfie != nil {
		rideHandler.SetPreSelfieAttacher(deps.PreSelfie)
	}
	// §3.4: inject the email-receipt port when main.go wired one (nil-safe —
	// the route then honestly reports 503 RECEIPT_EMAIL_UNAVAILABLE).
	if deps.ReceiptMailer != nil {
		rideHandler.SetReceiptMailer(deps.ReceiptMailer)
	}

	driverHandler := nidusHttp.NewDriverHandler(
		deps.MatchingEngine,
		deps.CacheService,
		deps.DB, // availability toggles persist driver status
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
			// §2.6 canonical GET surface for the ride-option carousel
			r.Get("/rides/estimates", estimateHandler.Estimates)
		})

		// §3.3 Split Fare — public tokenized accept. Mounted OUTSIDE
		// AuthRequired: possession of the random 192-bit invite token IS the
		// authorization (invitees may not have Nidus accounts). If a bearer
		// session happens to hit it, ExtractUserToContext still runs so the
		// handler can bind invitee_user_id; anonymous accepts leave it NULL.
		// When SplitSvc is unwired the routes are absent entirely — clients get
		// a clean 404, never a fake accept page. splitHandler lives in the outer
		// NewRouter scope so the protected /nidus/rides group below can mount
		// Create/Get on the same instance.
		var splitHandler *nidusHttp.SplitHandler
		if deps.SplitSvc != nil {
			splitHandler = nidusHttp.NewSplitHandler(deps.SplitSvc)
			r.Group(func(r chi.Router) {
				r.Use(middleware.ExtractUserToContext)
				r.Route("/split", func(r chi.Router) {
					r.Post("/{token}/accept", splitHandler.Accept)
				})
			})
		}

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
				// §2.5 — Authoritative cancellation-fee quote. Shares one policy
				// implementation with POST /{rideID}/cancel below, so the number
				// the rider sees is the number they pay.
				r.Get("/{rideID}/cancellation-quote", rideHandler.CancellationQuote)
				// Rider-side terminal actions.
				r.Post("/{rideID}/cancel", rideHandler.CancelRide)
				r.Post("/{rideID}/rate", rideHandler.RateRide)
				// §3.1 Cash Payment Option: driver confirms physical collection of
				// the cash fare (completed -> paid + deferred settlement). Mounted
				// only when cash is enabled; otherwise clients get a clean 404 and
				// the tracking page never shows a confirm affordance.
				if deps.CashEnabled {
					r.Post("/{rideID}/confirm-cash-collection", rideHandler.ConfirmCashCollection)
				}
				// §3.3 Split Fare (rider/owner surface): POST creates/replaces the
				// pending invite set on a completed ride; GET returns the
				// authoritative split view (server-computed inviter share, never
				// trusted from a client). Mounted only when SplitSvc is wired —
				// otherwise a clean 404 and the receipt sheet renders its honest
				// "split unavailable" state.
				if deps.SplitSvc != nil {
					r.Post("/{rideID}/split", splitHandler.Create)
					r.Get("/{rideID}/split", splitHandler.Get)
				}
				// §3.4 Email Receipt: dispatches the itemized receipt through the
				// notifications module's email adapter. Always mounted (the handler
				// itself reports 503 RECEIPT_EMAIL_UNAVAILABLE when no mailer was
				// injected), so the Flutter receipt page can offer the action and
				// show an honest retry state instead of hiding it per-environment.
				r.Post("/{rideID}/email-receipt", rideHandler.EmailReceipt)
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
			// PAYMENT-METHOD REGISTRY (§3.1 Cash Payment Option). Mounted only when
			// the payments module is wired into composition (main.go); a nil service
			// means the routes are absent entirely, so clients get a clean 404 and
			// render their honest "payment methods unavailable" state rather than a
			// broken list. Endpoints: GET/POST /nidus/payment-methods,
			// DELETE /nidus/payment-methods/{id}, PATCH .../{id}/default.
			// ====================================================================
			if deps.PaymentMethodSvc != nil {
				paymentMethodHandler := payhttp.NewPaymentMethodHandler(deps.PaymentMethodSvc)
				r.Route("/nidus/payment-methods", func(r chi.Router) {
					r.Get("/", paymentMethodHandler.List)
					r.Post("/", paymentMethodHandler.Create)
					r.Delete("/{id}", paymentMethodHandler.Delete)
					r.Patch("/{id}/default", paymentMethodHandler.SetDefault)
				})
			}

			// ====================================================================
			// PROMOTIONS (§3.2 Promo Codes & Coupons). Mounted only when the
			// promotions module is wired into composition (main.go) AND the
			// FEATURE_PROMOTIONS kill switch is on — same binary, env flips the
			// behaviour. POST /promotions/redeem validates + projects a discount
			// WITHOUT consuming it (consumption happens at ride creation); GET
			// /promotions/mine lists codes the caller can currently redeem. When
			// unmounted clients get a clean 404 and the promo row renders its
			// honest "promos unavailable" state, never a fake code list.
			// ====================================================================
			if deps.PromoSvc != nil && deps.PromosEnabled {
				promoHandler := promohttp.NewHandler(deps.PromoSvc)
				r.Route("/promotions", func(r chi.Router) {
					r.Post("/redeem", promoHandler.Redeem)
					r.Get("/mine", promoHandler.Mine)
				})
			}

			// ====================================================================
			// LOCATION TRACKING (real-time)
			// ====================================================================
			r.Route("/nidus/location", func(r chi.Router) {
				r.Post("/update", locationHandler.UpdateLocation)
				r.Get("/driver/{driverID}", locationHandler.GetDriverLocation)
			})

			// ====================================================================
			// WEBSOCKET (§1.5 router cleanup: the hub existed but was never
			// mounted). Auth is enforced inside HandleWebSocket itself
			// (subprotocol / bearer / deprecated query token), so this route sits
			// outside AuthRequired — gorilla upgrades cannot run behind the JSON
			// 401 middleware. One shared hub also serves the notifications
			// module's in-app pushes via BroadcastToTopic (§1.4).
			// ====================================================================
			if deps.WSHub != nil {
				r.Get("/ws", deps.WSHub.HandleWebSocket)
			}
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
