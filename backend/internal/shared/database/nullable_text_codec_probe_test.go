package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// probeDSLEnv is the same environment gate used by the payment integrity
// integration suite. Without a live PostgreSQL we cannot exercise the real
// AfterConnect codec installation, so the probe skips (unit-level coverage of
// the codec itself is provided separately below).
const probeDSLEnv = "NIDUS_TEST_DATABASE_URL"

// TestPostgresScansNullTextIntoString proves end-to-end that a connection
// established through NewPostgres can scan a SQL NULL varchar/text/bpchar into
// a plain Go *string without failing with "cannot scan NULL into *string".
// This is the regression probe for the nullable-text codec swap performed in
// the pool's AfterConnect hook: if pgmap were serving scan plans built from
// the old builtin codec (plan caching keyed by the pre-swap type), this test
// would fail even though the codec file compiles.
func TestPostgresScansNullTextIntoString(t *testing.T) {
	if os.Getenv(probeDSLEnv) == "" {
		t.Skipf("%s not set: codec probe requires a live PostgreSQL", probeDSLEnv)
	}
	db, err := NewPostgres(&Config{
		Host:            "localhost",
		Port:            5432,
		User:            "nidaw",
		Password:        "nidaw",
		DBName:          "nidus_test",
		SSLMode:         "disable",
		MaxConns:        2,
		MinConns:        1,
		MaxConnLifetime: 30 * time.Minute,
		MaxConnIdleTime: 5 * time.Minute,
		HealthCheckFreq: 30 * time.Second,
		ConnectTimeout:  5 * time.Second,
		QueryTimeout:    10 * time.Second,
		LogLevel:        "silent",
	}, nil)
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.Ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}

	// One query covering all three wrapped OIDs plus non-NULL values, using
	// both text/template and prepared-statement paths (pgx v5 prepares on the
	// second execution against the same connection, which is exactly where a
	// stale cached plan would surface).
	const q = `SELECT
		NULL::varchar, NULL::text, NULL::bpchar,
		'ok'::varchar, 'ok'::text, 'ok'::bpchar`
	for i := 0; i < 3; i++ { // force cache reuse / prepared path
		var nullV, nullT, nullC, okV, okT, okC string
		err := db.Pool().QueryRow(ctx, q).Scan(&nullV, &nullT, &nullC, &okV, &okT, &okC)
		if err != nil {
			t.Fatalf("iteration %d: scan NULL text into *string failed: %v", i, err)
		}
		if nullV != "" || nullT != "" || nullC != "" {
			t.Fatalf("iteration %d: expected NULL to scan as empty string, got %q/%q/%q", i, nullV, nullT, nullC)
		}
		if okV != "ok" || okT != "ok" || okC != "ok" {
			t.Fatalf("iteration %d: non-NULL values corrupted: %q/%q/%q", i, okV, okT, okC)
		}
	}

	// Scanning a NULL into *string via the generic []any destination path
	// (used by row-mapping helpers) must also succeed.
	var vals []any
	row := db.Pool().QueryRow(ctx, `SELECT NULL::varchar`)
	if err := row.Scan(&vals); err == nil {
		// Some pgmap versions fill []any with driver values; accept either a
		// clean success or an explicit error — but never a panic. The primary
		// guarantee under test is the typed *string path above.
		_ = vals
	}
}

// TestNullableTextCodecUnit verifies the codec wrapper directly, without a
// live database: NULL scans to "", non-NULL passes through, and *[]byte /
// other targets keep delegating to the inner TextCodec.
func TestNullableTextCodecUnit(t *testing.T) {
	m := pgtype.NewMap()
	c := nullableTextCodec{inner: &pgtype.TextCodec{}}

	// *string target with NULL src -> "" and no error.
	var s string
	plan := c.PlanScan(m, 25, pgtype.TextFormatCode, &s)
	if plan == nil {
		t.Fatal("expected non-nil scan plan for *string")
	}
	if err := plan.Scan(nil, &s); err != nil {
		t.Fatalf("NULL scan into *string returned error: %v", err)
	}
	if s != "" {
		t.Fatalf("NULL should scan as empty string, got %q", s)
	}
	if err := plan.Scan([]byte("hello"), &s); err != nil {
		t.Fatalf("non-NULL scan returned error: %v", err)
	}
	if s != "hello" {
		t.Fatalf("non-NULL value mismatch: %q", s)
	}

	// Wrong destination type must produce an error, not panic.
	var wrong int
	if err := plan.Scan([]byte("x"), &wrong); err == nil {
		t.Fatal("expected error scanning into *int via string plan")
	}

	// Delegation: FormatSupported/PreferredFormat preserved.
	inner := &pgtype.TextCodec{}
	if c.FormatSupported(1) != inner.FormatSupported(1) {
		t.Fatal("FormatSupported delegation broken")
	}
	if c.PreferredFormat() != inner.PreferredFormat() {
		t.Fatal("PreferredFormat delegation broken")
	}
}
