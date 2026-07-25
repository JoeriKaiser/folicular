package auth

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"folicular/internal/api/problem"
	"folicular/internal/db/dbgen"
)

type ctxKey int

const (
	accountIDKey ctxKey = iota
	deviceIDKey
	deviceNameKey
)

// AccountID returns the authenticated account ID, if any.
func AccountID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(accountIDKey).(string)
	return v, ok
}

// DeviceID returns the authenticated device ID, if any.
func DeviceID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(deviceIDKey).(string)
	return v, ok
}

// DeviceName returns the authenticated device name, if any.
func DeviceName(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(deviceNameKey).(string)
	return v, ok
}

// Middleware resolves the bearer device token to an account. Lookups are a
// single SHA-256 + indexed query; there is no user enumeration surface.
func Middleware(queries *dbgen.Queries, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				problem.Write(w, r, problem.Status(http.StatusUnauthorized, "Authentification requise", "Un jeton de périphérique valide est requis."))
				return
			}
			row, err := queries.GetDeviceByTokenHash(r.Context(), HashToken(token))
			if err != nil {
				if err == sql.ErrNoRows {
					problem.Write(w, r, problem.Status(http.StatusUnauthorized, "Authentification refusée", "Jeton inconnu ou révoqué."))
					return
				}
				log.Error("device lookup failed", "err", err)
				problem.Write(w, r, problem.Internal())
				return
			}
			if row.RevokedAt.Valid || row.AccountStatus != "active" {
				problem.Write(w, r, problem.Status(http.StatusUnauthorized, "Authentification refusée", "Jeton révoqué ou compte inactif."))
				return
			}
			// Best-effort presence signal; failure must not break the request.
			_ = queries.TouchDevice(r.Context(), dbgen.TouchDeviceParams{
				LastSeenAt: sql.NullString{String: time.Now().UTC().Format(time.RFC3339), Valid: true},
				ID:         row.ID,
			})

			ctx := context.WithValue(r.Context(), accountIDKey, row.AccountID)
			ctx = context.WithValue(ctx, deviceIDKey, row.ID)
			ctx = context.WithValue(ctx, deviceNameKey, row.Name)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}
