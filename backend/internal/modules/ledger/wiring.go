// Package ledger is the composition root for the private permissioned ledger
// module (Phase D). It exposes a single Build function that wires the
// hexagonal stack — repositories -> services -> commands -> HTTP handler —
// so cmd/server stays declarative.
package ledger

import (
	"errors"
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
}

// Config controls wiring. Optional interfaces (Verifier, Payouts, Fraud,
// DriverStats) are intentionally left nil until Phases E/G land; the command
// layer treats nil as "pass-through", so the ledger is fully functional with
// just Postgres + Redis.
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

	return &Components{Deps: deps, Handler: ledgerhttp.NewHandler(deps, log, signer), Resolver: cfg.Resolver}, nil
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }
