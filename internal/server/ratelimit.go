package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"folicular/internal/api/problem"
)

// ipLimiter is a simple in-memory per-client-IP token bucket. When the service
// sits behind a reverse proxy (e.g. Coolify/Traefik), configure trusted with
// the proxy's network so the true client IP is recovered from
// X-Forwarded-For / X-Real-IP; otherwise the peer address is used.
type ipLimiter struct {
	mu      sync.Mutex
	clients map[string]*rate.Limiter
	limit   rate.Limit
	burst   int
	trusted []*net.IPNet

	// pepper is random per process, so bucket keys are unlinkable across
	// restarts and a raw client IP is never held in the map. Rate limiting
	// only needs a stable opaque key, not the address itself. Client IPs are
	// never logged (see requestLogger) and never persisted.
	pepper []byte
}

func newIPLimiter(perMinute float64, burst int, trusted []*net.IPNet) *ipLimiter {
	pepper := make([]byte, 32)
	if _, err := rand.Read(pepper); err != nil {
		// A predictable pepper would only make bucket keys guessable, which
		// is not a security boundary; failing to start would be worse.
		pepper = []byte("folicular-ratelimit-fallback-pepper")
	}
	l := &ipLimiter{
		clients: make(map[string]*rate.Limiter),
		limit:   rate.Limit(perMinute / 60.0),
		burst:   burst,
		trusted: trusted,
		pepper:  pepper,
	}
	go l.janitor()
	return l
}

// bucketKey maps a client address to an opaque, per-process key. The raw
// address is discarded immediately and never stored.
func (l *ipLimiter) bucketKey(ip string) string {
	mac := hmac.New(sha256.New, l.pepper)
	mac.Write([]byte(ip))
	return base64.RawStdEncoding.EncodeToString(mac.Sum(nil))
}

func (l *ipLimiter) allow(ip string) (bool, time.Duration) {
	key := l.bucketKey(ip)
	l.mu.Lock()
	lim, ok := l.clients[key]
	if !ok {
		lim = rate.NewLimiter(l.limit, l.burst)
		l.clients[key] = lim
	}
	l.mu.Unlock()
	res := lim.Reserve()
	if res.OK() && res.Delay() == 0 {
		return true, 0
	}
	delay := res.Delay()
	res.Cancel()
	return false, delay
}

// janitor evicts idle entries so the map stays bounded.
func (l *ipLimiter) janitor() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		for ip, lim := range l.clients {
			if lim.Tokens() >= float64(l.burst) {
				delete(l.clients, ip)
			}
		}
		l.mu.Unlock()
	}
}

// Middleware rejects over-limit requests with 429 and Retry-After.
func (l *ipLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r, l.trusted)
		ok, delay := l.allow(ip)
		if !ok {
			retryAfter := int(delay.Seconds()) + 1
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			problem.Write(w, r, problem.Status(http.StatusTooManyRequests, "Trop de requêtes",
				"Limite de débit dépassée. Réessayez dans "+strconv.Itoa(retryAfter)+" secondes."))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP determines the true client address. Forwarding headers are honored
// only when the immediate peer is a configured trusted proxy; in that case the
// rightmost untrusted X-Forwarded-For entry (then X-Real-IP) is the client.
// Otherwise the peer address is used and forwarding headers are ignored, so a
// direct client cannot spoof its IP to evade the limiter.
func clientIP(r *http.Request, trusted []*net.IPNet) string {
	remote := parseAddrIP(r.RemoteAddr)
	if remote == nil || !ipTrusted(remote, trusted) {
		return ipKey(remote, r.RemoteAddr)
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(parts[i]))
			if ip == nil {
				continue
			}
			if !ipTrusted(ip, trusted) {
				return ip.String()
			}
		}
		// Every listed hop is trusted; attribute to the leftmost originator.
		if first := net.ParseIP(strings.TrimSpace(parts[0])); first != nil {
			return first.String()
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if ip := net.ParseIP(xri); ip != nil {
			return ip.String()
		}
	}
	return ipKey(remote, r.RemoteAddr)
}

func parseAddrIP(addr string) net.IP {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	return net.ParseIP(host)
}

func ipKey(ip net.IP, fallback string) string {
	if ip == nil {
		return fallback
	}
	return ip.String()
}

func ipTrusted(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
