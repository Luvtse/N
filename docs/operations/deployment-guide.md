# Deployment Guide

This guide covers the complete deployment process for NIDAW across all environments (development, staging, production).

## 🎯 Overview

NIDAW uses a **GitOps-based deployment pipeline** with automated testing, blue-green deployments, and multi-region failover capabilities.

## 🌍 Environments

| Environment | Purpose | URL | Auto-Deploy |
|-------------|---------|-----|-------------|
| **Development** | Local development | localhost | N/A |
| **Staging** | Pre-production testing | staging.nidaw.com | On PR merge to `develop` |
| **Production** | Live user traffic | nidaw.com | On release tag |

## 📋 Prerequisites

### Required Tools

```bash
# Install required tools
brew install terraform kubectl helm awscli

# Or via apt (Ubuntu/Debian)
sudo apt-get install -y terraform kubectl helm awscli

# Verify installations
terraform --version  # >= 1.5.0
kubectl version --client  # >= 1.28.0
helm version  # >= 3.12.0
aws --version  # >= 2.13.0

AWS Configuration

# Configure AWS credentials
aws configure

# Or use environment variables
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"
export AWS_DEFAULT_REGION="us-east-1"

Kubernetes Context

# Update kubeconfig
aws eks update-kubeconfig --name nidaw-production --region us-east-1

# Verify connection
kubectl get nodes

🚀 Deployment Process
1. Infrastructure Deployment (Terraform)
Staging Environment

cd infrastructure/environments/staging

# Initialize Terraform
terraform init

# Plan changes
terraform plan -out=plan.tfplan

# Review plan
terraform show plan.tfplan

# Apply changes
terraform apply plan.tfplan

Production Environment

cd infrastructure/environments/production

# Initialize Terraform
terraform init

# Plan changes
terraform plan -out=plan.tfplan

# Review plan (CRITICAL - review thoroughly)
terraform show plan.tfplan

# Apply changes
terraform apply plan.tfplan

2. Application Deployment (Helm)
Build Docker Images

# Backend
cd backend
docker build -t nidaw/backend:$(git rev-parse --short HEAD) .
docker push nidaw/backend:$(git rev-parse --short HEAD)

# Frontend
cd frontend
docker build -t nidaw/frontend:$(git rev-parse --short HEAD) .
docker push nidaw/frontend:$(git rev-parse --short HEAD)

Deploy with Helm

# Staging
helm upgrade --install nidaw-staging ./helm/nidaw \
  --namespace staging \
  --create-namespace \
  --values ./helm/nidaw/values-staging.yaml \
  --set image.backend.tag=$(git rev-parse --short HEAD) \
  --set image.frontend.tag=$(git rev-parse --short HEAD) \
  --wait --timeout 10m

# Production
helm upgrade --install nidaw-production ./helm/nidaw \
  --namespace production \
  --create-namespace \
  --values ./helm/nidaw/values-production.yaml \
  --set image.backend.tag=$(git rev-parse --short HEAD) \
  --set image.frontend.tag=$(git rev-parse --short HEAD) \
  --wait --timeout 15m

3. Database Migrations

# Run migrations
kubectl exec -n production deploy/nidaw-backend -- \
  go run cmd/migrate/main.go --up

# Verify migrations
kubectl exec -n production deploy/nidaw-backend -- \
  go run cmd/migrate/main.go --status

4. Kafka Topic Creation

# Create required topics
kubectl exec -n production statefulset/kafka -- \
  kafka-topics --create \
  --bootstrap-server localhost:9092 \
  --topic nidus.rides \
  --partitions 6 \
  --replication-factor 3 \
  --if-not-exists

kubectl exec -n production statefulset/kafka -- \
  kafka-topics --create \
  --bootstrap-server localhost:9092 \
  --topic nidus.locations \
  --partitions 12 \
  --replication-factor 3 \
  --if-not-exists

# Repeat for other topics...

🔄 Deployment Strategies
Blue-Green Deployment

# Deploy to Green environment
helm upgrade --install nidaw-green ./helm/nidaw \
  --namespace production \
  --set deployment.color=green \
  --wait

# Run smoke tests against Green
./scripts/smoke-test.sh https://green.api.nidaw.com

# Switch traffic to Green
kubectl patch ingress nidaw-ingress -n production -p \
  '{"spec":{"rules":[{"host":"api.nidaw.com","http":{"paths":[{"path":"/","backend":{"serviceName":"nidaw-green","servicePort":80}}]}}]}}'

# Monitor for 15 minutes
kubectl logs -f -n production deployment/nidaw-green

# Terminate Blue environment
helm uninstall nidaw-blue -n production

Canary Deployment

# Deploy canary (10% traffic)
helm upgrade --install nidaw-canary ./helm/nidaw \
  --namespace production \
  --set deployment.strategy=canary \
  --set deployment.canaryWeight=10 \
  --wait

# Monitor metrics
kubectl top pods -n production -l app=nidaw-canary

# Gradually increase traffic
helm upgrade nidaw-canary ./helm/nidaw \
  --set deployment.canaryWeight=25 \
  --wait

# Continue until 100%
helm upgrade nidaw-canary ./helm/nidaw \
  --set deployment.canaryWeight=100 \
  --wait

📊 Post-Deployment Verification
Health Checks

# Check pod status
kubectl get pods -n production

# Check deployment status
kubectl get deployments -n production

# Check services
kubectl get services -n production

# Check ingress
kubectl get ingress -n production

Smoke Tests

# Run automated smoke tests
./scripts/smoke-test.sh https://api.nidaw.com

# Manual verification
curl https://api.nidaw.com/health
curl https://api.nidaw.com/ready

# Test critical endpoints
curl -X POST https://api.nidaw.com/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"test@nidaw.com","password":"test123"}'

Monitoring

# Open Grafana
open https://grafana.nidaw.com

# Check Prometheus targets
open https://prometheus.nidaw.com/targets

# View Jaeger traces
open https://jaeger.nidaw.com

🔙 Rollback Procedures
Immediate Rollback

# Rollback to previous release
helm rollback nidaw-production 1

# Or rollback to specific version
helm rollback nidaw-production --revision 5

Manual Rollback

# Identify previous version
helm history nidaw-production

# Rollback
helm rollback nidaw-production <REVISION>

# Verify rollback
kubectl get pods -n production

📝 Deployment Checklist
Pre-Deployment
All tests passing in CI
Security scan clean
Terraform plan reviewed
Database migrations tested
Rollback plan documented
Stakeholders notified
Monitoring dashboards ready
On-call engineer assigned
During Deployment
Terraform apply successful
Helm upgrade successful
Database migrations complete
Kafka topics created
Pods running and healthy
Smoke tests passing
No error spikes in logs
Latency within normal range
Post-Deployment
Business metrics stable
User feedback monitored
Documentation updated
Deployment announced
Rollback window closed (1 hour)
Post-deployment review scheduled
🛠️ Automation
GitHub Actions Workflow

# .github/workflows/deploy-production.yml
name: Deploy to Production

on:
  push:
    tags:
      - 'v*'

jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: production
    
    steps:
      - uses: actions/checkout@v4
      
      - name: Configure AWS
        uses: aws-actions/configure-aws-credentials@v4
        with:
          aws-access-key-id: ${{ secrets.AWS_ACCESS_KEY_ID }}
          aws-secret-access-key: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
          aws-region: us-east-1
      
      - name: Deploy Infrastructure
        run: |
          cd infrastructure/environments/production
          terraform init
          terraform apply -auto-approve
      
      - name: Build and Push Images
        run: |
          docker build -t nidaw/backend:${{ github.ref_name }} ./backend
          docker push nidaw/backend:${{ github.ref_name }}
      
      - name: Deploy Application
        run: |
          helm upgrade --install nidaw-production ./helm/nidaw \
            --namespace production \
            --set image.backend.tag=${{ github.ref_name }} \
            --wait
      
      - name: Verify Deployment
        run: ./scripts/smoke-test.sh https://api.nidaw.com

📞 Support
Deployment Issues
Slack: #deployment-support
Email: platform@nidaw.com
PagerDuty: Production Deployment

Emergency Contacts

Role		Name	Phone	Email
Platform Lead	[Name]	-654	platform@nidaw.com
SRE Lead	[Name]	-655	sre@nidaw.com
CTO		[Name]	-656	cto@nidaw.com

Last Updated: July 2026
Owner: Platform Engineering Team
Review Cadence: Monthly
