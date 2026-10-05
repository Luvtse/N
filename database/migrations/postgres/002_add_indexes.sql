-- ============================================================================
-- Migration: 002_add_indexes.sql
-- Purpose: Add performance indexes for high-traffic queries
-- Date: 2026-07-26
-- ============================================================================

-- ============================================================================
-- USERS TABLE INDEXES
-- ============================================================================

-- Composite index for login queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_email_status 
ON users(email, status) 
WHERE status = 'active';

-- Index for phone-based lookups
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_phone_status 
ON users(phone, status) 
WHERE status = 'active';

-- Partial index for active users only
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_active 
ON users(id, email, full_name, role) 
WHERE status = 'active';

-- ============================================================================
-- DRIVERS TABLE INDEXES
-- ============================================================================

-- Composite index for driver availability queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_drivers_status_location 
ON drivers(status, current_lat, current_lng) 
WHERE status = 'available';

-- Index for rating-based sorting
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_drivers_rating 
ON drivers(rating DESC, total_rides DESC) 
WHERE status = 'available';

-- Index for acceptance rate filtering
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_drivers_acceptance_rate 
ON drivers(acceptance_rate DESC) 
WHERE status = 'available';

-- Partial index for verified drivers
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_drivers_verified 
ON drivers(id, user_id, rating) 
WHERE license_verified = true 
  AND background_check = true 
  AND vehicle_inspected = true;

-- ============================================================================
-- RIDES TABLE INDEXES
-- ============================================================================

-- Composite index for user ride history
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_rides_user_requested 
ON rides(user_id, requested_at DESC) 
INCLUDE (status, fare_amount, pickup_address, dropoff_address);

-- Composite index for driver ride history
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_rides_driver_requested 
ON rides(driver_id, requested_at DESC) 
WHERE driver_id IS NOT NULL
INCLUDE (status, fare_amount);

-- Partial index for active rides (critical for matching)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_rides_active 
ON rides(id, user_id, driver_id, status, requested_at) 
WHERE status IN ('requested', 'searching', 'matched', 'driver_en_route', 'in_progress');

-- Index for fare amount queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_rides_fare_amount 
ON rides(fare_amount DESC, requested_at DESC) 
WHERE status = 'completed';

-- Index for ride type filtering
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_rides_type_status 
ON rides(ride_type, status, requested_at DESC);

-- Time-based partitioning index
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_rides_requested_at_hour 
ON rides(DATE_TRUNC('hour', requested_at), status);

-- ============================================================================
-- HOTELS TABLE INDEXES
-- ============================================================================

-- Composite index for city-based searches
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_hotels_city_price 
ON hotels(city, price_per_night ASC) 
WHERE status = 'active';

-- Index for star rating filters
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_hotels_city_stars 
ON hotels(city, star_rating DESC, price_per_night ASC) 
WHERE status = 'active';

-- Partial index for available hotels
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_hotels_available 
ON hotels(id, name, city, price_per_night, star_rating) 
WHERE status = 'active';

-- ============================================================================
-- BOOKINGS TABLE INDEXES
-- ============================================================================

-- Composite index for user booking history
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bookings_user_checkin 
ON bookings(user_id, check_in DESC) 
INCLUDE (hotel_id, status, total_amount);

-- Index for date range queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bookings_date_range 
ON bookings(check_in, check_out) 
WHERE status IN ('pending', 'confirmed');

-- Partial index for active bookings
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_bookings_active 
ON bookings(id, user_id, hotel_id, check_in, check_out) 
WHERE status IN ('pending', 'confirmed', 'checked_in');

-- ============================================================================
-- RESTAURANTS TABLE INDEXES
-- ============================================================================

-- Composite index for city-based searches
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_restaurants_city_rating 
ON restaurants(city, rating DESC, delivery_fee ASC) 
WHERE status = 'active';

-- Index for cuisine type filtering
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_restaurants_cuisine_city 
ON restaurants(cuisine_type, city, rating DESC) 
WHERE status = 'active';

-- Partial index for open restaurants
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_restaurants_open 
ON restaurants(id, name, city, rating, estimated_delivery_minutes) 
WHERE status = 'active';

-- ============================================================================
-- MENU ITEMS TABLE INDEXES
-- ============================================================================

-- Composite index for restaurant menu queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_menu_items_restaurant_category 
ON menu_items(restaurant_id, category, name) 
WHERE is_available = true;

-- Partial index for available items
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_menu_items_available 
ON menu_items(id, restaurant_id, name, price) 
WHERE is_available = true;

-- ============================================================================
-- ORDERS TABLE INDEXES
-- ============================================================================

-- Composite index for user order history
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_user_created 
ON orders(user_id, created_at DESC) 
INCLUDE (restaurant_id, status, total_amount);

-- Index for restaurant order queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_restaurant_created 
ON orders(restaurant_id, created_at DESC) 
WHERE status IN ('pending', 'preparing', 'ready');

-- Partial index for active orders
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_orders_active 
ON orders(id, user_id, restaurant_id, driver_id, status) 
WHERE status IN ('pending', 'confirmed', 'preparing', 'ready', 'picked_up', 'in_transit');

-- ============================================================================
-- ORDER ITEMS TABLE INDEXES
-- ============================================================================

-- Composite index for order item queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_order_items_order_menu 
ON order_items(order_id, menu_item_id);

-- ============================================================================
-- PAYMENTS TABLE INDEXES
-- ============================================================================

-- Composite index for user payment history
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payments_user_created 
ON payments(user_id, created_at DESC) 
INCLUDE (amount, currency, status);

-- Index for payment status queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payments_status_created 
ON payments(status, created_at DESC);

-- Index for Stripe payment intent lookups
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payments_stripe_intent 
ON payments(stripe_payment_intent_id) 
WHERE stripe_payment_intent_id IS NOT NULL;

-- ============================================================================
-- LEGAL DOCUMENTS TABLE INDEXES
-- ============================================================================

-- Composite index for document lookups
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_legal_documents_type_region 
ON legal_documents(document_type, region, language) 
WHERE is_active = true;

-- Index for effective date queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_legal_documents_effective 
ON legal_documents(document_type, effective_date DESC);

-- ============================================================================
-- USER CONSENTS TABLE INDEXES
-- ============================================================================

-- Composite index for user consent queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_consents_user_type 
ON user_consents(user_id, document_type, consent_timestamp DESC);

-- Index for consent status
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_user_consents_status 
ON user_consents(user_id, consent_given) 
WHERE consent_given = true;

-- ============================================================================
-- CONSENT AUDIT LOG INDEXES
-- ============================================================================

-- Composite index for audit queries
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_consent_audit_user_action 
ON consent_audit_log(user_id, action, created_at DESC);

-- Time-based index for retention policies
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_consent_audit_created 
ON consent_audit_log(created_at DESC);

-- ============================================================================
-- ANALYTICS INDEXES (for reporting queries)
-- ============================================================================

-- Index for daily revenue reports
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_rides_daily_revenue 
ON rides(DATE(requested_at), status, fare_amount) 
WHERE status = 'completed';

-- Index for driver performance reports
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_rides_driver_performance 
ON rides(driver_id, DATE(completed_at), fare_amount, rating) 
WHERE status = 'completed' AND driver_id IS NOT NULL;

-- ============================================================================
-- MAINTENANCE OPERATIONS
-- ============================================================================

-- Update statistics after index creation
ANALYZE users;
ANALYZE drivers;
ANALYZE rides;
ANALYZE hotels;
ANALYZE bookings;
ANALYZE restaurants;
ANALYZE menu_items;
ANALYZE orders;
ANALYZE order_items;
ANALYZE payments;
ANALYZE legal_documents;
ANALYZE user_consents;
ANALYZE consent_audit_log;

-- ============================================================================
-- VERIFICATION
-- ============================================================================

-- Verify indexes were created
SELECT 
    schemaname,
    tablename,
    indexname,
    indexdef
FROM pg_indexes
WHERE schemaname = 'public'
ORDER BY tablename, indexname;