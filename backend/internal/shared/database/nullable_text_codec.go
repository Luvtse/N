package database

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
)

// nullableTextCodec decodes text-ish columns into a plain Go string, mapping
// SQL NULL to "" instead of failing the scan. pgx v5's default codec refuses
// `cannot scan NULL into *string`; legacy modules (and any future code) that
// scan nullable varchar/text columns directly into string fields rely on this
// codec being registered per-connection (see postgres.go AfterConnect).
type nullableTextCodec struct{}

func (c *nullableTextCodec) Format() string { return "text" }

func (c *nullableTextCodec) Decode(format string, value []byte, oid uint32) (any, error) {
	if format != "" && format != "text" {
		return nil, fmt.Errorf("nullableTextCodec: unexpected format %q", format)
	}
	s := pgtype.Text{}
	if err := s.Scan(value); err != nil { // []byte -> string; nil -> NULL
		return nil, err
	}
	if s.Valid {
		return s.String, nil
	}
	return "", nil
}

func (c *nullableTextCodec) Encode(format string, value any, oid uint32) ([]byte, error) {
	switch v := value.(type) {
	case string:
		return []byte(v), nil
	case nil:
		return nil, nil
	default:
		return nil, fmt.Errorf("nullableTextCodec: cannot encode %T", value)
	}
}
