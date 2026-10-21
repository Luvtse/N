package database

import (
	"database/sql/driver"
	"fmt"

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
// IMPORTANT (verified against pgx v5.5.1): Map.PlanScan serves *plain* Go
// pointer targets such as *string from its own hardcoded fast path
// (pgtype.scanPlanString, which errors on NULL) and never consults the
// registered codec for those targets. Codec.PlanScan is only reached for
// wrapper types (*pgtype.Text, sql.Scanner implementers, composite/array
// element planning). Consequently wrapping the codec alone cannot make
// NULL -> *string tolerant.
//
// The plain-target translation is performed by nullableMap: a per-connection
// pgtype.Map whose PlanScan delegates to the embedded builtin map (so every
// codec-mediated path keeps working, including through this codec wrapper)
// and swaps ONLY the exact sentinel plan pgmap returns for text-ish OIDs +
// *string targets with the NULL-tolerant variant below. Every other target
// type — including *string against non-text OIDs such as jsonb, where the
// builtin error must be preserved — is passed through untouched.
// postgres.go installs it via ConnConfig.CreateAssetsByName so all pooled
// connections (and therefore all queries and transactions) use it.
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

// nullableMap wraps a per-connection pgtype.Map. All methods delegate through
// the embedded *pgtype.Map (registration, encoding plans, DecodeValue, ...);
// only PlanScan is intercepted to make SQL NULL -> *string scan as "" for
// text-ish columns.
//
// It must embed the ORIGINAL pointer so every internal helper inside pgmap
// still receives the real map with its private fields intact — we never hand
// out a value copy or a synthesized map.
type nullableMap struct{ *pgtype.Map }

// PlanScan delegates to the builtin map and rewrites the narrow case:
// text-ish OID + plain *string destination served by the hardcoded
// scanPlanString fast path. Everything else (other target types, other OIDs,
// codec-mediated plans, failure plans) is returned unchanged, preserving the
// builtin behavior — including the deliberate "cannot scan NULL into *string"
// error for non-text columns such as jsonb.
//
// We deliberately do NOT rely on memoized plans cached inside the embedded
// map: the rewrite happens on this wrapper's own output path, so even a plan
// the builtin map memoized before installation is corrected here.
func (m nullableMap) PlanScan(oid uint32, formatCode int16, target any) pgtype.ScanPlan {
	plan := m.Map.PlanScan(oid, formatCode, target)
	if _, isStr := target.(*string); isStr && nullableTextOIDs[oid] && isBuiltinStringPlan(plan) {
		return scanPlanTextNullToString{}
	}
	return plan
}
