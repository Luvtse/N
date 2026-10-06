package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"nidaw-backend/internal/shared/cache"
)

// DriverLocationCache stores live driver positions in Redis GEO indexes for
// spatial queries and caches the full location payload per driver.
type DriverLocationCache struct {
	cache cache.Cache
}

func NewDriverLocationCache(c cache.Cache) *DriverLocationCache {
	return &DriverLocationCache{cache: c}
}

// redisClient unwraps the concrete client from the shared Cache abstraction.
// The GEO/pipeline commands used here are not part of the generic Cache
// interface, so we reach through to go-redis when a RedisCache is supplied.
func (c *DriverLocationCache) redisClient() (*redis.Client, error) {
	rc, ok := c.cache.(*cache.RedisCache)
	if !ok {
		return nil, fmt.Errorf("driver location cache requires a Redis-backed cache")
	}
	return rc.RawClient(), nil
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
	client, err := c.redisClient()
	if err != nil {
		return err
	}

	// Update GEO index
	key := fmt.Sprintf("drivers:geo:%s", loc.Status)
	err = client.GeoAdd(ctx, key, &redis.GeoLocation{
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

// GetNearbyDrivers uses GEORADIUS for efficient proximity search
func (c *DriverLocationCache) GetNearbyDrivers(ctx context.Context, lat, lng, radiusKm float64, status string) ([]*DriverLocation, error) {
	client, err := c.redisClient()
	if err != nil {
		return nil, err
	}

	key := fmt.Sprintf("drivers:geo:%s", status)

	locations, err := client.GeoRadius(ctx, key, lat, lng, &redis.GeoRadiusQuery{
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

		// Start from the cached payload (heading/speed/status/timestamp), then
		// overlay the authoritative coordinates from the GEO response.
		var entry DriverLocation
		if err := c.cache.Get(ctx, dataKey, &entry); err != nil {
			entry = DriverLocation{}
		}
		entry.DriverID = loc.Name
		entry.Latitude = loc.Latitude
		entry.Longitude = loc.Longitude
		drivers = append(drivers, &entry)
	}

	return drivers, nil
}

// BatchUpdate for high-throughput location updates
func (c *DriverLocationCache) BatchUpdate(ctx context.Context, locations []*DriverLocation) error {
	client, err := c.redisClient()
	if err != nil {
		return err
	}

	pipe := client.Pipeline()
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

	_, err = pipe.Exec(ctx)
	return err
}
