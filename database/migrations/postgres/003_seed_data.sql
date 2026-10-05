-- ============================================================================
-- Migration: 003_seed_data.sql
-- Purpose: Seed development/test data
-- Date: 2026-07-26
-- Note: Only run in development/staging environments
-- ============================================================================

-- ============================================================================
-- SAMPLE USERS
-- ============================================================================

INSERT INTO users (id, email, phone, full_name, password_hash, role, status, email_verified, country_code) VALUES
-- Test rider accounts
('550e8400-e29b-41d4-a716-446655440001', 'rider1@nidaw.com', '+12345678901', 'John Rider', '$2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u', 'rider', 'active', true, 'US'),
('550e8400-e29b-41d4-a716-446655440002', 'rider2@nidaw.com', '+12345678902', 'Jane Smith', '$2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u', 'rider', 'active', true, 'US'),
('550e8400-e29b-41d4-a716-446655440003', 'rider3@nidaw.com', '+12345678903', 'Bob Johnson', '$2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u', 'rider', 'active', true, 'US'),

-- Test driver accounts
('550e8400-e29b-41d4-a716-446655440011', 'driver1@nidaw.com', '+12345678911', 'Mike Driver', '$2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u', 'driver', 'active', true, 'US'),
('550e8400-e29b-41d4-a716-446655440012', 'driver2@nidaw.com', '+12345678912', 'Sarah Driver', '$2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u', 'driver', 'active', true, 'US'),
('550e8400-e29b-41d4-a716-446655440013', 'driver3@nidaw.com', '+12345678913', 'Tom Driver', '$2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u', 'driver', 'active', true, 'US'),

-- Admin account
('550e8400-e29b-41d4-a716-446655440099', 'admin@nidaw.com', '+12345678999', 'Admin User', '$2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u', 'admin', 'active', true, 'US')

ON CONFLICT (email) DO NOTHING;

-- ============================================================================
-- SAMPLE DRIVERS
-- ============================================================================

INSERT INTO drivers (id, user_id, rating, acceptance_rate, completion_rate, total_rides, total_earnings, status, current_lat, current_lng, vehicle_type, vehicle_model, vehicle_plate, vehicle_color, vehicle_year, license_verified, background_check, vehicle_inspected) VALUES
-- New York drivers
('660e8400-e29b-41d4-a716-446655440001', '550e8400-e29b-41d4-a716-446655440011', 4.9, 95.5, 98.2, 1250, 45678.50, 'available', 40.7128, -74.0060, 'sedan', 'Toyota Camry', 'ABC123', 'Black', 2022, true, true, true),
('660e8400-e29b-41d4-a716-446655440002', '550e8400-e29b-41d4-a716-446655440012', 4.8, 92.3, 97.5, 980, 38450.25, 'available', 40.7589, -73.9851, 'suv', 'Honda CR-V', 'DEF456', 'White', 2023, true, true, true),
('660e8400-e29b-41d4-a716-446655440003', '550e8400-e29b-41d4-a716-446655440013', 4.7, 88.9, 96.8, 756, 29876.75, 'available', 40.7484, -73.9857, 'electric', 'Tesla Model 3', 'GHI789', 'Red', 2024, true, true, true),

-- Los Angeles drivers
('660e8400-e29b-41d4-a716-446655440004', '550e8400-e29b-41d4-a716-446655440011', 4.9, 96.2, 98.5, 1450, 52340.00, 'available', 34.0522, -118.2437, 'sedan', 'Honda Accord', 'JKL012', 'Silver', 2022, true, true, true),
('660e8400-e29b-41d4-a716-446655440005', '550e8400-e29b-41d4-a716-446655440012', 4.8, 93.7, 97.9, 1120, 41230.50, 'available', 34.0195, -118.4912, 'luxury', 'BMW 5 Series', 'MNO345', 'Black', 2023, true, true, true),

-- Chicago drivers
('660e8400-e29b-41d4-a716-446655440006', '550e8400-e29b-41d4-a716-446655440013', 4.7, 90.5, 97.2, 890, 33450.25, 'available', 41.8781, -87.6298, 'suv', 'Ford Explorer', 'PQR678', 'Blue', 2022, true, true, true),

-- Offline drivers
('660e8400-e29b-41d4-a716-446655440007', '550e8400-e29b-41d4-a716-446655440011', 4.6, 85.3, 95.8, 650, 24560.75, 'offline', 40.7128, -74.0060, 'sedan', 'Nissan Altima', 'STU901', 'Gray', 2021, true, true, true)

ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- SAMPLE HOTELS
-- ============================================================================

INSERT INTO hotels (id, name, description, country_code, city, address, latitude, longitude, star_rating, price_per_night, currency, amenities, images, status) VALUES
-- New York hotels
('770e8400-e29b-41d4-a716-446655440001', 'Grand Plaza Hotel', 'Luxury hotel in the heart of Manhattan with stunning city views', 'US', 'New York', '123 Broadway, New York, NY 10001', 40.7589, -73.9851, 5, 299.99, 'USD', '["WiFi", "Pool", "Gym", "Spa", "Restaurant", "Bar", "Concierge"]', '["https://images.unsplash.com/photo-1566073771259-6a8506099945", "https://images.unsplash.com/photo-1582719508461-905c673771fd"]', 'active'),
('770e8400-e29b-41d4-a716-446655440002', 'City Inn', 'Budget-friendly accommodation with modern amenities', 'US', 'New York', '456 5th Ave, New York, NY 10018', 40.7549, -73.9840, 3, 129.99, 'USD', '["WiFi", "Breakfast", "Parking"]', '["https://images.unsplash.com/photo-1551882547-ff40c63fe5fa"]', 'active'),
('770e8400-e29b-41d4-a716-446655440003', 'Business Suites', 'Perfect for business travelers with meeting rooms', 'US', 'New York', '789 Park Ave, New York, NY 10022', 40.7624, -73.9712, 4, 199.99, 'USD', '["WiFi", "Business Center", "Gym", "Restaurant"]', '["https://images.unsplash.com/photo-1564501049412-61c2a3083791"]', 'active'),

-- Los Angeles hotels
('770e8400-e29b-41d4-a716-446655440004', 'Beach Resort', 'Oceanfront resort with private beach access', 'US', 'Los Angeles', '100 Ocean Blvd, Los Angeles, CA 90045', 33.9425, -118.4081, 5, 399.99, 'USD', '["WiFi", "Pool", "Beach", "Spa", "Restaurant", "Bar"]', '["https://images.unsplash.com/photo-1520250497591-112f2f40a3f4"]', 'active'),
('770e8400-e29b-41d4-a716-446655440005', 'Downtown Hotel', 'Modern hotel in downtown LA', 'US', 'Los Angeles', '200 Figueroa St, Los Angeles, CA 90012', 34.0522, -118.2437, 4, 179.99, 'USD', '["WiFi", "Pool", "Gym", "Restaurant"]', '["https://images.unsplash.com/photo-1542314831-068cd1dbfeeb"]', 'active'),

-- Chicago hotels
('770e8400-e29b-41d4-a716-446655440006', 'Lake View Hotel', 'Beautiful views of Lake Michigan', 'US', 'Chicago', '300 Lake Shore Dr, Chicago, IL 60611', 41.8781, -87.6298, 4, 219.99, 'USD', '["WiFi", "Pool", "Gym", "Restaurant", "Bar"]', '["https://images.unsplash.com/photo-1445019980597-93fa8acb246c"]', 'active'),

-- Inactive hotel
('770e8400-e29b-41d4-a716-446655440099', 'Closed Hotel', 'This hotel is temporarily closed', 'US', 'New York', '999 Test St, New York, NY 10001', 40.7128, -74.0060, 2, 99.99, 'USD', '["WiFi"]', '[]', 'inactive')

ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- SAMPLE RESTAURANTS
-- ============================================================================

INSERT INTO restaurants (id, name, description, cuisine_type, country_code, city, address, latitude, longitude, rating, delivery_fee, min_order_amount, estimated_delivery_minutes, images, status) VALUES
-- New York restaurants
('880e8400-e29b-41d4-a716-446655440001', 'Pizza Palace', 'Authentic Italian pizza with fresh ingredients', 'Italian', 'US', 'New York', '789 Pizza St, New York, NY 10001', 40.7282, -73.7949, 4.7, 3.99, 15.00, 25, '["https://images.unsplash.com/photo-1604068549290-dea0e4a305ca"]', 'active'),
('880e8400-e29b-41d4-a716-446655440002', 'Sushi Master', 'Premium Japanese sushi and sashimi', 'Japanese', 'US', 'New York', '321 Sushi Ave, New York, NY 10019', 40.7648, -73.9808, 4.8, 4.99, 20.00, 30, '["https://images.unsplash.com/photo-1579871494447-9811cf80d66c"]', 'active'),
('880e8400-e29b-41d4-a716-446655440003', 'Burger Joint', 'Gourmet burgers and fries', 'American', 'US', 'New York', '555 Burger Blvd, New York, NY 10003', 40.7308, -73.9973, 4.6, 2.99, 12.00, 20, '["https://images.unsplash.com/photo-1568901346375-23c9450c58cd"]', 'active'),
('880e8400-e29b-41d4-a716-446655440004', 'Taco Town', 'Authentic Mexican tacos and burritos', 'Mexican', 'US', 'New York', '777 Taco St, New York, NY 10013', 40.7193, -74.0010, 4.5, 3.49, 10.00, 25, '["https://images.unsplash.com/photo-1565299585323-38d6b0865b47"]', 'active'),

-- Los Angeles restaurants
('880e8400-e29b-41d4-a716-446655440005', 'Health Bowl', 'Fresh and healthy bowls and salads', 'Healthy', 'US', 'Los Angeles', '888 Health Ave, Los Angeles, CA 90024', 34.0668, -118.4486, 4.7, 4.49, 18.00, 30, '["https://images.unsplash.com/photo-1512621776951-a57141f2eefd"]', 'active'),
('880e8400-e29b-41d4-a716-446655440006', 'BBQ Pit', 'Smoked meats and classic BBQ sides', 'BBQ', 'US', 'Los Angeles', '999 BBQ Blvd, Los Angeles, CA 90015', 34.0407, -118.2468, 4.6, 5.99, 25.00, 35, '["https://images.unsplash.com/photo-1529193591184-b1d58069ecdd"]', 'active'),

-- Inactive restaurant
('880e8400-e29b-41d4-a716-446655440099', 'Closed Restaurant', 'Temporarily closed', 'American', 'US', 'New York', '111 Test St, New York, NY 10001', 40.7128, -74.0060, 0.0, 0.00, 0.00, 0, '[]', 'inactive')

ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- SAMPLE MENU ITEMS
-- ============================================================================

INSERT INTO menu_items (id, restaurant_id, name, description, price, currency, category, image_url, is_available) VALUES
-- Pizza Palace items
('990e8400-e29b-41d4-a716-446655440001', '880e8400-e29b-41d4-a716-446655440001', 'Margherita Pizza', 'Fresh tomatoes, mozzarella, basil', 14.99, 'USD', 'Pizza', 'https://images.unsplash.com/photo-1604068549290-dea0e4a305ca', true),
('990e8400-e29b-41d4-a716-446655440002', '880e8400-e29b-41d4-a716-446655440001', 'Pepperoni Pizza', 'Classic pepperoni with cheese', 16.99, 'USD', 'Pizza', 'https://images.unsplash.com/photo-1628840042765-356cda07504e', true),
('990e8400-e29b-41d4-a716-446655440003', '880e8400-e29b-41d4-a716-446655440001', 'Caesar Salad', 'Romaine lettuce, croutons, parmesan', 9.99, 'USD', 'Salads', 'https://images.unsplash.com/photo-1546793665-c74683f339c1', true),
('990e8400-e29b-41d4-a716-446655440004', '880e8400-e29b-41d4-a716-446655440001', 'Tiramisu', 'Classic Italian dessert', 7.99, 'USD', 'Desserts', 'https://images.unsplash.com/photo-1571877227200-a0d98ea607e9', true),

-- Sushi Master items
('990e8400-e29b-41d4-a716-446655440005', '880e8400-e29b-41d4-a716-446655440002', 'Salmon Nigiri', 'Fresh salmon on rice (2 pcs)', 8.99, 'USD', 'Nigiri', 'https://images.unsplash.com/photo-1579871494447-9811cf80d66c', true),
('990e8400-e29b-41d4-a716-446655440006', '880e8400-e29b-41d4-a716-446655440002', 'Tuna Sashimi', 'Fresh tuna slices (5 pcs)', 12.99, 'USD', 'Sashimi', 'https://images.unsplash.com/photo-1553621042-f6e147245754', true),
('990e8400-e29b-41d4-a716-446655440007', '880e8400-e29b-41d4-a716-446655440002', 'Dragon Roll', 'Eel and avocado roll', 14.99, 'USD', 'Rolls', 'https://images.unsplash.com/photo-1617196034796-73dfa7b1fd56', true),
('990e8400-e29b-41d4-a716-446655440008', '880e8400-e29b-41d4-a716-446655440002', 'Miso Soup', 'Traditional Japanese soup', 4.99, 'USD', 'Soups', 'https://images.unsplash.com/photo-1607301405390-d831c242f59b', true),

-- Burger Joint items
('990e8400-e29b-41d4-a716-446655440009', '880e8400-e29b-41d4-a716-446655440003', 'Classic Burger', 'Beef patty, lettuce, tomato, onion', 11.99, 'USD', 'Burgers', 'https://images.unsplash.com/photo-1568901346375-23c9450c58cd', true),
('990e8400-e29b-41d4-a716-446655440010', '880e8400-e29b-41d4-a716-446655440003', 'Cheese Burger', 'Beef patty with cheese', 12.99, 'USD', 'Burgers', 'https://images.unsplash.com/photo-1553979459-d2229ba7433b', true),
('990e8400-e29b-41d4-a716-446655440011', '880e8400-e29b-41d4-a716-446655440003', 'French Fries', 'Crispy golden fries', 4.99, 'USD', 'Sides', 'https://images.unsplash.com/photo-1573080496219-bb080dd4f877', true),
('990e8400-e29b-41d4-a716-446655440012', '880e8400-e29b-41d4-a716-446655440003', 'Milkshake', 'Vanilla, chocolate, or strawberry', 5.99, 'USD', 'Drinks', 'https://images.unsplash.com/photo-1572490122747-3968b75cc699', true)

ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- SAMPLE RIDES
-- ============================================================================

INSERT INTO rides (id, user_id, driver_id, pickup_lat, pickup_lng, dropoff_lat, dropoff_lng, pickup_address, dropoff_address, ride_type, status, fare_amount, currency, distance_km, duration_minutes, requested_at, matched_at, started_at, completed_at, rating, review) VALUES
-- Completed rides
('aa0e8400-e29b-41d4-a716-446655440001', '550e8400-e29b-41d4-a716-446655440001', '660e8400-e29b-41d4-a716-446655440001', 40.7128, -74.0060, 40.7589, -73.9851, '123 Broadway, NYC', '456 5th Ave, NYC', 'standard', 'completed', 15.50, 'USD', 5.2, 18, NOW() - INTERVAL '1 day', NOW() - INTERVAL '1 day' + INTERVAL '3 minutes', NOW() - INTERVAL '1 day' + INTERVAL '8 minutes', NOW() - INTERVAL '1 day' + INTERVAL '26 minutes', 5, 'Great ride!'),
('aa0e8400-e29b-41d4-a716-446655440002', '550e8400-e29b-41d4-a716-446655440002', '660e8400-e29b-41d4-a716-446655440002', 40.7484, -73.9857, 40.7614, -73.9776, 'Times Square, NYC', 'Central Park, NYC', 'premium', 'completed', 22.75, 'USD', 3.8, 15, NOW() - INTERVAL '2 days', NOW() - INTERVAL '2 days' + INTERVAL '2 minutes', NOW() - INTERVAL '2 days' + INTERVAL '7 minutes', NOW() - INTERVAL '2 days' + INTERVAL '22 minutes', 5, 'Excellent service'),
('aa0e8400-e29b-41d4-a716-446655440003', '550e8400-e29b-41d4-a716-446655440003', '660e8400-e29b-41d4-a716-446655440003', 40.7505, -73.9934, 40.7282, -73.7949, 'Penn Station, NYC', 'JFK Airport, NYC', 'electric', 'completed', 45.25, 'USD', 25.6, 45, NOW() - INTERVAL '3 days', NOW() - INTERVAL '3 days' + INTERVAL '5 minutes', NOW() - INTERVAL '3 days' + INTERVAL '12 minutes', NOW() - INTERVAL '3 days' + INTERVAL '57 minutes', 4, 'Good ride, car was clean'),

-- Active rides
('aa0e8400-e29b-41d4-a716-446655440004', '550e8400-e29b-41d4-a716-446655440001', '660e8400-e29b-41d4-a716-446655440001', 40.7128, -74.0060, 40.7484, -73.9857, 'Wall Street, NYC', 'Empire State Building, NYC', 'standard', 'in_progress', 18.90, 'USD', 6.8, 22, NOW() - INTERVAL '10 minutes', NOW() - INTERVAL '8 minutes', NOW() - INTERVAL '5 minutes', NULL, NULL, NULL),

-- Pending rides
('aa0e8400-e29b-41d4-a716-446655440005', '550e8400-e29b-41d4-a716-446655440002', NULL, 40.7589, -73.9851, 40.7614, -73.9776, 'Times Square, NYC', 'Madison Square Garden, NYC', 'standard', 'requested', 12.50, 'USD', 2.1, 8, NOW() - INTERVAL '2 minutes', NULL, NULL, NULL, NULL, NULL),

-- Cancelled rides
('aa0e8400-e29b-41d4-a716-446655440006', '550e8400-e29b-41d4-a716-446655440003', NULL, 40.7484, -73.9857, 40.7505, -73.9934, 'Midtown, NYC', 'Penn Station, NYC', 'standard', 'cancelled', 0.00, 'USD', 1.5, 5, NOW() - INTERVAL '5 days', NULL, NULL, NULL, NULL, NULL)

ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- SAMPLE BOOKINGS
-- ============================================================================

INSERT INTO bookings (id, user_id, hotel_id, check_in, check_out, guests, rooms, total_amount, currency, status) VALUES
-- Upcoming bookings
('bb0e8400-e29b-41d4-a716-446655440001', '550e8400-e29b-41d4-a716-446655440001', '770e8400-e29b-41d4-a716-446655440001', CURRENT_DATE + INTERVAL '7 days', CURRENT_DATE + INTERVAL '10 days', 2, 1, 899.97, 'USD', 'confirmed'),
('bb0e8400-e29b-41d4-a716-446655440002', '550e8400-e29b-41d4-a716-446655440002', '770e8400-e29b-41d4-a716-446655440004', CURRENT_DATE + INTERVAL '14 days', CURRENT_DATE + INTERVAL '18 days', 2, 1, 1599.96, 'USD', 'pending'),

-- Past bookings
('bb0e8400-e29b-41d4-a716-446655440003', '550e8400-e29b-41d4-a716-446655440003', '770e8400-e29b-41d4-a716-446655440002', CURRENT_DATE - INTERVAL '30 days', CURRENT_DATE - INTERVAL '27 days', 1, 1, 389.97, 'USD', 'checked_out'),
('bb0e8400-e29b-41d4-a716-446655440004', '550e8400-e29b-41d4-a716-446655440001', '770e8400-e29b-41d4-a716-446655440003', CURRENT_DATE - INTERVAL '60 days', CURRENT_DATE - INTERVAL '57 days', 1, 1, 599.97, 'USD', 'checked_out')

ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- SAMPLE ORDERS
-- ============================================================================

INSERT INTO orders (id, user_id, restaurant_id, driver_id, delivery_address, delivery_lat, delivery_lng, subtotal, delivery_fee, total_amount, currency, status, estimated_delivery_minutes) VALUES
-- Active orders
('cc0e8400-e29b-41d4-a716-446655440001', '550e8400-e29b-41d4-a716-446655440001', '880e8400-e29b-41d4-a716-446655440001', '660e8400-e29b-41d4-a716-446655440001', '123 Broadway, Apt 4B, NYC', 40.7128, -74.0060, 31.98, 3.99, 35.97, 'USD', 'in_transit', 15),
('cc0e8400-e29b-41d4-a716-446655440002', '550e8400-e29b-41d4-a716-446655440002', '880e8400-e29b-41d4-a716-446655440002', NULL, '456 5th Ave, NYC', 40.7549, -73.9840, 26.97, 4.99, 31.96, 'USD', 'preparing', 25),

-- Completed orders
('cc0e8400-e29b-41d4-a716-446655440003', '550e8400-e29b-41d4-a716-446655440003', '880e8400-e29b-41d4-a716-446655440003', '660e8400-e29b-41d4-a716-446655440002', '789 Park Ave, NYC', 40.7624, -73.9712, 24.97, 2.99, 27.96, 'USD', 'delivered', 0),
('cc0e8400-e29b-41d4-a716-446655440004', '550e8400-e29b-41d4-a716-446655440001', '880e8400-e29b-41d4-a716-446655440004', '660e8400-e29b-41d4-a716-446655440003', '321 Test St, NYC', 40.7193, -74.0010, 18.98, 3.49, 22.47, 'USD', 'delivered', 0)

ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- SAMPLE ORDER ITEMS
-- ============================================================================

INSERT INTO order_items (id, order_id, menu_item_id, quantity, unit_price, total_price, notes) VALUES
-- Order 1 items
('dd0e8400-e29b-41d4-a716-446655440001', 'cc0e8400-e29b-41d4-a716-446655440001', '990e8400-e29b-41d4-a716-446655440001', 1, 14.99, 14.99, 'Extra cheese'),
('dd0e8400-e29b-41d4-a716-446655440002', 'cc0e8400-e29b-41d4-a716-446655440001', '990e8400-e29b-41d4-a716-446655440003', 1, 9.99, 9.99, NULL),
('dd0e8400-e29b-41d4-a716-446655440003', 'cc0e8400-e29b-41d4-a716-446655440001', '990e8400-e29b-41d4-a716-446655440004', 1, 7.99, 7.99, NULL),

-- Order 2 items
('dd0e8400-e29b-41d4-a716-446655440004', 'cc0e8400-e29b-41d4-a716-446655440002', '990e8400-e29b-41d4-a716-446655440005', 2, 8.99, 17.98, 'No wasabi'),
('dd0e8400-e29b-41d4-a716-446655440005', 'cc0e8400-e29b-41d4-a716-446655440002', '990e8400-e29b-41d4-a716-446655440007', 1, 14.99, 14.99, NULL),

-- Order 3 items
('dd0e8400-e29b-41d4-a716-446655440006', 'cc0e8400-e29b-41d4-a716-446655440003', '990e8400-e29b-41d4-a716-446655440009', 2, 11.99, 23.98, 'No onions'),
('dd0e8400-e29b-41d4-a716-446655440007', 'cc0e8400-e29b-41d4-a716-446655440003', '990e8400-e29b-41d4-a716-446655440011', 1, 4.99, 4.99, NULL)

ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- SAMPLE PAYMENTS
-- ============================================================================

INSERT INTO payments (id, user_id, amount, currency, status, payment_method, order_type, order_id) VALUES
('ee0e8400-e29b-41d4-a716-446655440001', '550e8400-e29b-41d4-a716-446655440001', 15.50, 'USD', 'succeeded', 'card', 'ride', 'aa0e8400-e29b-41d4-a716-446655440001'),
('ee0e8400-e29b-41d4-a716-446655440002', '550e8400-e29b-41d4-a716-446655440002', 22.75, 'USD', 'succeeded', 'card', 'ride', 'aa0e8400-e29b-41d4-a716-446655440002'),
('ee0e8400-e29b-41d4-a716-446655440003', '550e8400-e29b-41d4-a716-446655440003', 45.25, 'USD', 'succeeded', 'card', 'ride', 'aa0e8400-e29b-41d4-a716-446655440003'),
('ee0e8400-e29b-41d4-a716-446655440004', '550e8400-e29b-41d4-a716-446655440001', 899.97, 'USD', 'succeeded', 'card', 'hotel', 'bb0e8400-e29b-41d4-a716-446655440001'),
('ee0e8400-e29b-41d4-a716-446655440005', '550e8400-e29b-41d4-a716-446655440001', 35.97, 'USD', 'succeeded', 'card', 'food', 'cc0e8400-e29b-41d4-a716-446655440001')

ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- SAMPLE LEGAL DOCUMENTS
-- ============================================================================

INSERT INTO legal_documents (id, document_type, version, title, content, language, region, effective_date, is_active, requires_reconsent) VALUES
('ff0e8400-e29b-41d4-a716-446655440001', 'terms_of_service', '1.0.0', 'NIDAW Rider Terms of Service', 'Full terms of service content here...', 'en', 'GLOBAL', NOW(), true, false),
('ff0e8400-e29b-41d4-a716-446655440002', 'privacy_policy', '1.0.0', 'NIDAW Privacy Policy', 'Full privacy policy content here...', 'en', 'GLOBAL', NOW(), true, true),
('ff0e8400-e29b-41d4-a716-446655440003', 'cookie_policy', '1.0.0', 'NIDAW Cookie Policy', 'Full cookie policy content here...', 'en', 'GLOBAL', NOW(), true, false),
('ff0e8400-e29b-41d4-a716-446655440004', 'driver_terms', '1.0.0', 'NIDAW Driver Terms of Service', 'Full driver terms content here...', 'en', 'GLOBAL', NOW(), true, true)

ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- VERIFICATION
-- ============================================================================

-- Count records in each table
SELECT 'users' as table_name, COUNT(*) as record_count FROM users
UNION ALL
SELECT 'drivers', COUNT(*) FROM drivers
UNION ALL
SELECT 'hotels', COUNT(*) FROM hotels
UNION ALL
SELECT 'restaurants', COUNT(*) FROM restaurants
UNION ALL
SELECT 'menu_items', COUNT(*) FROM menu_items
UNION ALL
SELECT 'rides', COUNT(*) FROM rides
UNION ALL
SELECT 'bookings', COUNT(*) FROM bookings
UNION ALL
SELECT 'orders', COUNT(*) FROM orders
UNION ALL
SELECT 'order_items', COUNT(*) FROM order_items
UNION ALL
SELECT 'payments', COUNT(*) FROM payments
UNION ALL
SELECT 'legal_documents', COUNT(*) FROM legal_documents
ORDER BY table_name;

-- ============================================================================
-- NOTES
-- ============================================================================

-- All test passwords are: "TestPass123!"
-- Hash: $2b$12$LQv3c1yqBWVHxkd0LHAkCOYz6TtxMQJqhN8/LewY5GyYIeJU5I5u
--
-- This seed data is for development/testing only
-- Do NOT run in production environments