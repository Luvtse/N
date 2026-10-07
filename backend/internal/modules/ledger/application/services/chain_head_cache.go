package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"nidaw-backend/internal/modules/ledger/domain/valueobjects"
)

// RedisChainHeadCache implements ChainHeadCache on top of Redis so every
// append can read the user's chain head in O(1) instead of walking the DB.
// The DB (user_balances.latest_tx_hash) remains the source of truth: cache
// misses simply fall back to the balance row, and stale entries are healed by
// the fork-preventing unique index on (user_id, prev_hash).
type RedisChainHeadCache struct {
	client *goredis.Client
	ttl    time.Duration
	prefix string
}

// NewRedisChainHeadCache wires the cache; heads expire after `ttl` (default
// 24h) so a crash between DB commit and cache write self-heals.
func NewRedisChainHeadCache(client *goredis.Client) *RedisChainHeadCache {
	return &RedisChainHeadCache{client: client, ttl: 24 * time.Hour, prefix: "ledger:head:"}
}

// SetTTL overrides the entry lifetime (tests / tuning).
func (c *RedisChainHeadCache) SetTTL(ttl time.Duration) { c.ttl = ttl }

func (c *RedisChainHeadCache) key(userID uuid.UUID) string {
	return fmt.Sprintf("%s%s", c.prefix, userID.String())
}

// GetHead returns (hash, found, err). A missing key is not an error — the
// HashChainService treats found=false as a cache miss.
func (c *RedisChainHeadCache) GetHead(ctx context.Context, userID uuid.UUID) (valueobjects.TransactionHash, bool, error) {
	val, err := c.client.Get(ctx, c.key(userID)).Result()
	if errors.Is(err, goredis.Nil) {
		return valueobjects.TransactionHash{}, false, nil
	}
	if err != nil {
		return valueobjects.TransactionHash{}, false, fmt.Errorf("ledger: redis get head: %w", err)
	}
	h, err := valueobjects.NewTransactionHash(val)
	if err != nil {
		// Corrupt/unparseable entry: treat as miss and drop it.
		_ = c.client.Del(ctx, c.key(userID)).Err()
		return valueobjects.TransactionHash{}, false, nil
	}
	return h, true, nil
}

// SetHead stores the newest chain-head hash for a user.
func (c *RedisChainHeadCache) SetHead(ctx context.Context, userID uuid.UUID, h valueobjects.TransactionHash) error {
	if err := c.client.Set(ctx, c.key(userID), h.Hex(), c.ttl).Err(); err != nil {
		return fmt.Errorf("ledger: redis set head: %w", err)
	}
	return nil
}

// Invalidate removes the cached head (used by rebuild/verification jobs).
func (c *RedisChainHeadCache) Invalidate(ctx context.Context, userID uuid.UUID) error {
	if err := c.client.Del(ctx, c.key(userID)).Err(); err != nil {
		return fmt.Errorf("ledger: redis del head: %w", err)
	}
	return nil
}

var _ ChainHeadCache = (*RedisChainHeadCache)(nil)
