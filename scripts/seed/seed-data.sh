#!/usr/bin/env bash
# ============================================================================
# NIDAW Database Seed Script
# Populates database with sample data
# ============================================================================

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $*"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $*"; }

ENVIRONMENT="${ENVIRONMENT:-development}"
SEED_DIR="${SEED_DIR:-$(dirname "$0")/../../database/seeds}"
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-nidaw}"
DB_USER="${DB_USER:-nidaw}"
DB_PASSWORD="${DB_PASSWORD:-}"
CLEAR_FIRST="${CLEAR_FIRST:-false}"

if [[ -n "$DB_PASSWORD" ]]; then
    export PGPASSWORD="$DB_PASSWORD"
fi

usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Seed database with sample data.

OPTIONS:
    -e, --environment ENV     Environment [default: development]
    --clear                   Clear existing data first
    -h, --help                Show this help

EOF
    exit 0
}

while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--environment) ENVIRONMENT="$2"; shift 2 ;;
        --clear) CLEAR_FIRST="true"; shift ;;
        -h|--help) usage ;;
        *) shift ;;
    esac
done

if [[ "$ENVIRONMENT" == "production" ]]; then
    echo "ERROR: Cannot seed production database!"
    exit 1
fi

clear_data() {
    if [[ "$CLEAR_FIRST" != "true" ]]; then
        return
    fi
    
    log_info "Clearing existing data..."
    
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" << 'EOF'
TRUNCATE TABLE
    order_items, orders, bookings, rides, drivers, users,
    restaurants, hotels, payments, legal_documents, user_consents
CASCADE;
EOF
    
    log_success "Data cleared"
}

seed_users() {
    log_info "Seeding users..."
    
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" << 'EOF'
INSERT INTO users (id, email, phone, full_name, password_hash, role, status, email_verified, country_code) VALUES
('550e8400-e29b-41d4-a716-446655440001', 'rider1@nidaw.com', '+12345678901', 'John Rider', '$2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u', 'rider', 'active', true, 'US'),
('550e8400-e29b-41d4-a716-446655440002', 'rider2@nidaw.com', '+12345678902', 'Jane Smith', '$2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u', 'rider', 'active', true, 'US'),
('550e8400-e29b-41d4-a716-446655440011', 'driver1@nidaw.com', '+12345678911', 'Mike Driver', '$2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u', 'driver', 'active', true, 'US'),
('550e8400-e29b-41d4-a716-446655440012', 'driver2@nidaw.com', '+12345678912', 'Sarah Driver', '$2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u', 'driver', 'active', true, 'US'),
('550e8400-e29b-41d4-a716-446655440099', 'admin@nidaw.com', '+12345678999', 'Admin User', '$2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u', 'admin', 'active', true, 'US')
ON CONFLICT (email) DO NOTHING;
EOF
    
    log_success "Users seeded"
}

seed_drivers() {
    log_info "Seeding drivers..."
    
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" << 'EOF'
INSERT INTO drivers (id, user_id, rating, acceptance_rate, completion_rate, total_rides, status, current_lat, current_lng, vehicle_type, vehicle_model, vehicle_plate, license_verified, background_check, vehicle_inspected) VALUES
('660e8400-e29b-41d4-a716-446655440001', '550e8400-e29b-41d4-a716-446655440011', 4.9, 95.5, 98.2, 1250, 'available', 40.7128, -74.0060, 'sedan', 'Toyota Camry', 'ABC123', true, true, true),
('660e8400-e29b-41d4-a716-446655440002', '550e8400-e29b-41d4-a716-446655440012', 4.8, 92.3, 97.5, 980, 'available', 40.7589, -73.9851, 'suv', 'Honda CR-V', 'DEF456', true, true, true)
ON CONFLICT (id) DO NOTHING;
EOF
    
    log_success "Drivers seeded"
}

seed_hotels() {
    log_info "Seeding hotels..."
    
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" << 'EOF'
INSERT INTO hotels (id, name, description, country_code, city, address, latitude, longitude, star_rating, price_per_night, currency, amenities, images, status) VALUES
('770e8400-e29b-41d4-a716-446655440001', 'Grand Plaza Hotel', 'Luxury hotel in Manhattan', 'US', 'New York', '123 Broadway', 40.7589, -73.9851, 5, 299.99, 'USD', '["WiFi", "Pool", "Gym", "Spa"]', '["https://images.unsplash.com/photo-1566073771259-6a8506099945"]', 'active'),
('770e8400-e29b-41d4-a716-446655440002', 'City Inn', 'Budget-friendly accommodation', 'US', 'New York', '456 5th Ave', 40.7549, -73.9840, 3, 129.99, 'USD', '["WiFi", "Breakfast"]', '["https://images.unsplash.com/photo-1551882547-ff40c63fe5fa"]', 'active')
ON CONFLICT (id) DO NOTHING;
EOF
    
    log_success "Hotels seeded"
}

seed_restaurants() {
    log_info "Seeding restaurants..."
    
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" << 'EOF'
INSERT INTO restaurants (id, name, description, cuisine_type, country_code, city, address, latitude, longitude, rating, delivery_fee, min_order_amount, estimated_delivery_minutes, images, status) VALUES
('880e8400-e29b-41d4-a716-446655440001', 'Pizza Palace', 'Authentic Italian pizza', 'Italian', 'US', 'New York', '789 Pizza St', 40.7282, -73.7949, 4.7, 3.99, 15.00, 25, '["https://images.unsplash.com/photo-1604068549290-dea0e4a305ca"]', 'active'),
('880e8400-e29b-41d4-a716-446655440002', 'Sushi Master', 'Premium Japanese sushi', 'Japanese', 'US', 'New York', '321 Sushi Ave', 40.7648, -73.9808, 4.8, 4.99, 20.00, 30, '["https://images.unsplash.com/photo-1579871494447-9811cf80d66c"]', 'active')
ON CONFLICT (id) DO NOTHING;
EOF
    
    log_success "Restaurants seeded"
}

seed_menu_items() {
    log_info "Seeding menu items..."
    
    psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" << 'EOF'
INSERT INTO menu_items (id, restaurant_id, name, description, price, currency, category, is_available) VALUES
('990e8400-e29b-41d4-a716-446655440001', '880e8400-e29b-41d4-a716-446655440001', 'Margherita Pizza', 'Fresh tomatoes, mozzarella, basil', 14.99, 'USD', 'Pizza', true),
('990e8400-e29b-41d4-a716-446655440002', '880e8400-e29b-41d4-a716-446655440001', 'Pepperoni Pizza', 'Classic pepperoni with cheese', 16.99, 'USD', 'Pizza', true),
('990e8400-e29b-41d4-a716-446655440005', '880e8400-e29b-41d4-a716-446655440002', 'Salmon Nigiri', 'Fresh salmon on rice (2 pcs)', 8.99, 'USD', 'Nigiri', true)
ON CONFLICT (id) DO NOTHING;
EOF
    
    log_success "Menu items seeded"
}

main() {
    log_info "Seeding database: $DB_NAME"
    
    clear_data
    seed_users
    seed_drivers
    seed_hotels
    seed_restaurants
    seed_menu_items
    
    log_success "Database seeding completed!"
}

main "$@"