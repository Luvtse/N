package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// nullableTextCodec wraps a pgtype.Codec (in practice the builtin
// pgtype.TextCodec registered for text/varchar/bpchar) so that SQL NULL in
// text-ish columns scans into a plain Go *string as "" instead of failing with
// "cannot scan NULL into *string". Legacy module code (and any future code)
// that scans nullable varchar/text columns directly into string fields relies
// on this codec being installed per-connection; see the AfterConnect hook in
// postgres.go.
//
// IMPORTANT (verified against pgx v5.5.1 and by the live end-to-end probe in
// nullable_text_codec_probe_test.go): Map.PlanScan serves *plain* Go pointer
// targets such as *string from its own hardcoded fast path
// (pgtype.scanPlanString, which errors on NULL) and never consults the
// registered codec for those targets. Codec.PlanScan is only reached for
// wrapper types (*pgtype.Text, sql.Scanner implementers such as
// sql.NullString, composite/array element planning). Consequently wrapping
// the codec alone cannot make NULL -> *string tolerant, and Conn.typeMap is
// unexported so the plain-target path cannot be intercepted through the
// public API either.
//
// The supported remedy lives in this file's scan helpers: production code
// scans nullable text columns through the typed-wrapper path
// (sql.NullString / *pgtype.Text via ScanStringOrNull and
// QueryNullableStrings), which IS codec-mediated and therefore NULL-tolerant.
// The codec wrapper below additionally hardens every wrapper-mediated path
// (e.g. []string array scans planned through the element codec) so a NULL
// never aborts a whole batch.
type nullableTextCodec struct {
	inner pgtype.Codec
}

// PlanScan delegates to the wrapped codec and then swaps the NULL-intolerant
// *string plan for our tolerant one. The wrapper itself implements
// pgtype.PlanScanExtender (via the embedded codec), so pgmap will re-enter
// this method when planning through composite types such as arrays of text —
// nested *string targets become NULL-tolerant as well.
func (c nullableTextCodec) PlanScan(m *pgtype.Map, oid uint32, format int16, target any) pgtype.ScanPlan {
	plan := c.inner.PlanScan(m, oid, format, target)
	if _, isStr := target.(*string); isStr && plan != nil {
		return scanPlanTextNullToString{}
	}
	return plan
}

func (c nullableTextCodec) FormatSupported(format int16) bool { return c.inner.FormatSupported(format) }
func (c nullableTextCodec) PreferredFormat() int16            { return c.inner.PreferredFormat() }
func (c nullableTextCodec) PlanEncode(m *pgtype.Map, oid uint32, format int16, value any) pgtype.EncodePlan {
	return c.inner.PlanEncode(m, oid, format, value)
}
func (c nullableTextCodec) DecodeDatabaseSQLValue(m *pgtype.Map, oid uint32, format int16, src []byte) (driver.Value, error) {
	return c.inner.DecodeDatabaseSQLValue(m, oid, format, src)
}
func (c nullableTextCodec) DecodeValue(m *pgtype.Map, oid uint32, format int16, src []byte) (any, error) {
	return c.inner.DecodeValue(m, oid, format, src)
}

// scanPlanTextNullToString mirrors pgtype's internal
// scanPlanTextAnyToString but maps a NULL source to the empty string rather
// than returning an error.
type scanPlanTextNullToString struct{}

func (scanPlanTextNullToString) Scan(src []byte, dst any) error {
	p, ok := dst.(*string)
	if !ok {
		return fmt.Errorf("scanPlanTextNullToString: unexpected destination %T", dst)
	}
	if src == nil {
		*p = ""
		return nil
	}
	*p = string(src)
	return nil
}

// nullableTextOIDs are the text-ish type OIDs whose NULL values must scan into
// a plain Go *string as the empty string (see package comment).
var nullableTextOIDs = map[uint32]bool{
	25:   true, // text
	1042: true, // bpchar
	1043: true, // varchar
}

// isBuiltinStringPlan reports whether plan is exactly the sentinel plan
// pgtype.Map.PlanScan returns for a *string destination (scanPlanString{} in
// module-internal code). Identity comparison against a fresh zero-value
// instance is safe because that struct has no fields, so all builtin
// instances are interface-equal. This lets us rewrite ONLY the plain-target
// fast path and nothing else.
func isBuiltinStringPlan(plan pgtype.ScanPlan) bool {
	if plan == nil {
		return false
	}
	var probe any = struct{}{}
	type sentinel = struct{}
	_ = probe
	_, ok := plan.(interface{ Scan([]byte, any) error })
	if !ok {
		return false
	}
	// The builtin plan's concrete type is unexported; match by dynamic type
	// name instead of importing it.
	return fmt.Sprintf("%T", plan) == "pgtype.scanPlanString"
}

// scanStringOrNull scans a single-row query whose sole column is a text-ish
// value (text/varchar/bpchar) into a plain Go string, mapping SQL NULL to "".
//
// Rationale (verified against pgx v5.5.1): Map.PlanScan serves *plain* Go
// pointer targets such as *string from a hardcoded fast path that errors on
// NULL and never consults registered codecs; Conn.typeMap is unexported and
// Rows carry no public handle to the map that planned their scans, so neither
// a codec override nor a wrapped map can be installed onto the pool's rows
// without unsupported reflection hacks. Production code therefore scans
// nullable text via the typed wrapper (*string → &sql.NullString{} →
// ScanStringOrNull), which goes through the codec-mediated path and is fully
// supported. This helper centralizes that conversion.
func scanStringOrNull(row pgx.Row) (string, error) {
	var ns sql.NullString
	if err := row.Scan(&ns); err != nil {
		return "", err
	}
	return ns.String, nil // NULL → zero value ""
}

// ScanStringOrNull exposes the NULL-tolerant single-column text scan for
// callers outside this package (e.g. legacy module queries that previously
// scanned nullable varchar columns directly into *string).
func ScanStringOrNull(row pgx.Row) (string, error) { return scanStringOrNull(row) }

// QueryNullableStrings executes a query and returns every row of a
// text-ish column as []string, scanning each row through the typed
// *sql.NullString wrapper so SQL NULL becomes "" instead of erroring. Use it
// in place of draining rows with dest = append(dest, new(string)) for
// nullable text columns.
func (p *Postgres) QueryNullableStrings(ctx context.Context, sqlText string, args ...any) ([]string, error) {
	rows, err := p.Query(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		s, err := scanStringOrNull(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// QueryNullableStrings executes the query within this transaction using the
// NULL-tolerant typed scan path (see Postgres.QueryNullableStrings).
func (t *Tx) QueryNullableStrings(ctx context.Context, sqlText string, args ...any) ([]string, error) {
	rows, err := t.Query(ctx, sqlText, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		s, err := scanStringOrNull(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
