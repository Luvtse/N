package commands

import (
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// pgtextScanner scans a possibly-NULL text column into a plain Go string
// (NULL -> "") so nullable provider references never fail a row scan.
type pgtextScanner struct {
	s string
}

// Scan implements sql.Scanner; pgx calls it with the raw column value.
func (o *pgtextScanner) Scan(src any) error {
	if src == nil {
		o.s = ""
		return nil
	}
	switch v := src.(type) {
	case string:
		o.s = v
	case []byte:
		o.s = string(v)
	default:
		return errors.New("pgtextScanner: unsupported source type")
	}
	return nil
}

// pgtimeScanner captures a possibly-NULL timestamptz as *time.Time.
type pgtimeScanner struct {
	p *time.Time
}

// Scan implements sql.Scanner.
func (o *pgtimeScanner) Scan(src any) error {
	if src == nil {
		o.p = nil
		return nil
	}
	switch v := src.(type) {
	case time.Time:
		t := v.UTC()
		o.p = &t
	default:
		return errors.New("pgtimeScanner: unsupported source type")
	}
	return nil
}

// isCheckViolation detects PostgreSQL SQLSTATE 23514 (CHECK constraint
// violation), including when the shared database wrapper has already wrapped
// the raw *pgconn.PgError. Used by the refund claim path so an over-refund
// attempt blocked by the migration-016 CHECK surfaces as a typed business
// error rather than an opaque infrastructure failure.
func isCheckViolation(err error) bool {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "23514"
	}
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 23514")
}

// isUndefinedTable detects PostgreSQL SQLSTATE 42P07 (defined table already
// exists) / 42501 insufficient privilege — the fault-injection probe in the
// refund finalize path treats any error that is NOT "relation does not exist"
// (42P01) as a real infrastructure failure.
func isUndefinedTable(err error) bool {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code == "42P01"
	}
	return err != nil && strings.Contains(err.Error(), "SQLSTATE 42P01")
}
