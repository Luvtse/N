// metrics_collector.go — Phase I Step 2: gauge collector for the two
// exposure metrics that cannot be cheaply incremented inline:
//
//   - ledger_escrow_holds_open        (count of held+disputed escrows)
//   - ledger_withdrawal_pending_etb   (sum of non-terminal withdrawal cents)
//
// The collector runs on a slow ticker (default 1m) and refreshes both gauges
// from Postgres. It is intentionally read-only and error-tolerant: a failed
// tick keeps the previous values (stale gauges are far better than zeroing
// real exposure, which would mask risk dashboards/alerts).
package adapters

import (
"context"
"time"

"go.uber.org/zap"

"nidaw-backend/internal/shared/observability"

"github.com/jackc/pgx/v5/pgxpool"
)

// MetricsCollectorConfig tunes the gauge refresh loop.
type MetricsCollectorConfig struct {
Interval time.Duration // default 1m
}

func (c *MetricsCollectorConfig) applyDefaults() {
if c.Interval <= 0 {
c.Interval = time.Minute
}
}

// MetricsCollector owns the DB-backed ledger gauges.
type MetricsCollector struct {
pool *pgxpool.Pool
cfg  MetricsCollectorConfig
log  *zap.Logger
}

func NewMetricsCollector(pool *pgxpool.Pool, cfg MetricsCollectorConfig, log *zap.Logger) *MetricsCollector {
if log == nil {
log = zap.NewNop()
}
cfg.applyDefaults()
return &MetricsCollector{pool: pool, cfg: cfg, log: log}
}

// Run refreshes gauges immediately, then on every tick until ctx is cancelled.
func (j *MetricsCollector) Run(ctx context.Context) {
j.Tick(ctx)
t := time.NewTicker(j.cfg.Interval)
defer t.Stop()
for {
select {
case <-ctx.Done():
return
case <-t.C:
j.Tick(ctx)
}
}
}

// Tick performs one refresh pass (exported for tests/manual triggers).
func (j *MetricsCollector) Tick(ctx context.Context) {
m := observability.Ledger()

var escrows float64
err := j.pool.QueryRow(ctx,
`SELECT count(*) FROM escrow_holds WHERE status IN ('held','disputed')`).Scan(&escrows)
if err != nil {
j.log.Warn("metrics: escrow gauge refresh failed", zap.Error(err))
} else {
m.EscrowHoldsOpen.Set(escrows)
}

var pendingCents int64
err = j.pool.QueryRow(ctx,
`SELECT COALESCE(sum(amount_cents),0) FROM withdrawal_requests
  WHERE status IN ('pending','fraud_hold','approved','processing')`).Scan(&pendingCents)
if err != nil {
j.log.Warn("metrics: payout gauge refresh failed", zap.Error(err))
} else {
// amount_cents are santim; expose ETB for currencyETB panels.
m.WithdrawalPendingGaugeETB.Set(float64(pendingCents) / 100.0)
}
}
