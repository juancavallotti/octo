// Package crypto provides a connector that owns key material and the
// encrypt/decrypt blocks that work through it. The connector holds the algorithm
// and the key; a block binds to it by name and never learns which cipher it got,
// so changing algorithms is a connector edit and no flow changes.
//
// Keys belong in the environment, not in a flow file: declare the variable and
// write `key: ${CRYPTO_KEY}`.
package crypto

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/cryptox"
	"github.com/juancavallotti/octo/runtime/types"
)

// The algorithms a crypto connector can be configured with, and the encodings a
// symmetric key can be written in.
const (
	algorithmAESGCM = "aes-gcm"
	algorithmChaCha = "chacha20-poly1305"
	algorithmRSA    = "rsa-oaep"

	keyEncodingBase64 = "base64"
	keyEncodingHex    = "hex"
	keyEncodingUTF8   = "utf8"
)

// init is this module's manifest: the one place that says what importing this
// package puts into the runtime, in a deterministic order. Each block's own
// registration lives beside the block as a registerX function called from here.
func init() {
	registerConnector()
	registerEncrypt()
	registerDecrypt()
}

func registerConnector() {
	core.MustRegisterConnector("crypto", func() core.Connector {
		return &Connector{}
	})

	// Package-level editor defaults: the connector and its two blocks share the
	// Data palette group and the KeyRound icon unless they set their own.
	core.RegisterExtension(core.ExtensionMeta{Group: "Data", Icon: "KeyRound"})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "crypto",
		Label:    "Crypto",
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

// connectorSettings is the key and the algorithm it is for. The symmetric and
// asymmetric halves are mutually exclusive in practice; which fields matter is
// decided by Algorithm.
type connectorSettings struct {
	// Cipher this connector's key is for.
	//nolint:lll // a struct tag cannot be wrapped, and the enum has to list every algorithm
	Algorithm string `json:"algorithm" octo:"label=Algorithm,type=enum,enum=aes-gcm|chacha20-poly1305|rsa-oaep,default=aes-gcm"`
	// Symmetric key, for aes-gcm and chacha20-poly1305; source from ${CRYPTO_KEY}.
	// Never logged.
	Key string `json:"key" octo:"label=Key"`
	// How Key is written. aes-gcm takes 16, 24 or 32 decoded bytes — which is what
	// selects AES-128, AES-192 or AES-256 — and chacha20-poly1305 takes 32.
	KeyEncoding string `json:"keyEncoding" octo:"label=Key encoding,type=enum,enum=base64|hex|utf8,default=base64"`
	// PEM public key, for rsa-oaep. A connector given only this one can encrypt
	// and not decrypt.
	PublicKey string `json:"publicKey" octo:"label=Public key,showIf=algorithm=rsa-oaep"`
	// PEM private key, for rsa-oaep. Never logged.
	PrivateKey string `json:"privateKey" octo:"label=Private key,showIf=algorithm=rsa-oaep"`
}

// Connector is a configured cipher that flows' encrypt and decrypt blocks work
// through. It holds no state beyond the key, so nothing needs closing on Stop.
type Connector struct {
	cipher cryptox.Cipher
}

// Start builds the cipher, so a key of the wrong length or a public key that is
// not PEM fails here rather than on the first message that needed it.
func (c *Connector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}

	cipher, err := buildCipher(set)
	if err != nil {
		return err
	}
	c.cipher = cipher
	return nil
}

// Stop releases nothing: the key lives as long as the connector does.
func (c *Connector) Stop(context.Context) error { return nil }

// Cipher returns the configured cipher. It is the capability an encrypt or
// decrypt block binds to by referencing this connector by name.
//
//nolint:ireturn // the capability is the cryptox.Cipher interface, so a block holds any algorithm
func (c *Connector) Cipher() (cryptox.Cipher, error) {
	if c.cipher == nil {
		return nil, fmt.Errorf("crypto connector not started")
	}
	return c.cipher, nil
}

// buildCipher is the one place that maps the configured algorithm to a cipher, so
// the blocks never branch on it.
//
//nolint:ireturn // buildCipher produces whichever cryptox.Cipher the settings asked for
func buildCipher(set connectorSettings) (cryptox.Cipher, error) {
	if set.Algorithm == algorithmRSA {
		return cryptox.NewRSAOAEP([]byte(set.PublicKey), []byte(set.PrivateKey))
	}

	key, err := decodeKey(set.Key, set.KeyEncoding)
	if err != nil {
		return nil, err
	}

	switch set.Algorithm {
	case "", algorithmAESGCM:
		return cryptox.NewAESGCM(key)
	case algorithmChaCha:
		return cryptox.NewChaCha20Poly1305(key)
	default:
		return nil, fmt.Errorf("crypto algorithm %q is not one of %s/%s/%s",
			set.Algorithm, algorithmAESGCM, algorithmChaCha, algorithmRSA)
	}
}

// decodeKey reads the key into the bytes the cipher will use.
//
// utf8 takes the setting's characters as the key bytes and derives nothing, so it
// is for a randomly generated key that happens to be written as text — not for a
// passphrase, whose entropy is its own however many bytes long it is. That is why
// base64 is the default: it is what a generator hands you.
func decodeKey(key, encoding string) ([]byte, error) {
	if key == "" {
		return nil, fmt.Errorf("crypto connector requires a key; source it from an env var, e.g. ${CRYPTO_KEY}")
	}

	switch encoding {
	case "", keyEncodingBase64:
		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil {
			return nil, fmt.Errorf("crypto key is not valid base64; set keyEncoding if it is written some other way")
		}
		return decoded, nil
	case keyEncodingHex:
		decoded, err := hex.DecodeString(key)
		if err != nil {
			return nil, fmt.Errorf("crypto key is not valid hex; set keyEncoding if it is written some other way")
		}
		return decoded, nil
	case keyEncodingUTF8:
		return []byte(key), nil
	default:
		return nil, fmt.Errorf("crypto keyEncoding %q is not one of %s/%s/%s",
			encoding, keyEncodingBase64, keyEncodingHex, keyEncodingUTF8)
	}
}
