package crypto

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/cryptox"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// What encrypt and decrypt do when nothing else is configured: work on the whole
// body, and render ciphertext as base64.
const (
	defaultValueExpression = "body"
	encodingBase64         = "base64"
	encodingHex            = "hex"
)

// cipherConfig is the part of the two blocks' settings that is identical in both,
// so one builder serves them.
type cipherConfig struct {
	kind     string
	crypto   string
	value    string
	encoding string
	target   string
}

// cipherBlock evaluates an expression, runs it through the cipher one way or the
// other, and lands the result. The direction is baked into transform at build
// time, so Process is the same code for both blocks.
type cipherBlock struct {
	kind      string
	value     *expr.Program
	env       expr.Env
	target    string
	transform func(string) (string, error)
}

// newCipherBlock resolves the connector and compiles the value expression once,
// so a bad connector reference or expression fails at startup rather than on the
// first message. direction receives the resolved cipher and returns the
// transformation that block performs.
func newCipherBlock(
	cfg cipherConfig,
	deps core.BlockDeps,
	direction func(cryptox.Cipher, codec) func(string) (string, error),
) (*cipherBlock, error) {
	cipher, err := resolveCipher(cfg.kind, cfg.crypto, deps)
	if err != nil {
		return nil, err
	}

	enc, err := resolveCodec(cfg.kind, cfg.encoding)
	if err != nil {
		return nil, err
	}

	expression := cfg.value
	if expression == "" {
		expression = defaultValueExpression
	}
	program, err := expr.CompileMessage(deps.Resources, expression)
	if err != nil {
		return nil, err
	}

	return &cipherBlock{
		kind:      cfg.kind,
		value:     program,
		env:       expr.EnvActivation(deps.Env),
		target:    cfg.target,
		transform: direction(cipher, enc),
	}, nil
}

// Process evaluates the value expression, transforms it, and stores the result in
// the target variable or replaces the message body when no target is set.
//
// Errors name the block and nothing else. A failure here is about a key or a
// tampered value, and repeating either in a log line is how secrets end up in an
// aggregator.
func (b *cipherBlock) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	value, err := b.value.EvalString(expr.MessageActivation(msg, b.env))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.kind, err)
	}

	out, err := b.transform(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", b.kind, err)
	}

	if b.target != "" {
		msg.Variables.Set(b.target, out)
	} else {
		msg.SetBody(out)
	}
	return msg, nil
}

// resolveCipher binds the block to its connector by concrete type, so a reference
// to a connector of some other kind is caught while the flow is being built.
//
//nolint:ireturn // the capability is the cryptox.Cipher interface
func resolveCipher(kind, name string, deps core.BlockDeps) (cryptox.Cipher, error) {
	if name == "" {
		return nil, fmt.Errorf("%s requires a crypto connector", kind)
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("crypto connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("crypto connector %q is not configured", name)
	}
	provider, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("connector %q does not provide a cipher", name)
	}
	return provider.Cipher()
}

// codec renders sealed bytes as text and reads them back. Ciphertext travels
// through a JSON body, so it is never raw bytes by the time a flow sees it.
type codec struct {
	encode func([]byte) string
	decode func(string) ([]byte, error)
}

// resolveCodec picks the rendering. Which one a flow wants is decided by whoever
// reads the ciphertext next, so it is a setting rather than a choice made here.
func resolveCodec(kind, encoding string) (codec, error) {
	switch encoding {
	case "", encodingBase64:
		return codec{
			encode: base64.StdEncoding.EncodeToString,
			decode: base64.StdEncoding.DecodeString,
		}, nil
	case encodingHex:
		return codec{
			encode: hex.EncodeToString,
			decode: hex.DecodeString,
		}, nil
	default:
		return codec{}, fmt.Errorf("%s encoding %q is not one of %s/%s",
			kind, encoding, encodingBase64, encodingHex)
	}
}
