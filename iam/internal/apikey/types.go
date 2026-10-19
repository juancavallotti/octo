// Package apikey holds the per-user bearer tokens iam exchanges for platform
// tokens. A key belongs to exactly one user and only ever speaks for them: it
// carries no roles of its own, and what it may do is whatever its owner holds when
// it is presented. The plaintext token is shown exactly once at creation; only its
// SHA-256 hash is stored, so the database holds no usable credential.
package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// APIKey is a per-user bearer token's metadata. It never carries the plaintext
// token or its hash on reads — those exist only transiently at creation and
// verification.
type APIKey struct {
	ID         string
	UserID     string
	Name       string
	Prefix     string
	Last4      string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

const (
	// tokenPrefix marks octo-issued tokens so they are recognizable in logs and
	// secret scanners.
	tokenPrefix = "octo_"
	// tokenBytes is the amount of entropy behind each token before encoding.
	tokenBytes = 32
	// maxNameLen bounds a key's human label.
	maxNameLen = 200
	// maxTTL caps how far out a key may be set to expire.
	maxTTL = 365 * 24 * time.Hour
)

// generateToken mints a new random token. It returns the plaintext (shown to the
// user once), its hex SHA-256 hash (stored), and the non-secret display fragments.
func generateToken() (plaintext, hash, prefix, last4 string, err error) {
	buf := make([]byte, tokenBytes)
	if _, err = rand.Read(buf); err != nil {
		return "", "", "", "", fmt.Errorf("apikey: generate token: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(buf)
	plaintext = tokenPrefix + body
	return plaintext, hashToken(plaintext), tokenPrefix + body[:4], body[len(body)-4:], nil
}

// IsKey reports whether a presented bearer is shaped like an API key rather than
// a JWT. The prefix is enough to route on: a compact JWT opens with its
// base64url-encoded JSON header, which always begins "eyJ".
func IsKey(token string) bool {
	return strings.HasPrefix(token, tokenPrefix)
}

// hashToken returns the hex SHA-256 of a plaintext token. The same function is
// used to store (at creation) and to look up (at verification) so the two always
// agree.
func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// validName reports whether name is a usable, length-bounded label.
func validName(name string) bool {
	n := strings.TrimSpace(name)
	return n != "" && len(n) <= maxNameLen
}
