-- ============================================================================
-- Sample Data for Development/Testing
-- This file is a convenience wrapper that runs all seed migrations
-- ============================================================================

-- Run PostgreSQL seed data
\i ../migrations/postgres/003_seed_data.sql

-- Insert sample time-series data
INSERT INTO driver_locations (time, driver_id, latitude, longitude, heading, speed, status)
SELECT 
    NOW() - (random() * INTERVAL '24 hours') as time,
    '660e8400-e29b-41d4-a716-446655440001'::UUID as driver_id,
    40.7128 + (random() - 0.5) * 0.1 as latitude,
    -74.0060 + (random() - 0.5) * 0.1 as longitude,
    random() * 360 as heading,
    random() * 60 as speed,
    'available' as status
FROM generate_series(1, 1000);

INSERT INTO api_metrics (time, endpoint, method, status_code, response_time_ms, request_size_bytes, response_size_bytes)
SELECT 
    NOW() - (random() * INTERVAL '7 days') as time,
    '/api/v1/nidus/rides' as endpoint,
    CASE WHEN random() > 0.5 THEN 'POST' ELSE 'GET' END as method,
    CASE 
        WHEN random() > 0.95 THEN 500
        WHEN random() > 0.90 THEN 400
        ELSE 200
    END as status_code,
    (random() * 500 + 50)::INTEGER as response_time_ms,
    (random() * 1000)::INTEGER as request_size_bytes,
    (random() * 5000)::INTEGER as response_size_bytes
FROM generate_series(1, 10000);

INSERT INTO business_metrics (time, metric_name, metric_value, dimensions)
SELECT 
    NOW() - (random() * INTERVAL '30 days') as time,
    'daily_revenue' as metric_name,
    (random() * 10000 + 5000)::DECIMAL(15,2) as metric_value,
    '{"city": "New York", "region": "US"}'::JSONB as dimensions
FROM generate_series(1, 720);

-- Verify counts
SELECT 'driver_locations' as table_name, COUNT(*) as record_count FROM driver_locations
UNION ALL
SELECT 'api_metrics', COUNT(*) FROM api_metrics
UNION ALL
SELECT 'business_metrics', COUNT(*) FROM business_metrics;