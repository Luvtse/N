package auth

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// ============================================================================
// PHASE B/B5: REFRESH-TOKEN ROTATION + REVOCATION STORE
//
// Refresh tokens are now single-use. On each /auth/refresh call the presented
// token's JTI is marked "used"; any replay of the same token is treated as a
// theft indicator and revokes the entire family for that user. Logout writes
// both the access-token JTI (short TTL) and the current refresh JTI (full TTL)
// to a denylist that middleware consults on every request.
// ============================================================================

var (
	ErrTokenRevoked = errors.New("token has been revoked")
	ErrTokenReused  = errors.New("refresh token reuse detected")
)

// TokenStore persists rotation state and revocations in Redis.
type TokenStore struct {
	client *redis.Client
}

// NewTokenStore wraps an initialized Redis client. A nil store is tolerated by
// callers so environments without Redis can still boot (rotation degrades to
// disabled rather than crashing the auth path).
func NewTokenStore(client *redis.Client) *TokenStore {
	if client == nil {
		return nil
	}
	return &TokenStore{client: client}
}

// redis key helpers -----------------------------------------------------------

func usedJTIKey(jti string) string      { return "auth:jti:used:" + jti }
func revokedJTIKey(jti string) string    { return "auth:jti:revoked:" + jti }
func currentFamilyKey(userID string) string { return "auth:family:" + userID }

// MarkRefreshUsed records a refresh JTI as consumed. The entry lives for the
// remaining lifetime of the token so replays stay detectable.
func (s *TokenStore) MarkRefreshUsed(ctx context.Context, jti string, ttl time.Duration) error {
	if s == nil {
		return nil
	}
	return s.client.Set(ctx, usedJTIKey(jti), "1", ttl).Err()
}

// IsRefreshUsed reports whether a refresh JTI was already consumed.
func (s *TokenStore) IsRefreshUsed(ctx context.Context, jti string) (bool, error) {
	if s == nil {
		return false, nil
	}
	val, err := s.client.Get(ctx, usedJTIKey(jti)).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return val == "1", nil
}

// RevokeJTI adds a JTI to the denylist with the given TTL (bounded by the
// token's own expiry — no point remembering a dead token).
func (s *TokenStore) RevokeJTI(ctx context.Context, jti string, ttl time.Duration) error {
	if s == nil || jti == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = time.Second // never persist forever; expired tokens self-heal
	}
	return s.client.Set(ctx, revokedJTIKey(jti), "logout", ttl).Err()
}

// IsRevoked checks the denylist. Returns true when the JTI was logged out or
// killed via family revocation.
func (s *TokenStore) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if s == nil || jti == "" {
		return false, nil
	}
	exists, err := s.client.Exists(ctx, revokedJTIKey(jti)).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// SetUserFamily stores the currently-valid refresh JTI per user so we can kill
// the whole lineage when reuse/theft is suspected.
func (s *TokenStore) SetUserFamily(ctx context.Context, userID, jti string, ttl time.Duration) error {
	if s == nil {
		return nil
	}
	return s.client.Set(ctx, currentFamilyKey(userID), jti, ttl).Err()
}

// GetUserFamily returns the active refresh JTI for a user, if tracked.
func (s *TokenStore) GetUserFamily(ctx context.Context, userID string) (string, error) {
	if s == nil {
		return "", nil
	}
	jti, err := s.client.Get(ctx, currentFamilyKey(userID)).Result()
	if err == redis.Nil {
		return "", nil
	}
	return jti, err
}

// RevokeUserFamily kills the user's active refresh token. Access tokens remain
// valid until their short TTL expires (by design); callers should also revoke
// the presented access JTI when available.
func (s *TokenStore) RevokeUserFamily(ctx context.Context, userID string, ttl time.Duration) error {
	if s == nil {
		return nil
	}
	current, err := s.GetUserFamily(ctx, userID)
	if err != nil {
		return err
	}
	if current == "" {
		return nil
	}
	if err := s.RevokeJTI(ctx, current, ttl); err != nil {
		return err
	}
	return s.client.Del(ctx, currentFamilyKey(userID)).Err()
}
