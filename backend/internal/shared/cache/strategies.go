package cache

import (
	"context"
	"fmt"
	"time"
)

// Cache key patterns
const (
	// User-related
	UserProfileKey     = "user:profile:%s"
	UserSessionKey     = "user:session:%s"
	
	// Driver-related
	DriverLocationKey  = "driver:location:%s"
	DriverProfileKey   = "driver:profile:%s"
	DriverEarningsKey  = "driver:earnings:%s:%s"  // driver_id:date
	
	// Ride-related
	RideKey            = "ride:%s"
	RideStatusKey      = "ride:status:%s"
	ActiveRidesByUser  = "user:active_rides:%s"
	
	// Hotel-related
	HotelKey           = "hotel:%s"
	HotelSearchKey     = "hotel:search:%s"  // hash of search params
	HotelAvailability  = "hotel:avail:%s:%s"  // hotel_id:date
	
	// Food-related
	RestaurantKey      = "restaurant:%s"
	MenuKey            = "menu:%s"
	OrderKey           = "order:%s"
	
	// Rate limiting
	RateLimitKey       = "ratelimit:%s:%s"  // user_id:endpoint
	
	// Geospatial
	DriversGeoKey      = "drivers:geo:%s"  // status
	NearbyDriversKey   = "nearby:%f:%f"    // lat:lng
)

// TTLs for different data types
var (
	// Short-lived (real-time data)
	DriverLocationTTL = 60 * time.Second
	RideStatusTTL     = 5 * time.Minute
	ActiveRideTTL     = 10 * time.Minute
	
	// Medium-lived (session data)
	UserSessionTTL    = 24 * time.Hour
	RateLimitTTL      = 1 * time.Minute
	
	// Long-lived (reference data)
	HotelTTL          = 1 * time.Hour
	RestaurantTTL     = 30 * time.Minute
	MenuTTL           = 15 * time.Minute
	UserProfileTTL    = 7 * 24 * time.Hour
	
	// Very long-lived (static data)
	CountryListTTL    = 30 * 24 * time.Hour
	CurrencyRatesTTL  = 1 * time.Hour
)

// Cache warming strategies
type CacheWarmer struct {
	cache Cache
	db    *database.Postgres
}

func NewCacheWarmer(c Cache, db *database.Postgres) *CacheWarmer {
	return &CacheWarmer{cache: c, db: db}
}

// WarmPopularRestaurants pre-loads top restaurants into cache
func (w *CacheWarmer) WarmPopularRestaurants(ctx context.Context) error {
	rows, err := w.db.Query(ctx, `
		SELECT id, name, cuisine_type, rating, delivery_fee, min_order_amount, 
		       estimated_delivery_minutes, latitude, longitude
		FROM restaurants
		WHERE status = 'active'
		ORDER BY rating DESC
		LIMIT 1000
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	
	for rows.Next() {
		var restaurant Restaurant
		if err := rows.Scan(&restaurant.ID, &restaurant.Name, &restaurant.CuisineType,
			&restaurant.Rating, &restaurant.DeliveryFee, &restaurant.MinOrderAmount,
			&restaurant.EstimatedDeliveryMinutes, &restaurant.Latitude, &restaurant.Longitude); err != nil {
			continue
		}
		
		key := fmt.Sprintf(RestaurantKey, restaurant.ID)
		if err := w.cache.Set(ctx, key, restaurant, RestaurantTTL); err != nil {
			continue
		}
	}
	
	return nil
}

// WarmHotelAvailability pre-loads hotel availability for next 30 days
func (w *CacheWarmer) WarmHotelAvailability(ctx context.Context) error {
	rows, err := w.db.Query(ctx, `
		SELECT h.id, r.date, r.available_rooms, r.price_per_night
		FROM hotels h
		JOIN room_availability r ON h.id = r.hotel_id
		WHERE r.date BETWEEN CURRENT_DATE AND CURRENT_DATE + INTERVAL '30 days'
		AND h.status = 'active'
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	
	for rows.Next() {
		var hotelID, date string
		var availableRooms int
		var price float64
		
		if err := rows.Scan(&hotelID, &date, &availableRooms, &price); err != nil {
			continue
		}
		
		key := fmt.Sprintf(HotelAvailability, hotelID, date)
		availability := map[string]interface{}{
			"available_rooms": availableRooms,
			"price_per_night": price,
		}
		
		if err := w.cache.Set(ctx, key, availability, 1*time.Hour); err != nil {
			continue
		}
	}
	
	return nil
}

// Cache invalidation patterns
type CacheInvalidator struct {
	cache Cache
}

func NewCacheInvalidator(c Cache) *CacheInvalidator {
	return &CacheInvalidator{cache: c}
}

// InvalidateOnRideComplete clears related caches when ride completes
func (ci *CacheInvalidator) InvalidateOnRideComplete(ctx context.Context, rideID, userID, driverID string) error {
	// Invalidate ride cache
	ci.cache.Delete(ctx, fmt.Sprintf(RideKey, rideID))
	ci.cache.Delete(ctx, fmt.Sprintf(RideStatusKey, rideID))
	
	// Invalidate user's active rides list
	ci.cache.Delete(ctx, fmt.Sprintf(ActiveRidesByUser, userID))
	
	// Invalidate driver's current status
	ci.cache.Delete(ctx, fmt.Sprintf(DriverProfileKey, driverID))
	
	return nil
}

// InvalidateHotelSearch clears search result caches for a city
func (ci *CacheInvalidator) InvalidateHotelSearch(ctx context.Context, city string) error {
	// Use pattern matching to delete all related keys
	// Note: Redis KEYS command is expensive, use SCAN in production
	pattern := fmt.Sprintf("hotel:search:*%s*", city)
	// Implementation would use SCAN + DEL
	return nil
}