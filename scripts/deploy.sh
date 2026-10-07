#!/usr/bin/env bash
set -euo pipefail

# ==========================================
# NIDAW One-Click Production Deployment
# ==========================================
echo "🚀 Starting NIDAW Production Deployment..."

# 1. Validate environment
echo "✅ Validating prerequisites..."
command -v terraform >/dev/null || { echo "❌ terraform required"; exit 1; }
command -v kubectl >/dev/null    || { echo "❌ kubectl required"; exit 1; }
command -v helm >/dev/null       || { echo "❌ helm required"; exit 1; }
command -v aws >/dev/null        || { echo "❌ aws cli required"; exit 1; }

# Load env
source .env.production 2>/dev/null || { echo "❌ .env.production missing"; exit 1; }

# 2. Apply Infrastructure
echo "🌍 Provisioning Infrastructure (Terraform)..."
cd terraform/environments/prod
terraform init -input=false
terraform plan -input=false -out=tfplan
terraform apply -input=false tfplan
cd ../../..

# 3. Build & Push Images
echo "📦 Building Docker images..."
aws ecr get-login-password --region "$AWS_REGION" | docker login --username AWS --password-stdin "$AWS_ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com"

docker build -t nidaw/backend:latest ./backend
docker build -t nidaw/frontend:latest ./frontend
docker build -t nidaw/driver:latest ./driver-app
docker build -t nidaw/ml:latest ./ml

docker tag nidaw/backend:latest "$ECR_BACKEND"
docker tag nidaw/frontend:latest "$ECR_FRONTEND"
docker push "$ECR_BACKEND"
docker push "$ECR_FRONTEND"

# 4. Run Database Migrations
echo "🗄️ Running database migrations..."
kubectl run --rm -it migrate --image=nidaw/backend:latest -- \
  go run cmd/migrate/main.go --up

# 5. Deploy to Kubernetes
echo "☸️ Deploying to Kubernetes (Helm)..."
helm upgrade --install nidaw k8s/nidaw \
  --namespace production \
  --create-namespace \
  --values k8s/nidaw/values-prod.yaml \
  --set backend.image.tag=latest \
  --set frontend.image.tag=latest \
  --set driver.image.tag=latest \
  --wait --timeout 15m

# 6. Deploy ML & Analytics
echo "🤖 Deploying ML & Analytics..."
helm install ml k8s/ml --namespace production
helm install analytics k8s/analytics --namespace production

# 7. Health Checks
echo "🔍 Running post-deployment health checks..."
./scripts/validate-production.sh

echo "✅ NIDAW Production Deployment Complete!"
echo "🌐 API: https://api.nidaw.com"
echo "📱 Web: https://app.nidaw.com"
echo "📊 Grafana: https://monitor.nidaw.com"