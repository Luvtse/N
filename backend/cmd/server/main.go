package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	authhttp "nidaw-backend/internal/modules/auth/interfaces/http"
	ledgermod "nidaw-backend/internal/modules/ledger"
	ledgeradapters "nidaw-backend/internal/modules/ledger/infrastructure/adapters"
	ledgerhttp "nidaw-backend/internal/modules/ledger/interfaces/http"
	legalsvc "nidaw-backend/internal/modules/legal/application/services"
	legalhttp "nidaw-backend/internal/modules/legal/interfaces/http"
	nidusservices "nidaw-backend/internal/modules/nidus/application/services"
	nidusinfra "nidaw-backend/internal/modules/nidus/infrastructure/cache"
	nidushttp "nidaw-backend/internal/modules/nidus/interfaces/http"
	"nidaw-backend/internal/shared/auth"
	sharedcache "nidaw-backend/internal/shared/cache"
	"nidaw-backend/internal/shared/config"
	"nidaw-backend/internal/shared/database"
	"nidaw-backend/internal/shared/eventbus"

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
			Pool:      db.Pool(),
			Redis:     redisCache.RawClient(),
			Logger:    logger,
			ReportKey: os.Getenv("LEDGER_REPORT_KEY"), // B9: secret via env/secret manager, never committed
			Events:    ledgeradapters.NewEventBusPublisher(eventBus),
			Resolver:  resolver, // Phase E: bridges Verifier/Payouts automatically
			// Fraud adapter arrives with Phase G; nil means pass-through.
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

	// Mount module routers
	r.Mount("/", authhttp.NewRouter(db, authService, logger))

	// Legal/consent module (Phase B/B2: admin-gated document management)
	consentService := legalsvc.NewConsentService(db)
	legalhttp.RegisterConsentRoutes(r,
		legalhttp.NewConsentHandler(consentService),
		legalhttp.NewAdminHandler(db),
		authService)
	r.Mount("/", nidushttp.NewRouter(&nidushttp.Dependencies{
		DB:             db,
		EventBus:       eventBus,
		Logger:         logger,
		AuthService:    authService,
		CacheService:   locationCache,
		MatchingEngine: matchingEngine,
		ETAService:     etaService,
		PricingService: pricingService,
		CORSOrigins:    cfg.Server.CORSOrigins, // Phase B/B4: env-driven allowlist
	}))

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
