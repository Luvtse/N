# 🐳 NIDAW Docker Stack

Complete Docker Compose setup for local development, staging, and production deployment.

## 📁 Structure

docker/
├── compose/
│ ├── base.yaml # Core infrastructure (Postgres, Redis, Kafka)
│ ├── dev.yaml # Development overrides (backend, frontend)
│ ├── prod.yaml # Production overrides
│ ├── analytics.yaml # Analytics stack (Pinot, Feast, Superset)
│ └── monitoring.yaml # Monitoring stack (Prometheus, Grafana, Loki)
├── config/
│ ├── postgres/
│ │ ├── init.sql
│ │ └── postgresql.conf
│ ├── kafka/
│ │ └── server.properties
│ ├── nginx/
│ │ └── nginx.conf
│ ├── prometheus/
│ │ ├── prometheus.yml
│ │ └── alert-rules.yml
│ ├── grafana/
│ │ ├── provisioning/
│ │ └── dashboards/
│ ├── loki/
│ │ └── loki-config.yml
│ └── alertmanager/
│ └── alertmanager.yml
└── README.md



## 🚀 Quick Start

### 1. Development Environment

```bash
# Start core infrastructure
docker-compose -f compose/base.yaml up -d

# Start development services
docker-compose -f compose/base.yaml -f compose/dev.yaml up -d

# Wait for services to be healthy
docker-compose -f compose/base.yaml -f compose/dev.yaml ps

# View logs
docker-compose -f compose/base.yaml -f compose/dev.yaml logs -f backend

2. Full Stack (with Analytics & Monitoring)

# Start everything
docker-compose \
  -f compose/base.yaml \
  -f compose/dev.yaml \
  -f compose/analytics.yaml \
  -f compose/monitoring.yaml \
  up -d

# Check status
docker-compose ps

3. Production Environment

# Set production environment variables
export $(cat .env.production | xargs)

# Start production stack
docker-compose -f compose/base.yaml -f compose/prod.yaml up -d

🔌 Service URLs
Core Services

Service	                URL	        Credentials
Backend API	http://localhost:8080	   -
Frontend Web	http://localhost:3000	   -
Kong Gateway	http://localhost:8000	   -
Kong Admin	http://localhost:8001	   -

Infrastructure

Service	             URL	Credentials
PostgreSQL	localhost:5432	nidaw/nidaw_dev
TimescaleDB	localhost:5433	nidaw/nidaw_dev
Redis	        localhost:6379   	-
Redis UI	http://localhost:8081	admin/admin
Kafka	           localhost:29092	-
Schema Registry	http://localhost:8082	-
Kafka UI	http://localhost:8083	-
MinIO API	http://localhost:9000	minioadmin/minioadmin
MinIO Console	http://localhost:9001	minioadmin/minioadmin

Analytics

Service	          URL	                Credentials
Pinot Controller http://localhost:9000	-
Feast	         http://localhost:6566	-
Superset	 http://localhost:8088	admin/admin
Metabase	 http://localhost:3030	-
MLflow	         http://localhost:5000	-

Monitoring

Service	               URL	      Credentials
Prometheus	http://localhost:9090	-
Grafana 	http://localhost:3001	admin/admin
Loki	        http://localhost:3100	-
Jaeger	        http://localhost:16686	-
AlertManager	http://localhost:9093	-
cAdvisor	http://localhost:8089	-
Node Exporter	http://localhost:9100	-
Uptime Kuma	http://localhost:3002	-

🛠️ Common Commands
Start/Stop Services

# Start all services
docker-compose -f compose/base.yaml -f compose/dev.yaml up -d

# Stop all services
docker-compose -f compose/base.yaml -f compose/dev.yaml down

# Stop and remove volumes (⚠️ deletes data)
docker-compose -f compose/base.yaml -f compose/dev.yaml down -v

# Restart a specific service
docker-compose -f compose/base.yaml -f compose/dev.yaml restart backend

View Logs

# All services
docker-compose -f compose/base.yaml -f compose/dev.yaml logs -f

# Specific service
docker-compose -f compose/base.yaml -f compose/dev.yaml logs -f backend

# Last 100 lines
docker-compose -f compose/base.yaml -f compose/dev.yaml logs --tail=100 backend

Execute Commands

# Enter backend container
docker-compose -f compose/base.yaml -f compose/dev.yaml exec backend sh

# Run database migration
docker-compose -f compose/base.yaml -f compose/dev.yaml exec backend go run cmd/migrate/main.go --up

# Access PostgreSQL
docker-compose -f compose/base.yaml -f compose/dev.yaml exec postgres psql -U nidaw -d nidaw

# Access Redis CLI
docker-compose -f compose/base.yaml -f compose/dev.yaml exec redis redis-cli

# Create Kafka topic
docker-compose -f compose/base.yaml -f compose/dev.yaml exec kafka \
  kafka-topics --create --bootstrap-server localhost:9092 \
  --topic test-topic --partitions 3 --replication-factor 1

Resource Management

# View resource usage
docker stats

# Clean up unused resources
docker system prune -a

# Remove all volumes
docker volume prune

🔧 Configuration
Environment Variables
Create a .env file in the docker/ directory:

# Environment
ENVIRONMENT=development

# Database
DB_USER=nidaw
DB_PASSWORD=nidaw_dev
DB_NAME=nidaw
DB_PORT=5432

# Redis
REDIS_PORT=6379
REDIS_MAX_MEMORY=512mb

# Kafka
KAFKA_PORT=29092
KAFKA_RETENTION_HOURS=168

# Backend
BACKEND_PORT=8080
JWT_SECRET=your-secret-key-min-32-chars

# Frontend
FRONTEND_PORT=3000

# Monitoring
GRAFANA_ADMIN_PASSWORD=admin

Custom Configuration
PostgreSQL
Edit config/postgres/postgresql.conf to tune database settings.
Kafka
Edit config/kafka/server.properties to adjust broker settings.
Nginx
Edit config/nginx/nginx.conf to customize web server behavior.
📊 Monitoring
Prometheus Targets
Access Prometheus at http://localhost:9090/targets to see all monitored services.
Grafana Dashboards
Pre-configured dashboards:
NIDAW Overview: System-wide metrics
API Performance: Request latency, error rates
Business Metrics: Revenue, rides, users
Database Metrics: Query performance, connections
Kafka Metrics: Consumer lag, throughput
Logs
View logs in Grafana via Loki:
Go to http://localhost:3001
Click "Explore"
Select "Loki" datasource
Use LogQL queries like: {container="nidaw-backend"}
🔐 Security
Production Checklist
Change all default passwords
Enable TLS/SSL for all services
Use strong JWT secrets (64+ characters)
Enable Kafka authentication
Configure firewall rules
Use Docker secrets for sensitive data
Enable audit logging
Set up automated backups
Configure rate limiting
Enable WAF rules
Secrets Management
For production, use one of:
Docker Secrets: docker secret create
AWS Secrets Manager: Via CSI driver
HashiCorp Vault: Via agent
Sealed Secrets: For GitOps
🐛 Troubleshooting
Common Issues
Port Already in Use

# Find process using port
lsof -i :8080

# Kill process
kill -9 <PID>

# Or change port in .env

Container Won't Start

# Check logs
docker-compose logs <service>

# Check container status
docker-compose ps

# Restart service
docker-compose restart <service>

Database Connection Issues

# Test connection
docker-compose exec postgres psql -U nidaw -d nidaw -c "SELECT 1"

# Check if database exists
docker-compose exec postgres psql -U nidaw -l

Kafka Issues

# Check broker status
docker-compose exec kafka kafka-broker-api-versions --bootstrap-server localhost:9092

# List topics
docker-compose exec kafka kafka-topics --list --bootstrap-server localhost:9092

# Check consumer lag
docker-compose exec kafka kafka-consumer-groups --bootstrap-server localhost:9092 --describe --all-groups

Health Checks

# Check all service health
curl http://localhost:8080/health
curl http://localhost:9090/-/healthy
curl http://localhost:3001/api/health
curl http://localhost:16686/


---

## ✅ Summary

| File | Purpose | Status |
|------|---------|--------|
| `docker/compose/base.yaml` | Core infrastructure (Postgres, Redis, Kafka, MinIO) | ✅ |
| `docker/compose/dev.yaml` | Development overrides with hot reload | ✅ |
| `docker/compose/prod.yaml` | Production configuration with HA | ✅ |
| `docker/compose/analytics.yaml` | Analytics stack (Pinot, Feast, Superset) | ✅ |
| `docker/compose/monitoring.yaml` | Full observability stack | ✅ |
| `docker/config/kafka/server.properties` | Kafka broker configuration | ✅ |
| `docker/config/nginx/nginx.conf` | Nginx with SSL, caching, security | ✅ |
| `docker/README.md` | Complete documentation | ✅ |

**Key Features:**
- ✅ Modular compose files (mix and match)
- ✅ Health checks on all services
- ✅ Resource limits and reservations
- ✅ Named volumes for persistence
- ✅ Shared network for service discovery
- ✅ Production-ready security headers
- ✅ Gzip compression
- ✅ Rate limiting
- ✅ SSL/TLS support
- ✅ Comprehensive logging
- ✅ Auto-initialization scripts
- ✅ Profile-based service grouping

All files follow Docker best practices and are ready for immediate use! 🚀