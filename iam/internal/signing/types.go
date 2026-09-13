// Package signing owns the keypairs the iam service signs platform tokens with,
// and the JWKS it publishes them under so any other service can verify a token
// without asking iam anything.
//
// The keyset rotates by itself, with nothing for an operator to configure: a key
// signs for a while, then stops signing but keeps being published long enough for
// the last tokens it signed to expire, and is then deleted. Whichever request first
// notices the current key has retired performs the rotation, under an advisory lock
// so replicas produce one new key rather than one each.
//
// Keys are ES256 (ECDSA on P-256): small, fast, and verifiable by any standard JWT
// library.
package signing

import "time"

// Key is one keypair in the set. The DER encodings are what the database stores:
// PKCS#8 for the private half, PKIX for the public one.
type Key struct {
	// KID is the RFC 7638 thumbprint of the public key, derived from the key rather
	// than assigned, so a kid cannot name two different keys.
	KID       string
	Algorithm string
	Private   []byte
	Public    []byte
	CreatedAt time.Time
	// RetireAfter is when this key stops signing new tokens. It goes on verifying
	// them until it expires.
	RetireAfter time.Time
}

// Token is a minted platform token: the compact JWS and the moment it stops being
// valid, which callers store beside it rather than decoding the token to find.
type Token struct {
	Value     string
	ExpiresAt time.Time
}
