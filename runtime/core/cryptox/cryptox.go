// Package cryptox holds the runtime's encryption primitives: the ciphers behind
// the encrypt/decrypt capability, with one construction and one stored format
// per algorithm so there is a single place to change if a format ever moves.
//
// Every cipher here is authenticated. Opening data that was altered fails rather
// than returning plausible bytes, which is the property that makes ciphertext
// safe to hand to a queue, a column, or an object store and read back later.
//
// The package is named cryptox rather than crypto so it does not shadow the
// standard library package of that name, which these files import.
package cryptox

// Cipher seals and opens a value. The sealed form is opaque: it carries whatever
// the algorithm needs to open it again (a nonce, an authentication tag), so a
// caller stores the bytes and nothing else.
type Cipher interface {
	Seal(plaintext []byte) ([]byte, error)
	Open(sealed []byte) ([]byte, error)
}
