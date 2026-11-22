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

const (
	maxBodyBytes        = 1 << 20  // 1 MiB for normal requests
	maxUpgradeBodyBytes = 16 << 20 // 16 MiB for atomic v1 -> v2 account upgrade
	minEnvelopeBytes    = 1 + 12 + 16
	envelopeVersionV1   = 0x01
	envelopeVersionV2   = 0x02
)

func expectedEnvelopeVersion(authVersion int64) byte {
	if authVersion == auth.AuthVersionV2 {
		return envelopeVersionV2
	}
	return envelopeVersionV1
}

// validateSealedEnvelope checks that a sealed payload meets minimum AES-256-GCM
// framing (1 version byte + 12-byte nonce + 16-byte tag) and carries the
// envelope version matching the account's auth_version.
func validateSealedEnvelope(ciphertext []byte, authVersion int64) error {
	if len(ciphertext) < minEnvelopeBytes {
		return fmt.Errorf("ciphertext: enveloppe trop courte (minimum %d octets)", minEnvelopeBytes)
	}
	if len(ciphertext) > maxCiphertextBytes {
		return fmt.Errorf("ciphertext: au plus %d octets", maxCiphertextBytes)
	}
	want := expectedEnvelopeVersion(authVersion)
	if ciphertext[0] != want {
		return fmt.Errorf("ciphertext: version d'enveloppe 0x%02x incompatible avec le compte (attendu 0x%02x)", ciphertext[0], want)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeJSONWithLimit(w, r, v, maxBodyBytes)
}

func decodeJSONWithLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, limit))
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
