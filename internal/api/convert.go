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

func stringPtr(s string) *string { return &s }
