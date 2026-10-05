-- Create extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "postgis";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Create MLflow database
CREATE DATABASE mlflow;

-- Create tables
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    phone VARCHAR(20) UNIQUE NOT NULL,
    full_name VARCHAR(255) NOT NULL,
    country_code CHAR(2) NOT NULL DEFAULT 'US',
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL DEFAULT 'rider',
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS drivers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id),
    rating DECIMAL(3,2) DEFAULT 5.0,
    acceptance_rate DECIMAL(5,2) DEFAULT 100.0,
    total_rides INTEGER DEFAULT 0,
    status VARCHAR(50) NOT NULL DEFAULT 'offline',
    current_lat DECIMAL(10,8),
    current_lng DECIMAL(11,8),
    vehicle_type VARCHAR(50),
    vehicle_plate VARCHAR(20),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS rides (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id),
    driver_id UUID REFERENCES drivers(id),
    pickup_lat DECIMAL(10,8) NOT NULL,
    pickup_lng DECIMAL(11,8) NOT NULL,
    dropoff_lat DECIMAL(10,8) NOT NULL,
    dropoff_lng DECIMAL(11,8) NOT NULL,
    pickup_address TEXT,
    dropoff_address TEXT,
    ride_type VARCHAR(50) NOT NULL DEFAULT 'standard',
    status VARCHAR(50) NOT NULL DEFAULT 'requested',
    fare_amount DECIMAL(10,2),
    currency CHAR(3) DEFAULT 'USD',
    distance_km DECIMAL(8,2),
    duration_minutes INTEGER,
    requested_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    matched_at TIMESTAMP WITH TIME ZONE,
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS payments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id),
    ride_id UUID REFERENCES rides(id),
    amount DECIMAL(10,2) NOT NULL,
    currency CHAR(3) DEFAULT 'USD',
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    payment_method VARCHAR(50),
    stripe_payment_intent_id VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS hotels (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    country_code CHAR(2) NOT NULL,
    city VARCHAR(100) NOT NULL,
    address TEXT,
    latitude DECIMAL(10,8),
    longitude DECIMAL(11,8),
    star_rating INTEGER CHECK (star_rating BETWEEN 1 AND 5),
    price_per_night DECIMAL(10,2),
    currency CHAR(3) DEFAULT 'USD',
    amenities JSONB DEFAULT '[]',
    images JSONB DEFAULT '[]',
    status VARCHAR(50) DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS bookings (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id),
    hotel_id UUID NOT NULL REFERENCES hotels(id),
    check_in DATE NOT NULL,
    check_out DATE NOT NULL,
    guests INTEGER NOT NULL DEFAULT 1,
    rooms INTEGER NOT NULL DEFAULT 1,
    total_amount DECIMAL(10,2) NOT NULL,
    currency CHAR(3) DEFAULT 'USD',
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS restaurants (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    cuisine_type VARCHAR(100),
    country_code CHAR(2) NOT NULL,
    city VARCHAR(100) NOT NULL,
    address TEXT,
    latitude DECIMAL(10,8),
    longitude DECIMAL(11,8),
    rating DECIMAL(3,2) DEFAULT 0.0,
    delivery_fee DECIMAL(10,2) DEFAULT 0.0,
    min_order_amount DECIMAL(10,2) DEFAULT 0.0,
    estimated_delivery_minutes INTEGER DEFAULT 30,
    images JSONB DEFAULT '[]',
    status VARCHAR(50) DEFAULT 'active',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS menu_items (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    restaurant_id UUID NOT NULL REFERENCES restaurants(id),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    price DECIMAL(10,2) NOT NULL,
    currency CHAR(3) DEFAULT 'USD',
    category VARCHAR(100),
    image_url TEXT,
    is_available BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS orders (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id),
    restaurant_id UUID NOT NULL REFERENCES restaurants(id),
    driver_id UUID REFERENCES drivers(id),
    delivery_address TEXT NOT NULL,
    delivery_lat DECIMAL(10,8),
    delivery_lng DECIMAL(11,8),
    subtotal DECIMAL(10,2) NOT NULL,
    delivery_fee DECIMAL(10,2) NOT NULL,
    total_amount DECIMAL(10,2) NOT NULL,
    currency CHAR(3) DEFAULT 'USD',
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    estimated_delivery_minutes INTEGER,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS order_items (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id UUID NOT NULL REFERENCES orders(id),
    menu_item_id UUID NOT NULL REFERENCES menu_items(id),
    quantity INTEGER NOT NULL DEFAULT 1,
    unit_price DECIMAL(10,2) NOT NULL,
    total_price DECIMAL(10,2) NOT NULL,
    notes TEXT
);

-- Indexes
CREATE INDEX idx_rides_user_id ON rides(user_id);
CREATE INDEX idx_rides_driver_id ON rides(driver_id);
CREATE INDEX idx_rides_status ON rides(status);
CREATE INDEX idx_rides_requested_at ON rides(requested_at);
CREATE INDEX idx_drivers_status ON drivers(status);
CREATE INDEX idx_drivers_location ON drivers USING GIST (
    ST_SetSRID(ST_MakePoint(current_lng, current_lat), 4326)
);
CREATE INDEX idx_hotels_city ON hotels(city);
CREATE INDEX idx_hotels_location ON hotels USING GIST (
    ST_SetSRID(ST_MakePoint(longitude, latitude), 4326)
);
CREATE INDEX idx_bookings_user_id ON bookings(user_id);
CREATE INDEX idx_restaurants_city ON restaurants(city);
CREATE INDEX idx_orders_user_id ON orders(user_id);
CREATE INDEX idx_orders_status ON orders(status);

-- Insert sample data
INSERT INTO users (id, email, phone, full_name, country_code, password_hash, role) VALUES
('550e8400-e29b-41d4-a716-446655440000', 'rider@nidaw.com', '+1234567890', 'John Rider', 'US', '$2b$12$sample_hash', 'rider'),
('550e8400-e29b-41d4-a716-446655440001', 'driver@nidaw.com', '+1234567891', 'Mike Driver', 'US', '$2b$12$sample_hash', 'driver');

INSERT INTO drivers (id, user_id, rating, acceptance_rate, total_rides, status, current_lat, current_lng, vehicle_type, vehicle_plate) VALUES
('660e8400-e29b-41d4-a716-446655440000', '550e8400-e29b-41d4-a716-446655440001', 4.9, 95.5, 1250, 'available', 40.7128, -74.0060, 'sedan', 'ABC123');

INSERT INTO hotels (id, name, description, country_code, city, address, latitude, longitude, star_rating, price_per_night, amenities, images) VALUES
('770e8400-e29b-41d4-a716-446655440000', 'Grand Plaza Hotel', 'Luxury hotel in downtown', 'US', 'New York', '123 Broadway', 40.7589, -73.9851, 5, 299.99, '["wifi", "pool", "gym", "spa"]', '["https://example.com/hotel1.jpg"]'),
('770e8400-e29b-41d4-a716-446655440001', 'City Inn', 'Budget-friendly accommodation', 'US', 'New York', '456 5th Ave', 40.7505, -73.9934, 3, 129.99, '["wifi", "breakfast"]', '["https://example.com/hotel2.jpg"]');

INSERT INTO restaurants (id, name, description, cuisine_type, country_code, city, address, latitude, longitude, rating, delivery_fee, min_order_amount, estimated_delivery_minutes) VALUES
('880e8400-e29b-41d4-a716-446655440000', 'Pizza Palace', 'Best pizza in town', 'Italian', 'US', 'New York', '789 Pizza St', 40.7282, -73.7949, 4.7, 3.99, 15.00, 25),
('880e8400-e29b-41d4-a716-446655440001', 'Sushi Master', 'Authentic Japanese cuisine', 'Japanese', 'US', 'New York', '321 Sushi Ave', 40.7484, -73.9857, 4.8, 4.99, 20.00, 30);

INSERT INTO menu_items (restaurant_id, name, description, price, category) VALUES
('880e8400-e29b-41d4-a716-446655440000', 'Margherita Pizza', 'Classic tomato and mozzarella', 14.99, 'Pizza'),
('880e8400-e29b-41d4-a716-446655440000', 'Pepperoni Pizza', 'Pepperoni and cheese', 16.99, 'Pizza'),
('880e8400-e29b-41d4-a716-446655440001', 'Salmon Nigiri', 'Fresh salmon on rice (2 pcs)', 8.99, 'Nigiri'),
('880e8400-e29b-41d4-a716-446655440001', 'Dragon Roll', 'Eel and avocado roll', 12.99, 'Rolls');