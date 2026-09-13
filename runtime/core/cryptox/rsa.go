package cryptox

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// rsaAlgName prefixes every error this file produces.
const rsaAlgName = "rsa-oaep"

// rsaCipher seals with RSA-OAEP over SHA-256. Unlike the symmetric ciphers the
// two directions use different keys, and either may be absent: a holder of only
// the public key can seal and not open, which is frequently the whole reason to
// reach for this algorithm.
type rsaCipher struct {
	public  *rsa.PublicKey
	private *rsa.PrivateKey
}

// NewRSAOAEP builds a cipher from PEM-encoded keys. Either may be empty, and the
// direction it serves then reports that it has no key rather than failing
// obscurely at the first message. Both empty is a configuration that can do
// nothing, so it is rejected here.
//
//nolint:ireturn // the constructors return the Cipher interface so a caller can hold any algorithm
func NewRSAOAEP(publicPEM, privatePEM []byte) (Cipher, error) {
	ret := &rsaCipher{}

	if len(publicPEM) > 0 {
		public, err := parsePublicKey(publicPEM)
		if err != nil {
			return nil, err
		}
		ret.public = public
	}

	if len(privatePEM) > 0 {
		private, err := parsePrivateKey(privatePEM)
		if err != nil {
			return nil, err
		}
		ret.private = private
		// A private key carries its public half, so a keypair supplied as one PEM
		// can still seal.
		if ret.public == nil {
			ret.public = &private.PublicKey
		}
	}

	if ret.public == nil {
		return nil, fmt.Errorf("%s: needs a public key to encrypt with, a private key to decrypt with, or both", rsaAlgName)
	}
	return ret, nil
}

// Seal encrypts under the public key. RSA can only carry a payload smaller than
// its modulus, so a long plaintext is rejected with that said plainly instead of
// the size arithmetic the standard library reports.
func (c *rsaCipher) Seal(plaintext []byte) ([]byte, error) {
	if c.public == nil {
		return nil, fmt.Errorf("%s: no public key is configured, so this key can only decrypt", rsaAlgName)
	}

	sealed, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, c.public, plaintext, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: could not encrypt %d bytes; this key holds at most %d",
			rsaAlgName, len(plaintext), c.maxPlaintext())
	}
	return sealed, nil
}

// Open decrypts under the private key.
func (c *rsaCipher) Open(sealed []byte) ([]byte, error) {
	if c.private == nil {
		return nil, fmt.Errorf("%s: no private key is configured, so this key can only encrypt", rsaAlgName)
	}

	plaintext, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, c.private, sealed, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"%s: could not open the value; it was sealed for a different key, or it has been altered", rsaAlgName)
	}
	return plaintext, nil
}

// maxPlaintext is what OAEP with SHA-256 leaves for the payload: the modulus,
// minus a hash for the label and a hash for the seed, minus the two bytes the
// padding spends on its own leading octets.
func (c *rsaCipher) maxPlaintext() int {
	const leadingOctets = 2
	return c.public.Size() - 2*sha256.Size - leadingOctets
}

// parsePublicKey reads a PKIX or PKCS#1 public key. Both spellings are in the
// wild and the PEM block type is not a reliable guide, so we try one and fall
// back to the other.
func parsePublicKey(data []byte) (*rsa.PublicKey, error) {
	block, err := decodePEM(data, "public key")
	if err != nil {
		return nil, err
	}

	if parsed, pkixErr := x509.ParsePKIXPublicKey(block.Bytes); pkixErr == nil {
		public, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("%s: the public key is not an RSA key", rsaAlgName)
		}
		return public, nil
	}

	public, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: the public key is not a PKIX or PKCS#1 RSA key", rsaAlgName)
	}
	return public, nil
}

// parsePrivateKey reads a PKCS#8 or PKCS#1 private key, for the same reason
// parsePublicKey accepts two encodings.
func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, err := decodePEM(data, "private key")
	if err != nil {
		return nil, err
	}

	if parsed, pkcs8Err := x509.ParsePKCS8PrivateKey(block.Bytes); pkcs8Err == nil {
		private, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%s: the private key is not an RSA key", rsaAlgName)
		}
		return private, nil
	}

	private, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: the private key is not a PKCS#8 or PKCS#1 RSA key", rsaAlgName)
	}
	return private, nil
}

// decodePEM unwraps the armour, naming which key failed so an operator with two
// of them configured knows which one to look at.
func decodePEM(data []byte, which string) (*pem.Block, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%s: the %s is not PEM-encoded", rsaAlgName, which)
	}
	return block, nil
}
