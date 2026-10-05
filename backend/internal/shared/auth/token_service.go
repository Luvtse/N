package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrInvalidToken      = errors.New("invalid token")
	ErrTokenExpired      = errors.New("token has expired")
	ErrTokenNotValidYet  = errors.New("token not valid yet")
	ErrTokenMalformed    = errors.New("malformed token")
	ErrInvalidSigningMethod = errors.New("invalid signing method")
)

// ============================================================================
// TYPES
// ============================================================================

// TokenType represents the type of JWT token
type TokenType string

const (
	AccessToken  TokenType = "access"
	RefreshToken TokenType = "refresh"
)

// Claims represents JWT claims
type Claims struct {
	jwt.RegisteredClaims
	UserID   uuid.UUID `json:"uid"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	TenantID string    `json:"tid,omitempty"`
	Type     TokenType `json:"type"`
}

// TokenPair contains access and refresh tokens
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresIn    int64     `json:"expires_in"`
	TokenType    string    `json:"token_type"`
	IssuedAt     time.Time `json:"issued_at"`
}

// TokenServiceConfig holds token service configuration
type TokenServiceConfig struct {
	Secret        string
	Issuer        string
	Audience      string
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
}

// TokenService handles JWT token operations
type TokenService struct {
	config TokenServiceConfig
}

// NewTokenService creates a new token service
func NewTokenService(config TokenServiceConfig) *TokenService {
	return &TokenService{config: config}
}

// GenerateTokenPair generates both access and refresh tokens
func (s *TokenService) GenerateTokenPair(userID uuid.UUID, email, role, tenantID string) (*TokenPair, error) {
	now := time.Now()

	// Generate access token
	accessClaims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.config.Issuer,
			Audience:  jwt.ClaimStrings{s.config.Audience},
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.config.AccessTTL)),
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
	accessTokenString, err := accessToken.SignedString([]byte(s.config.Secret))
	if err != nil {
		return nil, err
	}

	// Generate refresh token
	refreshClaims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.config.Issuer,
			Audience:  jwt.ClaimStrings{s.config.Audience},
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.config.RefreshTTL)),
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
	refreshTokenString, err := refreshToken.SignedString([]byte(s.config.Secret))
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresIn:    int64(s.config.AccessTTL.Seconds()),
		TokenType:    "Bearer",
		IssuedAt:     now,
	}, nil
}

// ValidateAccessToken validates an access token
func (s *TokenService) ValidateAccessToken(tokenString string) (*Claims, error) {
	return s.validateToken(tokenString, AccessToken)
}

// ValidateRefreshToken validates a refresh token
func (s *TokenService) ValidateRefreshToken(tokenString string) (*Claims, error) {
	return s.validateToken(tokenString, RefreshToken)
}

// validateToken validates a JWT token
func (s *TokenService) validateToken(tokenString string, expectedType TokenType) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidSigningMethod
		}
		return []byte(s.config.Secret), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		if errors.Is(err, jwt.ErrTokenNotValidYet) {
			return nil, ErrTokenNotValidYet
		}
		if errors.Is(err, jwt.ErrTokenMalformed) {
			return nil, ErrTokenMalformed
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrInvalidToken         = errors.New("invalid token")
	ErrTokenExpired         = errors.New("token has expired")
	ErrTokenNotValidYet     = errors.New("token not valid yet")
	ErrTokenMalformed       = errors.New("malformed token")
	ErrInvalidSigningMethod = errors.New("invalid signing method")
)

// ============================================================================
// TYPES
// ============================================================================

// TokenType represents the type of JWT token
type TokenType string

const (
	AccessToken  TokenType = "access"
	RefreshToken TokenType = "refresh"
)

// Claims represents JWT claims
type Claims struct {
	jwt.RegisteredClaims
	UserID   uuid.UUID `json:"uid"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	TenantID string    `json:"tid,omitempty"`
	Type     TokenType `json:"type"`
}

// TokenPair contains access and refresh tokens
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresIn    int64     `json:"expires_in"`
	TokenType    string    `json:"token_type"`
	IssuedAt     time.Time `json:"issued_at"`
}

// TokenServiceConfig holds token service configuration
type TokenServiceConfig struct {
	Secret    string
	Issuer    string
	Audience  string
	AccessTTL time.Duration
	RefreshTTL time.Duration
}

// TokenService handles JWT token operations
type TokenService struct {
	config TokenServiceConfig
}

// NewTokenService creates a new token service
func NewTokenService(config TokenServiceConfig) *TokenService {
	return &TokenService{config: config}
}

// GenerateTokenPair generates both access and refresh tokens
func (s *TokenService) GenerateTokenPair(userID uuid.UUID, email, role, tenantID string) (*TokenPair, error) {
	now := time.Now()

	// Generate access token
	accessClaims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.config.Issuer,
			Audience:  jwt.ClaimStrings{s.config.Audience},
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.config.AccessTTL)),
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
	accessTokenString, err := accessToken.SignedString([]byte(s.config.Secret))
	if err != nil {
		return nil, err
	}

	// Generate refresh token
	refreshClaims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.config.Issuer,
			Audience:  jwt.ClaimStrings{s.config.Audience},
			Subject:   userID.String(),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.config.RefreshTTL)),
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
	refreshTokenString, err := refreshToken.SignedString([]byte(s.config.Secret))
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresIn:    int64(s.config.AccessTTL.Seconds()),
		TokenType:    "Bearer",
		IssuedAt:     now,
	}, nil
}

// ValidateAccessToken validates an access token
func (s *TokenService) ValidateAccessToken(tokenString string) (*Claims, error) {
	return s.validateToken(tokenString, AccessToken)
}

// ValidateRefreshToken validates a refresh token
func (s *TokenService) ValidateRefreshToken(tokenString string) (*Claims, error) {
	return s.validateToken(tokenString, RefreshToken)
}

// validateToken validates a JWT token
func (s *TokenService) validateToken(tokenString string, expectedType TokenType) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidSigningMethod
		}
		return []byte(s.config.Secret), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		if errors.Is(err, jwt.ErrTokenNotValidYet) {
			return nil, ErrTokenNotValidYet
		}
		if errors.Is(err, jwt.ErrTokenMalformed) {
			return nil, ErrTokenMalformed
		}
		return nil, ErrInvalidToken
	}

	if !token.Valid {
		return nil, ErrInvalidToken
	}

	// Verify token type
	if claims.Type != expectedType {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// ExtractUserID extracts user ID from token
func (s *TokenService) ExtractUserID(tokenString string) (uuid.UUID, error) {
	claims, err := s.ValidateAccessToken(tokenString)
	if err != nil {
		return uuid.Nil, err
	}
	return claims.UserID, nil
}

// ExtractEmail extracts email from token
func (s *TokenService) ExtractEmail(tokenString string) (string, error) {
	claims, err := s.ValidateAccessToken(tokenString)
	if err != nil {
		return "", err
	}
	return claims.Email, nil
}

// ExtractRole extracts role from token
func (s *TokenService) ExtractRole(tokenString string) (string, error) {
	claims, err := s.ValidateAccessToken(tokenString)
	if err != nil {
		return "", err
	}
	return claims.Role, nil
}

// IsTokenExpired checks if token is expired
func (s *TokenService) IsTokenExpired(tokenString string) bool {
	_, err := s.ValidateAccessToken(tokenString)
	return errors.Is(err, ErrTokenExpired)
}

// GetTokenExpiry returns token expiration time
func (s *TokenService) GetTokenExpiry(tokenString string) (time.Time, error) {
	claims, err := s.ValidateAccessToken(tokenString)
	if err != nil {
		return time.Time{}, err
	}
	return claims.ExpiresAt.Time, nil
}

// RefreshAccessToken generates new access token using refresh token
func (s *TokenService) RefreshAccessToken(refreshTokenString string) (*TokenPair, error) {
	// Validate refresh token
	claims, err := s.ValidateRefreshToken(refreshTokenString)
	if err != nil {
		return nil, err
	}

	// Generate new token pair
	return s.GenerateTokenPair(
		claims.UserID,
		claims.Email,
		claims.Role,
		claims.TenantID,
	)
}