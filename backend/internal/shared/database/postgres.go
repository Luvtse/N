package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/tracelog"
	"go.uber.org/zap"
)

// ============================================================================
// ERRORS
// ============================================================================

var (
	ErrNoRows            = errors.New("no rows in result set")
	ErrConnectionFailed  = errors.New("failed to connect to database")
	ErrQueryFailed       = errors.New("query execution failed")
	ErrTransactionFailed = errors.New("transaction failed")
)

// ============================================================================
// CONFIGURATION
// ============================================================================

// Config holds database connection configuration
type Config struct {
	// Connection
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string // disable, require, verify-ca, verify-full

	// Connection Pool
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	HealthCheckFreq time.Duration

	// Timeouts
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration

	// Logging
	LogLevel string // silent, error, warn, info, debug, trace
}

// DefaultConfig returns sensible defaults.
// Phase B/B9: no password default is embedded — callers MUST supply one via
// configuration/env. An empty password fails fast at connect time rather than
// silently trying a guessable credential.
func DefaultConfig() *Config {
	return &Config{
		Host:            "localhost",
		Port:            5432,
		User:            "nidaw",
		Password:        "", // intentionally no default (B9)
		DBName:          "nidaw",
		SSLMode:         "disable",
		MaxConns:        50,
		MinConns:        5,
		MaxConnLifetime: 30 * time.Minute,
		MaxConnIdleTime: 5 * time.Minute,
		HealthCheckFreq: 30 * time.Second,
		ConnectTimeout:  10 * time.Second,
		QueryTimeout:    30 * time.Second,
		LogLevel:        "warn",
	}
}

// ConnectionString builds the PostgreSQL connection string
func (c *Config) ConnectionString() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s&connect_timeout=%d",
		c.User, c.Password, c.Host, c.Port, c.DBName, c.SSLMode,
		int(c.ConnectTimeout.Seconds()),
	)
}

// ============================================================================
// POSTGRES CLIENT
// ============================================================================

// Postgres is the main database client wrapping pgxpool
type Postgres struct {
	pool   *pgxpool.Pool
	config *Config
	logger *zap.Logger
}

// NewPostgres creates a new database connection pool
func NewPostgres(cfg *Config, logger *zap.Logger) (*Postgres, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	// Parse pool config
	poolCfg, err := pgxpool.ParseConfig(cfg.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnectionFailed, err)
	}

	// Configure pool
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolCfg.HealthCheckPeriod = cfg.HealthCheckFreq

	// Configure connection tracer for query logging
	tracerLogLevel := mapLogLevel(cfg.LogLevel)
	poolCfg.ConnConfig.Tracer = &tracelog.TraceLog{
		Logger:   &pgxLogger{logger: logger},
		LogLevel: tracerLogLevel,
	}

	// Configure custom type mappings (UUID, JSONB, etc.)
	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		// pgx v5 refuses to scan SQL NULL directly into a plain Go string
		// (it only accepts the typed-NULL wrappers). Several legacy columns
		// are nullable text (e.g. transactions.description), so install a
		// NULL-tolerant codec for every text-ish OID on this connection's
		// type map. This hardens all codec-mediated scan paths (typed
		// wrappers such as sql.NullString/*pgtype.Text and array element
		// planning). Plain *string destinations are served by pgmap's
		// hardcoded fast path that no codec can intercept — production code
		// must scan nullable text via ScanStringOrNull / QueryNullableStrings
		// (see nullable_text_codec.go); the live probe test documents this.
		// We override the Codec on per-connection *Type registrations rather
		// than calling RegisterType with duplicate names — registering three
		// types under the same name would corrupt the map's name index — and
		// we never mutate the process-wide builtin entries (that left stale
		// memoized scan plans in play; verified by
		// TestPostgresScansNullTextIntoString).
		m := conn.TypeMap()
		for _, oid := range []uint32{
			25,   // text
			1042, // bpchar
			1043, // varchar
		} {
			codec := nullableTextCodec{inner: &pgtype.TextCodec{}}
			name := fmt.Sprintf("nullable_text_%d", oid)
			if existing, ok := m.TypeForOID(oid); ok {
				name = existing.Name + "_nullable"
			}
			m.RegisterType(&pgtype.Type{Name: name, OID: oid, Codec: codec})
		}
		return nil
	}

	// Create pool with context timeout
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConnectionFailed, err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("%w: ping failed: %v", ErrConnectionFailed, err)
	}

	logger.Info("database connection established",
		zap.String("host", cfg.Host),
		zap.Int("port", cfg.Port),
		zap.String("database", cfg.DBName),
		zap.Int32("max_conns", cfg.MaxConns),
	)

	return &Postgres{
		pool:   pool,
		config: cfg,
		logger: logger,
	}, nil
}

// ============================================================================
// QUERY METHODS (Context-aware, with timeouts)
// ============================================================================

// ErrDatabaseClosed is returned by query methods when the client was never
// initialized (nil pool/config), e.g. in unit tests using a zero-value
// Postgres to simulate an unreachable database. It prevents nil-pointer
// panics from propagating through background workers.
var ErrDatabaseClosed = errors.New("database client not initialized")

// ready reports whether the pool and config are usable.
func (p *Postgres) ready() bool { return p != nil && p.pool != nil && p.config != nil }

// IsReady reports whether the pool is initialized. Exported for module
// handlers that must degrade explicitly when the database is unavailable
// instead of letting queries fail deep inside pgx.
func (p *Postgres) IsReady() bool { return p.ready() }

// Query executes a query that returns multiple rows
func (p *Postgres) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	if !p.ready() {
		return nil, fmt.Errorf("%w: %v", ErrQueryFailed, ErrDatabaseClosed)
	}
	ctx, cancel := context.WithTimeout(ctx, p.config.QueryTimeout)
	defer cancel()

	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		p.logger.Error("query failed",
			zap.String("sql", sql),
			zap.Error(err),
		)
		return nil, fmt.Errorf("%w: %v", ErrQueryFailed, err)
	}

	return rows, nil
}

// QueryRow executes a query that returns a single row.
// When the client is uninitialized it returns a row whose Scan yields an
// error rather than panicking, so background workers stay crash-safe.
func (p *Postgres) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if !p.ready() {
		return errRow{err: fmt.Errorf("%w: %v", ErrQueryFailed, ErrDatabaseClosed)}
	}
	ctx, cancel := context.WithTimeout(ctx, p.config.QueryTimeout)
	// Note: cancel is called when the row is scanned or context expires
	_ = cancel

	return p.pool.QueryRow(ctx, sql, args...)
}

// errRow is a pgx.Row that always fails to scan.
type errRow struct{ err error }

func (e errRow) Scan(dest ...any) error { return e.err }

func (e errRow) Err() error { return e.err }

// Exec executes a query that doesn't return rows (INSERT, UPDATE, DELETE)
func (p *Postgres) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	if !p.ready() {
		return pgconn.CommandTag{}, fmt.Errorf("%w: %v", ErrQueryFailed, ErrDatabaseClosed)
	}
	ctx, cancel := context.WithTimeout(ctx, p.config.QueryTimeout)
	defer cancel()

	tag, err := p.pool.Exec(ctx, sql, args...)
	if err != nil {
		p.logger.Error("exec failed",
			zap.String("sql", sql),
			zap.Error(err),
		)
		return pgconn.CommandTag{}, fmt.Errorf("%w: %v", ErrQueryFailed, err)
	}

	return tag, nil
}

// ============================================================================
// TRANSACTION SUPPORT
// ============================================================================

// Tx represents a database transaction
type Tx struct {
	tx     pgx.Tx
	logger *zap.Logger
}

// Begin starts a new transaction
func (p *Postgres) Begin(ctx context.Context) (*Tx, error) {
	if !p.ready() {
		return nil, fmt.Errorf("%w: %v", ErrTransactionFailed, ErrDatabaseClosed)
	}
	ctx, cancel := context.WithTimeout(ctx, p.config.QueryTimeout)
	defer cancel()

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTransactionFailed, err)
	}

	return &Tx{tx: tx, logger: p.logger}, nil
}

// Commit commits the transaction
func (t *Tx) Commit(ctx context.Context) error {
	if err := t.tx.Commit(ctx); err != nil {
		t.logger.Error("transaction commit failed", zap.Error(err))
		return fmt.Errorf("%w: commit: %v", ErrTransactionFailed, err)
	}
	return nil
}

// Rollback rolls back the transaction
func (t *Tx) Rollback(ctx context.Context) error {
	if err := t.tx.Rollback(ctx); err != nil {
		t.logger.Error("transaction rollback failed", zap.Error(err))
		return fmt.Errorf("%w: rollback: %v", ErrTransactionFailed, err)
	}
	return nil
}

// Query executes a query within the transaction
func (t *Tx) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return t.tx.Query(ctx, sql, args...)
}

// QueryRow executes a single-row query within the transaction
func (t *Tx) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return t.tx.QueryRow(ctx, sql, args...)
}

// Exec executes a statement within the transaction
func (t *Tx) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return t.tx.Exec(ctx, sql, args...)
}

// ============================================================================
// HEALTH CHECK & MONITORING
// ============================================================================

// Ping verifies the database is reachable
func (p *Postgres) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return p.pool.Ping(ctx)
}

// Stats returns pool statistics for monitoring
func (p *Postgres) Stats() *pgxpool.Stat {
	return p.pool.Stat()
}

// Pool exposes the underlying pgx connection pool to modules that require
// native pgx semantics (e.g. the ledger module's SELECT ... FOR UPDATE NOWAIT
// transactions, Phase D Step 4). Callers must NOT close the returned pool;
// its lifecycle belongs to Postgres.Close.
func (p *Postgres) Pool() *pgxpool.Pool {
	return p.pool
}

// Close gracefully closes the connection pool
func (p *Postgres) Close() {
	if p.pool != nil {
		p.pool.Close()
		p.logger.Info("database connection pool closed")
	}
}

// ============================================================================
// HELPER METHODS
// ============================================================================

// WithTx executes a function within a transaction, handling commit/rollback
func (p *Postgres) WithTx(ctx context.Context, fn func(tx *Tx) error) error {
	tx, err := p.Begin(ctx)
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			p.logger.Error("rollback failed after error",
				zap.Error(rbErr),
				zap.Error(err),
			)
		}
		return err
	}

	return tx.Commit(ctx)
}

// ============================================================================
// INTERNAL HELPERS
// ============================================================================

func mapLogLevel(level string) tracelog.LogLevel {
	switch level {
	case "silent":
		return tracelog.LogLevelNone
	case "error":
		return tracelog.LogLevelError
	case "warn":
		return tracelog.LogLevelWarn
	case "info":
		return tracelog.LogLevelInfo
	case "debug":
		return tracelog.LogLevelDebug
	case "trace":
		return tracelog.LogLevelTrace
	default:
		return tracelog.LogLevelWarn
	}
}

// pgxLogger adapts zap.Logger to pgx's logger interface
type pgxLogger struct {
	logger *zap.Logger
}

func (l *pgxLogger) Log(ctx context.Context, level tracelog.LogLevel, msg string, data map[string]interface{}) {
	fields := make([]zap.Field, 0, len(data)+1)
	fields = append(fields, zap.String("component", "pgx"))

	for k, v := range data {
		fields = append(fields, zap.Any(k, v))
	}

	switch level {
	case tracelog.LogLevelError:
		l.logger.Error(msg, fields...)
	case tracelog.LogLevelWarn:
		l.logger.Warn(msg, fields...)
	case tracelog.LogLevelInfo:
		l.logger.Info(msg, fields...)
	case tracelog.LogLevelDebug, tracelog.LogLevelTrace:
		l.logger.Debug(msg, fields...)
	}
}
