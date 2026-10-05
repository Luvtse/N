-- ============================================================================
-- Migration: 001_hypertables.sql
-- Purpose: Create TimescaleDB hypertables for time-series data
-- Date: 2026-07-26
-- ============================================================================

-- Enable TimescaleDB extension
CREATE EXTENSION IF NOT EXISTS timescaledb;

-- ============================================================================
-- DRIVER LOCATIONS HYPERTABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS driver_locations (
    time TIMESTAMPTZ NOT NULL,
    driver_id UUID NOT NULL,
    latitude DECIMAL(10,8) NOT NULL,
    longitude DECIMAL(11,8) NOT NULL,
    heading DECIMAL(5,2),
    speed DECIMAL(5,2),
    accuracy DECIMAL(5,2),
    altitude DECIMAL(8,2),
    status VARCHAR(50)
);

-- Convert to hypertable with 1-day chunks
SELECT create_hypertable('driver_locations', 'time', 
    chunk_time_interval => INTERVAL '1 day',
    if_not_exists => TRUE
);

-- Add compression policy (compress data older than 7 days)
ALTER TABLE driver_locations SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'driver_id',
    timescaledb.compress_orderby = 'time DESC'
);

SELECT add_compression_policy('driver_locations', INTERVAL '7 days');

-- Add retention policy (drop data older than 90 days)
SELECT add_retention_policy('driver_locations', INTERVAL '90 days');

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_driver_locations_driver_time 
ON driver_locations(driver_id, time DESC);

CREATE INDEX IF NOT EXISTS idx_driver_locations_time 
ON driver_locations(time DESC);

-- ============================================================================
-- RIDE METRICS HYPERTABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS ride_metrics (
    time TIMESTAMPTZ NOT NULL,
    ride_id UUID NOT NULL,
    metric_type VARCHAR(50) NOT NULL, -- 'location', 'speed', 'distance'
    latitude DECIMAL(10,8),
    longitude DECIMAL(11,8),
    speed DECIMAL(5,2),
    distance_km DECIMAL(8,2),
    duration_seconds INTEGER
);

SELECT create_hypertable('ride_metrics', 'time', 
    chunk_time_interval => INTERVAL '1 day',
    if_not_exists => TRUE
);

ALTER TABLE ride_metrics SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'ride_id',
    timescaledb.compress_orderby = 'time DESC'
);

SELECT add_compression_policy('ride_metrics', INTERVAL '7 days');
SELECT add_retention_policy('ride_metrics', INTERVAL '180 days');

CREATE INDEX IF NOT EXISTS idx_ride_metrics_ride_time 
ON ride_metrics(ride_id, time DESC);

-- ============================================================================
-- API METRICS HYPERTABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS api_metrics (
    time TIMESTAMPTZ NOT NULL,
    endpoint VARCHAR(255) NOT NULL,
    method VARCHAR(10) NOT NULL,
    status_code INTEGER NOT NULL,
    response_time_ms INTEGER NOT NULL,
    request_size_bytes INTEGER,
    response_size_bytes INTEGER,
    user_id UUID,
    ip_address INET
);

SELECT create_hypertable('api_metrics', 'time', 
    chunk_time_interval => INTERVAL '1 day',
    if_not_exists => TRUE
);

ALTER TABLE api_metrics SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'endpoint,method',
    timescaledb.compress_orderby = 'time DESC'
);

SELECT add_compression_policy('api_metrics', INTERVAL '3 days');
SELECT add_retention_policy('api_metrics', INTERVAL '30 days');

CREATE INDEX IF NOT EXISTS idx_api_metrics_endpoint_time 
ON api_metrics(endpoint, time DESC);

CREATE INDEX IF NOT EXISTS idx_api_metrics_status_time 
ON api_metrics(status_code, time DESC);

-- ============================================================================
-- BUSINESS METRICS HYPERTABLE
-- ============================================================================

CREATE TABLE IF NOT EXISTS business_metrics (
    time TIMESTAMPTZ NOT NULL,
    metric_name VARCHAR(100) NOT NULL, -- 'revenue', 'rides', 'users', etc.
    metric_value DECIMAL(15,2) NOT NULL,
    dimensions JSONB, -- Additional dimensions (city, region, etc.)
    tags TEXT[]
);

SELECT create_hypertable('business_metrics', 'time', 
    chunk_time_interval => INTERVAL '1 day',
    if_not_exists => TRUE
);

ALTER TABLE business_metrics SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'metric_name',
    timescaledb.compress_orderby = 'time DESC'
);

SELECT add_compression_policy('business_metrics', INTERVAL '7 days');
SELECT add_retention_policy('business_metrics', INTERVAL '365 days');

CREATE INDEX IF NOT EXISTS idx_business_metrics_name_time 
ON business_metrics(metric_name, time DESC);

-- ============================================================================
-- CONTINUOUS AGGREGATES
-- ============================================================================

-- Hourly ride statistics
CREATE MATERIALIZED VIEW IF NOT EXISTS ride_stats_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', time) AS bucket,
    COUNT(*) as total_locations,
    COUNT(DISTINCT driver_id) as unique_drivers,
    AVG(speed) as avg_speed,
    MAX(speed) as max_speed
FROM driver_locations
GROUP BY bucket;

SELECT add_continuous_aggregate_policy('ride_stats_hourly',
    start_offset => INTERVAL '3 hours',
    end_offset => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour');

-- Daily API performance
CREATE MATERIALIZED VIEW IF NOT EXISTS api_performance_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', time) AS bucket,
    endpoint,
    method,
    COUNT(*) as request_count,
    AVG(response_time_ms) as avg_response_time,
    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY response_time_ms) as p95_response_time,
    COUNT(*) FILTER (WHERE status_code >= 500) as error_count
FROM api_metrics
GROUP BY bucket, endpoint, method;

SELECT add_continuous_aggregate_policy('api_performance_daily',
    start_offset => INTERVAL '3 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '1 day');

-- Daily business metrics
CREATE MATERIALIZED VIEW IF NOT EXISTS business_metrics_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', time) AS bucket,
    metric_name,
    SUM(metric_value) as total_value,
    AVG(metric_value) as avg_value,
    MAX(metric_value) as max_value,
    MIN(metric_value) as min_value
FROM business_metrics
GROUP BY bucket, metric_name;

SELECT add_continuous_aggregate_policy('business_metrics_daily',
    start_offset => INTERVAL '3 days',
    end_offset => INTERVAL '1 day',
    schedule_interval => INTERVAL '1 day');

-- ============================================================================
-- VERIFICATION
-- ============================================================================

-- List all hypertables
SELECT hypertable_name, num_chunks 
FROM timescaledb_information.hypertables;

-- List all continuous aggregates
SELECT view_name, materialization_hypertable_name 
FROM timescaledb_information.continuous_aggregates;