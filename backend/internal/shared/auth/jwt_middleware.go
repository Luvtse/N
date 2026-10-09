package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrInvalidToken      = errors.New("invalid or expired token")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountLocked     = errors.New("account temporarily locked")
	ErrTokenExpired      = errors.New("token has expired")
	ErrMalformedToken    = errors.New("malformed token")
	ErrMissingToken      = errors.New("missing authorization token")
)

// ============================================================================
// CONTEXT KEYS (typed to avoid collisions)
// ============================================================================

type contextKey string

const (
	UserIDKey     contextKey = "user_id"
	UserEmailKey  contextKey = "user_email"
	UserRoleKey   contextKey = "user_role"
	TenantIDKey   contextKey = "tenant_id"
	TokenClaimsKey contextKey = "token_claims"
)

// ============================================================================
// TOKEN TYPES & CLAIMS
// ============================================================================

// TokenType distinguishes access vs refresh tokens
type TokenType string

const (
	AccessToken  TokenType = "access"
	RefreshToken TokenType = "refresh"
)

// Claims represents the JWT claims payload
type Claims struct {
	jwt.RegisteredClaims
	UserID   uuid.UUID `json:"uid"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	TenantID string    `json:"tid,omitempty"`
	Type     TokenType `json:"type"`
}

// IsAccessToken reports whether these claims carry an access-token credential.
// Entry points that need defense-in-depth against refresh/other token classes
// (e.g., the WebSocket hub) should assert this explicitly rather than relying
// solely on which Validate* helper was called upstream.
func (c *Claims) IsAccessToken() bool {
	return c != nil && c.Type == AccessToken
}

// TokenPair holds both access and refresh tokens
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresIn    int64     `json:"expires_in"` // seconds
	TokenType    string    `json:"token_type"` // "Bearer"
	IssuedAt     time.Time `json:"issued_at"`
}

// ============================================================================
// SERVICE
// ============================================================================

// Service handles all authentication operations
type Service struct {
	accessSecret  []byte
	refreshSecret []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
	issuer        string
	audience      string
	logger        *zap.Logger
	tokenStore    *TokenStore // Phase B/B5: rotation + revocation (nil-safe)
}

// SetTokenStore attaches the Redis-backed revocation/rotation store.
// Called from main.go after Redis is initialized; nil disables the feature.
func (s *Service) SetTokenStore(store *TokenStore) {
	s.tokenStore = store
}

// TokenStore returns the attached revocation store (may be nil).
func (s *Service) TokenStore() *TokenStore {
	return s.tokenStore
}

// ServiceConfig holds auth service configuration
type ServiceConfig struct {
	JWTSecret      string
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	Issuer         string
	Audience       string
}

// NewService creates a new auth service
func NewService(cfg ServiceConfig, logger *zap.Logger) (*Service, error) {
	if cfg.JWTSecret == "" {
		return nil, errors.New("JWT secret is required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	// Use separate secrets for access and refresh tokens (derived from main secret)
	accessSecret := []byte(cfg.JWTSecret + ":access")
	refreshSecret := []byte(cfg.JWTSecret + ":refresh")

	return &Service{
		accessSecret:  accessSecret,
		refreshSecret: refreshSecret,
		accessTTL:     cfg.AccessTTL,
		refreshTTL:    cfg.RefreshTTL,
		issuer:        cfg.Issuer,
		audience:      cfg.Audience,
		logger:        logger,
	}, nil
}

// ============================================================================
// TOKEN GENERATION
// ============================================================================

// GenerateTokenPair creates both access and refresh tokens for a user
func (s *Service) GenerateTokenPair(userID uuid.UUID, email, role, tenantID string) (*TokenPair, error) {
	now := time.Now()

	// Access token (short-lived)
	accessClaims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.New().String(),
		},
		UserID:   userID,
		Email:    email,
		Role:     role,
		TenantID: tenantID,
		Type:     AccessToken,
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenStr, err := accessToken.SignedString(s.accessSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// Refresh token (long-lived)
	refreshClaims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.refreshTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ID:        uuid.New().String(),
		},
		UserID: userID,
		Email:  email,
		Role:   role,
		Type:   RefreshToken,
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenStr, err := refreshToken.SignedString(s.refreshSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessTokenStr,
		RefreshToken: refreshTokenStr,
		ExpiresIn:    int64(s.accessTTL.Seconds()),
		TokenType:    "Bearer",
		IssuedAt:     now,
	}, nil
}

// ============================================================================
// TOKEN VALIDATION
// ============================================================================

// ValidateAccessToken validates an access token and returns its claims
func (s *Service) ValidateAccessToken(tokenStr string) (*Claims, error) {
	return s.validateToken(tokenStr, s.accessSecret, AccessToken)
}

// ValidateRefreshToken validates a refresh token and returns its claims
func (s *Service) ValidateRefreshToken(tokenStr string) (*Claims, error) {
	return s.validateToken(tokenStr, s.refreshSecret, RefreshToken)
}

func (s *Service) validateToken(tokenStr string, secret []byte, expectedType TokenType) (*Claims, error) {
	if tokenStr == "" {
		return nil, ErrMissingToken
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer(s.issuer),
	)

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		if errors.Is(err, jwt.ErrTokenMalformed) || errors.Is(err, jwt.ErrTokenUnverifiable) {
			return nil, ErrMalformedToken
		}
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	// Verify token type
	if claims.Type != expectedType {
		return nil, fmt.Errorf("%w: expected %s, got %s", ErrInvalidToken, expectedType, claims.Type)
	}

	return claims, nil
}

// ============================================================================
// PASSWORD HASHING
// ============================================================================

// HashPassword hashes a plaintext password using bcrypt
func HashPassword(password string, cost int) (string, error) {
	if cost == 0 {
		cost = bcrypt.DefaultCost
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", fmt.Errorf("failed to hash password: %w", err)
	}
	return string(hash), nil
}

// ComparePassword compares a plaintext password with a bcrypt hash
func ComparePassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// ============================================================================
// HTTP MIDDLEWARE
// ============================================================================

// Middleware returns an HTTP middleware that validates JWT tokens
func (s *Service) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				s.writeError(w, http.StatusUnauthorized, ErrMissingToken)
				return
			}

			// Expect "Bearer <token>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				s.writeError(w, http.StatusUnauthorized, ErrMalformedToken)
				return
			}

			tokenStr := strings.TrimSpace(parts[1])
			if tokenStr == "" {
				s.writeError(w, http.StatusUnauthorized, ErrMissingToken)
				return
			}

			// Validate token
			claims, err := s.ValidateAccessToken(tokenStr)
			if err != nil {
				status := http.StatusUnauthorized
				if errors.Is(err, ErrTokenExpired) {
					w.Header().Set("X-Token-Expired", "true")
				}
				s.writeError(w, status, err)
				return
			}

			// Phase B/B5: deny revoked JTIs (logout / family kill). Fail closed on
			// store errors only when a store is configured.
			if s.tokenStore != nil {
				revoked, rerr := s.tokenStore.IsRevoked(r.Context(), claims.ID)
				if rerr != nil {
					s.logger.Warn("revocation check failed; failing closed", zap.Error(rerr))
					s.writeError(w, http.StatusServiceUnavailable, ErrInvalidToken)
					return
				}
				if revoked {
					s.writeError(w, http.StatusUnauthorized, ErrTokenRevoked)
					return
				}
			}

			// Inject claims into context
			ctx := r.Context()
			ctx = context.WithValue(ctx, UserIDKey, claims.UserID)
			ctx = context.WithValue(ctx, UserEmailKey, claims.Email)
			ctx = context.WithValue(ctx, UserRoleKey, claims.Role)
			ctx = context.WithValue(ctx, TokenClaimsKey, claims)
			if claims.TenantID != "" {
				ctx = context.WithValue(ctx, TenantIDKey, claims.TenantID)
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole returns middleware that checks if user has one of the required roles
func (s *Service) RequireRole(roles ...string) func(http.Handler) http.Handler {
	roleSet := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		roleSet[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := r.Context().Value(TokenClaimsKey).(*Claims)
			if !ok {
				s.writeError(w, http.StatusUnauthorized, ErrInvalidToken)
				return
			}

			if _, hasRole := roleSet[claims.Role]; !hasRole {
				s.writeError(w, http.StatusForbidden, errors.New("insufficient permissions"))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ============================================================================
// CONTEXT HELPERS
// ============================================================================

// GetUserIDFromContext extracts the user ID from the request context
func GetUserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	userID, ok := ctx.Value(UserIDKey).(uuid.UUID)
	return userID, ok
}

// GetUserEmailFromContext extracts the user email from the request context
func GetUserEmailFromContext(ctx context.Context) (string, bool) {
	email, ok := ctx.Value(UserEmailKey).(string)
	return email, ok
}

// GetUserRoleFromContext extracts the user role from the request context
func GetUserRoleFromContext(ctx context.Context) (string, bool) {
	role, ok := ctx.Value(UserRoleKey).(string)
	return role, ok
}

// GetClaimsFromContext extracts the full claims from the request context
func GetClaimsFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(TokenClaimsKey).(*Claims)
	return claims, ok
}

// ============================================================================
// ERROR RESPONSE HELPER
// ============================================================================

func (s *Service) writeError(w http.ResponseWriter, status int, err error) {
	s.logger.Debug("auth middleware error",
		zap.Int("status", status),
		zap.Error(err),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	errorCode := "UNAUTHORIZED"
	switch {
	case errors.Is(err, ErrMissingToken):
		errorCode = "MISSING_TOKEN"
	case errors.Is(err, ErrMalformedToken):
		errorCode = "MALFORMED_TOKEN"
	case errors.Is(err, ErrTokenExpired):
		errorCode = "TOKEN_EXPIRED"
	case errors.Is(err, ErrInvalidToken):
		errorCode = "INVALID_TOKEN"
	}

	response := fmt.Sprintf(
		`{"error":{"code":"%s","message":"%s"}}`,
		errorCode, err.Error(),
	)
	_, _ = w.Write([]byte(response))
}