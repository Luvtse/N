// Package observability provides the shared Prometheus metrics surface for
// the NIDAW backend (Phase I Step 2). All modules register their collectors
// here so /metrics exposes a single consistent namespace:
//
//   - Transaction throughput ......... ledger_transactions_total
//   - Latency p95 .................... http_request_duration_seconds (histogram)
//     + ledger_operation_duration_seconds
//   - Dispute rate ................... ledger_disputes_total / ledger_transactions_total{type="ride_payment"}
//   - Fraud alerts ................... ledger_fraud_flags_total
//   - Integrity alarms ............... ledger_hash_chain_corruptions_total,
//     ledger_balance_drift_total
//   - Provider health ................ ledger_provider_requests_total,
//     ledger_provider_down
//
// The registry is a package-level singleton; collectors are registered once
// lazily. Metrics must never carry user-identifying labels (cardinality +
// privacy): only operation/type/status-class labels are permitted.
package observability

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	registryOnce sync.Once
	registry     *prometheus.Registry
)

// Registry returns the process-wide metrics registry (Go runtime + process
// collectors included).
func Registry() *prometheus.Registry {
	registryOnce.Do(func() {
		registry = prometheus.NewRegistry()
		registry.MustRegister(
			collectors.NewGoCollector(),
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		)
	})
	return registry
}

// LedgerMetrics holds the money-movement collectors consumed by the ledger
// module (services, commands and Phase G jobs all increment through it).
type LedgerMetrics struct {
	// TransactionsTotal counts committed ledger entries by type — this is the
	// transaction-throughput metric Grafana graphs per second.
	TransactionsTotal *prometheus.CounterVec // labels: type
	// OperationDurationSeconds times ledger domain operations (credit, debit,
	// escrow_release, topup_verify, payout_submit...). Use with ObserveDuration.
	OperationDurationSeconds *prometheus.HistogramVec // labels: op
	// ErrorsTotal counts failed ledger operations by error class (the coded
	// error name, e.g. INSUFFICIENT_FUNDS, LOCK_CONFLICT) — feeds error-rate
	// alerting without leaking internal messages.
	ErrorsTotal *prometheus.CounterVec // labels: op, code
	// DisputesTotal counts filed disputes by outcome class ("filed", "refund",
	// "release"). Dispute rate = disputes / ride payments over a window.
	DisputesTotal *prometheus.CounterVec // labels: outcome
	// FraudFlagsTotal counts fraud evaluations that produced a hold by rule
	// reason (velocity, device, ip, ml, composite).
	FraudFlagsTotal *prometheus.CounterVec // labels: reason
	// HashChainCorruptionsTotal counts VerifyChain failures. ANY increase in
	// 5m pages on-call (PagerDuty route: severity=critical).
	HashChainCorruptionsTotal prometheus.Counter
	// BalanceDriftTotal counts cached-vs-rebuilt balance mismatches found by
	// the weekly rebuild job.
	BalanceDriftTotal *prometheus.CounterVec // labels: corrected
	// ProviderRequestsTotal counts outbound calls to payment rails.
	ProviderRequestsTotal *prometheus.CounterVec // labels: provider, status(ok/error)
	// ProviderDown is set to 1 when a rail's failure circuit has tripped;
	// 0 = healthy. Alert: any provider reporting 1 for 5m.
	ProviderDown *prometheus.GaugeVec // labels: provider
	// EscrowHoldsOpen gauges escrow holds awaiting release.
	EscrowHoldsOpen prometheus.Gauge
	// WithdrawalPendingGaugeETB gauges pending payout exposure in ETB.
	WithdrawalPendingGaugeETB prometheus.Gauge
}

var (
	ledgerOnce sync.Once
	ledgerM    *LedgerMetrics
)

// Ledger returns the shared ledger collector set (registered lazily on first
// use so tests that never touch metrics stay clean).
func Ledger() *LedgerMetrics {
	ledgerOnce.Do(func() {
		r := Registry()
		m := &LedgerMetrics{
			TransactionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "ledger",
				Name:      "transactions_total",
				Help:      "Committed ledger transactions by type (throughput).",
			}, []string{"type"}),
			OperationDurationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "ledger",
				Name:      "operation_duration_seconds",
				Help:      "Latency of ledger domain operations (p95 via histogram_quantile).",
				Buckets:   prometheus.ExponentialBuckets(0.005, 2, 12), // 5ms..~20s
			}, []string{"op"}),
			ErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "ledger",
				Name:      "operation_errors_total",
				Help:      "Failed ledger operations by op and coded error class.",
			}, []string{"op", "code"}),
			DisputesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "ledger",
				Name:      "disputes_total",
				Help:      "Ride disputes by outcome class (numerator of dispute rate).",
			}, []string{"outcome"}),
			FraudFlagsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "ledger",
				Name:      "fraud_flags_total",
				Help:      "Fraud evaluations resulting in a hold, by triggering rule.",
			}, []string{"reason"}),
			HashChainCorruptionsTotal: prometheus.NewCounter(prometheus.CounterOpts{
				Namespace: "ledger",
				Name:      "hash_chain_corruptions_total",
				Help:      "Hash-chain verification failures. Any increase is critical.",
			}),
			BalanceDriftTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "ledger",
				Name:      "balance_drift_total",
				Help:      "Cached-vs-chain balance mismatches detected by the rebuild job.",
			}, []string{"corrected"}),
			ProviderRequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "ledger",
				Name:      "provider_requests_total",
				Help:      "Outbound payment-rail calls by provider and result.",
			}, []string{"provider", "status"}),
			ProviderDown: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: "ledger",
				Name:      "provider_down",
				Help:      "1 when a payment rail's failure circuit is tripped.",
			}, []string{"provider"}),
			EscrowHoldsOpen: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "ledger",
				Name:      "escrow_holds_open",
				Help:      "Escrow holds currently awaiting release.",
			}),
			WithdrawalPendingGaugeETB: prometheus.NewGauge(prometheus.GaugeOpts{
				Namespace: "ledger",
				Name:      "withdrawal_pending_etb",
				Help:      "Aggregate withdrawal amount awaiting payout, in ETB.",
			}),
		}
		r.MustRegister(
			m.TransactionsTotal, m.OperationDurationSeconds, m.ErrorsTotal,
			m.DisputesTotal, m.FraudFlagsTotal, m.HashChainCorruptionsTotal,
			m.BalanceDriftTotal, m.ProviderRequestsTotal, m.ProviderDown,
			m.EscrowHoldsOpen, m.WithdrawalPendingGaugeETB,
		)
		ledgerM = m
	})
	return ledgerM
}

// HTTPMetrics backs the chi latency middleware feeding the standard
// http_request_duration_seconds histogram (P95 alert source).
type HTTPMetrics struct {
	RequestsTotal *prometheus.CounterVec   // labels: method, pattern, status
	InFlight      *prometheus.GaugeVec     // labels: method
	DurationSecs  *prometheus.HistogramVec // labels: method, pattern
}

var (
	httpOnce sync.Once
	httpM    *HTTPMetrics
)

// HTTP returns the shared HTTP collector set.
func HTTP() *HTTPMetrics {
	httpOnce.Do(func() {
		r := Registry()
		m := &HTTPMetrics{
			RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
				Namespace: "http",
				Name:      "requests_total",
				Help:      "HTTP requests by method, route pattern and status class.",
			}, []string{"method", "pattern", "status"}),
			InFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Namespace: "http",
				Name:      "in_flight_requests",
				Help:      "In-flight HTTP requests by method.",
			}, []string{"method"}),
			DurationSecs: prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "http",
				Name:      "request_duration_seconds",
				Help:      "HTTP request latency histogram (p50/p95/p99).",
				Buckets:   prometheus.DefBuckets,
			}, []string{"method", "pattern"}),
		}
		r.MustRegister(m.RequestsTotal, m.InFlight, m.DurationSecs)
		httpM = m
	})
	return httpM
}

// Middleware returns a chi middleware recording request latency/counters. It
// uses the chi route Pattern so label cardinality stays bounded (raw URLs
// containing ids would explode the series count).
func Middleware() func(http.Handler) http.Handler {
	m := HTTP()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			m.InFlight.WithLabelValues(r.Method).Inc()
			sw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			m.InFlight.WithLabelValues(r.Method).Dec()

			// chi exposes the matched route template via RoutePattern in its
			// context (http.Request.Pattern only exists on Go 1.20+; this
			// module builds on go 1.19).
			pattern := chi.RouteContext(r.Context()).RoutePattern()
			if pattern == "" {
				pattern = "unmatched"
			}
			elapsed := time.Since(start).Seconds()
			m.DurationSecs.WithLabelValues(r.Method, pattern).Observe(elapsed)
			m.RequestsTotal.WithLabelValues(
				r.Method, pattern, strconv.Itoa(sw.status/100)+"xx",
			).Inc()
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Handler returns the /metrics promhttp handler.
func Handler() http.Handler {
	return promhttp.HandlerFor(Registry(), promhttp.HandlerOpts{})
}

// RegisterMetricsRoute mounts GET /metrics on r (used at the top level of the
// server router; kept off the public ingress by Kong rules — see
// monitoring/prometheus/prometheus.yml scrape config).
func RegisterMetricsRoute(r chi.Router) {
	r.Get("/metrics", func(w http.ResponseWriter, req *http.Request) {
		Handler().ServeHTTP(w, req)
	})
}

// ObserveDuration times an operation: defer observability.Ledger().
// ObserveDuration("credit", time.Now(), err-var). On non-nil err it also
// increments ErrorsTotal with code "error" (callers with typed codes should
// increment directly).
func (l *LedgerMetrics) ObserveDuration(op string, start time.Time, err error) {
	l.OperationDurationSeconds.WithLabelValues(op).Observe(time.Since(start).Seconds())
	if err != nil {
		l.ErrorsTotal.WithLabelValues(op, "error").Inc()
	}
}

// RecordProviderResult increments the rail counter for a completed call.
func (l *LedgerMetrics) RecordProviderResult(provider string, ok bool) {
	status := "ok"
	if !ok {
		status = "error"
	}
	l.ProviderRequestsTotal.WithLabelValues(provider, status).Inc()
}

// RecordDispute counts one dispute lifecycle event by outcome class
// ("filed", "refund", "release", "split"). Dispute rate is computed in
// Grafana as disputes_total{outcome="filed"} / transactions_total{type="ride_payment"}.
func (l *LedgerMetrics) RecordDispute(outcome string) {
	l.DisputesTotal.WithLabelValues(outcome).Inc()
}

// RecordDrift logs one cached-vs-chain balance mismatch found by the weekly
// rebuild job; corrected reports whether ApplyCorrections rewrote the cache.
func (l *LedgerMetrics) RecordDrift(corrected bool) {
	c := "false"
	if corrected {
		c = "true"
	}
	l.BalanceDriftTotal.WithLabelValues(c).Inc()
}

// SetProviderDown flips the health gauge for a rail (1 = circuit tripped).
// Any provider reporting 1 for 5m fires the PagerDuty ProviderDown alert.
func (l *LedgerMetrics) SetProviderDown(provider string, down bool) {
	v := 0.0
	if down {
		v = 1.0
	}
	l.ProviderDown.WithLabelValues(provider).Set(v)
}
