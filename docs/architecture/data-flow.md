# Data Flow

## 🎯 Overview

This document describes how data flows through the NIDAW platform, from user interaction to storage, processing, and analytics.

## 🌊 Data Flow Patterns

### 1. Synchronous Request-Response

**Use Case:** User requests a ride

┌────────┐ HTTP ┌──────────┐ Route ┌──────────┐
│ Client │ ──────────▶ │ Kong │ ──────────▶ │ Nidus │
│ (App) │ │ Gateway │ │ Module │
└────────┘ └──────────┘ └────┬─────┘
▲ │
│ ▼
│ ┌──────────┐
└──────────────── JSON ────────────────── │PostgreSQL│
└──────────┘


**Steps:**
1. Client sends POST `/api/v1/nidus/rides`
2. Kong validates JWT, applies rate limiting
3. Request routed to Nidus module
4. Command handler validates input
5. Domain logic creates ride entity
6. Persist to PostgreSQL
7. Return ride_id to client

### 2. Event-Driven Asynchronous

**Use Case:** Ride status change notification

┌──────────┐ Event ┌──────────┐ Publish ┌──────────┐
│ Nidus │ ─────────▶ │ Event │ ────────▶ │ Kafka │
│ Module │ │ Bus │ │ Topic │
└──────────┘ └──────────┘ └────┬─────┘
│
┌─────────────────────┼─────────────┐
│ │ │
▼ ▼ ▼
┌──────────┐ ┌──────────┐ ┌──────────┐
│ Notifier │ │Analytics │ │ ML │
│ Service │ │ Pipeline │ │ Pipeline │
└──────────┘ └──────────┘ └──────────┘


**Steps:**
1. Ride status changes (e.g., `matched` → `in_progress`)
2. Nidus emits `ride.status_changed` event
3. Event published to Kafka topic `nidus.rides`
4. Multiple consumers process independently:
   - **Notifier**: Sends push notification to rider
   - **Analytics**: Updates real-time dashboard
   - **ML Pipeline**: Feeds feature store

### 3. Real-Time Streaming (WebSocket)

**Use Case:** Live driver location tracking

┌──────────┐ GPS ┌──────────┐ Update ┌──────────┐
│ Driver │ ────────▶ │ Nidus │ ────────▶ │ Redis │
│ App │ │ Module │ │ (Geo) │
└──────────┘ └────┬─────┘ └──────────┘
│
▼
┌──────────┐ Broadcast ┌──────────┐
│WebSocket │ ──────────▶ │ Rider │
│ Server │ │ App │
└──────────┘ └──────────┘


**Steps:**
1. Driver app sends GPS update every 3 seconds
2. Nidus updates Redis GEO index
3. WebSocket server broadcasts to subscribed riders
4. Rider app renders driver position on map

### 4. Batch Processing (ETL)

**Use Case:** Daily revenue aggregation

┌──────────┐ Extract ┌──────────┐ Transform ┌──────────┐
│PostgreSQL│ ─────────▶ │ Airflow │ ────────▶ │ClickHouse│
│ (OLTP) │ │ (DAG) │ │ (OLAP) │
└──────────┘ └──────────┘ └──────────┘
│
▼
┌──────────┐
│ Grafana │
│Dashboard │
└──────────┘


## 📊 Event Schema

### Ride Requested Event

```json
{
  "event_id": "evt_abc123",
  "event_type": "ride.requested",
  "timestamp": "2026-07-27T10:30:00Z",
  "payload": {
    "ride_id": "ride_xyz789",
    "user_id": "user_123",
    "pickup": { "lat": 40.7128, "lng": -74.0060 },
    "dropoff": { "lat": 40.7589, "lng": -73.9851 },
    "ride_type": "standard",
    "fare_estimate": 15.50,
    "currency": "USD"
  },
  "metadata": {
    "correlation_id": "corr_456",
    "region": "na-east",
    "version": "1.0"
  }
}

🔄 Data Synchronization
Cross-Region Replication

┌──────────────┐    MirrorMaker 2    ┌──────────────┐
│  NA-East     │ ─────────────────▶ │  EU-West     │
│  Kafka       │                    │  Kafka       │
│  (Primary)   │ ◀───────────────── │  (Secondary) │
└──────────────┘    (Bi-directional) └──────────────┘

Topics Replicated:
User profiles (for global authentication)
Corporate accounts (multi-region access)
Audit logs (compliance)
Topics NOT Replicated:
High-frequency GPS data (local only)
PII (privacy compliance)
Real-time ride events (local processing)
📈 Analytics Data Flow
Real-Time Analytics (Apache Pinot)

┌──────────┐   Event    ┌──────────┐  Ingest   ┌──────────┐
│  Kafka   │ ─────────▶ │  Pinot   │ ────────▶ │  Query   │
│  Topic   │            │ Realtime │           │  Layer   │
└──────────┘            └──────────┘           └──────────┘

Batch Analytics (Data Warehouse)

┌──────────┐   CDC     ┌──────────┐   Load    ┌──────────┐
│PostgreSQL│ ────────▶ │  S3      │ ────────▶ │ Snowflake│
│          │           │  (Lake)  │           │          │
└──────────┘            └──────────┘           └──────────┘

🤖 ML Data Flow
Feature Pipeline

┌──────────┐   Events   ┌──────────┐ Compute  ┌──────────┐
│  Kafka   │ ─────────▶ │  Feast   │ ───────▶ │  Redis   │
│          │            │ Pipeline │          │ (Online) │
└──────────┘            └──────────┘          └──────────┘
                              │
                              ▼
                         ┌──────────┐
                         │PostgreSQL│
                         │(Offline) │
                         └──────────┘

Model Training Flow

┌──────────┐   Features  ┌──────────┐  Train   ┌──────────┐
│  Feast   │ ──────────▶ │  MLflow  │ ───────▶ │  Model   │
│(Offline) │             │          │          │ Registry │
└──────────┘             └──────────┘          └──────────┘
                                                    │
                                                    ▼
                                               ┌──────────┐
                                               │TorchServe│
                                               │(Serving) │
                                               └──────────┘


🔐 Data Classification

Classification	Examples	Handling
Public	Marketing content	No restrictions
Internal	Aggregated metrics	Internal access only
Confidential	User profiles, rides	Encrypted, RBAC
Restricted	Payment data, PII	HSM, audit logged


📊 Data Retention

Data Type	Retention	Archive
Active rides	90 days	        Delete
Completed rides	7 years	        S3 Glacier
GPS telemetry	90 days	        Compress
Audit logs	7 years		Immutable
Analytics	3 years		Aggregate


🔄 Change Data Capture (CDC)
Debezium CDC Flow

┌──────────┐   WAL     ┌──────────┐  Publish  ┌──────────┐
│PostgreSQL│ ────────▶ │ Debezium │ ────────▶ │  Kafka   │
│          │           │Connector │           │  Topic   │
└──────────┘            └──────────┘           └──────────┘

Use Cases:
Sync to Elasticsearch for search
Feed analytics pipeline
Update cache layers
📈 Performance Considerations
Write Path Optimization
Batching: Combine multiple events into single Kafka message
Async Writes: Don't block on persistence
Connection Pooling: PgBouncer for PostgreSQL
Write-Ahead Log: Event sourcing for durability
Read Path Optimization
Caching: Redis for hot data
Materialized Views: Pre-computed aggregations
CDN: Static assets at edge
Query Optimization: Proper indexes, query plans
🛡️ Data Integrity
Guarantees
At-Least-Once: Kafka with acknowledgments
Idempotency: Event IDs prevent duplicates
Compensation: Saga pattern for distributed transactions
Reconciliation: Periodic consistency checks
Monitoring
Lag Monitoring: Kafka consumer lag alerts
Data Quality: Great Expectations validation
Schema Drift: Schema Registry enforcement
Last Updated: July 2026
Owner: Data Engineering Team
Review Cadence: Monthly





