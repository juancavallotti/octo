package crypto

import (
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/cryptox"
	"github.com/juancavallotti/octo/runtime/types"
)

// kindEncrypt is the block type, and the name its errors are reported under.
const kindEncrypt = "encrypt"

func registerEncrypt() {
	core.MustRegisterBlock(kindEncrypt, newEncrypt)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     kindEncrypt,
		Label:    "Encrypt",
		Category: core.CategoryProcessor,
		Description: "Encrypts a value with a crypto connector's key and stores the ciphertext, " +
			"encoded as base64 or hex, in a variable or the message body.",
		Config: reflect.TypeFor[encryptSettings](),
	})
}

// encryptSettings is the encrypt block's typed configuration.
type encryptSettings struct {
	// Crypto connector whose key the value is encrypted with.
	Crypto string `json:"crypto" octo:"label=Crypto,ref=connector:crypto,required"`
	// CEL expression for the value to encrypt. Defaults to the whole body. A
	// non-string value is encrypted as its compact JSON.
	Value string `json:"value" octo:"label=Value,type=cel"`
	// How the ciphertext is rendered, since it travels as text.
	Encoding string `json:"encoding" octo:"label=Encoding,type=enum,enum=base64|hex,default=base64"`
	// Variable to store the ciphertext in. Leave empty to replace the message body.
	Target string `json:"target" octo:"label=Target variable"`
}

// newEncrypt builds an encrypt processor.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newEncrypt(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg encryptSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}

	return newCipherBlock(cipherConfig{
		kind:     kindEncrypt,
		crypto:   cfg.Crypto,
		value:    cfg.Value,
		encoding: cfg.Encoding,
		target:   cfg.Target,
	}, deps, sealing)
}

// sealing encrypts the value and renders the result.
func sealing(cipher cryptox.Cipher, enc codec) func(string) (string, error) {
	return func(value string) (string, error) {
		sealed, err := cipher.Seal([]byte(value))
		if err != nil {
			return "", err
		}
		return enc.encode(sealed), nil
	}
}
