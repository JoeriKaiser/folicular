// Package config loads environment configuration. Environment only, with
// safe defaults for local development.
package config

import (
	"log/slog"
	"net"
	"os"
	"strings"

	"folicular/internal/auth"
)

// DefaultPairingBaseURL is the default upstream service for Duo pairing links.
const DefaultPairingBaseURL = "https://luteal-duo.waldemar.site"

type Config struct {
	Addr           string
	DBPath         string
	LogLevel       slog.Level
	PairingBaseURL string
	// TrustedProxies are the networks the reverse proxy connects from (e.g.
	// the Docker bridge network behind Coolify/Traefik). Only when the
	// immediate peer is in this set are X-Forwarded-For / X-Real-IP honored to
	// recover the true client IP for rate limiting. Empty trusts no proxy, so
	// the peer address is used and forwarding headers cannot be spoofed.
	TrustedProxies []*net.IPNet
	// InviteCodes is the set of SHA-256 hex hashes of configured invite codes.
	// When non-empty, account registration requires a matching code (closed
	// rollout); empty leaves registration open (the server logs a warning).
	InviteCodes map[string]struct{}
}

func Load() Config {
	pairingBaseURL := strings.TrimRight(strings.TrimSpace(env("FOLICULAR_PAIRING_BASE_URL", DefaultPairingBaseURL)), "/")
	if pairingBaseURL == "" {
		pairingBaseURL = DefaultPairingBaseURL
	}
	return Config{
		Addr:     env("FOLICULAR_ADDR", ":8080"),
		DBPath:   env("FOLICULAR_DB_PATH", "folicular.db"),
		LogLevel: parseLevel(env("FOLICULAR_LOG_LEVEL", "info")),
		// Base URL used to build Duo pairing links (rendered as QR codes
		// and shareable links by the client). Change per deployment.
		PairingBaseURL: pairingBaseURL,
		TrustedProxies: parseCIDRs(env("FOLICULAR_TRUSTED_PROXIES", "")),
		InviteCodes:    parseInviteCodes(env("FOLICULAR_INVITE_CODES", "")),
	}
}

// IsDefaultPairingBaseURL reports whether the configured PairingBaseURL is
// using the default upstream demo pairing service.
func (c Config) IsDefaultPairingBaseURL() bool {
	return c.PairingBaseURL == DefaultPairingBaseURL
}

// parseCIDRs parses a comma-separated list of IPs or CIDR ranges into
// networks. A bare IP becomes a /32 (IPv4) or /128 (IPv6) host route. Invalid
// entries are skipped so a typo cannot take the service down.
func parseCIDRs(s string) []*net.IPNet {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var nets []*net.IPNet
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, n, err := net.ParseCIDR(part); err == nil {
			nets = append(nets, n)
			continue
		}
		if ip := net.ParseIP(part); ip != nil {
			if v4 := ip.To4(); v4 != nil {
				ip = v4
			}
			bits := 8 * len(ip)
			nets = append(nets, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
		}
	}
	return nets
}

// parseInviteCodes builds a set of invite-code hashes from a comma-separated
// list. When the set is non-empty, registration is gated on a matching code.
func parseInviteCodes(s string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		set[auth.HashInviteCode(part)] = struct{}{}
	}
	return set
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseLevel(v string) slog.Level {
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
