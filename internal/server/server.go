// Package server wires the chi router, middleware, and rate limiting.
package server

import (
	"database/sql"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"folicular/internal/api"
	"folicular/internal/auth"
	"folicular/internal/db/dbgen"
)

// NewRouter builds the v1 HTTP handler. trustedProxies are the networks the
// reverse proxy connects from; they let the rate limiter recover the true
// client IP behind a proxy (see ratelimit.go). inviteCodes gates registration
// when non-empty (closed rollout).
func NewRouter(log *slog.Logger, q *dbgen.Queries, db *sql.DB, version, pairingBaseURL string, trustedProxies []*net.IPNet, inviteCodes map[string]struct{}) http.Handler {
	deps := &api.Deps{Q: q, DB: db, Log: log, PairingBaseURL: pairingBaseURL, InviteCodes: inviteCodes}
	authLimiter := newIPLimiter(10, 20, trustedProxies) // 10 req/min sustained, burst 20

	r := chi.NewRouter()
	r.Use(chimw.Recoverer)
	r.Use(chimw.RequestID)
	r.Use(requestLogger(log))

	// Operations -----------------------------------------------------------
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writePlain(w, http.StatusOK, `{"status":"ok"}`)
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			writePlain(w, http.StatusServiceUnavailable, `{"status":"unavailable"}`)
			return
		}
		writePlain(w, http.StatusOK, `{"status":"ready"}`)
	})
	r.Get("/version", func(w http.ResponseWriter, _ *http.Request) {
		writePlain(w, http.StatusOK, `{"version":"`+version+`"}`)
	})

	// API v1 ----------------------------------------------------------------
	r.Route("/v1", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			r.With(authLimiter.Middleware).Post("/register", deps.HandleRegister)
			r.With(authLimiter.Middleware).Post("/devices", deps.HandleAddDevice)
			r.Group(func(r chi.Router) {
				r.Use(auth.Middleware(q, log))
				r.Get("/devices", deps.HandleListDevices)
				r.Delete("/devices/{deviceID}", deps.HandleRevokeDevice)
			})
		})

		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(q, log))

			r.Get("/me", deps.HandleMe)
			r.Patch("/me", deps.HandlePatchMe)
			r.Delete("/me", deps.HandleDeleteMe)
			r.Get("/export", deps.HandleExport)

			r.Post("/sync/push", deps.HandleSyncPush)
			r.Get("/sync/pull", deps.HandleSyncPull)

			r.Get("/cycles", deps.HandleListCycles)
			r.Get("/days", deps.HandleDays)
			r.Get("/predictions/current", deps.HandlePredictionsCurrent)

			// Duo: purpose-designed partner surface. Pairing codes are
			// short-lived and single-use; acceptance is rate limited.
			r.Post("/duo/invitations", deps.HandleCreateInvitation)
			r.With(authLimiter.Middleware).Post("/duo/links", deps.HandleAcceptLink)
			r.Get("/duo/links", deps.HandleListLinks)
			r.Patch("/duo/links/{linkID}/grants", deps.HandlePatchGrants)
			r.Delete("/duo/links/{linkID}", deps.HandleRevokeLink)
			r.Get("/duo/view", deps.HandleDuoView)
			r.Post("/duo/support-requests", deps.HandleCreateSupportRequest)
			r.Patch("/duo/support-requests/{requestID}/ack", deps.HandleAckSupportRequest)
		})
	})

	return r
}

func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// requestLogger logs one line per request with method, route pattern,
// status, and duration. No headers, tokens, or bodies are logged.
func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"route", chi.RouteContext(r.Context()).RoutePattern(),
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", chimw.GetReqID(r.Context()),
			)
		})
	}
}
