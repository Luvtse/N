## 📄 File #4: `docs/architecture/deployment.md`

```markdown
# Deployment Architecture

## 🎯 Overview

NIDAW uses a **multi-region, active-passive deployment architecture** with automated failover, blue-green deployments, and GitOps practices.

## 🌍 Global Topology

┌─────────────────────────────────────────────────────────────┐
│ Cloudflare Edge │
│ • Global CDN • WAF • DDoS Protection • Load Balancing │
└──────────────────────┬──────────────────────────────────────┘
│
┌──────────────┼──────────────┐
│ │ │
▼ ▼ ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│ NA-East │ │ EU-West │ │ APAC-South │
│ us-east-1 │ │ eu-west-1 │ │ ap-southeast│
│ (Primary) │ │ (Secondary) │ │ (Secondary) │
│ │ │ │ │ │
│ 5 AZs │ │ 3 AZs │ │ 3 AZs │
└──────────────┘ └──────────────┘ └──────────────┘


## ☸️ Kubernetes Architecture

### Cluster Structure

```yaml
EKS Cluster (per region)
├── Node Groups
│   ├── general (m5.xlarge)     # Application workloads
│   ├── compute (c5.2xlarge)    # CPU-intensive (ML, analytics)
│   ├── memory (r5.2xlarge)     # Memory-intensive (caching)
│   └── gpu (p3.2xlarge)        # ML training (APAC only)
│
├── Namespaces
│   ├── nidaw-production        # Production workloads
│   ├── nidaw-staging           # Staging environment
│   ├── monitoring              # Prometheus, Grafana, Loki
│   ├── ingress-nginx           # Ingress controller
│   ├── cert-manager            # TLS certificates
│   └── external-secrets        # Secret management
│
└── Add-ons
    ├── Cluster Autoscaler
    ├── Metrics Server
    ├── AWS Load Balancer Controller
    ├── EBS CSI Driver
    └── CoreDNS

🔄 Deployment Pipeline
CI/CD Flow

┌─────────┐   Push    ┌─────────┐  Test   ┌─────────┐
│Developer│ ────────▶ │ GitHub  │ ──────▶ │  CI     │
│         │           │  Repo   │         │Pipeline │
└─────────┘           └─────────┘         └────┬────┘
                                               │
                              ┌────────────────┼────────────────┐
                              │                │                │
                              ▼                ▼                ▼
                       ┌─────────┐      ┌─────────┐      ┌─────────┐
                       │  Build  │      │  Test   │      │Security │
                       │  Image  │      │  Suite  │      │  Scan   │
                       └────┬────┘      └────┬────┘      └────┬────┘
                            │                │                │
                            └────────────────┼────────────────┘
                                             │
                                             ▼
                                      ┌─────────┐
                                      │  ECR    │
                                      │Registry │
                                      └────┬────┘
                                           │
                              ┌────────────┼────────────┐
                              │            │            │
                              ▼            ▼            ▼
                       ┌─────────┐  ┌─────────┐  ┌─────────┐
                       │  Dev    │  │ Staging │  │  Prod   │
                       │ Deploy  │  │ Deploy  │  │ Deploy  │
                       └─────────┘  └─────────┘  └─────────┘

Deployment Strategies
Blue-Green Deployment

┌─────────────────────────────────────────┐
│           Load Balancer                  │
│         (Route 53 / ALB)                │
└────────────────┬────────────────────────┘
                 │
        ┌────────┴────────┐
        │                 │
        ▼                 ▼
┌──────────────┐   ┌──────────────┐
│   Blue       │   │   Green      │
│  (Current)   │   │  (New)       │
│  v1.0.0      │   │  v1.1.0      │
│              │   │              │
│  ✅ Active   │   │  🔄 Testing  │
└──────────────┘   └──────────────┘

Process:
Deploy new version to Green environment
Run smoke tests against Green
Shift 10% traffic to Green (canary)
Monitor metrics for 15 minutes
If healthy, shift 100% traffic
Keep Blue on standby for 1 hour
Terminate Blue environment
Canary Deployment

Traffic Split:
├── 90% → Current version (stable)
├── 9%  → Canary version (new)
└── 1%  → Shadow traffic (testing)

📦 Infrastructure as Code
Terraform Structure

infrastructure/
├── modules/
│   ├── vpc/
│   ├── k8s-cluster/
│   ├── database/
│   ├── kafka/
│   ├── redis/
│   ├── security/
│   ├── multi_region/
│   └── secrets_manager/
│
├── environments/
│   ├── dev/
│   │   ├── main.tf
│   │   ├── terraform.tfvars
│   │   └── backend.tf
│   ├── staging/
│   └── production/
│
└── global/
    ├── main.tf
    └── backend.tf

Deployment Commands

# Plan infrastructure changes
cd infrastructure/environments/staging
terraform init
terraform plan -out=plan.tfplan

# Review and apply
terraform apply plan.tfplan

# Deploy application
cd ../../../
make deploy-staging

🔐 Secrets Management
AWS Secrets Manager Integration

┌──────────┐   Fetch    ┌──────────┐  Sync   ┌──────────┐
│   App    │ ─────────▶ │ Secrets  │ ──────▶ │   K8s    │
│  Pod     │            │ Manager  │         │  Secret  │
└──────────┘            └──────────┘         └──────────┘
                              ▲
                              │
                              ▼
                         ┌──────────┐
                         │External  │
                         │Secrets   │
                         │Operator  │
                         └──────────┘

Secret Rotation

Secret		Rotation 	Period	Method
DB Password	30 days		Lambda function
JWT Secret	90 days		Manual + audit
API Keys	180 days	Automated
TLS Certs	90 days		cert-manager

📊 Monitoring & Alerting
Deployment Monitoring

Metrics Tracked:
- Deployment duration
- Rollback count
- Error rate during deployment
- P95 latency comparison (before/after)
- Business metrics impact

Health Checks

Liveness Probe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 10

Readiness Probe:
  httpGet:
    path: /ready
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5

Startup Probe:
  httpGet:
    path: /health
    port: 8080
  failureThreshold: 30
  periodSeconds: 10

🔄 Rollback Procedures
Automated Rollback Triggers
Error rate > 5% for 5 minutes
P95 latency > 1000ms for 5 minutes
Health check failures > 3
Business metric degradation > 20%
Rollback Process

# Immediate rollback via Helm
helm rollback nidaw-production 1

# Or via ArgoCD
argocd app rollback nidaw-production

# Or via kubectl
kubectl rollout undo deployment/nidaw-backend

🌐 DNS & Traffic Management
Route 53 Configuration

Hosted Zone: nidaw.com
├── api.nidaw.com (A - Alias)
│   ├── Primary: NA-East ALB
│   └── Secondary: EU-West ALB (failover)
│
├── app.nidaw.com (CNAME)
│   └── CloudFront Distribution
│
└── ws.nidaw.com (A - Alias)
    └── WebSocket ALB (sticky sessions)

Traffic Routing Rules

Geo-Routing:
  - EU users → EU-West region
  - NA users → NA-East region
  - APAC users → APAC-South region

Latency-Based:
  - Default: Lowest latency region

Failover:
  - Primary unhealthy → Secondary
  - All unhealthy → Maintenance page

📈 Scaling Strategy
Horizontal Pod Autoscaling

apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: nidaw-backend-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: nidaw-backend
  minReplicas: 5
  maxReplicas: 50
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
    - type: Pods
      pods:
        metric:
          name: http_requests_per_second
        target:
          type: AverageValue
          averageValue: "100"

Cluster Autoscaling

Node Group Scaling:
  general:
    min: 3
    max: 20
    target: 70% CPU
  compute:
    min: 2
    max: 10
    target: 80% CPU

🔄 Disaster Recovery
RTO/RPO Targets

Environment	RTO	RPO
Production	15 min	5 min
Staging		1 hour	1 hour
Dev		4 hours	24 hours

Backup Strategy

Database:
  - Continuous WAL archiving
  - Hourly snapshots
  - Daily cross-region copy
  - 35-day retention

Kafka:
  - MirrorMaker 2 replication
  - 7-day topic retention
  - Daily topic backups to S3

S3/MinIO:
  - Versioning enabled
  - Cross-region replication
  - Lifecycle policies

📋 Deployment Checklist
Pre-Deployment
All tests passing in CI
Security scan clean
Terraform plan reviewed
Database migrations tested
Rollback plan documented
Stakeholders notified
During Deployment
Monitoring dashboards open
On-call engineer available
Rollback command ready
Communication channel active
Post-Deployment
Smoke tests passing
Metrics baseline established
Error rates normal
Business metrics stable
Deployment documented
Success announced
🛠️ Tools & Technologies

Layer		Technology
IaC		Terraform, Terragrunt
Container	Docker, containerd
Orchestration	Kubernetes (EKS)
Package Manager	Helm 3
GitOps		ArgoCD
CI/CD		GitHub Actions
Registry	Amazon ECR
DNS		Route 53
CDN		Cloudflare
Secrets		AWS Secrets Manager
Certificates	cert-manager + Let's Encrypt

Last Updated: July 2026
Owner: Platform Engineering Team
Review Cadence: Monthly

Last Updated: July 2026
Owner: Platform Engineering Team
Review Cadence: Monthly
