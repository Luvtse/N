
---

## 📄 File #17: `docs/api/api-examples.md`

```markdown
# API Examples

Common API usage patterns and code examples for the NIDAW platform.

## 🔐 Authentication

### Login

```bash
curl -X POST https://api.nidaw.com/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@nidaw.com",
    "password": "password123"
  }'

Response:

{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_in": 900,
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440001",
    "email": "user@nidaw.com",
    "full_name": "John Doe",
    "role": "rider"
  }
}

Using Access Token

curl -X GET https://api.nidaw.com/api/v1/nidus/rides \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."

Refresh Token

curl -X POST https://api.nidaw.com/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{
    "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
  }'

🚗 Rides (Nidus)
Request a Ride

curl -X POST https://api.nidaw.com/api/v1/nidus/rides \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "pickup_lat": 40.7128,
    "pickup_lng": -74.0060,
    "dropoff_lat": 40.7589,
    "dropoff_lng": -73.9851,
    "ride_type": "standard",
    "payment_method_id": "pm_123"
  }'

Response:

{
  "ride_id": "aa0e8400-e29b-41d4-a716-446655440001",
  "status": "requested",
  "fare": 15.50,
  "eta": 8
}

Get Ride Status

curl -X GET https://api.nidaw.com/api/v1/nidus/rides/aa0e8400-e29b-41d4-a716-446655440001 \
  -H "Authorization: Bearer $TOKEN"

Response:

{
  "id": "aa0e8400-e29b-41d4-a716-446655440001",
  "status": "in_progress",
  "driver": {
    "id": "660e8400-e29b-41d4-a716-446655440001",
    "name": "Mike Driver",
    "rating": 4.9,
    "vehicle_type": "sedan",
    "vehicle_plate": "ABC123",
    "current_lat": 40.7200,
    "current_lng": -74.0100,
    "eta_minutes": 5
  },
  "pickup_address": "123 Broadway, NYC",
  "dropoff_address": "456 5th Ave, NYC",
  "fare_amount": 15.50,
  "currency": "USD"
}

Cancel a Ride

curl -X POST https://api.nidaw.com/api/v1/nidus/rides/aa0e8400-e29b-41d4-a716-446655440001/cancel \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "reason": "Change of plans"
  }'

Get Fare Estimate

curl -X POST https://api.nidaw.com/api/v1/nidus/rides/estimate \
  -H "Content-Type: application/json" \
  -d '{
    "pickup_lat": 40.7128,
    "pickup_lng": -74.0060,
    "dropoff_lat": 40.7589,
    "dropoff_lng": -73.9851
  }'

Response:

{
  "estimates": [
    {
      "ride_type": "standard",
      "base_fare": 3.00,
      "distance_fare": 7.80,
      "time_fare": 5.40,
      "surge_multiplier": 1.0,
      "total_fare": 16.20,
      "currency": "USD",
      "distance_km": 5.2,
      "duration_minutes": 18
    },
    {
      "ride_type": "premium",
      "total_fare": 28.50,
      "currency": "USD"
    }
  ]
}

🏨 Hotels (Haven)
Search Hotels

curl -X GET "https://api.nidaw.com/api/v1/haven/hotels?city=New+York&check_in=2026-08-01&check_out=2026-08-05&guests=2" \
  -H "Authorization: Bearer $TOKEN"

Response:

{
  "hotels": [
    {
      "id": "770e8400-e29b-41d4-a716-446655440001",
      "name": "Grand Plaza Hotel",
      "city": "New York",
      "star_rating": 5,
      "price_per_night": 299.99,
      "currency": "USD",
      "rating": 4.8,
      "latitude": 40.7589,
      "longitude": -73.9851
    }
  ],
  "total": 15,
  "limit": 20,
  "offset": 0
}

Book a Hotel

curl -X POST https://api.nidaw.com/api/v1/haven/bookings \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "hotel_id": "770e8400-e29b-41d4-a716-446655440001",
    "check_in": "2026-08-01",
    "check_out": "2026-08-05",
    "guests": 2,
    "rooms": 1,
    "payment_method_id": "pm_123"
  }'

🍔 Food (Vorax)
Search Restaurants

curl -X GET "https://api.nidaw.com/api/v1/vorax/restaurants?city=New+York&cuisine=Italian" \
  -H "Authorization: Bearer $TOKEN"

Get Restaurant Menu

curl -X GET https://api.nidaw.com/api/v1/vorax/restaurants/880e8400-e29b-41d4-a716-446655440001/menu \
  -H "Authorization: Bearer $TOKEN"

Response:

{
  "categories": [
    {
      "name": "Pizza",
      "items": [
        {
          "id": "990e8400-e29b-41d4-a716-446655440001",
          "name": "Margherita Pizza",
          "description": "Fresh tomatoes, mozzarella, basil",
          "price": 14.99,
          "currency": "USD",
          "is_available": true
        }
      ]
    }
  ]
}

Place an Order

curl -X POST https://api.nidaw.com/api/v1/vorax/orders \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "restaurant_id": "880e8400-e29b-41d4-a716-446655440001",
    "items": [
      {
        "menu_item_id": "990e8400-e29b-41d4-a716-446655440001",
        "quantity": 1,
        "notes": "Extra cheese"
      }
    ],
    "delivery_address": "123 Broadway, Apt 4B, NYC",
    "delivery_lat": 40.7128,
    "delivery_lng": -74.0060,
    "payment_method_id": "pm_123"
  }'

Track Order

curl -X GET https://api.nidaw.com/api/v1/vorax/orders/cc0e8400-e29b-41d4-a716-446655440001/tracking \
  -H "Authorization: Bearer $TOKEN"

Response:

{
  "order_id": "cc0e8400-e29b-41d4-a716-446655440001",
  "status": "in_transit",
  "estimated_delivery_time": "2026-07-27T11:15:00Z",
  "driver_location": {
    "latitude": 40.7150,
    "longitude": -74.0080,
    "heading": 45
  },
  "stages": [
    {"name": "Order Placed", "status": "completed", "timestamp": "2026-07-27T11:00:00Z"},
    {"name": "Preparing", "status": "completed", "timestamp": "2026-07-27T11:05:00Z"},
    {"name": "Ready for Pickup", "status": "completed", "timestamp": "2026-07-27T11:10:00Z"},
    {"name": "In Transit", "status": "in_progress", "timestamp": "2026-07-27T11:12:00Z"},
    {"name": "Delivered", "status": "pending"}
  ]
}

📱 WebSocket (Real-Time Updates)
Connect to WebSocket

const ws = new WebSocket('wss://ws.nidaw.com/ws?token=YOUR_JWT_TOKEN');

ws.onopen = () => {
  console.log('Connected to WebSocket');
  
  // Subscribe to ride updates
  ws.send(JSON.stringify({
    type: 'subscribe',
    data: { topic: 'ride:aa0e8400-e29b-41d4-a716-446655440001' }
  }));
};

ws.onmessage = (event) => {
  const message = JSON.parse(event.data);
  console.log('Received:', message);
  
  if (message.type === 'ride.status_changed') {
    updateRideStatus(message.payload);
  }
  
  if (message.type === 'driver.location_updated') {
    updateDriverLocation(message.payload);
  }
};

ws.onerror = (error) => {
  console.error('WebSocket error:', error);
};

ws.onclose = () => {
  console.log('WebSocket closed');
};

🐹 Go SDK Example

package main

import (
    "context"
    "fmt"
    "log"
    
    "github.com/nidaw/nidaw-sdk-go"
)

func main() {
    // Initialize client
    client := nidaw.NewClient("YOUR_API_KEY")
    
    // Request a ride
    ride, err := client.Rides.Create(context.Background(), &nidaw.CreateRideRequest{
        PickupLat:  40.7128,
        PickupLng:  -74.0060,
        DropoffLat: 40.7589,
        DropoffLng: -73.9851,
        RideType:   "standard",
    })
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Ride created: %s\n", ride.ID)
    
    // Get ride status
    ride, err = client.Rides.Get(context.Background(), ride.ID)
    if err != nil {
        log.Fatal(err)
    }
    
    fmt.Printf("Ride status: %s\n", ride.Status)
}

📱 Flutter SDK Example

import 'package:nidaw_sdk/nidaw_sdk.dart';

void main() async {
  // Initialize SDK
  final nidaw = NidawSDK(apiKey: 'YOUR_API_KEY');
  
  // Login
  final authResult = await nidaw.auth.login(
    email: 'user@nidaw.com',
    password: 'password123',
  );
  
  print('Access token: ${authResult.accessToken}');
  
  // Request a ride
  final ride = await nidaw.rides.create(
    pickupLat: 40.7128,
    pickupLng: -74.0060,
    dropoffLat: 40.7589,
    dropoffLng: -73.9851,
    rideType: 'standard',
  );
  
  print('Ride created: ${ride.id}');
  
  // Subscribe to ride updates
  nidaw.rides.subscribe(ride.id).listen((update) {
    print('Ride update: ${update.status}');
  });
}

📚 Additional Resources
OpenAPI Specification
Postman Collection
API Documentation
Last Updated: July 2026
Owner: API Team
Review Cadence: Monthly