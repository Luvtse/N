package logger

import (
	"context"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ============================================================================
// CONTEXT KEYS
// ============================================================================

type contextKey string

const (
	RequestIDKey contextKey = "request_id"
	UserIDKey    contextKey = "user_id"
	TenantIDKey  contextKey = "tenant_id"
	TraceIDKey   contextKey = "trace_id"
)

// ============================================================================
// CONFIGURATION
// ============================================================================

// Config holds logger configuration
type Config struct {
	Level      string // debug, info, warn, error
	Format     string // json, console
	OutputPath string // stdout, stderr, or file path
	EnableOTEL bool   // OpenTelemetry integration
}

// DefaultConfig returns sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Level:      "info",
		Format:     "json",
		OutputPath: "stdout",
		EnableOTEL: true,
	}
}

// ============================================================================
// LOGGER INITIALIZATION
// ============================================================================

// New creates a new structured logger
func New(cfg *Config) (*zap.Logger, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Parse log level
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		level = zapcore.InfoLevel
	}

	// Configure encoder
	var encoder zapcore.Encoder
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	encoderConfig.EncodeDuration = zapcore.SecondsDurationEncoder
	encoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	if cfg.Format == "console" {
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	// Configure output
	var output zapcore.WriteSyncer
	switch cfg.OutputPath {
	case "stdout":
		output = zapcore.AddSync(os.Stdout)
	case "stderr":
		output = zapcore.AddSync(os.Stderr)
	default:
		file, err := os.OpenFile(cfg.OutputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		output = zapcore.AddSync(file)
	}

	// Create core
	core := zapcore.NewCore(encoder, output, level)

	// Create logger with options
	logger := zap.New(core,
		zap.AddCaller(),
		zap.AddCallerSkip(1),
		zap.AddStacktrace(zapcore.ErrorLevel),
	)

	return logger, nil
}

// MustNew creates a logger or panics
func MustNew(cfg *Config) *zap.Logger {
	logger, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return logger
}

// ============================================================================
// CONTEXT-AWARE LOGGING
// ============================================================================

// WithContext adds context fields to logger
func WithContext(ctx context.Context, logger *zap.Logger) *zap.Logger {
	fields := []zap.Field{}

	if requestID, ok := ctx.Value(RequestIDKey).(string); ok {
		fields = append(fields, zap.String("request_id", requestID))
	}
	if userID, ok := ctx.Value(UserIDKey).(string); ok {
		fields = append(fields, zap.String("user_id", userID))
	}
	if tenantID, ok := ctx.Value(TenantIDKey).(string); ok {
		fields = append(fields, zap.String("tenant_id", tenantID))
	}
	if traceID, ok := ctx.Value(TraceIDKey).(string); ok {
		fields = append(fields, zap.String("trace_id", traceID))
	}

	return logger.With(fields...)
}

// ============================================================================
// CONVENIENCE FUNCTIONS
// ============================================================================

// Info logs at info level with context
func Info(ctx context.Context, logger *zap.Logger, msg string, fields ...zap.Field) {
	WithContext(ctx, logger).Info(msg, fields...)
}

// Error logs at error level with context
func Error(ctx context.Context, logger *zap.Logger, msg string, fields ...zap.Field) {
	WithContext(ctx, logger).Error(msg, fields...)
}

// Warn logs at warn level with context
func Warn(ctx context.Context, logger *zap.Logger, msg string, fields ...zap.Field) {
	WithContext(ctx, logger).Warn(msg, fields...)
}

// Debug logs at debug level with context
func Debug(ctx context.Context, logger *zap.Logger, msg string, fields ...zap.Field) {
	WithContext(ctx, logger).Debug(msg, fields...)
}

// ============================================================================
// PERFORMANCE LOGGING
// ============================================================================

// MeasureDuration logs the duration of an operation
func MeasureDuration(logger *zap.Logger, operation string, start time.Time) {
	duration := time.Since(start)
	logger.Info("operation completed",
		zap.String("operation", operation),
		zap.Duration("duration", duration),
		zap.Float64("duration_ms", float64(duration.Milliseconds())),
	)
}

// ============================================================================
// HTTP REQUEST LOGGING
// ============================================================================

// HTTPRequest logs HTTP request details
func HTTPRequest(logger *zap.Logger, method, path string, status int, duration time.Duration, requestID string) {
	logger.Info("http_request",
		zap.String("method", method),
		zap.String("path", path),
		zap.Int("status", status),
		zap.Duration("duration", duration),
		zap.String("request_id", requestID),
	)
}

// ============================================================================
// ERROR LOGGING
// ============================================================================

// LogError logs an error with optional context
func LogError(ctx context.Context, logger *zap.Logger, err error, msg string, fields ...zap.Field) {
	allFields := append([]zap.Field{zap.Error(err)}, fields...)
	WithContext(ctx, logger).Error(msg, allFields...)
}

// ============================================================================
// AUDIT LOGGING
// ============================================================================

// Audit logs security-relevant events
func Audit(ctx context.Context, logger *zap.Logger, action, resource string, fields ...zap.Field) {
	allFields := []zap.Field{
		zap.String("audit_action", action),
		zap.String("audit_resource", resource),
	}
	allFields = append(allFields, fields...)
	WithContext(ctx, logger).Info("audit_event", allFields...)
}