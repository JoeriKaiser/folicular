// folicular: backend service for Luteal. Anonymous identity, offline-first
// delta sync, research-backed canonical data model. See AGENTS.md and docs/.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"folicular/internal/config"
	"folicular/internal/db/dbgen"
	"folicular/internal/db"
	"folicular/internal/server"
)

// version is set at build time: -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)

	sqlDB, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Error("database open failed", "err", err)
		os.Exit(1)
	}
	if err := db.Migrate(sqlDB); err != nil {
		log.Error("migration failed", "err", err)
		os.Exit(1)
	}
	log.Info("database ready", "path", cfg.DBPath)
	if n := len(cfg.TrustedProxies); n > 0 {
		log.Info("trusted proxies configured", "networks", n)
	}
	if n := len(cfg.InviteCodes); n > 0 {
		log.Info("registration gated by invite code(s)", "codes", n)
	} else {
		log.Warn("registration is OPEN: set FOLICULAR_INVITE_CODES to restrict account creation")
	}

	handler := server.NewRouter(log, dbgen.New(sqlDB), sqlDB, version, cfg.PairingBaseURL, cfg.TrustedProxies, cfg.InviteCodes)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info("folicular listening", "addr", cfg.Addr, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("graceful shutdown failed", "err", err)
		os.Exit(1)
	}
	log.Info("shutdown complete")
}
