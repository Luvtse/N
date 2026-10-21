package database

import (
	"database/sql/driver"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// nullableTextCodec wraps a pgtype.Codec (in practice the builtin
// pgtype.TextCodec registered for text/varchar/bpchar) so that SQL NULL in
// text-ish columns scans into a plain Go string as "" instead of failing with
// "cannot scan NULL into *string". Legacy module code (and any future code)
// that scans nullable varchar/text columns directly into string fields relies
// on this codec being installed per-connection; see the AfterConnect hook in
// postgres.go.
//
// All methods except PlanScan delegate to the wrapped codec, preserving every
// other behavior: supported formats, all scan-plan target types (*[]byte,
// TextScanner, BytesScanner, ...), encoding plans, and DecodeValue /
// DecodeDatabaseSQLValue (where NULL already maps to nil cleanly). Only the
// *string scan plan is replaced with a NULL-tolerant variant.
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
