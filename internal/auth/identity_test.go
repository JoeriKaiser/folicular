package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
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

func TestParseAuthKey(t *testing.T) {
	raw := sha256.Sum256([]byte("client-k-auth-entropy"))

	validCases := []struct {
		name  string
		input string
	}{
		{"hex lowercase", hex.EncodeToString(raw[:])},
		{"hex uppercase", strings.ToUpper(hex.EncodeToString(raw[:]))},
		{"hex with whitespace", "  " + hex.EncodeToString(raw[:]) + "  \n"},
		{"base64 standard", base64.StdEncoding.EncodeToString(raw[:])},
		{"base64 raw standard", base64.RawStdEncoding.EncodeToString(raw[:])},
		{"base64 url", base64.URLEncoding.EncodeToString(raw[:])},
		{"base64 raw url", base64.RawURLEncoding.EncodeToString(raw[:])},
		{"base64 with whitespace", " \t" + base64.StdEncoding.EncodeToString(raw[:]) + " \n"},
	}

	for _, tc := range validCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseAuthKey(tc.input)
			if err != nil {
				t.Fatalf("ParseAuthKey(%q) unexpected error: %v", tc.input, err)
			}
			if !bytes.Equal(got, raw[:]) {
				t.Fatalf("ParseAuthKey(%q) = %x, want %x", tc.input, got, raw)
			}
		})
	}

	invalidCases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"whitespace only", "   "},
		{"too short hex", hex.EncodeToString(raw[:16])},
		{"too long hex", hex.EncodeToString(raw[:]) + "ab"},
		{"odd hex length", hex.EncodeToString(raw[:])[:63]},
		{"invalid hex character", strings.Repeat("z", 64)},
		{"too short base64", base64.StdEncoding.EncodeToString(raw[:16])},
		{"invalid base64 symbols", "???invalid-base64-not-32-bytes???"},
	}

	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseAuthKey(tc.input)
			if !errors.Is(err, ErrInvalidAuthKey) {
				t.Fatalf("ParseAuthKey(%q) error = %v, want %v", tc.input, err, ErrInvalidAuthKey)
			}
		})
	}
}

func TestHashAuthKeyAndVerifierMatches(t *testing.T) {
	// Cross-language domain separation check: HashAuthKey(k) must equal
	// SHA-256("folicular/v2/auth-verifier\x00" || k) and never equal raw k.
	k1 := bytes.Repeat([]byte{0x42}, 32)
	k2 := bytes.Repeat([]byte{0x43}, 32)

	v1 := HashAuthKey(k1)
	v2 := HashAuthKey(k2)

	if bytes.Equal(v1, k1) {
		t.Fatal("verifier must not equal raw auth_key")
	}

	h := sha256.New()
	h.Write([]byte("folicular/v2/auth-verifier\x00"))
	h.Write(k1)
	want := h.Sum(nil)
	if !bytes.Equal(v1, want) {
		t.Fatalf("HashAuthKey mismatch: got %x, want %x", v1, want)
	}

	if !VerifierMatches(v1, want) {
		t.Error("identical verifiers must match")
	}
	if VerifierMatches(v1, v2) {
		t.Error("different verifiers must not match")
	}
	if VerifierMatches(v1[:16], v1[:16]) {
		t.Error("verifiers shorter than 32 bytes must not match")
	}
	if VerifierMatches(v1, v1[:16]) {
		t.Error("length mismatch must not match")
	}
}
