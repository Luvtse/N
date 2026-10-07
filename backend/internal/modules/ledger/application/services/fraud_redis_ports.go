// fraud_redis_ports.go — Phase G Step 1: Redis-backed implementations of the
// DeviceIndex and IPDenylist ports consumed by FraudDetectionService.
//
// Key layout (all under a single namespace for ops tooling):
//
//	lf:device:<fingerprint>   SET of user ids that used this device
//	lf:ip:deny                SET of denied client IPs (populated by ops or
//	                          an upstream threat-feed syncer via AddDeniedIP)
//
// The device index is written opportunistically: RecordDevice is called from
// the withdrawal HTTP path with the X-Device-Fingerprint header (if any). A
// missing key simply means "no other accounts observed" — absence is never
// treated as risky. TTL keeps the sets from growing unbounded (90 days).
package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	deviceKeyPrefix = "lf:device:"
	ipDenySetKey    = "lf:ip:deny"
	fraudKeyTTL     = 90 * 24 * time.Hour
)

// RedisDeviceIndex implements DeviceIndex over Redis sets.
type RedisDeviceIndex struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewRedisDeviceIndex wraps a go-redis client. rdb must not be nil.
func NewRedisDeviceIndex(rdb *redis.Client) *RedisDeviceIndex {
	return &RedisDeviceIndex{rdb: rdb, ttl: fraudKeyTTL}
}

// RecordDevice associates fingerprint with userID (idempotent SADD + TTL
// refresh). Empty fingerprints are silently ignored.
func (i *RedisDeviceIndex) RecordDevice(ctx context.Context, fingerprint string, userID uuid.UUID) error {
	if i == nil || i.rdb == nil {
		return errors.New("ledger/fraud: redis device index not configured")
	}
	if fingerprint == "" || userID == uuid.Nil {
		return nil
	}
	key := deviceKeyPrefix + fingerprint
	pipe := i.rdb.TxPipeline()
	pipe.SAdd(ctx, key, userID.String())
	pipe.Expire(ctx, key, i.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("ledger/fraud: record device: %w", err)
	}
	return nil
}

// OtherUsersForDevice counts distinct OTHER accounts sharing the fingerprint.
// SCARD minus self-membership; a missing key yields 0 (clean).
func (i *RedisDeviceIndex) OtherUsersForDevice(ctx context.Context, fingerprint string, self uuid.UUID) (int, error) {
	if i == nil || i.rdb == nil {
		return 0, errors.New("ledger/fraud: redis device index not configured")
	}
	if fingerprint == "" {
		return 0, nil
	}
	key := deviceKeyPrefix + fingerprint
	total, err := i.rdb.SCard(ctx, key).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("ledger/fraud: device scard: %w", err)
	}
	mine, err := i.rdb.SIsMember(ctx, key, self.String()).Result()
	if err != nil && err != redis.Nil {
		return 0, fmt.Errorf("ledger/fraud: device membership: %w", err)
	}
	n := int(total)
	if mine {
		n--
	}
	if n < 0 {
		n = 0
	}
	return n, nil
}

// RedisIPDenylist implements IPDenylist over a Redis set maintained by ops /
// threat-feed syncers (AddDeniedIP / RemoveDeniedIP are the admin-side hooks).
type RedisIPDenylist struct {
	rdb *redis.Client
}

// NewRedisIPDenylist wraps a go-redis client.
func NewRedisIPDenylist(rdb *redis.Client) *RedisIPDenylist {
	return &RedisIPDenylist{rdb: rdb}
}

// IsDenied reports whether ip sits in the denylist. Unknown IP -> (false, nil)
// per the port contract; redis.Nil is treated the same way defensively.
func (d *RedisIPDenylist) IsDenied(ctx context.Context, ip string) (bool, error) {
	if d == nil || d.rdb == nil {
		return false, errors.New("ledger/fraud: redis ip denylist not configured")
	}
	if ip == "" {
		return false, nil
	}
	ok, err := d.rdb.SIsMember(ctx, ipDenySetKey, ip).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ledger/fraud: ip denylist lookup: %w", err)
	}
	return ok, nil
}

// AddDeniedIP flags an IP (ops/threat-feed ingestion path).
func (d *RedisIPDenylist) AddDeniedIP(ctx context.Context, ip string) error {
	if d == nil || d.rdb == nil || ip == "" {
		return errors.New("ledger/fraud: invalid denylist write")
	}
	if err := d.rdb.SAdd(ctx, ipDenySetKey, ip).Err(); err != nil {
		return fmt.Errorf("ledger/fraud: add denied ip: %w", err)
	}
	return nil
}

// RemoveDeniedIP clears a false positive.
func (d *RedisIPDenylist) RemoveDeniedIP(ctx context.Context, ip string) error {
	if d == nil || d.rdb == nil || ip == "" {
		return errors.New("ledger/fraud: invalid denylist write")
	}
	if err := d.rdb.SRem(ctx, ipDenySetKey, ip).Err(); err != nil {
		return fmt.Errorf("ledger/fraud: remove denied ip: %w", err)
	}
	return nil
}
