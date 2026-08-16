package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestGenerateAccountCode(t *testing.T) {
	display, hash, err := GenerateAccountCode()
	if err != nil {
		t.Fatalf("GenerateAccountCode() error: %v", err)
	}

	if !strings.HasPrefix(display, "LTL-") {
		t.Errorf("expected prefix 'LTL-', got %q", display)
	}

	parts := strings.Split(display, "-")
	if len(parts) != 5 {
		t.Fatalf("expected 5 segments separated by dashes, got %d in %q", len(parts), display)
	}
	if parts[0] != "LTL" {
		t.Errorf("first segment should be 'LTL', got %q", parts[0])
	}
	for i := 1; i <= 4; i++ {
		if len(parts[i]) != 5 {
			t.Errorf("segment %d length = %d, want 5 (%q)", i, len(parts[i]), parts[i])
		}
		for _, c := range parts[i] {
			if !strings.ContainsRune(crockford, c) {
				t.Errorf("invalid Crockford base32 character %c in %q", c, display)
			}
		}
	}

	norm := NormalizeCode(display)
	if len(norm) != 20 {
		t.Errorf("normalized length = %d, want 20", len(norm))
	}

	expectedHash := sha256.Sum256([]byte(norm))
	if string(hash) != string(expectedHash[:]) {
		t.Errorf("hash mismatch: got %x, want %x", hash, expectedHash)
	}
}

func TestNormalizeCode(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"LTL-8K3FQ-Z2WNT-7HJMC-4XRDB", "8K3FQZ2WNT7HJMC4XRDB"},
		{"ltl-8k3fq-z2wnt-7hjmc-4xrdb", "8K3FQZ2WNT7HJMC4XRDB"},
		{"  LTL-8K3FQ-Z2WNT-7HJMC-4XRDB  ", "8K3FQZ2WNT7HJMC4XRDB"},
		{"8K3FQ Z2WNT 7HJMC 4XRDB", "8K3FQZ2WNT7HJMC4XRDB"},
		{"8k3fqz2wnt7hjmc4xrdb", "8K3FQZ2WNT7HJMC4XRDB"},
		{"8K3FQ-Z2WNT", "8K3FQZ2WNT"},
	}

	for _, tc := range cases {
		got := NormalizeCode(tc.input)
		if got != tc.want {
			t.Errorf("NormalizeCode(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestCodeMatches(t *testing.T) {
	norm := "8K3FQZ2WNT7HJMC4XRDB"
	hash := HashCode(norm)

	if !CodeMatches(norm, hash) {
		t.Error("CodeMatches() = false for identical normalized code")
	}
	if CodeMatches("DIFFERENTCODE1234567", hash) {
		t.Error("CodeMatches() = true for different code")
	}
	if CodeMatches("", hash) {
		t.Error("CodeMatches() = true for empty code")
	}
}

func TestGeneratePairingCode(t *testing.T) {
	display, hash, err := GeneratePairingCode()
	if err != nil {
		t.Fatalf("GeneratePairingCode() error: %v", err)
	}

	parts := strings.Split(display, "-")
	if len(parts) != 2 {
		t.Fatalf("expected 2 segments separated by a dash, got %d in %q", len(parts), display)
	}
	if len(parts[0]) != 5 || len(parts[1]) != 5 {
		t.Errorf("segments should be 5 chars each, got %q", display)
	}

	norm := NormalizeCode(display)
	if len(norm) != 10 {
		t.Errorf("normalized pairing code length = %d, want 10", len(norm))
	}

	expectedHash := sha256.Sum256([]byte(norm))
	if string(hash) != string(expectedHash[:]) {
		t.Errorf("hash mismatch: got %x, want %x", hash, expectedHash)
	}
}

func TestGenerateDeviceToken(t *testing.T) {
	token, hash, err := GenerateDeviceToken()
	if err != nil {
		t.Fatalf("GenerateDeviceToken() error: %v", err)
	}

	if !strings.HasPrefix(token, "ltok_") {
		t.Errorf("device token must start with %q, got %q", tokenPrefix, token)
	}

	expectedHash := sha256.Sum256([]byte(token))
	if string(hash) != string(expectedHash[:]) {
		t.Errorf("token hash mismatch: got %x, want %x", hash, expectedHash)
	}

	token2, _, _ := GenerateDeviceToken()
	if token == token2 {
		t.Error("subsequent tokens should not be identical")
	}
}

func TestHashInviteCode(t *testing.T) {
	raw := "  BETA-TEST-1234  "
	hashed := HashInviteCode(raw)

	expectedSum := sha256.Sum256([]byte("BETA-TEST-1234"))
	expectedHex := hex.EncodeToString(expectedSum[:])

	if hashed != expectedHex {
		t.Errorf("HashInviteCode(%q) = %q, want %q", raw, hashed, expectedHex)
	}
}
