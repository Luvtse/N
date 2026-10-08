#!/usr/bin/env python3
"""One-shot generator for monitoring/grafana/dashboards/ledger-metrics.json.

Phase I Step 2: Grafana panels for Real-time Volume, Balance Distribution and
Revenue/rails over the ledger_* metrics exported by
backend/internal/shared/observability/metrics.go at GET /metrics.

Run from repo root: python3 monitoring/grafana/dashboards/gen_ledger_dashboard.py
"""
import json
import os


def ts_panel(pid, title, x, y, w, h, exprs, unit=None):
    return {
        "id": pid, "type": "timeseries", "title": title,
        "datasource": {"type": "prometheus", "uid": "${DS_PROMETHEUS}"},
        "gridPos": {"h": h, "w": w, "x": x, "y": y},
        "fieldConfig": {"defaults": {"unit": unit or "short",
                                     "custom": {"drawStyle": "line", "fillOpacity": 10}},
                        "overrides": []},
        "targets": [{"refId": chr(65 + i), "expr": e, "legendFormat": l}
                    for i, (e, l) in enumerate(exprs)],
    }


def stat_panel(pid, title, x, y, expr, unit=None):
    return {
        "id": pid, "type": "stat", "title": title,
        "datasource": {"type": "prometheus", "uid": "${DS_PROMETHEUS}"},
        "gridPos": {"h": 4, "w": 6, "x": x, "y": y},
        "fieldConfig": {"defaults": {"unit": unit or "short"}, "overrides": []},
        "targets": [{"refId": "A", "expr": expr}],
    }


def row(pid, title, y):
    return {"id": pid, "type": "row", "title": title, "collapsed": False,
            "gridPos": {"h": 1, "w": 24, "x": 0, "y": y}}


# Dispute-rate ratio built via concatenation to keep quoting simple.
DISPUTE_RATIO = (
    'sum(rate(ledger_disputes_total{outcome="filed"}[1h]))'
    " / "
    'clamp_min(sum(rate(ledger_transactions_total{type="ride_payment"}[1h])), 0.001)'
)

PAYOUT_OK_RATIO = (
    'sum by (provider) (rate(ledger_provider_requests_total{status="ok"}[15m]))'
    " / "
    'clamp_min(sum by (provider) (rate(ledger_provider_requests_total[15m])), 0.001)'
)

panels = [
    row(1, "Real-time volume", 0),
    stat_panel(2, "Tx throughput (5m rate)", 0, 1,
               "sum(rate(ledger_transactions_total[5m]))"),
    stat_panel(3, "Ledger p95 latency", 6, 1,
               "histogram_quantile(0.95, sum(rate(ledger_operation_duration_seconds_bucket[5m])) by (le))", "s"),
    stat_panel(4, "Open escrow holds", 12, 1, "ledger_escrow_holds_open"),
    stat_panel(5, "Pending payout exposure (ETB)", 18, 1,
               "ledger_withdrawal_pending_etb", "currencyETB"),
    ts_panel(6, "Transaction throughput by type", 0, 5, 12, 8, [
        ("sum by (type) (rate(ledger_transactions_total[$__rate_interval]))", "{{type}}")]),
    ts_panel(7, "Operation latency p50/p95/p99", 12, 5, 12, 8, [
        ("histogram_quantile(0.50, sum(rate(ledger_operation_duration_seconds_bucket[$__rate_interval])) by (le, op))", "p50 {{op}}"),
        ("histogram_quantile(0.95, sum(rate(ledger_operation_duration_seconds_bucket[$__rate_interval])) by (le, op))", "p95 {{op}}"),
        ("histogram_quantile(0.99, sum(rate(ledger_operation_duration_seconds_bucket[$__rate_interval])) by (le, op))", "p99 {{op}}")], "s"),

    row(8, "Integrity & fraud", 13),
    ts_panel(9, "Hash chain corruptions (must stay flat)", 0, 14, 8, 7, [
        ("increase(ledger_hash_chain_corruptions_total[5m])", "corruptions/5m")]),
    ts_panel(10, "Fraud flags by rule", 8, 14, 8, 7, [
        ("sum by (reason) (increase(ledger_fraud_flags_total[$__rate_interval]))", "{{reason}}")]),
    ts_panel(11, "Balance drift (rebuild job)", 16, 14, 8, 7, [
        ("sum by (corrected) (increase(ledger_balance_drift_total[24h]))", "drift corrected={{corrected}}")]),

    row(12, "Disputes", 21),
    ts_panel(13, "Dispute rate vs ride payments", 0, 22, 12, 7, [
        (DISPUTE_RATIO, "dispute ratio")]),
    ts_panel(14, "Dispute outcomes", 12, 22, 12, 7, [
        ("sum by (outcome) (increase(ledger_disputes_total[$__rate_interval]))", "{{outcome}}")]),

    row(15, "Payment rails (Telebirr / Chapa / M-Pesa)", 29),
    ts_panel(16, "Provider call rate", 0, 30, 8, 7, [
        ("sum by (provider, status) (rate(ledger_provider_requests_total[$__rate_interval]))", "{{provider}} {{status}}")]),
    ts_panel(17, "Provider health gauge (1 = down)", 8, 30, 8, 7, [
        ("max by (provider) (ledger_provider_down)", "{{provider}}")]),
    ts_panel(18, "Payout success ratio", 16, 30, 8, 7, [
        (PAYOUT_OK_RATIO, "{{provider}} ok ratio")], "percentunit"),
]

dash = {
    "annotations": {"list": [{
        "builtIn": 1, "datasource": "-- Grafana --", "enable": True, "hide": True,
        "iconColor": "rgba(0, 211, 255, 1)", "name": "Annotations & Alerts",
        "type": "dashboard"}]},
    "editable": True, "graphTooltip": 1, "id": None, "links": [],
    "refresh": "30s", "schemaVersion": 39,
    "tags": ["ledger", "payments", "phase-i"],
    "templating": {"list": [{
        "name": "DS_PROMETHEUS", "type": "datasource", "query": "prometheus",
        "current": {"selected": False, "text": "Prometheus", "value": "prometheus"}}]},
    "time": {"from": "now-6h", "to": "now"},
    "timezone": "Africa/Addis_Ababa",
    "title": "NIDAW - Private Ledger: Volume, Balances & Revenue",
    "uid": "nidaw-ledger-ops",
    "version": 1,
    "panels": panels,
}

out = os.path.join(os.path.dirname(os.path.abspath(__file__)), "ledger-metrics.json")
with open(out, "w") as f:
    json.dump(dash, f, indent=2)
print("wrote", out, "-", len(panels), "panels")
