// Package api implements the HTTP handlers for the v1 contract documented in
// docs/api.md. Handlers parse, domain validates, sqlc persists.
package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"folicular/internal/api/problem"
	"folicular/internal/auth"
	"folicular/internal/db/dbgen"
)

// Deps carries handler dependencies.
type Deps struct {
	Q              *dbgen.Queries
	DB             *sql.DB
	Log            *slog.Logger
	PairingBaseURL string
	// InviteCodes gates registration when non-empty (closed rollout): the set
	// of SHA-256 hex hashes of valid invite codes. See config.parseInviteCodes.
	InviteCodes map[string]struct{}
}

// registrationGated reports whether registration requires an invite code.
func (d *Deps) registrationGated() bool { return len(d.InviteCodes) > 0 }

// validInviteCode reports whether code matches a configured invite code. When
// registration is not gated, every code (including empty) is accepted.
func (d *Deps) validInviteCode(code string) bool {
	if !d.registrationGated() {
		return true
	}
	_, ok := d.InviteCodes[auth.HashInviteCode(code)]
	return ok
}

const maxBodyBytes = 1 << 20 // 1 MiB

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		problem.Write(w, r, problem.Status(http.StatusBadRequest, "Corps invalide",
			fmt.Sprintf("JSON invalide : %v", err)))
		return false
	}
	return true
}

// txFunc runs fn inside a transaction with sqlc queries bound to it.
func (d *Deps) txFunc(r *http.Request, fn func(q *dbgen.Queries) error) error {
	tx, err := d.DB.BeginTx(r.Context(), nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	q := d.Q.WithTx(tx)
	if err := fn(q); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func isNotFound(err error) bool { return errors.Is(err, sql.ErrNoRows) }
