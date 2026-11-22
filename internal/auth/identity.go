// Package auth implements anonymous Mullvad-style identity: a high-entropy
// account code shown once, plus per-device bearer tokens. Only SHA-256
// hashes are stored. No email, OAuth, phone, or password exists anywhere.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Crockford base32 alphabet (excludes I, L, O, U to stay human-friendly).
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// codeSymbols is 100 bits of entropy (20 x 5-bit symbols), displayed as
// LTL-XXXXX-XXXXX-XXXXX-XXXXX.
const codeSymbols = 20

// GenerateAccountCode returns the display form of a fresh account code and
// its storage hash. The display form is shown to the user exactly once.
func GenerateAccountCode() (display string, hash []byte, err error) {
	raw, err := randomSymbols(codeSymbols)
	if err != nil {
		return "", nil, err
	}
	display = "LTL-" + raw[0:5] + "-" + raw[5:10] + "-" + raw[10:15] + "-" + raw[15:20]
	return display, HashCode(raw), nil
}

// NormalizeCode strips the prefix and separators and uppercases, yielding
// the canonical 20-symbol form used for hashing.
func NormalizeCode(input string) string {
	s := strings.ToUpper(strings.TrimSpace(input))
	s = strings.TrimPrefix(s, "LTL-")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

// HashCode hashes a canonical (normalized) account code for storage.
func HashCode(normalized string) []byte {
	sum := sha256.Sum256([]byte(normalized))
	return sum[:]
}

// CodeMatches compares a normalized input against a stored hash in constant
// time.
func CodeMatches(normalized string, stored []byte) bool {
	return subtle.ConstantTimeCompare(HashCode(normalized), stored) == 1
}

// Auth protocol versions stored in accounts.auth_version.
const (
	AuthVersionLegacy int64 = 1
	AuthVersionV2     int64 = 2
)

// v2AuthVerifierDomain domain-separates server-stored v2 auth verifiers from
// legacy v1 account-code hashes so they cannot collide or impersonate each other.
const v2AuthVerifierDomain = "folicular/v2/auth-verifier\x00"

// ErrInvalidAuthKey indicates an auth_key cannot be parsed as a 32-byte key.
var ErrInvalidAuthKey = errors.New("auth_key must be a 32-byte key in hex or base64")

// ParseAuthKey decodes a client-provided 32-byte authentication key (K_auth).
// It accepts 32 bytes encoded as hex (64 chars) or base64 (standard or URL-safe,
// with or without padding).
func ParseAuthKey(input string) ([]byte, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return nil, ErrInvalidAuthKey
	}

	if len(s) == 64 {
		if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}

	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, enc := range encodings {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}

	return nil, ErrInvalidAuthKey
}

// HashAuthKey derives the server-stored verifier from a 32-byte client auth_key.
// Hashing server-side ensures a read-only database leak cannot be replayed
// against POST /v1/auth/devices (preventing pass-the-hash).
func HashAuthKey(authKey []byte) []byte {
	h := sha256.New()
	h.Write([]byte(v2AuthVerifierDomain))
	h.Write(authKey)
	return h.Sum(nil)
}

// VerifierMatches compares two 32-byte verifiers in constant time.
func VerifierMatches(candidate, stored []byte) bool {
	if len(candidate) != len(stored) || len(stored) != 32 {
		return false
	}
	return subtle.ConstantTimeCompare(candidate, stored) == 1
}

// pairingCodeSymbols is 50 bits (10 x 5-bit symbols), displayed as
// XXXXX-XXXXX. Short-lived (7 days), single-use, and protected by rate
// limiting on the accept endpoint - adequate for a pairing secret that is
// also transported as QR code and link.
const pairingCodeSymbols = 10

// GeneratePairingCode returns the display form of a fresh Duo pairing code
// and its storage hash.
func GeneratePairingCode() (display string, hash []byte, err error) {
	raw, err := randomSymbols(pairingCodeSymbols)
	if err != nil {
		return "", nil, err
	}
	return raw[0:5] + "-" + raw[5:10], HashCode(raw), nil
}

const tokenPrefix = "ltok_"

// GenerateDeviceToken returns a 256-bit bearer token and its storage hash.
func GenerateDeviceToken() (token string, hash []byte, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, fmt.Errorf("device token randomness: %w", err)
	}
	token = tokenPrefix + base64.RawURLEncoding.EncodeToString(buf)
	return token, HashToken(token), nil
}

// HashToken hashes a device token for storage.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// HashInviteCode hashes an invite code (trimmed) to its comparison form
// (SHA-256 hex). Invite codes are configured server-side and matched by hash;
// the plaintext is never stored or logged.
func HashInviteCode(code string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

func randomSymbols(n int) (string, error) {
	max := big.NewInt(int64(len(crockford)))
	var b strings.Builder
	b.Grow(n)
	for i := 0; i < n; i++ {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("account code randomness: %w", err)
		}
		b.WriteByte(crockford[idx.Int64()])
	}
	return b.String(), nil
}
