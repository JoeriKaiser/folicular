package server

import (
	"net"
	"net/http"
	"testing"
)

func mustNets(t *testing.T, cidrs ...string) []*net.IPNet {
	t.Helper()
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			t.Fatalf("bad cidr %q: %v", c, err)
		}
		out = append(out, n)
	}
	return out
}

func req(remote, xff, xri string) *http.Request {
	r := &http.Request{RemoteAddr: remote, Header: http.Header{}}
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	if xri != "" {
		r.Header.Set("X-Real-IP", xri)
	}
	return r
}

func TestClientIP(t *testing.T) {
	proxy := mustNets(t, "10.0.0.0/8")
	cases := []struct {
		name    string
		trusted []*net.IPNet
		remote  string
		xff     string
		xri     string
		want    string
	}{
		{"no trusted proxies ignores XFF (spoof resistance)", nil, "203.0.113.5:1234", "198.51.100.9", "", "203.0.113.5"},
		{"direct client without headers", nil, "203.0.113.5:1234", "", "", "203.0.113.5"},
		{"trusted proxy with single client", proxy, "10.1.2.3:9999", "203.0.113.5", "", "203.0.113.5"},
		{"trusted proxy chained through another proxy", proxy, "10.1.2.3:9999", "203.0.113.5, 10.0.0.9", "", "203.0.113.5"},
		{"trusted proxy rightmost untrusted wins", proxy, "10.1.2.3:9999", "198.51.100.1, 203.0.113.5, 10.0.0.9", "", "203.0.113.5"},
		{"trusted proxy falls back to X-Real-IP", proxy, "10.1.2.3:9999", "", "203.0.113.5", "203.0.113.5"},
		{"trusted proxy all hops trusted uses leftmost", proxy, "10.1.2.3:9999", "10.9.9.9, 10.8.8.8", "", "10.9.9.9"},
		{"untrusted peer with XFF is ignored", proxy, "203.0.113.5:1234", "198.51.100.9", "198.51.100.9", "203.0.113.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clientIP(req(tc.remote, tc.xff, tc.xri), tc.trusted); got != tc.want {
				t.Fatalf("clientIP = %q, want %q", got, tc.want)
			}
		})
	}
}
