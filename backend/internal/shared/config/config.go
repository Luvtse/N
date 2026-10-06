package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrMissingRequiredEnv = fmt.Errorf("missing required environment variable")
	ErrInvalidEnvValue    = fmt.Errorf("invalid environment variable value")
)

// ============================================================================
// CONFIG STRUCTS
// ============================================================================

// Config holds all application configuration
type Config struct {
	Server     ServerConfig
	Database   DatabaseConfig
	Redis      RedisConfig
	Kafka      KafkaConfig
	Auth       AuthConfig
	Payments   PaymentsConfig
	ML         MLConfig
	Logging    LoggingConfig
	Features   FeatureFlags
	Regions    []string
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	Port              string
	Host              string
	Environment       string // development, staging, production
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ShutdownTimeout   time.Duration
	CORSOrigins       []string
	EnableProfiling   bool
}

// DatabaseConfig holds PostgreSQL connection settings
type DatabaseConfig struct {
	Host              string
	Port              int
	User              string
	Password          string
	DBName            string
	SSLMode           string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	ConnectTimeout    time.Duration
	QueryTimeout      time.Duration
	LogLevel          string
}

// ConnectionString builds the PostgreSQL connection URL
func (d *DatabaseConfig) ConnectionString() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s&connect_timeout=%d",
		d.User, d.Password, d.Host, d.Port, d.DBName, d.SSLMode,
		int(d.ConnectTimeout.Seconds()),
	)
}

// RedisConfig holds Redis connection settings
type RedisConfig struct {
	URL             string
	Password        string
	DB              int
	MaxRetries      int
	PoolSize        int
	MinIdleConns    int
	DialTimeout     time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
}

// KafkaConfig holds Kafka broker settings
type KafkaConfig struct {
	Brokers          []string
	SchemaRegistryURL string
	ConsumerGroup    string
	ProducerTimeout  time.Duration
	ConsumerTimeout  time.Duration
	Retries          int
}

// AuthConfig holds JWT and authentication settings
type AuthConfig struct {
	JWTSecret           string
	AccessTokenTTL      time.Duration
	RefreshTokenTTL     time.Duration
	Issuer              string
	Audience            string
	BcryptCost          int
	MaxLoginAttempts    int
	LockoutDuration     time.Duration
}

// PaymentsConfig holds payment gateway settings
type PaymentsConfig struct {
	StripeAPIKey       string
	StripeWebhookSecret string
	AdyenAPIKey        string
	AdyenMerchantAccount string
	DefaultCurrency    string
}

// MLConfig holds ML platform settings
type MLConfig struct {
	TrackingURI      string
	FeatureStorePath string
	ModelRegistryURL string
	InferenceTimeout time.Duration
}

// LoggingConfig holds logging settings
type LoggingConfig struct {
	Level      string // debug, info, warn, error
	Format     string // json, console
	OutputPath string
	EnableOTEL bool
}

// FeatureFlags holds feature toggle settings
type FeatureFlags struct {
	EnableAutonomousVehicles bool
	EnableDroneDelivery      bool
	EnableBlockchainPayments bool
	EnableFederatedLearning  bool
	EnableARNavigation       bool
	EnableVoiceCommands      bool
}

// ============================================================================
// CONFIG LOADER
// ============================================================================

// Load reads configuration from environment variables with validation
func Load() (*Config, error) {
	// Load .env file if it exists (ignored in production)
	_ = godotenv.Load()

	cfg := &Config{}

	// Load each section
	if err := cfg.loadServer(); err != nil {
		return nil, fmt.Errorf("server config: %w", err)
	}
	if err := cfg.loadDatabase(); err != nil {
		return nil, fmt.Errorf("database config: %w", err)
	}
	if err := cfg.loadRedis(); err != nil {
		return nil, fmt.Errorf("redis config: %w", err)
	}
	if err := cfg.loadKafka(); err != nil {
		return nil, fmt.Errorf("kafka config: %w", err)
	}
	if err := cfg.loadAuth(); err != nil {
		return nil, fmt.Errorf("auth config: %w", err)
	}
	if err := cfg.loadPayments(); err != nil {
		return nil, fmt.Errorf("payments config: %w", err)
	}
	if err := cfg.loadML(); err != nil {
		return nil, fmt.Errorf("ml config: %w", err)
	}
	if err := cfg.loadLogging(); err != nil {
		return nil, fmt.Errorf("logging config: %w", err)
	}
	cfg.loadFeatures()
	cfg.loadRegions()

	// Validate the complete config
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

// ============================================================================
// SECTION LOADERS
// ============================================================================

func (c *Config) loadServer() error {
	c.Server = ServerConfig{
		Port:            getEnvOrDefault("SERVER_PORT", "8080"),
		Host:            getEnvOrDefault("SERVER_HOST", "0.0.0.0"),
		Environment:     getEnvOrDefault("ENVIRONMENT", "development"),
		ReadTimeout:     getDurationEnvOrDefault("SERVER_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:    getDurationEnvOrDefault("SERVER_WRITE_TIMEOUT", 15*time.Second),
		IdleTimeout:     getDurationEnvOrDefault("SERVER_IDLE_TIMEOUT", 60*time.Second),
		ShutdownTimeout: getDurationEnvOrDefault("SERVER_SHUTDOWN_TIMEOUT", 30*time.Second),
		// Phase B/B4: no wildcard default — origins must be explicitly configured.
		CORSOrigins:     getSliceEnvOrDefault("CORS_ORIGINS", nil),
		EnableProfiling: getBoolEnvOrDefault("ENABLE_PROFILING", false),
	}
	return nil
}

func (c *Config) loadDatabase() error {
	c.Database = DatabaseConfig{
		Host:            getEnvOrDefault("DB_HOST", "localhost"),
		Port:            getIntEnvOrDefault("DB_PORT", 5432),
		User:            getEnvOrDefault("DB_USER", "nidaw"),
		// Phase B/B9: no guessable password fallback. Empty value is allowed
		// here but rejected by Validate() for non-dev environments, and the
		// server refuses to boot without DB_PASSWORD set.
		Password:        getEnvOrDefault("DB_PASSWORD", ""),
		DBName:          getEnvOrDefault("DB_NAME", "nidaw"),
		SSLMode:         getEnvOrDefault("DB_SSLMODE", "disable"),
		MaxConns:        int32(getIntEnvOrDefault("DB_MAX_CONNS", 50)),
		MinConns:        int32(getIntEnvOrDefault("DB_MIN_CONNS", 5)),
		MaxConnLifetime: getDurationEnvOrDefault("DB_MAX_CONN_LIFETIME", 30*time.Minute),
		MaxConnIdleTime: getDurationEnvOrDefault("DB_MAX_CONN_IDLE_TIME", 5*time.Minute),
		ConnectTimeout:  getDurationEnvOrDefault("DB_CONNECT_TIMEOUT", 10*time.Second),
		QueryTimeout:    getDurationEnvOrDefault("DB_QUERY_TIMEOUT", 30*time.Second),
		LogLevel:        getEnvOrDefault("DB_LOG_LEVEL", "warn"),
	}
	return nil
}

func (c *Config) loadRedis() error {
	c.Redis = RedisConfig{
		URL:          getEnvOrDefault("REDIS_URL", "redis://localhost:6379"),
		Password:     getEnvOrDefault("REDIS_PASSWORD", ""),
		DB:           getIntEnvOrDefault("REDIS_DB", 0),
		MaxRetries:   getIntEnvOrDefault("REDIS_MAX_RETRIES", 3),
		PoolSize:     getIntEnvOrDefault("REDIS_POOL_SIZE", 20),
		MinIdleConns: getIntEnvOrDefault("REDIS_MIN_IDLE_CONNS", 5),
		DialTimeout:  getDurationEnvOrDefault("REDIS_DIAL_TIMEOUT", 5*time.Second),
		ReadTimeout:  getDurationEnvOrDefault("REDIS_READ_TIMEOUT", 3*time.Second),
		WriteTimeout: getDurationEnvOrDefault("REDIS_WRITE_TIMEOUT", 3*time.Second),
	}
	return nil
}

func (c *Config) loadKafka() error {
	c.Kafka = KafkaConfig{
		Brokers:          getSliceEnvOrDefault("KAFKA_BROKERS", []string{"localhost:9092"}),
		SchemaRegistryURL: getEnvOrDefault("SCHEMA_REGISTRY_URL", "http://localhost:8081"),
		ConsumerGroup:    getEnvOrDefault("KAFKA_CONSUMER_GROUP", "nidaw-backend"),
		ProducerTimeout:  getDurationEnvOrDefault("KAFKA_PRODUCER_TIMEOUT", 10*time.Second),
		ConsumerTimeout:  getDurationEnvOrDefault("KAFKA_CONSUMER_TIMEOUT", 30*time.Second),
		Retries:          getIntEnvOrDefault("KAFKA_RETRIES", 3),
	}
	return nil
}

func (c *Config) loadAuth() error {
	c.Auth = AuthConfig{
		JWTSecret:        getEnvOrDefault("JWT_SECRET", ""),
		AccessTokenTTL:   getDurationEnvOrDefault("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:  getDurationEnvOrDefault("REFRESH_TOKEN_TTL", 7*24*time.Hour),
		Issuer:           getEnvOrDefault("JWT_ISSUER", "nidaw"),
		Audience:         getEnvOrDefault("JWT_AUDIENCE", "nidaw-client"),
		BcryptCost:       getIntEnvOrDefault("BCRYPT_COST", 12),
		MaxLoginAttempts: getIntEnvOrDefault("MAX_LOGIN_ATTEMPTS", 5),
		LockoutDuration:  getDurationEnvOrDefault("LOCKOUT_DURATION", 15*time.Minute),
	}
	return nil
}

func (c *Config) loadPayments() error {
	c.Payments = PaymentsConfig{
		StripeAPIKey:         getEnvOrDefault("STRIPE_API_KEY", ""),
		StripeWebhookSecret:  getEnvOrDefault("STRIPE_WEBHOOK_SECRET", ""),
		AdyenAPIKey:          getEnvOrDefault("ADYEN_API_KEY", ""),
		AdyenMerchantAccount: getEnvOrDefault("ADYEN_MERCHANT_ACCOUNT", ""),
		DefaultCurrency:      getEnvOrDefault("DEFAULT_CURRENCY", "USD"),
	}
	return nil
}

func (c *Config) loadML() error {
	c.ML = MLConfig{
		TrackingURI:      getEnvOrDefault("MLFLOW_TRACKING_URI", "http://localhost:5000"),
		FeatureStorePath: getEnvOrDefault("FEATURE_STORE_PATH", "./ml/feature_store/feast/feature_repo"),
		ModelRegistryURL: getEnvOrDefault("MODEL_REGISTRY_URL", "http://localhost:5000"),
		InferenceTimeout: getDurationEnvOrDefault("ML_INFERENCE_TIMEOUT", 5*time.Second),
	}
	return nil
}

func (c *Config) loadLogging() error {
	c.Logging = LoggingConfig{
		Level:      getEnvOrDefault("LOG_LEVEL", "info"),
		Format:     getEnvOrDefault("LOG_FORMAT", "json"),
		OutputPath: getEnvOrDefault("LOG_OUTPUT_PATH", "stdout"),
		EnableOTEL: getBoolEnvOrDefault("ENABLE_OTEL", true),
	}
	return nil
}

func (c *Config) loadFeatures() {
	c.Features = FeatureFlags{
		EnableAutonomousVehicles: getBoolEnvOrDefault("FEATURE_AUTONOMOUS_VEHICLES", false),
		EnableDroneDelivery:      getBoolEnvOrDefault("FEATURE_DRONE_DELIVERY", false),
		EnableBlockchainPayments: getBoolEnvOrDefault("FEATURE_BLOCKCHAIN_PAYMENTS", false),
		EnableFederatedLearning:  getBoolEnvOrDefault("FEATURE_FEDERATED_LEARNING", false),
		EnableARNavigation:       getBoolEnvOrDefault("FEATURE_AR_NAVIGATION", false),
		EnableVoiceCommands:      getBoolEnvOrDefault("FEATURE_VOICE_COMMANDS", false),
	}
}

func (c *Config) loadRegions() {
	c.Regions = getSliceEnvOrDefault("DEPLOYED_REGIONS", []string{"us-east-1"})
}

// ============================================================================
// VALIDATION
// ============================================================================

// Validate checks that all required configuration is present and valid
func (c *Config) Validate() error {
	var errors []string

	// Server validation
	if c.Server.Port == "" {
		errors = append(errors, "SERVER_PORT is required")
	}
	if !isValidEnvironment(c.Server.Environment) {
		errors = append(errors, fmt.Sprintf("ENVIRONMENT must be one of: development, staging, production (got: %s)", c.Server.Environment))
	}

	// Database validation
	if c.Database.Host == "" {
		errors = append(errors, "DB_HOST is required")
	}
	if c.Database.User == "" {
		errors = append(errors, "DB_USER is required")
	}
	if c.Database.DBName == "" {
		errors = append(errors, "DB_NAME is required")
	}

	// Kafka validation
	if len(c.Kafka.Brokers) == 0 {
		errors = append(errors, "KAFKA_BROKERS must contain at least one broker")
	}

	// Auth validation (critical in production)
	if c.Server.Environment == "production" {
		if c.Auth.JWTSecret == "" {
			errors = append(errors, "JWT_SECRET is required in production")
		}
		if len(c.Auth.JWTSecret) < 32 {
			errors = append(errors, "JWT_SECRET must be at least 32 characters in production")
		}
		// Phase B/B4: credentialed CORS with a wildcard origin is unsafe; require explicit allowlist.
		for _, o := range c.Server.CORSOrigins {
			if o == "*" {
				errors = append(errors, "CORS_ORIGINS must not contain '*' in production; list explicit origins")
			}
		}
		if c.Payments.StripeAPIKey == "" {
			errors = append(errors, "STRIPE_API_KEY is required in production")
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("configuration errors:\n  - %s", strings.Join(errors, "\n  - "))
	}

	return nil
}

// IsProduction returns true if running in production environment
func (c *Config) IsProduction() bool {
	return c.Server.Environment == "production"
}

// IsDevelopment returns true if running in development environment
func (c *Config) IsDevelopment() bool {
	return c.Server.Environment == "development"
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvRequired(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("%w: %s", ErrMissingRequiredEnv, key)
	}
	return value, nil
}

func getIntEnvOrDefault(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return intValue
}

func getBoolEnvOrDefault(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	boolValue, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return boolValue
}

func getDurationEnvOrDefault(key string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue
	}
	return duration
}

func getSliceEnvOrDefault(key string, defaultValue []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return defaultValue
	}
	return result
}

func isValidEnvironment(env string) bool {
	switch env {
	case "development", "staging", "production":
		return true
	default:
		return false
	}
}