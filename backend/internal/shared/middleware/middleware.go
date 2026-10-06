package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// ============================================================================
// CONTEXT KEYS
// ============================================================================

type contextKey string

const (
	RequestIDKey contextKey = "request_id"
	StartTimeKey contextKey = "start_time"
)

// ============================================================================
// REQUEST ID MIDDLEWARE
// ============================================================================

// RequestIDToContext injects a request ID into the context.
// Uses X-Request-ID header if present, otherwise generates a new UUID.
func RequestIDToContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = uuid.New().String()
		}

		ctx := context.WithValue(r.Context(), RequestIDKey, requestID)
		r = r.WithContext(ctx)

		// Echo back the request ID in response headers
		w.Header().Set("X-Request-ID", requestID)

		next.ServeHTTP(w, r)
	})
}

// GetRequestID extracts the request ID from context
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// ============================================================================
// LOGGER MIDDLEWARE
// ============================================================================

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	bytes      int
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += n
	return n, err
}

// Unwrap supports http.ResponseController
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Logger logs every HTTP request with structured fields
func Logger(logger *zap.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx := context.WithValue(r.Context(), StartTimeKey, start)
			r = r.WithContext(ctx)

			wrapped := newResponseWriter(w)
			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)
			requestID := GetRequestID(r.Context())

			// Determine log level based on status code
			var logEvent *zap.Logger
			switch {
			case wrapped.statusCode >= 500:
				logEvent = logger
			case wrapped.statusCode >= 400:
				logEvent = logger
			default:
				logEvent = logger
			}

			fields := []zap.Field{
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("query", r.URL.RawQuery),
				zap.Int("status", wrapped.statusCode),
				zap.Int("bytes", wrapped.bytes),
				zap.Duration("duration", duration),
				zap.String("ip", r.RemoteAddr),
				zap.String("user_agent", r.UserAgent()),
				zap.String("request_id", requestID),
			}

			// Add user info if available
			if userID, ok := r.Context().Value("user_id").(interface{ String() string }); ok {
				fields = append(fields, zap.String("user_id", userID.String()))
			}

			// Log at appropriate level
			switch {
			case wrapped.statusCode >= 500:
				logEvent.Error("server error", fields...)
			case wrapped.statusCode >= 400:
				logEvent.Warn("client error", fields...)
			case duration > 1*time.Second:
				logEvent.Warn("slow request", fields...)
			default:
				logEvent.Info("request", fields...)
			}
		})
	}
}

// ============================================================================
// RECOVERER MIDDLEWARE
// ============================================================================

// Recoverer catches panics and returns a 500 error instead of crashing
func Recoverer(logger *zap.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = zap.NewNop()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					stack := debug.Stack()
					requestID := GetRequestID(r.Context())

					logger.Error("panic recovered",
						zap.Any("panic", rec),
						zap.String("stack", string(stack)),
						zap.String("request_id", requestID),
						zap.String("method", r.Method),
						zap.String("path", r.URL.Path),
					)

					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"error":{"code":"INTERNAL_ERROR","message":"internal server error"}}`))
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// ============================================================================
// TIMEOUT MIDDLEWARE
// ============================================================================

// Timeout applies a context timeout to the request
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()

			r = r.WithContext(ctx)

			// Set up done channel to detect timeout
			done := make(chan struct{})
			tw := newTimeoutWriter(w)

			go func() {
				next.ServeHTTP(tw, r)
				close(done)
			}()

			select {
			case <-done:
				// Request completed normally
				tw.mu.Lock()
				defer tw.mu.Unlock()
				tw.flush()
			case <-ctx.Done():
				tw.mu.Lock()
				defer tw.mu.Unlock()
				tw.timedOut = true
				if !tw.wroteHeader {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusRequestTimeout)
					_, _ = w.Write([]byte(`{"error":{"code":"TIMEOUT","message":"request timeout"}}`))
				}
			}
		})
	}
}

// timeoutWriter prevents writes after timeout
type timeoutWriter struct {
	w           http.ResponseWriter
	mu          sync.Mutex
	timedOut    bool
	wroteHeader bool
}

func newTimeoutWriter(w http.ResponseWriter) *timeoutWriter {
	return &timeoutWriter{w: w}
}

func (tw *timeoutWriter) Header() http.Header {
	return tw.w.Header()
}

func (tw *timeoutWriter) WriteHeader(code int) {
	if tw.timedOut || tw.wroteHeader {
		return
	}
	tw.wroteHeader = true
	tw.w.WriteHeader(code)
}

func (tw *timeoutWriter) Write(b []byte) (int, error) {
	if tw.timedOut {
		return 0, errors.New("timeout")
	}
	tw.WriteHeader(http.StatusOK)
	return tw.w.Write(b)
}

func (tw *timeoutWriter) flush() {
	// No-op for now; could flush buffered data if needed
}

// ============================================================================
// RATE LIMITER MIDDLEWARE
// ============================================================================

// ipLimiter holds rate limiters per IP
type ipLimiter struct {
	limiters sync.Map
	rate     rate.Limit
	burst    int
}

func newIPLimiter(requestsPerMinute int) *ipLimiter {
	r := rate.Limit(float64(requestsPerMinute) / 60.0)
	return &ipLimiter{
		rate:  r,
		burst: requestsPerMinute,
	}
}

func (l *ipLimiter) getLimiter(ip string) *rate.Limiter {
	if v, exists := l.limiters.Load(ip); exists {
		return v.(*rate.Limiter)
	}
	limiter := rate.NewLimiter(l.rate, l.burst)
	actual, _ := l.limiters.LoadOrStore(ip, limiter)
	return actual.(*rate.Limiter)
}

// adjustBudgetToMinute normalizes a budget expressed over an arbitrary window
// into requests-per-minute so the underlying token-bucket stays consistent.
func adjustBudgetToMinute(budget int, window time.Duration) int {
	if window == time.Minute {
		return budget
	}
	perMinute := float64(budget) * float64(time.Minute) / float64(window)
	if perMinute < 1 {
		return 1
	}
	return int(perMinute)
}

// RateLimit creates a per-IP rate limiter.
// The first argument is the request budget; the optional second argument is
// the window duration (defaults to one minute).
func RateLimit(requestsPerMinute int, window ...time.Duration) func(http.Handler) http.Handler {
	if len(window) > 0 && window[0] > 0 {
		requestsPerMinute = adjustBudgetToMinute(requestsPerMinute, window[0])
	}
	limiter := newIPLimiter(requestsPerMinute)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract IP (handle proxies)
			ip := extractIP(r)

			lim := limiter.getLimiter(ip)
			if !lim.Allow() {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", requestsPerMinute))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":"RATE_LIMITED","message":"too many requests"}}`))
				return
			}

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", requestsPerMinute))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", int(lim.Tokens())))

			next.ServeHTTP(w, r)
		})
	}
}

func extractIP(r *http.Request) string {
	// Check X-Forwarded-For first (for proxied requests)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take first IP in chain
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr (strip port)
	ip := r.RemoteAddr
	for i := len(ip) - 1; i >= 0; i-- {
		if ip[i] == ':' {
			return ip[:i]
		}
	}
	return ip
}

// ============================================================================
// CORS MIDDLEWARE (simple version; use chi/cors for complex cases)
// ============================================================================

// CORS adds CORS headers
func CORS(origins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			allowed := false
			for _, o := range origins {
				if o == "*" || o == origin {
					allowed = true
					break
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-Request-ID")
				w.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, X-RateLimit-Remaining")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			// Handle preflight
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ============================================================================
// CONTENT TYPE MIDDLEWARE
// ============================================================================

// ContentType sets the Content-Type header for JSON responses
func ContentType(contentType string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			next.ServeHTTP(w, r)
		})
	}
}

// ============================================================================
// SECURITY HEADERS MIDDLEWARE
// ============================================================================

// SecurityHeaders adds common security headers
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		next.ServeHTTP(w, r)
	})
}
