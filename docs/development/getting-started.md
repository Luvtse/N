
---

## 📄 File #6: `docs/development/getting-started.md`

```markdown
# Getting Started

Welcome to NIDAW development! This guide will help you set up your local development environment in under 15 minutes.

## 📋 Prerequisites

### Required Software

| Tool | Version | Installation |
|------|---------|--------------|
| **Go** | 1.21+ | [go.dev](https://go.dev/dl/) |
| **Flutter** | 3.16+ | [flutter.dev](https://flutter.dev/docs/get-started/install) |
| **Docker** | 24.0+ | [docker.com](https://www.docker.com/get-started) |
| **Docker Compose** | 2.20+ | Included with Docker Desktop |
| **Git** | 2.40+ | [git-scm.com](https://git-scm.com/) |
| **Make** | 4.3+ | Pre-installed on macOS/Linux |
| **kubectl** | 1.28+ | [kubernetes.io](https://kubernetes.io/docs/tasks/tools/) |

### Recommended IDE

- **VS Code** with extensions:
  - Go (golang.go)
  - Dart (Dart-Code.dart-code)
  - Flutter (Dart-Code.flutter)
  - Docker (ms-azuretools.vscode-docker)
  - Kubernetes (ms-kubernetes-tools.vscode-kubernetes-tools)
  - YAML (redhat.vscode-yaml)

## 🚀 Quick Start

### 1. Clone the Repository

```bash
git clone https://github.com/nidaw/nidaw.git
cd nidaw

2. Setup Environment

# Run setup script
make setup

# Or manually:
cp config/environments/dev.env .env
cp backend/.env.example backend/.env

3. Start Infrastructure

# Start all infrastructure services
docker-compose -f docker/compose/base.yaml up -d

# Wait for services to be healthy
docker-compose -f docker/compose/base.yaml ps

4. Start Development Services

# Start backend and frontend
docker-compose -f docker/compose/base.yaml -f docker/compose/dev.yaml up -d

# View logs
docker-compose -f docker/compose/base.yaml -f docker/compose/dev.yaml logs -f

5. Access Services

Service		URL			Credentials
Frontend	http://localhost:3000	-
Backend API	http://localhost:8080	-
API Docs	http://localhost:8080/swagger	-
Kong Gateway	http://localhost:8000	-
Kong Admin	http://localhost:8001	-
PostgreSQL	localhost:5432	nidaw/nidaw_dev
Redis		localhost:6379	-
Kafka UI	http://localhost:8083	-
Grafana		http://localhost:3001	admin/admin
Prometheus	http://localhost:9090	-
Jaeger		http://localhost:16686	-


🏗️ Project Structure

nidaw/
├── backend/          # Go backend (modular monolith)
├── frontend/         # Flutter super-app
├── driver-app/       # Flutter driver app
├── database/         # SQL migrations
├── infrastructure/   # Terraform configs
├── k8s/              # Kubernetes manifests
├── helm/             # Helm charts
├── docker/           # Docker configs
├── ml/               # ML pipelines
├── blockchain/       # Smart contracts
├── api-gateway/      # Kong configs
├── monitoring/       # Observability
├── analytics/        # Analytics stack
├── legal/            # Legal documents
├── docs/             # Documentation
└── scripts/          # Automation

💻 Backend Development
Run Backend Locally

cd backend

# Install dependencies
go mod download

# Run migrations
go run cmd/migrate/main.go --up

# Start server
go run cmd/server/main.go

# Or with hot reload
air

Run Tests

# All tests
go test ./...

# With coverage
go test -cover ./...

# Specific package
go test ./internal/modules/nidus/...


Code Generation

# Generate mocks
go generate ./...

# Generate protobuf
protoc --go_out=. --go-grpc_out=. api/proto/*.proto

📱 Frontend Development
Run Flutter App

cd frontend

# Get dependencies
flutter pub get

# Run on Chrome
flutter run -d chrome

# Run on Android emulator
flutter run -d android

# Run on iOS simulator
flutter run -d ios

Run Tests

# All tests
flutter test

# With coverage
flutter test --coverage

# Specific test
flutter test test/features/auth/

Code Generation

# Generate code (Freezed, JSON serializable)
flutter pub run build_runner build

# Watch mode
flutter pub run build_runner watch

🗄️ Database Development
Connect to PostgreSQL

# Using psql
docker-compose -f docker/compose/base.yaml exec postgres \
  psql -U nidaw -d nidaw

# Using GUI (DBeaver, TablePlus)
Host: localhost
Port: 5432
Database: nidaw
Username: nidaw
Password: nidaw_dev

Run Migrations

# Up
./scripts/migrations/migrate-up.sh

# Down
./scripts/migrations/migrate-down.sh

# Specific version
./scripts/migrations/migrate-up.sh --version 002

Seed Data

./scripts/seed/seed-data.sh

📨 Kafka Development
Create Topic

docker-compose -f docker/compose/base.yaml exec kafka \
  kafka-topics --create \
  --bootstrap-server localhost:9092 \
  --topic test-topic \
  --partitions 3 \
  --replication-factor 1

List Topics

docker-compose -f docker/compose/base.yaml exec kafka \
  kafka-topics --list --bootstrap-server localhost:9092

Consume Messages

docker-compose -f docker/compose/base.yaml exec kafka \
  kafka-console-consumer \
  --bootstrap-server localhost:9092 \
  --topic nidus.rides \
  --from-beginning

🤖 ML Development
Setup Python Environment

cd ml

# Create virtual environment
python -m venv venv
source venv/bin/activate  # Linux/Mac
# venv\Scripts\activate   # Windows

# Install dependencies
pip install -r requirements.txt

Train Model

# Train ETA prediction model
python training/eta_prediction/train.py

# Train demand forecasting
python training/demand_forecasting/train.py

Start Feature Store

cd feature_store/feast

# Apply feature definitions
feast apply

# Materialize features
feast materialize-incremental

⛓️ Blockchain Development
Setup Hardhat

cd blockchain

# Install dependencies
npm install

# Start local node
npx hardhat node

# Deploy contract
npx hardhat run scripts/deploy.js --network localhost

Run Tests

npx hardhat test

🧪 Testing
Run All Tests

make test

Run Specific Test Suites

# Backend unit tests
cd backend && go test ./...

# Frontend unit tests
cd frontend && flutter test

# Integration tests
cd tests/integration && ./run.sh

# E2E tests
cd tests/e2e && ./run.sh

# Performance tests
cd tests/performance && k6 run load-test.js

🐛 Debugging
Backend Debugging

# Install Delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug with Delve
dlv debug cmd/server/main.go

# Or in VS Code
# Use .vscode/launch.json configuration

Frontend Debugging

# Enable DevTools
flutter run --enable-software-rendering

# Or in VS Code
# Use Flutter extension debugger

Database Debugging

# Enable query logging
docker-compose -f docker/compose/base.yaml exec postgres \
  psql -U nidaw -d nidaw -c "ALTER SYSTEM SET log_statement = 'all';"

# View logs
docker-compose -f docker/compose/base.yaml logs postgres

📚 Common Commands

# Setup
make setup              # Initial setup
make dev                # Start dev environment
make test               # Run all tests
make build              # Build for production
make clean              # Clean up

# Database
make migrate            # Run migrations
make seed               # Seed data
make backup             # Backup database

# Monitoring
make monitoring         # Open dashboards
make logs               # View logs
make metrics            # View metrics

# Deployment
make deploy-staging     # Deploy to staging
make deploy-prod        # Deploy to production


🆘 Troubleshooting
Common Issues
Port Already in Use

# Find process using port
lsof -i :8080

# Kill process
kill -9 <PID>

Docker Issues

# Restart Docker
docker-compose down
docker-compose up -d

# Clean up
docker system prune -a

Flutter Issues

# Clean build
flutter clean
flutter pub get

# Check doctor
flutter doctor

Go Issues

# Clean module cache
go clean -modcache

# Re-download
go mod download

See Troubleshooting Guide for more issues.
📖 Next Steps
Read Coding Standards
Learn Git Workflow
Explore Architecture
Check API Documentation
📞 Support
Slack: #dev-support
Email: engineering@nidaw.com
Discord: discord.gg/nidaw
Last Updated: July 2026
Owner: Developer Experience Team

