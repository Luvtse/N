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

### Added (on main, pending release — ride-parity Phase 1, continued)
- **Authoritative cancellation-fee quote (§2.5):** Backend shared policy service (`ComputeCancellationQuote`) as the single source of truth for both the quote and the charge, with `GET /api/v1/nidus/rides/{rideId}/cancellation-quote` returning fee, currency, reason code, free-window countdown, and itemized breakdown; per-ride 30 s quote cache evicted on cancel; `CancelRide` now persists the authoritative `cancel_fee`/`cancelled_by` (migration `006_cancellation_quote.sql`). Flutter: `cancel_ride_sheet.dart` fully rewritten to fetch the server quote on open, render a live countdown and breakdown lines, and degrade honestly to "Fee will be confirmed on cancel" on fetch failure — client-side fee heuristics removed entirely.
- **Server-driven ride-option carousel (§2.6):** Canonical `GET /api/v1/nidus/rides/estimates?pickup=&dropoff=&scheduled_at=` endpoint returning per-type display name, price range, ETA, capacity, surge multiplier/reason, and availability for all seven types (including Shared and Wheelchair). Flutter `ride_type_selector.dart` rewritten to render one card per backend estimate with surge badges; hardcoded demo cards and fake price strings deleted; selected type now flows into the booking request.
- **Notifications substrate gap closure (§1.4 follow-up):** WS event bridge is now actually wired into the server event bus (previously dead code — real-time ride updates were silently dropped). Added the missing `driver.arriving` producer: `DriverArrivingWatcher` subscribes to driver location updates, computes haversine distance to pickup, and emits tiered events (≤300 m approaching, ≤80 m arriving) once per ride/tier with dedupe, TTL pickup-coordinate cache, and fail-safe degradation; wired into the server behind `FEATURE_NOTIFICATIONS`. The notifications consumer dispatches `driver_arriving` with a tracking-page deep link. Integration tests added for the notifications consumer (3 cases incl. category/deep-link assertions through an in-process memory bus) and the telephony consumer (4 cases: provision-on-matched, idempotent teardown, unknown-event no-op, fail-closed authorization). Watcher unit tests cover tier sequencing, dedupe, publish-failure rollback, caching, and lookup degradation.

### Fixed (audit follow-ups)
- Chat real-time delivery: backend published to topic `chat.{rideId}` while the client subscribed to `chat.ride.{rideId}` — every chat message was silently dropped. Publisher corrected to the documented `chat.ride.{rideId}` contract; regression test asserts the topic string.
- Flutter analyzer exclusion that had been masking errors in `lib/features/nidus/**` removed; two real constructor-argument errors in `nidus_providers.dart` fixed; seven pre-existing widget-test fakes extended to satisfy the new repository interface.

### Added (on main, pending release — ride-parity Phase 3)

- **Follow-up polish pass (§4.3/§4.4, post-audit):** Repo-wide `Colors.white` audit of the nidus tree converted every accidental light-mode bleed to theme tokens — request-page app bar, history page (app bar + filter-chip strip + selected-chip foreground → `AppColors.surface`/`textOnDark`), safety-toolkit sheets (`AppColors.surface`). Deliberate white-on-dark overlay foregrounds keep the documented `AppColors.textOnDark` token; the remaining raw `Colors.white` sites are all on intentionally dark surfaces (AR camera-overlay chrome, SOS red banner in preserved peripheral safety widgets) and are annotated as such. §4.3 orphan-key audit: zero dangling ARB references found; two previously-unreferenced shipped strings (request/history app-bar titles, history empty state) were still hardcoded English and are now extracted — catalogs at 135-key parity across en/am/om, JSON-validated, placeholder sets identical per locale. Deferred literals (cancellation-reason list, GPS-banner copy, safety-toolkit body text) remain tracked follow-ups in `doc/FOLLOWUPS-PHASE3.md`, not silent drift.
- **AR navigation entry point & 2D fallback (§4.1):** Tracking page now shows a "Walk to pickup (AR)" button during DriverEnRoute, gated by the `ar_navigation` runtime flag (default off, kill-switch documented in `config/feature-flags/flags.json`) and the `AppConstants.enableARNavigation` compile switch; it deep-links `/nidus/ar-nav/{rideId}` with the ride's authoritative pickup coordinates and place name (new nullable lat/lng/name fields propagated through RideRequested → DriverMatched → DriverEnRoute bloc states from the WS/REST DTOs — no fabricated positions). `ar_navigation_page.dart` rewritten: AR availability probed via guarded `MethodChannel('ar_flutter_plugin').invokeMethod('isSupported')` with timeout (typed-symbol probe removed — `ar_flutter_plugin ^0.7.x` exports no static `isSupported()`); unsupported/failed devices render a real `LiveMapWidget` 2D fallback (rider GPS marker, pickup pin, walking polyline, live distance/bearing header) instead of a blank camera rectangle; the header "Map View" button toggles the fallback explicitly; both location stream subscriptions are cancelled in `dispose` (previous code leaked them). `vector_math ^2.1.2` declared explicitly in pubspec.yaml per the dependency rule.

- **Voice Booking GA (§4.2):** `voice_booking` feature flag added (default off, kill-switch documented in both `config/feature-flags/flags.json` and the bundled client copy). Mic FAB on the Request page opens a listening sheet backed by the rewritten `VoiceCommandService`: recognizer confidence now flows on every `VoiceCommand`, high-confidence `bookRide` transcripts resolve through the same geocoding chain as typed input and fill the dropoff via `_applySuggestion` (rider still presses Request — voice never books silently), low-confidence or zero-match transcripts fall back to `PlaceAutocompleteSheet` prefilled with the utterance (new `initialQuery` parameter), and recognizer failures surface honest "unavailable" states. The fabricated "Your driver is 3 minutes away" feedback string was deleted; no shipped voice string asserts a live fact the service cannot read. Stream subscription + mic are torn down in dispose on both sheet and page.

- **Localization — Amharic / Oromo (§4.3):** `flutter_localizations` + `intl` declared in pubspec.yaml; `l10n.yaml` configured for gen-l10n with `AppLocalizations` output. Full ARB catalogs shipped: `app_en.arb`, `app_am.arb`, `app_om.arb` covering ride request, tracking, chat, receipt, cancel, payment, and settings surfaces. `LocaleController` (`lib/l10n/l10n.dart`) persists the user's choice in Hive and broadcasts session-level changes via a ValueNotifier that `MaterialApp.router` consumes for live switching without restart; supported locales wired in `main.dart`. Settings page replaced its static language dropdown with functional radio tiles driven by `LocaleController.choices`. Backend notification templates are selected per-category by the dispatcher using the recipient's stored locale field (Amharic/Oromo template variants land alongside English; missing locale falls back to en with telemetry, never a blank body). Remaining nidus-sheet literals not yet routed through ARB keys are tracked as deferred follow-ups rather than left as silent drift.

- **Accessibility pass (§4.4):** Semantics labels added across the request/tracking journey sheets — cancel-ride sheet (radio reasons marked selected/checked with consequence-aware button labels including an honest fee suffix that degrades to "fee will be confirmed on cancel"), payment-method selector (registry rows expose selection + default state to screen readers), ride-type selector (composite label: name, price band, ETA, surge, availability), tracking bottom sheet and AR entry. Hardcoded `Colors.white` in shipped sheets replaced with theme tokens (`AppColors.surface` for sheet backgrounds; new named token `AppColors.textOnDark` documents intentional white-on-dark overlay foregrounds such as the safety banner and star glyphs, so residual uses are deliberate, not accidental light-mode bleed). Dynamic-type audit removed fixed-height clipping containers from the touched sheets so labels grow at largest text scale instead of truncating. TalkBack/VoiceOver journey verification remains a device-side CI task (no Flutter SDK in this build environment).

- **RideCheck anomaly check-ins (§4.5):** `ridecheck_anomaly` feature flag added (default false in production, kill-switch documented in both flags.json copies; with the flag off the detector is fully dormant and no prompt can appear). Backend plumbs the ETA service's existing confidence score onto the driver DTO as `eta_confidence` (nullable — absent means "not measured", never defaulted). Flutter side: `RideBloc` consumes the WS driver-position stream through `ReportRidePosition` and trips an anomaly when any of — vehicle stationary >3 min outside pickup/dropoff radius, position deviates >500 m from the pickup→dropoff corridor (latlong2 `Distance()`), or `eta_confidence` < 0.4 — with a snooze grace after dismissal and per-ride cache resets on ride-id change / `ClearCurrentRide`. The Tracking page renders an "Is everything OK?" modal carrying only measured detail (meters/confidence shown only when actually computed); "I'm OK" dispatches `DismissRideCheckPrompt.ok()`, "Call for help" routes into the existing SOS countdown with the rider's real GPS fix from `DeviceLocationService` — no parallel emergency path, no fabricated location.

- **Price-extension negotiation (§4.6):** `price_extension` feature flag added (default false, kill-switch documented in both flags.json copies; the HTTP route itself is unmounted when the server-side gate is off, so the endpoint 404s rather than half-working). New server-authoritative endpoint `POST /api/v1/nidus/rides/{rideID}/price-extension` accepts only the approved multiplier buckets (+10% → 1.10, +20% → 1.20 — validated against a server-owned list, never a client-supplied fare), recomputes the fare through `PricingService`, and persists it with a race-guarded `UPDATE rides SET fare_amount=$2 … WHERE id=$1 AND status IN ('requested','searching') AND driver_id IS NULL AND fare_amount=$4` — persistence alone re-broadcasts the ride because the auto-matcher selects searching rides with no driver on its next tick. A matched/completed ride rejects with `409 RIDE_NOT_EXTENSIBLE`; integration tests cover the 503 NO_DRIVERS → 200 transition, bucket validation, auth, unwired-store honesty (501, never a fake 200), and the sentinel→HTTP-code contract map. Flutter side: `RideRepository.applyPriceExtension` mirrors the `CancelRideResult` named-constructor pattern (`PriceExtensionResult.extended/.failure`); the Tracking page's `NoDriversFound` listener no longer pops back — behind the flag it opens a sheet projecting +10%/+20% from the real `currentFare` the bloc now carries on `NoDriversFound` (null fare → "Current fare unavailable"; apply failure → "New price will be confirmed on retry"; success → re-subscribes `SubscribeToRideUpdates`). With the flag off, today's pop-back behavior is unchanged.

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