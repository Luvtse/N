#!/usr/bin/env bash
set -euo pipefail
echo "🔍 Validating Production Deployment..."

# API Health
curl -sf https://api.nidaw.com/health || { echo "❌ API unhealthy"; exit 1; }

# Kubernetes Pods
PODS=$(kubectl get pods -n production -o jsonpath='{.items[*].status.phase}' | tr ' ' '\n' | sort -u)
if [ "$PODS" != "Running" ]; then
  echo "❌ Not all pods are running: $PODS"
  exit 1
fi

# Database Connection
kubectl exec -n production deploy/nidaw-backend -- psql -U $DB_USER -d $DB_NAME -c "SELECT 1;" >/dev/null 2>&1 || { echo "❌ DB unreachable"; exit 1; }

# Kafka
kubectl exec -n production deploy/nidaw-backend -- kafka-topics --bootstrap-server $KAFKA_BROKERS --list >/dev/null 2>&1 || { echo "❌ Kafka unreachable"; exit 1; }

echo "✅ All health checks passed. System is LIVE."