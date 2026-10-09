package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	authhttp "nidaw-backend/internal/modules/auth/interfaces/http"
	ledgermod "nidaw-backend/internal/modules/ledger"
	ledgeradapters "nidaw-backend/internal/modules/ledger/infrastructure/adapters"
	ledgerhttp "nidaw-backend/internal/modules/ledger/interfaces/http"
	legalsvc "nidaw-backend/internal/modules/legal/application/services"
	legalhttp "nidaw-backend/internal/modules/legal/interfaces/http"
	niduscmds "nidaw-backend/internal/modules/nidus/application/commands"
	nidusservices "nidaw-backend/internal/modules/nidus/application/services"
	nidusinfra "nidaw-backend/internal/modules/nidus/infrastructure/cache"
	nidushttp "nidaw-backend/internal/modules/nidus/interfaces/http"
	nidushandlers "nidaw-backend/internal/modules/nidus/interfaces/http/handlers"
	"nidaw-backend/internal/shared/auth"
	sharedcache "nidaw-backend/internal/shared/cache"
	"nidaw-backend/internal/shared/config"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"
	"nidaw-backend/internal/shared/observability"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Initialize logger
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// Initialize database
	db, err := database.NewPostgres(&database.Config{
		Host:            cfg.Database.Host,
		Port:            cfg.Database.Port,
		User:            cfg.Database.User,
		Password:        cfg.Database.Password,
		DBName:          cfg.Database.DBName,
		SSLMode:         cfg.Database.SSLMode,
		MaxConns:        cfg.Database.MaxConns,
		MinConns:        cfg.Database.MinConns,
		MaxConnLifetime: cfg.Database.MaxConnLifetime,
		MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
		ConnectTimeout:  cfg.Database.ConnectTimeout,
		QueryTimeout:    cfg.Database.QueryTimeout,
		LogLevel:        cfg.Database.LogLevel,
	}, logger)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}
	defer db.Close()

	// Initialize Kafka event bus
	eventBus := eventbus.NewKafkaBus(cfg.Kafka.Brokers)
	defer eventBus.Close()

	// Initialize auth service
	authService, err := auth.NewService(auth.ServiceConfig{
		JWTSecret:  cfg.Auth.JWTSecret,
		AccessTTL:  cfg.Auth.AccessTokenTTL,
		RefreshTTL: cfg.Auth.RefreshTokenTTL,
		Issuer:     cfg.Auth.Issuer,
		Audience:   cfg.Auth.Audience,
	}, logger)
	if err != nil {
		logger.Fatal("Failed to create auth service", zap.Error(err))
	}

	// Initialize nidus domain services
	pricingService := nidusservices.NewPricingService(db, nidusservices.DefaultPricingConfig())
	matchingEngine := nidusservices.NewMatchingEngine(db, eventBus)
	etaService := nidusservices.NewETAService(db)

	// Initialize Redis cache + driver location cache (required by nidus router)
	redisCache, err := sharedcache.NewRedisCache(cfg.Redis.URL)
	if err != nil {
		logger.Fatal("Failed to create redis cache", zap.Error(err))
	}
	defer redisCache.Close()
	locationCache := nidusinfra.NewDriverLocationCache(redisCache)

	// Phase B/B5: wire Redis-backed token store (refresh rotation + revocation denylist).
	tokenStore := auth.NewTokenStore(redisCache.RawClient())
	authService.SetTokenStore(tokenStore)

	// Phase D: private permissioned ledger (feature-flagged until rollout).
	// Phase E: Ethiopian payment rails (Telebirr / Chapa / M-Pesa) feed the
	// ledger's TopupVerifier + PayoutInitiator ports through a shared gateway
	// resolver; unconfigured rails simply leave the module in pass-through
	// mode so boot never hard-fails on missing sandbox credentials.
	var ledgerComps *ledgermod.Components
	var ledgerResolver ledgeradapters.GatewayResolver
	if cfg.Features.EnableInternalLedger {
		resolver, err := ledgeradapters.BuildResolver(cfg, logger)
		if err != nil {
			logger.Fatal("Failed to build payment gateway resolver", zap.Error(err))
		}
		ledgerResolver = resolver

		lc, err := ledgermod.Build(ledgermod.Config{
			Pool:         db.Pool(),
			Redis:        redisCache.RawClient(),
			Logger:       logger,
			ReportKey:    os.Getenv("LEDGER_REPORT_KEY"), // B9: secret via env/secret manager, never committed
			EscrowWindow: escrowWindowFromEnv(logger),    // Phase F: 72h default, overridable for staged rollouts
			Events:       ledgeradapters.NewEventBusPublisher(eventBus),
			Resolver:     resolver, // Phase E: bridges Verifier/Payouts automatically
			// Phase G: fraud rule stack (velocity + device/IP + ML-with-fallback)
			// and the reconciliation / balance-rebuild integrity jobs.
			EnableFraud:      true,
			MLBaseURL:        os.Getenv("FRAUD_ML_BASE_URL"), // "" => rules-only fallback
			MLToken:          os.Getenv("FRAUD_ML_TOKEN"),    // env-driven, never committed
			ReconInterval:    durationFromEnv("LEDGER_RECON_INTERVAL", 24*time.Hour, logger),
			RebuildInterval:  durationFromEnv("LEDGER_REBUILD_INTERVAL", 7*24*time.Hour, logger),
			ApplyCorrections: os.Getenv("LEDGER_REBUILD_APPLY") == "true", // opt-in; default alert-only
		})
		if err != nil {
			logger.Fatal("Failed to build ledger module", zap.Error(err))
		}
		ledgerComps = lc
	}

	// Create main router
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.Timeout(30 * time.Second))
	// Phase I Step 2: Prometheus request instrumentation (bounded-label
	// counters/histograms; exported at GET /metrics below).
	r.Use(observability.Middleware())

	// Phase I Step 2: metrics scrape endpoint. Kong strips this path from the
	// public ingress (see monitoring/prometheus/prometheus.yml, which scrapes
	// backend:8080 directly on the private network).
	observability.RegisterMetricsRoute(r)

	// Mount module routers
	r.Mount("/", authhttp.NewRouter(db, authService, logger))

	// Legal/consent module (Phase B/B2: admin-gated document management)
	consentService := legalsvc.NewConsentService(db)
	legalhttp.RegisterConsentRoutes(r,
		legalhttp.NewConsentHandler(consentService),
		legalhttp.NewAdminHandler(db),
		authService)
	// Nidus ride module router. Tip settlement is wired only when the ledger
	// is enabled; otherwise rated tips fail explicitly (never silently lost).
	nidusDeps := &nidushttp.Dependencies{
		DB:             db,
		EventBus:       eventBus,
		Logger:         logger,
		AuthService:    authService,
		CacheService:   locationCache,
		MatchingEngine: matchingEngine,
		ETAService:     etaService,
		PricingService: pricingService,
		CORSOrigins:    cfg.Server.CORSOrigins, // Phase B/B4: env-driven allowlist
	}
	if ledgerComps != nil {
		nidusDeps.TipSettler = nidushandlers.NewLedgerTipSettler(ledgerComps.Deps.Ledger)
	}
	r.Mount("/", nidushttp.NewRouter(nidusDeps))

	// Ride lifecycle durability workers (Nidus):
	//  - AutoMatcher closes the dead-code matching gap: every tick it assigns
	//    the best available driver to unassigned rides (race-guarded UPDATE,
	//    multi-replica safe).
	//  - SettlementOutbox re-publishes ride_completed_events rows whose broker
	//    publish failed at completion time, guaranteeing the ledger eventually
	//    settles every completed ride.
	ctxRideWorkers, stopRideWorkers := context.WithCancel(context.Background())
	defer stopRideWorkers()
	autoMatcher := niduscmds.NewAutoMatcher(db, matchingEngine, eventBus, niduscmds.AutoMatchConfig{}, logger)
	go autoMatcher.Run(ctxRideWorkers)
	settlementOutbox := niduscmds.NewSettlementOutbox(db, eventBus, 30*time.Second)
	go settlementOutbox.Run(ctxRideWorkers)
	logger.Info("Nidus ride lifecycle workers online",
		zap.String("auto_matcher", "5s cadence"),
		zap.String("settlement_outbox", "30s cadence"))

	// Phase D Step 6: ledger REST surface behind JWT auth.
	if ledgerComps != nil {
		r.Mount("/api/v1/ledger", ledgerhttp.NewRouter(ledgerComps.Deps, authService, logger))

		// Inbound settlement: ride.completed events debit the rider and
		// credit the driver into escrow (idempotent per ride id).
		settlement := ledgeradapters.NewRideSettlementHandler(ledgerComps.Deps)
		if err := eventBus.Subscribe(context.Background(), "nidaw.events", func(ev eventbus.Event) {
			if herr := settlement.Handle(context.Background(), ev); herr != nil {
				logger.Error("ledger settlement failed",
					zap.String("event_type", ev.Type), zap.Error(herr))
			}
		}); err != nil {
			logger.Error("Failed to subscribe ledger settlement consumer", zap.Error(err))
		}

		// Phase E: provider callbacks + async top-up/payout workers.
		if ledgerResolver != nil {
			// Public webhook endpoints (signature-verified inside each rail
			// adapter; they never settle money without re-verifying via
			// VerifyPayment — see adapters/webhooks.go).
			webhooks := ledgeradapters.NewWebhookHandler(ledgerComps.Deps, ledgerResolver, logger)
			r.Route("/api/v1/payments", func(pr chi.Router) {
				webhooks.Register(pr)
			})

			ctxJobs, stopJobs := context.WithCancel(context.Background())
			defer stopJobs()

			topupJob := ledgeradapters.NewTopupJob(ledgerComps.Deps, ledgerResolver, nil, ledgeradapters.TopupJobConfig{}, logger)
			go topupJob.Run(ctxJobs)

			payoutJob := ledgeradapters.NewPayoutJob(ledgerComps.Deps, ledgerResolver, ledgeradapters.PayoutJobConfig{}, logger)
			go payoutJob.Run(ctxJobs)

			logger.Info("Phase E payment rails online",
				zap.Bool("active_provider_configured", true))
		} else {
			logger.Warn("No payment rails configured — ledger running in pass-through mode (top-ups/withdrawals will not reach providers)")
		}

		// Phase F Step 2: hourly escrow release worker (held -> available for
		// matured, undisputed holds; idempotent per hold id). Runs regardless
		// of payment-rail configuration — escrow maturity is a ledger-only
		// concern. Dispute pauses are enforced by the hold status itself
		// (FileDispute flips 'held' -> 'disputed', excluding it from the
		// releasable query), so no extra coordination is needed here.
		escrowReleaseJob := ledgeradapters.NewEscrowReleaseJob(
			ledgerComps.Deps, ledgerComps.Deps.EscrowPolicyOrNil(),
			ledgeradapters.EscrowReleaseConfig{}, logger)
		ctxEscrow, stopEscrow := context.WithCancel(context.Background())
		defer stopEscrow()
		go escrowReleaseJob.Run(ctxEscrow)
		if p := ledgerComps.Deps.EscrowPolicyOrNil(); p != nil {
			logger.Info("Phase F escrow release job online",
				zap.String("window", p.Window().String()))
		} else {
			logger.Info("Phase F escrow release job online (default 72h window)")
		}

		// Phase G Steps 2-3: integrity jobs. Reconciliation compares completed
		// top-ups/payouts against provider truth daily (mismatches > 0.01 ETB
		// become fraud_flags + finance report logs); the balance rebuild walks
		// every user's hash chain weekly and alerts on cache drift (corrections
		// are opt-in via LEDGER_REBUILD_APPLY). Both are constructed inside
		// ledgermod.Build only when EnableFraud, so a nil check is enough here.
		if ledgerComps.Reconciliation != nil {
			ctxRecon, stopRecon := context.WithCancel(context.Background())
			defer stopRecon()
			go ledgerComps.Reconciliation.Run(ctxRecon)
			logger.Info("Phase G reconciliation job online (daily provider-vs-ledger diff)")
		}
		if ledgerComps.BalanceRebuild != nil {
			ctxRebuild, stopRebuild := context.WithCancel(context.Background())
			defer stopRebuild()
			go ledgerComps.BalanceRebuild.Run(ctxRebuild)
			logger.Info("Phase G balance rebuild job online (weekly chain-truth audit)")
		}
	}

	// Create server
	srv := &http.Server{
		Addr:         ":" + cfg.Server.Port,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Start server
	go func() {
		logger.Info("Starting server",
			zap.String("port", cfg.Server.Port),
			zap.String("environment", cfg.Server.Environment),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("Server forced to shutdown", zap.Error(err))
	}

	logger.Info("Server exited gracefully")
}

// escrowWindowFromEnv reads the Phase F dispute/release hold window from
// LEDGER_ESCROW_WINDOW_HOURS (integer hours). Unset or invalid values fall
// back to the product default of 72h; non-positive overrides are rejected so
// a typo can never disable the safety window entirely.
func escrowWindowFromEnv(logger *zap.Logger) time.Duration {
	v := os.Getenv("LEDGER_ESCROW_WINDOW_HOURS")
	if v == "" {
		return ledgermod.EscrowWindow
	}
	h, err := strconv.Atoi(v)
	if err != nil || h <= 0 {
		logger.Warn("invalid LEDGER_ESCROW_WINDOW_HOURS, using 72h default",
			zap.String("value", v))
		return ledgermod.EscrowWindow
	}
	return time.Duration(h) * time.Hour
}

// durationFromEnv parses a Go duration (e.g. "24h", "90m") from the named env
// var, falling back to def when unset/invalid/non-positive. Used by the Phase
// G job cadences so ops can tune them without redeploying code.
func durationFromEnv(key string, def time.Duration, logger *zap.Logger) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		logger.Warn("invalid "+key+", using default",
			zap.String("value", v), zap.String("default", def.String()))
		return def
	}
	return d
}
