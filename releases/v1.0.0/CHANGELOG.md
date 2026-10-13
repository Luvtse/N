---

# Changelog

All notable changes to the NIDAW platform will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-07-27

### 🎉 Initial Release

#### Added

**Core Services**
- **Nidus (Rides)**
  - On-demand ride hailing
  - Real-time driver matching with multi-factor scoring
  - Dynamic pricing with surge multipliers
  - ETA prediction using ML models
  - Ride types: Standard, Premium, Electric, Shared, Wheelchair
  - Real-time GPS tracking via WebSocket
  - In-app chat between rider and driver *(see CORRECTIONS.md — not shipped in v1.0.0; targeted for v1.1.0 per ride-parity plan §2.1)*
  - Rating and review system
  - Cancellation with fee policy

- **Haven (Hotels)**
  - Hotel search with filters (city, dates, price, amenities)
  - Real-time availability checking
  - Booking with instant confirmation
  - Multi-room bookings
  - Corporate rate integration
  - Review and rating system
  - Photo gallery with zoom

- **Vorax (Food)**
  - Restaurant discovery
  - Menu browsing with categories
  - Cart management
  - Real-time order tracking
  - Delivery driver tracking
  - Multi-item orders
  - Special instructions
  - Rating and reviews

- **Logix (Freight)**
  - Shipment creation
  - Load matching
  - Real-time tracking
  - Customs documentation
  - Multi-modal transport
  - Proof of delivery

- **Corpus (Corporate)**
  - Corporate account management
  - Employee onboarding (SSO, CSV, API)
  - Travel policy configuration
  - Real-time policy enforcement
  - Expense tracking
  - Approval workflows
  - ERP integration (SAP, Oracle, NetSuite)
  - Custom reporting

**Platform Features**
- Multi-region deployment (NA-East, EU-West, APAC-South)
- Active-passive failover with automatic DNS switching
- Event-driven architecture with Apache Kafka
- CQRS pattern with event sourcing
- Real-time WebSocket updates
- Offline mode with operation queuing
- Push notifications (Firebase)
- Email notifications (SendGrid)
- SMS notifications (Twilio)

**AI/ML Features**
- ETA prediction (XGBoost, P95 < 200ms)
- Demand forecasting (LSTM)
- Dynamic pricing optimization
- Driver churn prediction
- Restaurant recommendations (Neural Collaborative Filtering)
- Feature store (Feast) with real-time serving
- Model monitoring with drift detection
- Federated learning for privacy

**Blockchain Features**
- NIDAW Token (ERC-20)
- Staking with 5-15% APY
- Loyalty tiers (Bronze → Diamond)
- Governance voting (planned)
- Smart contract audits completed

**Autonomous Features (Experimental)**
- Autonomous vehicle orchestration
- Drone delivery control
- Safety monitoring
- Remote operations

**Client Applications**
- Flutter Super-App (iOS, Android, Web)
- Driver Mobile App (iOS, Android)
- Corporate Web Portal (React)
- Partner Dashboard (React)

**Security**
- JWT authentication (RS256)
- Role-Based Access Control (RBAC)
- Multi-factor Authentication (MFA)
- Encryption at rest (AES-256)
- Encryption in transit (TLS 1.3)
- Web Application Firewall (Cloudflare)
- DDoS protection
- Secret management (AWS Secrets Manager)
- SOC 2 Type II compliance
- PCI DSS Level 1 compliance
- GDPR compliance
- CCPA compliance

**Infrastructure**
- Kubernetes (EKS) with auto-scaling
- PostgreSQL 15 with Citus
- TimescaleDB for time-series data
- Redis 7 with cluster mode
- Apache Kafka 3.5 (MSK)
- Kong API Gateway 3.5
- Prometheus + Grafana monitoring
- Loki log aggregation
- Jaeger distributed tracing
- Terraform for IaC
- Helm for package management
- GitHub Actions for CI/CD

**Observability**
- 50+ Grafana dashboards
- 100+ Prometheus metrics
- Structured logging (JSON)
- Distributed tracing (OpenTelemetry)
- AlertManager with PagerDuty integration
- Status page (Uptime Kuma)

**Documentation**
- Architecture documentation
- API documentation (OpenAPI)
- Deployment guides
- Monitoring guides
- Disaster recovery procedures
- Incident response runbooks
- Security policies
- Developer onboarding

#### Security
- Zero known vulnerabilities at release
- All dependencies scanned with Trivy
- All secrets scanned with Gitleaks
- DAST scanning completed with OWASP ZAP
- Penetration test completed by external firm
- Code review by security team

#### Performance
- API P95 latency: 320ms (target: < 500ms)
- API P99 latency: 680ms (target: < 1000ms)
- Error rate: 0.3% (target: < 1%)
- Throughput: 15K req/s (target: 10K req/s)
- Concurrent users: 2000 (target: 1000)

#### Documentation
- Complete API documentation
- Architecture diagrams
- Deployment guides
- Monitoring guides
- Security policies

### 🔧 Technical Details

**Backend**
- Go 1.21
- Chi Router v5
- pgx v5
- JWT v5
- Kafka-go v0.47
- Zap logger

**Frontend**
- Flutter 3.16
- Dart 3.2
- flutter_bloc 8.1
- go_router 13.0
- dio 5.4
- google_maps_flutter 2.5

**Infrastructure**
- AWS EKS 1.28
- AWS RDS PostgreSQL 15
- AWS ElastiCache Redis 7
- AWS MSK Kafka 3.5
- Cloudflare CDN/WAF

**AI/ML**
- Python 3.11
- PyTorch 2.1
- XGBoost 2.0
- Feast 0.34
- MLflow 2.9
- TorchServe 0.9

**Blockchain**
- Solidity 0.8.20
- Hardhat 2.19
- Ethers.js 6.9

## [Unreleased]

### Added (on main, pending release — ride-parity Foundation phase)
- **Map provider migration to OpenStreetMap (§1.1):** `flutter_map`-backed `LiveMapWidget` with raster-tile fallback chain (primary mirror → secondary mirror → Hive tile cache → last-known-good), marker clustering support, camera auto-fit, recenter FAB, and a `MapHealth` banner with retry when all tile sources fail. Removed `google_maps_flutter`, the static placeholder widget, and the duplicate home request page.
- **Device location & GPS pickup pin (§1.2):** `DeviceLocationService` with discriminated permission/service results, live position stream (10 m distance filter), last-known-position instant pin, and a `LocationPermissionGate` covering denied / permanently-denied / service-disabled states with manual-pin fallback. All hardcoded demo coordinates removed.
- **Geocoding & place autocomplete (§1.3):** Provider-chain geocoder (Nominatim ≤1 req/s token bucket → Photon → device-native geocoder → Hive cache with staleness note), TTL/LRU cache, debounced autocomplete sheet with "Use current location" and "Set pin on map", reverse-geocode-on-drag, and a debug GeocodingHealth indicator.
- **Notifications substrate (§1.4):** Backend `notifications` module (domain entity, Postgres + in-memory repositories, config-driven dispatcher policy, in-app WS adapter, FCM adapter, email adapter, 3-attempt exponential-backoff retry worker, device-token registry) with REST endpoints `POST /api/v1/notifications/devices`, `DELETE /api/v1/notifications/devices/{token}`, `GET /api/v1/notifications/inbox`, `GET /api/v1/notifications/inbox/unread-count`, `POST /api/v1/notifications/{id}/read`; event-bus consumer mapping ride.matched/completed, payment.failed, promo.granted, sos.confirmed to notification categories. Flutter client: FCM service (foreground banner, background/killed deep-link routing), inbox bell with unread badge, notification inbox page, in-app overlay banner. Gated by `FEATURE_NOTIFICATIONS` (default false in production).
- **Router cleanup & feature flags (§1.5):** Registered `/nidus/ar-nav/:rideId` behind the `ar_navigation` flag (now default-off with documented kill-switch); added client-side `FeatureFlagService` loading `assets/config/feature-flags/flags.json` with deterministic rollout bucketing; deleted dead map/request-page code and bare TODO markers from shipped surfaces.

### Fixed
- Device-token revocation is now ownership-scoped (a user can only revoke their own tokens; foreign tokens return 404 without probing).
- Notification HTTP handlers derive recipient identity exclusively from the JWT subject, never from request bodies.

### Added (on main, pending release — ride-parity Phase 1)
- **In-app chat rider↔driver (§2.1):** Backend `chat` module (Message aggregate, Postgres + in-memory repositories, keyset pagination, server-side canned-message catalogs per role/locale, optional flag-gated LibreTranslate adapter) with REST endpoints `POST/GET /api/v1/chat/rides/{rideId}/messages`, `GET .../unread`, `POST .../read`, `GET /api/v1/chat/canned`; real-time delivery over WS topic `chat.ride.{rideId}`. Flutter: full-screen `ChatPage` with optimistic send + retry affordance, canned chips, unread badge on the Tracking driver card. Gated by `FEATURE_CHAT` / `FEATURE_CHAT_TRANSLATION` (default false).
- **Masked/anonymous calling (§2.2):** Backend `telephony` module (ProxySession aggregate, pluggable provider port with Twilio Proxy implementation and in-memory pool for tests/dev, participant-only authorization that never returns the other party's real number, TTL = ride duration + 30 min, expiry sweeper emitting terminal events, event-driven provision-on-matched / teardown-on-completed|cancelled) with `GET /api/v1/telephony/rides/{rideId}/contact`. Flutter: "Call driver" button on the tracking page fetches the proxy number and launches the OS dialer via `url_launcher` `tel:`; unprovisioned/unavailable states show "Calling unavailable — try chat" instead of a fake number. Gated by `FEATURE_MASKED_CALLING` (default false; fail-loud startup validation when enabled without Twilio credentials).
- **Pre-selfie verification (§2.3):** Backend `selfie` module (PreSelfie aggregate with 15-min signed-URL expiry, driver-only multipart upload with ≤200 KB and JPEG/PNG/WebP enforcement, idempotent re-upload, HMAC-SHA256 signed redemption URLs with constant-time verification and tamper rejection, participant-gated redemption) with `POST /api/v1/driver/rides/{rideId}/pre-selfie` and `GET /api/v1/selfies/{id}?exp&sig`; attacher embeds `pre_selfie` state (`available`/`not_sent`/`expired`/`unavailable`) on the rider ride DTO. Flutter: driver capture sheet fires once on DriverEnRoute start (real image_picker camera flow with quality-ladder compression, honest failure messages + Retry, no fake success); rider "Verify your driver" card renders exactly one honest state per backend value with full-screen selfie-vs-profile comparison; bloc splits DriverEnRoute into rider vs. driver views via AuthBloc session identity. Gated by `FEATURE_PRE_SELFIE` (default false).

### Planned for v1.1.0
- In-app chat between rider and driver (moved from v1.0.0 claim — see CORRECTIONS.md)
- Additional regions (UK, Japan, Australia, Brazil)
- Enhanced autonomous vehicle features
- Drone delivery expansion
- Advanced analytics dashboards
- Mobile app offline sync improvements
- Corporate API v2
- Partner portal enhancements

### Planned for v1.2.0
- Multi-language support (10+ languages)
- Accessibility improvements (WCAG 2.1 AA)
- Advanced fraud detection
- Real-time translation
- Voice commands expansion
- AR navigation improvements

---

**For more information, see:**
- [Release Notes](README.md)
- [Documentation](../../docs/README.md)
- [Roadmap](../../docs/architecture/roadmap.md)