-- Create hypertable for driver locations
CREATE TABLE driver_locations (
    time TIMESTAMP WITH TIME ZONE NOT NULL,
    driver_id UUID NOT NULL,
    latitude DECIMAL(10, 8) NOT NULL,
    longitude DECIMAL(11, 8) NOT NULL,
    speed DECIMAL(5, 2),
    heading DECIMAL(5, 2)
);

-- Convert to hypertable (partitioned by time)
SELECT create_hypertable('driver_locations', 'time');

-- Add compression policy (compress data older than 7 days)
ALTER TABLE driver_locations SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'driver_id'
);

SELECT add_compression_policy('driver_locations', INTERVAL '7 days');

-- Create continuous aggregate for real-time analytics
CREATE MATERIALIZED VIEW driver_location_summary
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 minute', time) AS bucket,
    driver_id,
    AVG(latitude) AS avg_lat,
    AVG(longitude) AS avg_lng,
    COUNT(*) AS ping_count
FROM driver_locations
GROUP BY bucket, driver_id;

-- Index for fast queries
CREATE INDEX idx_driver_locations_driver_time ON driver_locations(driver_id, time DESC);