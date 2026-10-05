# NIDAW Documentation

Welcome to the official documentation for **NIDAW** - the world's first AI-native mobility super-app integrating transportation, accommodation, nourishment, logistics, and business services into a single self-optimizing ecosystem.

## 📖 Documentation Overview

This documentation is organized into the following sections:

### 🏗️ Architecture
- [System Design](architecture/system-design.md) - High-level architecture and design principles
- [Data Flow](architecture/data-flow.md) - How data moves through the system
- [Deployment](architecture/deployment.md) - Infrastructure and deployment topology
- [Diagrams](architecture/diagrams/) - Visual architecture diagrams

### 💻 Development
- [Getting Started](development/getting-started.md) - Set up your local environment
- [Coding Standards](development/coding-standards.md) - Code style and best practices
- [Git Workflow](development/git-workflow.md) - Branching strategy and PR process
- [Troubleshooting](development/troubleshooting.md) - Common issues and solutions

### ⚙️ Operations
- [Deployment Guide](operations/deployment-guide.md) - Production deployment procedures
- [Monitoring Guide](operations/monitoring-guide.md) - Observability and alerting
- [Disaster Recovery](operations/disaster-recovery.md) - Backup and recovery strategies
- [Incident Response](operations/incident-response.md) - Incident management process
- [Runbooks](operations/runbooks/) - Step-by-step operational procedures

### 🔌 API
- [OpenAPI Specification](api/openapi.yaml) - Complete API reference
- [Postman Collection](api/postman-collection.json) - Ready-to-use API tests
- [API Examples](api/api-examples.md) - Common API usage patterns

### 🔒 Security
- [Security Audit Report](security/SECURITY_AUDIT_REPORT.md) - Latest security assessment
- [Security Policy](security/security-policy.md) - Security standards and practices
- [Compliance](security/compliance/) - SOC 2, GDPR, PCI DSS documentation

### 💰 Finance
- [Cost Optimization](finance/cost-optimization.md) - Cloud cost management
- [Cloud Resources](finance/cloud-resources.md) - Resource inventory and tagging

## 🚀 Quick Start

```bash
# Clone the repository
git clone https://github.com/nidaw/nidaw.git
cd nidaw

# Setup development environment
make setup

# Start local services
make dev

# Run tests
make test

📞 Support
Documentation Issues: GitHub Issues
Engineering Team: engineering@nidaw.com
Slack: #docs-nidaw
Discord: discord.gg/nidaw
📝 Contributing
We welcome contributions to our documentation! Please see our Contributing Guide for details.
📄 License
© 2026 NIDAW Inc. All rights reserved. Proprietary and confidential.
Last Updated: July 2026
Documentation Version: 1.0.0


---

## 📄 File #2: `docs/architecture/system-design.md`

```markdown
# System Design

## 🎯 Overview

NIDAW is built on a **Geo-Distributed Event-Driven Architecture (GDEA)** using a **Modular Monolith** pattern, combining the simplicity of monolithic deployment with the scalability of event-driven systems.

## 🏛️ Architectural Principles

1. **Event-Driven First**: All state changes emit events; services communicate via Kafka
2. **Regional Isolation**: Each geographic region operates independently with local data
3. **AI-Native**: Machine learning is embedded at every layer, not bolted on
4. **Privacy by Design**: PII never crosses regional boundaries
5. **CQRS + Event Sourcing**: Separate read/write paths for optimal performance
6. **Modular Monolith**: Single deployable with strict domain boundaries

## 🧩 High-Level Architecture

┌─────────────────────────────────────────────────────────────┐
│ Global Edge Layer │
│ Cloudflare CDN • WAF • DDoS Protection • Smart DNS │
└──────────────────────┬──────────────────────────────────────┘
│
┌──────────────┼──────────────┐
▼ ▼ ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│ NA-East │ │ EU-West │ │ APAC-South │
│ (Primary) │ │ (Secondary) │ │ (Secondary) │
└──────┬───────┘ └──────┬───────┘ └──────┬───────┘
│ │ │
▼ ▼ ▼
┌─────────────────────────────────────────────────────────────┐
│ Regional Kubernetes Cluster │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ API Gateway (Kong) │ │
│ │ • JWT Validation • Rate Limiting • Routing │ │
│ └──────────────────────┬──────────────────────────────┘ │
│ │ │
│ ┌──────────────────────▼──────────────────────────────┐ │
│ │ NIDAW Modular Monolith (Go) │ │
│ │ ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐ │ │
│ │ │ Nidus │ │ Haven │ │ Vorax │ │ Logix │ │ │
│ │ │ (Rides) │ │(Hotels) │ │ (Food) │ │(Freight)│ │ │
│ │ └─────────┘ └─────────┘ └─────────┘ └─────────┘ │ │
│ │ ┌─────────┐ ┌─────────┐ ┌─────────────────────┐ │ │
│ │ │ Corpus │ │Autonomous│ │ Shared Kernel │ │ │
│ │ │(Corp.) │ │(AV/Drone)│ │ (Auth, Payments) │ │ │
│ │ └─────────┘ └─────────┘ └─────────────────────┘ │ │
│ └──────────────────────┬──────────────────────────────┘ │
│ │ │
│ ┌──────────────────────▼──────────────────────────────┐ │
│ │ Data Layer │ │
│ │ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌────────┐│ │
│ │ │PostgreSQL│ │TimescaleDB│ │ Redis │ │ Kafka ││ │
│ │ │ (Citus) │ │(TimeSeries│ │ (Cache) │ │(Events)││ │
│ │ └──────────┘ └──────────┘ └──────────┘ └────────┘│ │
│ └─────────────────────────────────────────────────────┘ │
│ │
│ ┌─────────────────────────────────────────────────────┐ │
│ │ AI/ML Platform │ │
│ │ Feast • MLflow • TorchServe • Ray Serve │ │
│ └─────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘



## 📦 Module Architecture

### Domain Modules

Each module follows **Clean Architecture** with four layers:

module/
├── domain/ # Pure business logic, entities, events
│ ├── entities/
│ ├── events/
│ └── repositories/ (interfaces)
├── application/ # Use cases, commands, queries
│ ├── commands/
│ ├── queries/
│ └── services/
├── infrastructure/ # External integrations, DB implementations
│ ├── repositories/
│ └── integrations/
└── interfaces/ # HTTP handlers, WebSocket, gRPC
└── http/


### Module List

| Module | Purpose | Key Services |
|--------|---------|--------------|
| **Nidus** | Ride hailing & movement | Matching, ETA, Pricing, Location |
| **Haven** | Hotel & accommodation booking | Search, Booking, PMS Integration |
| **Vorax** | Food delivery | Orders, Menu, Delivery Tracking |
| **Logix** | Freight & logistics | Shipments, Load Matching, Customs |
| **Corpus** | Corporate travel | Policies, Expenses, Approvals |
| **Autonomous** | Self-driving & drones | AV Orchestration, Drone Control |
| **Legal** | Consent management | Documents, Audit Logs |
| **Identity** | Decentralized identity | DID, Verifiable Credentials |

## 🔄 Communication Patterns

### Synchronous (HTTP/gRPC)
- Client → API Gateway → Backend
- Service-to-service (rare, only for critical paths)

### Asynchronous (Kafka Events)
- All state changes emit events
- Cross-module communication
- Analytics and ML pipelines
- Audit logging

### Real-time (WebSocket)
- Driver location updates
- Ride status changes
- Order tracking
- Chat/messaging

## 🗄️ Data Architecture

### Polyglot Persistence

| Data Type | Storage | Purpose |
|-----------|---------|---------|
| Transactional | PostgreSQL (Citus) | Users, rides, bookings |
| Time-series | TimescaleDB | GPS, metrics, telemetry |
| Cache | Redis | Sessions, locations, rate limits |
| Events | Kafka | Event streaming, CDC |
| Search | Elasticsearch | Full-text search |
| Objects | MinIO/S3 | Files, images, backups |
| Vectors | Qdrant | AI embeddings |
| Graph | Memgraph | Relationships |

### CQRS Pattern

Write Path:
Client → Command Handler → Domain Logic → Event Store → Kafka
Read Path:
Client → Query Handler → Read Model (Materialized View) → Response


## 🤖 AI/ML Integration

### Model Serving Architecture

┌──────────────┐
│ Client │
└──────┬───────┘
│
▼
┌──────────────┐ ┌──────────────┐
│ API Gateway │────▶│ Feature Store│
└──────┬───────┘ │ (Feast) │
│ └──────┬───────┘
▼ │
┌──────────────┐ │
│Model Serving │◀───────────┘
│(TorchServe/ │
│ Ray) │
└──────────────┘



### Models

- **ETA Prediction**: XGBoost, real-time inference
- **Demand Forecasting**: LSTM, hourly predictions
- **Dynamic Pricing**: Gradient Boosting, surge optimization
- **Driver Churn**: Classification, retention triggers
- **Recommendations**: Neural Collaborative Filtering

## 🔐 Security Architecture

### Defense in Depth

1. **Edge**: Cloudflare WAF, DDoS protection
2. **Network**: VPC, security groups, network policies
3. **Gateway**: JWT validation, rate limiting
4. **Application**: RBAC, input validation, CSRF protection
5. **Data**: Encryption at rest (AES-256) and in transit (TLS 1.3)
6. **Audit**: Immutable logs, SOC 2 compliance

## 📊 Observability Stack

- **Metrics**: Prometheus + Grafana
- **Logs**: Loki + Promtail
- **Traces**: Jaeger (OpenTelemetry)
- **Alerts**: AlertManager + PagerDuty
- **Status**: Uptime Kuma

## 🌍 Multi-Region Strategy

### Active-Passive Failover

- **Primary**: NA-East (us-east-1)
- **Secondary**: EU-West (eu-west-1)
- **Tertiary**: APAC-South (ap-southeast-1)

### Data Replication

- **Database**: Read replicas + cross-region snapshots
- **Kafka**: MirrorMaker 2 for event replication
- **S3**: Cross-region replication for objects
- **DNS**: Route53 health checks + automatic failover

## 📈 Scalability Targets

| Metric | Target |
|--------|--------|
| Concurrent Users | 1M+ |
| Requests/second | 100K+ |
| Daily Rides | 10M+ |
| Data Volume | 100TB+ |
| Uptime | 99.99% |
| P95 Latency | < 200ms |

## 🔄 Future Considerations

- **Service Extraction**: Modular monolith → microservices as needed
- **Edge Computing**: Local inference at edge nodes
- **Quantum-Ready**: Cryptography prepared for post-quantum
- **Autonomous Fleet**: Scaling to 10K+ AVs

---

**Last Updated:** July 2026  
**Owner:** Architecture Team  
**Review Cadence:** Quarterly

