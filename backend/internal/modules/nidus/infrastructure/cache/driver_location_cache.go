package cache

import (
	"context"
	"fmt"
	"time"

	"nidaw-backend/internal/shared/cache"
	"github.com/redis/go-redis/v9"
)

type DriverLocationCache struct {
	cache cache.Cache
}

func NewDriverLocationCache(c cache.Cache) *DriverLocationCache {
	return &DriverLocationCache{cache: c}
}

type DriverLocation struct {
	DriverID  string  `json:"driver_id"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timestamp int64   `json:"timestamp"`
	Heading   float64 `json:"heading"`
	Speed     float64 `json:"speed"`
	Status    string  `json:"status"`
}

// UpdateDriverLocation uses Redis GEO for efficient spatial queries
func (c *DriverLocationCache) UpdateDriverLocation(ctx context.Context, loc *DriverLocation) error {
	// Update GEO index
	key := fmt.Sprintf("drivers:geo:%s", loc.Status)
	err := c.(*redis.Client).GeoAdd(ctx, key, &redis.GeoLocation{
		Name:      loc.DriverID,
		Latitude:  loc.Latitude,
		Longitude: loc.Longitude,
	}).Err()
	if err != nil {
		return err
	}
	
	// Store full location data
	dataKey := fmt.Sprintf("driver:location:%s", loc.DriverID)
	return c.cache.Set(ctx, dataKey, loc, 60*time.Second)
}

// GetNearbyDrivers uses Redis GEORADIUS for efficient proximity search
func (c *DriverLocationCache) GetNearbyDrivers(ctx context.Context, lat, lng, radiusKm float64, status string) ([]*DriverLocation, error) {
	key := fmt.Sprintf("drivers:geo:%s", status)
	
	// Use GEORADIUS to find drivers within radius
	locations, err := c.(*redis.Client).GeoRadius(ctx, key, &redis.GeoLocation{
		Latitude:  lat,
		Longitude: lng,
	}, &redis.GeoRadiusQuery{
		Radius:    radiusKm,
		Unit:      "km",
		Sort:      "ASC",
		Count:     50,
		WithCoord: true,
		WithDist:  true,
	}).Result()
	if err != nil {
		return nil, err
	}
	
	drivers := make([]*DriverLocation, 0, len(locations))
	for _, loc := range locations {
		dataKey := fmt.Sprintf("driver:location:%s", loc.Name)
		var driverLoc DriverLocation
		err := c.cache.Get(ctx, dataKey, &driverLoc)
		if err == nil {
			driverLoc.Latitude = loc.Latitude
			driverLoc.Longitude = loc.Longitude
			drivers = append(drivers, &driverLoc)
		}
	}
	
	return drivers, nil
}

// BatchUpdate for high-throughput location updates
func (c *DriverLocationCache) BatchUpdate(ctx context.Context, locations []*DriverLocation) error {
	pipe := c.cache.Pipeline()
	
	for _, loc := range locations {
		key := fmt.Sprintf("drivers:geo:%s", loc.Status)
		pipe.GeoAdd(ctx, key, &redis.GeoLocation{
			Name:      loc.DriverID,
			Latitude:  loc.Latitude,
			Longitude: loc.Longitude,
		})
		
		dataKey := fmt.Sprintf("driver:location:%s", loc.DriverID)
		data, _ := json.Marshal(loc)
		pipe.Set(ctx, dataKey, data, 60*time.Second)
	}
	
	_, err := pipe.Exec(ctx)
	return err
}