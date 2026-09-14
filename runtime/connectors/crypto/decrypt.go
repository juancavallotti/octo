package crypto

import (
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/cryptox"
	"github.com/juancavallotti/octo/runtime/types"
)

// kindDecrypt is the block type, and the name its errors are reported under.
const kindDecrypt = "decrypt"

func registerDecrypt() {
	core.MustRegisterBlock(kindDecrypt, newDecrypt)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     kindDecrypt,
		Label:    "Decrypt",
		Category: core.CategoryProcessor,
		Description: "Decrypts a value with a crypto connector's key and stores the plaintext in a " +
			"variable or the message body. The plaintext is text; use fromJson on it to get a structured body.",
		Config: reflect.TypeFor[decryptSettings](),
	})
}

// decryptSettings is the decrypt block's typed configuration.
type decryptSettings struct {
	// Crypto connector whose key the value is decrypted with.
	Crypto string `json:"crypto" octo:"label=Crypto,ref=connector:crypto,required"`
	// CEL expression for the ciphertext to decrypt. Defaults to the whole body.
	Value string `json:"value" octo:"label=Value,type=cel"`
	// How the ciphertext being read is encoded.
	Encoding string `json:"encoding" octo:"label=Encoding,type=enum,enum=base64|hex,default=base64"`
	// Variable to store the plaintext in. Leave empty to replace the message body.
	Target string `json:"target" octo:"label=Target variable"`
}

// newDecrypt builds a decrypt processor.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newDecrypt(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg decryptSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}

	return newCipherBlock(cipherConfig{
		kind:     kindDecrypt,
		crypto:   cfg.Crypto,
		value:    cfg.Value,
		encoding: cfg.Encoding,
		target:   cfg.Target,
	}, deps, opening)
}

// opening reads the rendered ciphertext and decrypts it. A value that is not in
// the configured encoding is reported as that, rather than as a decryption
// failure, because it is a different mistake with a different fix.
func opening(cipher cryptox.Cipher, enc codec) func(string) (string, error) {
	return func(value string) (string, error) {
		sealed, err := enc.decode(value)
		if err != nil {
			return "", fmt.Errorf("the value is not in the configured encoding")
		}
		plaintext, err := cipher.Open(sealed)
		if err != nil {
			return "", err
		}
		return string(plaintext), nil
	}
}
