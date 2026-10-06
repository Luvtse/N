package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"nidaw-backend/internal/shared/auth"
	"nidaw-backend/internal/shared/database"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ============================================================================
// HANDLER
// ============================================================================

// AuthHandler handles all authentication-related HTTP requests
type AuthHandler struct {
	db          *database.Postgres
	authService *auth.Service
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(db *database.Postgres, authService *auth.Service) *AuthHandler {
	return &AuthHandler{
		db:          db,
		authService: authService,
	}
}

// ============================================================================
// REQUEST/RESPONSE TYPES
// ============================================================================

// LoginRequest is the JSON body for POST /auth/login
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RegisterRequest is the JSON body for POST /auth/register
type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	FullName string `json:"full_name"`
	Phone    string `json:"phone"`
}

// RefreshRequest is the JSON body for POST /auth/refresh
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// AuthResponse is the standard auth response
type AuthResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiresIn    int64        `json:"expires_in"`
	TokenType    string       `json:"token_type"`
	User         UserResponse `json:"user"`
}

// UserResponse contains user information
type UserResponse struct {
	ID              uuid.UUID `json:"id"`
	Email           string    `json:"email"`
	FullName        string    `json:"full_name"`
	Phone           string    `json:"phone"`
	Role            string    `json:"role"`
	EmailVerified   bool      `json:"email_verified"`
	ProfileImageURL *string   `json:"profile_image_url,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// ============================================================================
// ENDPOINTS
// ============================================================================

// Login handles POST /api/v1/auth/login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// Validate input
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "email and password are required")
		return
	}

	// Find user by email
	var userID uuid.UUID
	var email, fullName, phone, role, passwordHash string
	var emailVerified bool

	err := h.db.QueryRow(ctx, `
		SELECT id, email, full_name, phone, role, password_hash, email_verified
		FROM users
		WHERE email = $1 AND status = 'active'
	`, req.Email).Scan(&userID, &email, &fullName, &phone, &role, &passwordHash, &emailVerified)

	if err != nil {
		// Don't reveal if user exists or not
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		return
	}

	// Compare password
	if !auth.ComparePassword(passwordHash, req.Password) {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		return
	}

	// Generate tokens
	tokenPair, err := h.authService.GenerateTokenPair(userID, email, role, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", "failed to generate tokens")
		return
	}

	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(AuthResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresIn:    tokenPair.ExpiresIn,
		TokenType:    tokenPair.TokenType,
		User: UserResponse{
			ID:            userID,
			Email:         email,
			FullName:      fullName,
			Phone:         phone,
			Role:          role,
			EmailVerified: emailVerified,
			CreatedAt:     time.Now(),
		},
	})
}

// Register handles POST /api/v1/auth/register
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// Validate input
	if req.Email == "" || req.Password == "" || req.FullName == "" || req.Phone == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_ERROR", "all fields are required")
		return
	}

	// Validate password strength
	if len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "WEAK_PASSWORD", "password must be at least 8 characters")
		return
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "PASSWORD_HASH_FAILED", "failed to hash password")
		return
	}

	// Create user
	userID := uuid.New()
	now := time.Now().UTC()

	_, err = h.db.Exec(ctx, `
		INSERT INTO users (id, email, full_name, phone, role, password_hash, email_verified, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'rider', $5, false, $6, $7)
	`, userID, req.Email, req.FullName, req.Phone, string(passwordHash), now, now)

	if err != nil {
		// Check for duplicate email
		if isDuplicateKeyError(err) {
			writeError(w, http.StatusConflict, "EMAIL_EXISTS", "an account with this email already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "REGISTRATION_FAILED", "failed to create account")
		return
	}

	// Generate tokens
	tokenPair, err := h.authService.GenerateTokenPair(userID, req.Email, "rider", "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", "failed to generate tokens")
		return
	}

	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(AuthResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresIn:    tokenPair.ExpiresIn,
		TokenType:    tokenPair.TokenType,
		User: UserResponse{
			ID:            userID,
			Email:         req.Email,
			FullName:      req.FullName,
			Phone:         req.Phone,
			Role:          "rider",
			EmailVerified: false,
			CreatedAt:     now,
		},
	})
}

// Refresh handles POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	// Validate refresh token
	claims, err := h.authService.ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "invalid or expired refresh token")
		return
	}

	// Get user
	var email, fullName, phone, role string
	var emailVerified bool

	err = h.db.QueryRow(ctx, `
		SELECT email, full_name, phone, role, email_verified
		FROM users
		WHERE id = $1 AND status = 'active'
	`, claims.UserID).Scan(&email, &fullName, &phone, &role, &emailVerified)

	if err != nil {
		writeError(w, http.StatusUnauthorized, "USER_NOT_FOUND", "user not found or inactive")
		return
	}

	// Generate new token pair
	tokenPair, err := h.authService.GenerateTokenPair(claims.UserID, email, role, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "TOKEN_GENERATION_FAILED", "failed to generate tokens")
		return
	}

	// Return response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(AuthResponse{
		AccessToken:  tokenPair.AccessToken,
		RefreshToken: tokenPair.RefreshToken,
		ExpiresIn:    tokenPair.ExpiresIn,
		TokenType:    tokenPair.TokenType,
		User: UserResponse{
			ID:            claims.UserID,
			Email:         email,
			FullName:      fullName,
			Phone:         phone,
			Role:          role,
			EmailVerified: emailVerified,
			CreatedAt:     time.Now(),
		},
	})
}

// GetCurrentUser handles GET /api/v1/auth/me
func (h *AuthHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get user ID from context (set by auth middleware)
	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing user context")
		return
	}

	// Get user details
	var email, fullName, phone, role string
	var emailVerified bool
	var profileImageURL *string
	var createdAt time.Time

	err := h.db.QueryRow(ctx, `
		SELECT email, full_name, phone, role, email_verified, profile_image_url, created_at
		FROM users
		WHERE id = $1
	`, userID).Scan(&email, &fullName, &phone, &role, &emailVerified, &profileImageURL, &createdAt)

	if err != nil {
		writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
		return
	}

	// Return response
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(UserResponse{
		ID:              userID,
		Email:           email,
		FullName:        fullName,
		Phone:           phone,
		Role:            role,
		EmailVerified:   emailVerified,
		ProfileImageURL: profileImageURL,
		CreatedAt:       createdAt,
	})
}

// Logout handles POST /api/v1/auth/logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// In a stateless JWT system, logout is handled client-side by deleting tokens
	// Optionally, you could blacklist the token in Redis

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "logged out successfully",
	})
}

// ============================================================================
// HELPERS
// ============================================================================

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func isDuplicateKeyError(err error) bool {
	return err != nil && (errors.Is(err, errors.New("duplicate key")) ||
		err.Error() == "pq: duplicate key value violates unique constraint")
}

// UpdateProfile handles PUT /api/v1/auth/profile.
// Only the authenticated user's own row is modified (user id comes from the
// JWT context populated by auth.Service.Middleware, never from the body).
func (h *AuthHandler) UpdateProfile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing authenticated user")
		return
	}

	var req struct {
		FullName *string `json:"full_name"`
		Phone    *string `json:"phone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}

	if req.FullName == nil && req.Phone == nil {
		writeError(w, http.StatusBadRequest, "NO_FIELDS", "at least one of full_name or phone is required")
		return
	}
	if req.FullName != nil && (len(*req.FullName) == 0 || len(*req.FullName) > 200) {
		writeError(w, http.StatusBadRequest, "INVALID_NAME", "full_name must be between 1 and 200 characters")
		return
	}
	if req.Phone != nil && len(*req.Phone) > 20 {
		writeError(w, http.StatusBadRequest, "INVALID_PHONE", "phone is too long")
		return
	}

	tag, err := h.db.Exec(ctx, `
UPDATE users
SET full_name = COALESCE($1, full_name),
    phone     = COALESCE($2, phone),
    updated_at = $3
WHERE id = $4
`, req.FullName, req.Phone, time.Now(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", "failed to update profile")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "USER_NOT_FOUND", "user not found")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "updated"})
}

// ChangePassword handles POST /api/v1/auth/change-password.
// Verifies the current password before replacing the bcrypt hash.
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := auth.GetUserIDFromContext(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing authenticated user")
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON body")
		return
	}
	if len(req.NewPassword) < 8 || len(req.NewPassword) > 72 {
		writeError(w, http.StatusBadRequest, "WEAK_PASSWORD", "new password must be between 8 and 72 characters")
		return
	}

	var storedHash string
	err := h.db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1 AND status = 'active'`, userID).Scan(&storedHash)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "USER_NOT_FOUND", "user not found or inactive")
		return
	}

	if bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(req.CurrentPassword)) != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "current password is incorrect")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "HASHING_FAILED", "failed to hash password")
		return
	}

	_, err = h.db.Exec(ctx, `UPDATE users SET password_hash = $1, updated_at = $2 WHERE id = $3`,
		string(newHash), time.Now(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "DATABASE_ERROR", "failed to change password")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "password_changed"})
}
