package api

// Null-helpers shared by the handlers.
//
// The per-record converters that used to live here were removed with the
// end-to-end encryption migration: record content is ciphertext the server
// cannot decode, so there is nothing to convert between domain types and
// typed columns any more.

import "database/sql"

// Nullable helpers between domain pointers and sqlc null types.

func ns(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func sp(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func ni(p *int) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*p), Valid: true}
}

func ip(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}

func nf(p *float64) sql.NullFloat64 {
	if p == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *p, Valid: true}
}

func fp(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	return &v.Float64
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// Cycles ----------------------------------------------------------------

func stringPtr(s string) *string { return &s }
