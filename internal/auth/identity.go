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
