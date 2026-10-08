// Package ledger is the composition root for the private permissioned ledger
// module (Phase D). It exposes a single Build function that wires the
// hexagonal stack — repositories -> services -> commands -> HTTP handler —
// so cmd/server stays declarative.
package ledger

import (
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"nidaw-backend/internal/modules/ledger/application/commands"
	"nidaw-backend/internal/modules/ledger/application/services"
	"nidaw-backend/internal/modules/ledger/infrastructure/adapters"
	"nidaw-backend/internal/modules/ledger/infrastructure/repositories"
	ledgerhttp "nidaw-backend/internal/modules/ledger/interfaces/http"
)

// EscrowWindow is the 3-day safety hold applied to driver earnings after a
// completed ride (Phase D Step 1 / Phase F).
const EscrowWindow = 72 * time.Hour

// Components is the assembled ledger dependency graph.
type Components struct {
	Deps     *commands.Deps
	Handler  *ledgerhttp.Handler
	Resolver adapters.GatewayResolver // Phase E rails (nil when unconfigured)

	// Phase G integrity jobs (nil unless EnableFraud). Callers start them with
	// go Components.Reconciliation.Run(ctx) / Components.BalanceRebuild.Run(ctx).
	Reconciliation *adapters.ReconciliationJob
	BalanceRebuild *adapters.BalanceRebuildJob
}

// Config controls wiring. Optional interfaces (Verifier, Payouts, Fraud,
// DriverStats) may be nil; the command layer treats nil as "pass-through",
// so the ledger is fully functional with just Postgres + Redis. When
// EnableFraud is set and Fraud was not supplied explicitly, Build wires the
// full Phase G stack itself (Postgres fraud_flags repo + Redis device/IP
// ports + optional ML endpoint).
type Config struct {
	Pool         *pgxpool.Pool
	Redis        *redis.Client
	Logger       *zap.Logger
	ReportKey    string        // HMAC secret for signed audit reports (env-driven)
	EscrowWindow time.Duration // Phase F: dispute/release hold window (<=0 => 72h default)
	Events       commands.EventPublisher
	Resolver     adapters.GatewayResolver // Phase E: Ethiopian rails (may be nil)
	Verifier     commands.TopupVerifier
	Payouts      commands.PayoutInitiator
	Fraud        commands.FraudEvaluator
	DriverStats  commands.DriverStatsProvider

	// Phase G: fraud detection & integrity jobs.
	EnableFraud      bool                 // construct the rule-stack evaluator when true
	FraudConfig      services.FraudConfig // zero values => product defaults
	MLBaseURL        string               // ml/fraud-detection endpoint ("" => rules-only fallback)
	MLToken          string               // optional bearer token (env-driven, never committed)
	ReconInterval    time.Duration        // reconciliation cadence (<=0 => daily)
	RebuildCron      string               // reserved: cron expression support (unused; ticker interval below)
	RebuildInterval  time.Duration        // balance-rebuild cadence (<=0 => weekly)
	ApplyCorrections bool                 // rebuild job rewrites cached balances from chain truth
}

// Build constructs the ledger module. Pool is mandatory; Redis may be nil
// (chain-head cache then falls back to the DB every append); an empty
// ReportKey yields unsigned reports (dev only).
func Build(cfg Config) (*Components, error) {
	if cfg.Pool == nil {
		return nil, errors.New("ledger: pgx pool is required")
	}
	log := cfg.Logger
	if log == nil {
		log = zap.NewNop()
	}

	// --- payment rails (Phase E): optional ------------------------------------
	// When a resolver is supplied and no explicit Verifier/Payouts were wired,
	// build the bridges so top-ups settle against provider truth and payouts
	// submit to the correct rail.
	if cfg.Resolver != nil {
		if cfg.Verifier == nil {
			cfg.Verifier = adapters.NewTopupBridge(cfg.Resolver)
		}
		if cfg.Payouts == nil {
			cfg.Payouts = adapters.NewPayoutBridge(cfg.Resolver)
		}
	}

	// --- infrastructure adapters -------------------------------------------
	uow := repositories.NewUow(cfg.Pool)
	bals := repositories.NewBalanceRepo(cfg.Pool)
	txs := repositories.NewTxRepo(cfg.Pool)
	topups := repositories.NewTopupRepo(cfg.Pool)
	withdrawals := repositories.NewWithdrawalRepo(cfg.Pool)
	escrows := repositories.NewEscrowRepo(cfg.Pool)
	disputes := repositories.NewDisputeRepo(cfg.Pool)
	audit := repositories.NewAuditRepo(cfg.Pool)

	var heads services.ChainHeadCache
	if cfg.Redis != nil {
		heads = services.NewRedisChainHeadCache(cfg.Redis)
	}

	// --- application services (Steps 3 & 4) --------------------------------
	chain := services.NewHashChainService(txs, bals, heads, log)
	ledgerSvc := services.NewLedgerService(uow, bals, chain, log)
	// Phase F: single source of truth for escrow/dispute policy, shared by
	// settlement, FileDispute and the hourly EscrowReleaseJob.
	escrowSvc := services.NewEscrowServiceWithWindow(cfg.EscrowWindow)

	// --- Phase G: fraud detection stack --------------------------------------
	// EnableFraud builds the full rule stack: velocity (TxRepo), device/IP
	// signals (Redis ports), optional ML scoring with rule-based fallback, and
	// fraud_flags persistence (Postgres). Redis being nil degrades gracefully —
	// FraudDetectionService skips missing signals rather than treating them as
	// risky, so a Postgres-only deployment still gets velocity + flags.
	var fraudSvc *services.FraudDetectionService
	if cfg.EnableFraud && cfg.Fraud == nil {
		fraudRepo := repositories.NewFraudRepo(cfg.Pool)
		var devices services.DeviceIndex
		var ips services.IPDenylist
		if cfg.Redis != nil {
			devices = services.NewRedisDeviceIndex(cfg.Redis)
			ips = services.NewRedisIPDenylist(cfg.Redis)
		}
		ml, err := services.NewMLClient(services.MLClientConfig{BaseURL: cfg.MLBaseURL, Token: cfg.MLToken})
		if err != nil {
			return nil, fmt.Errorf("ledger: ml fraud client: %w", err)
		}
		fraudSvc, err = services.NewFraudDetectionService(txs, fraudRepo, devices, ips, ml, cfg.FraudConfig, log)
		if err != nil {
			return nil, fmt.Errorf("ledger: fraud service: %w", err)
		}
		cfg.Fraud = fraudSvc
		if ml == nil {
			log.Info("ledger: fraud evaluation running rules-only (no ML endpoint configured)")
		}
	}

	deps := &commands.Deps{
		Uow:          uow,
		Ledger:       ledgerSvc,
		Chain:        chain,
		Balances:     bals,
		Txs:          txs,
		Topups:       topups,
		Withdrawals:  withdrawals,
		Escrows:      escrows,
		Disputes:     disputes,
		Audit:        audit,
		Events:       cfg.Events,
		Verifier:     cfg.Verifier,
		Payouts:      cfg.Payouts,
		Fraud:        cfg.Fraud,
		DriverStats:  cfg.DriverStats,
		EscrowPolicy: escrowSvc,
		Clock:        systemClock{},
	}

	// --- HTTP interface (Step 6) --------------------------------------------
	var signer ledgerhttp.ReportSigner
	if cfg.ReportKey != "" {
		s, err := ledgerhttp.NewHMACReportSigner(cfg.ReportKey)
		if err != nil {
			return nil, err
		}
		signer = s
	} else {
		log.Warn("ledger audit reports will be UNSIGNED: set LEDGER_REPORT_KEY")
	}

	comps := &Components{Deps: deps, Handler: ledgerhttp.NewHandler(deps, log, signer), Resolver: cfg.Resolver}

	// --- Phase G integrity jobs (constructed here, started by the caller) ----
	if fraudSvc != nil {
		fraudRepo := repositories.NewFraudRepo(cfg.Pool)
		recon, err := adapters.NewReconciliationJob(deps, cfg.Resolver, fraudRepo,
			adapters.ReconConfig{Interval: cfg.ReconInterval}, log)
		if err != nil {
			return nil, fmt.Errorf("ledger: reconciliation job: %w", err)
		}
		rebuild, err := adapters.NewBalanceRebuildJob(deps, fraudRepo,
			adapters.RebuildConfig{Interval: cfg.RebuildInterval, ApplyCorrections: cfg.ApplyCorrections}, log)
		if err != nil {
			return nil, fmt.Errorf("ledger: balance rebuild job: %w", err)
		}
		comps.Reconciliation = recon
		comps.BalanceRebuild = rebuild

		// Admin console read APIs (Phase H Step 3): fraud review queue and
		// reconciliation dashboard. Degrade independently if absent.
		comps.Handler.SetAdminConsolePorts(fraudRepo, recon)
	}

	return comps, nil
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }
