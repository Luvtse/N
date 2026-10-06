package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrCacheMiss = errors.New("cache miss")
)

type Cache interface {
	Get(ctx context.Context, key string, dest interface{}) error
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Incr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error)
}

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(redisURL string) (*RedisCache, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &RedisCache{client: client}, nil
}

func (c *RedisCache) Get(ctx context.Context, key string, dest interface{}) error {
	val, err := c.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return ErrCacheMiss
	}
	if err != nil {
		return err
	}

	return json.Unmarshal([]byte(val), dest)
}

func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return c.client.Set(ctx, key, data, ttl).Err()
}

func (c *RedisCache) Delete(ctx context.Context, key string) error {
	return c.client.Del(ctx, key).Err()
}

func (c *RedisCache) Incr(ctx context.Context, key string) (int64, error) {
	return c.client.Incr(ctx, key).Result()
}

func (c *RedisCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return c.client.Expire(ctx, key, ttl).Err()
}

func (c *RedisCache) SetNX(ctx context.Context, key string, value interface{}, ttl time.Duration) (bool, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return false, err
	}

	return c.client.SetNX(ctx, key, data, ttl).Result()
}

// Pipeline for batch operations
func (c *RedisCache) Pipeline() redis.Pipeliner {
	return c.client.Pipeline()
}

// Cache-aside pattern helper
type CacheAside struct {
	cache   Cache
	keyFunc func(args ...interface{}) string
	ttl     time.Duration
}

func NewCacheAside(cache Cache, keyFunc func(...interface{}) string, ttl time.Duration) *CacheAside {
	return &CacheAside{cache: cache, keyFunc: keyFunc, ttl: ttl}
}

// GetOrLoad implements cache-aside pattern
func (ca *CacheAside) GetOrLoad(ctx context.Context, loader func() (interface{}, error), args ...interface{}) (interface{}, error) {
	key := ca.keyFunc(args...)

	// Try cache first
	var result interface{}
	err := ca.cache.Get(ctx, key, &result)
	if err == nil {
		return result, nil
	}
	if err != ErrCacheMiss {
		return nil, err
	}

	// Cache miss - load from source
	result, err = loader()
	if err != nil {
		return nil, err
	}

	// Store in cache (ignore errors)
	_ = ca.cache.Set(ctx, key, result, ca.ttl)

	return result, nil
}

// RawClient exposes the underlying go-redis client for commands that are not
// part of the generic Cache interface (GEO, pipelines, etc.). Callers must be
// Redis-aware infrastructure components.
func (c *RedisCache) RawClient() *redis.Client {
	return c.client
}

// Close releases the underlying Redis connection pool. Safe to call once at
// process shutdown.
func (c *RedisCache) Close() error {
	return c.client.Close()
}
