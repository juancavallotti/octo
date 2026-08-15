package expr

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // G505: sha1 is here for legacy webhook schemes that sign with it, not for hashing anything of ours
	"crypto/sha256"
	"encoding/hex"
	"hash"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// The message functions for authenticating a signed webhook.
//
// Every provider worth verifying — GitHub, Slack, Stripe, Notion, Shopify —
// signs some arrangement of the request with a shared secret and puts the digest
// in a header. What differs between them is only which bytes are signed, how the
// digest is rendered, and what prefixes it. Those are all things an expression
// can already say; what was missing was the HMAC itself, a hex renderer, and a
// comparison that does not leak.
//
// So these are primitives rather than a per-provider block: a scheme nobody has
// implemented yet is a `validate` rule away, and no runtime release is needed to
// reach it. Base64 rendering already exists via ext.Encoders (see stdext.go).
const (
	hmacSha256FuncName    = "hmacSha256"
	hmacSha1FuncName      = "hmacSha1"
	hexEncodeFuncName     = "hexEncode"
	secureCompareFuncName = "secureCompare"
)

func registerCryptoExtension() {
	RegisterMessageExtension(func(MessageContext) []cel.EnvOption { return cryptoOptions() })
}

// cryptoOptions declares the signature-verification functions. They operate
// purely on their arguments (no activation access), so plain unary and binary
// functions suffice.
//
// The arguments are DynType because a key or a payload is a string in some
// schemes and bytes in others, and an author should not have to convert between
// them to write a comparison; each binding accepts either.
func cryptoOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function(hmacSha256FuncName,
			cel.Overload(hmacSha256FuncName+"_dyn_dyn_bytes",
				[]*cel.Type{cel.DynType, cel.DynType}, cel.BytesType,
				cel.BinaryBinding(hmacBinding(hmacSha256FuncName, sha256.New)))),
		cel.Function(hmacSha1FuncName,
			cel.Overload(hmacSha1FuncName+"_dyn_dyn_bytes",
				[]*cel.Type{cel.DynType, cel.DynType}, cel.BytesType,
				cel.BinaryBinding(hmacBinding(hmacSha1FuncName, sha1.New)))),
		cel.Function(hexEncodeFuncName,
			cel.Overload(hexEncodeFuncName+"_dyn_string",
				[]*cel.Type{cel.DynType}, cel.StringType,
				cel.UnaryBinding(hexEncodeBinding))),
		cel.Function(secureCompareFuncName,
			cel.Overload(secureCompareFuncName+"_dyn_dyn_bool",
				[]*cel.Type{cel.DynType, cel.DynType}, cel.BoolType,
				cel.BinaryBinding(secureCompareBinding))),
	}
}

// hmacBinding builds the binding for one hash: the HMAC of the payload under the
// key, as raw bytes. Rendering is a separate step so the same function serves a
// hex scheme and a base64 one.
func hmacBinding(name string, newHash func() hash.Hash) func(key, payload ref.Val) ref.Val {
	//nolint:ireturn // a CEL BinaryBinding returns the ref.Val interface
	return func(key, payload ref.Val) ref.Val {
		k, err := celBytes(name, "key", key)
		if err != nil {
			return err
		}
		p, err := celBytes(name, "payload", payload)
		if err != nil {
			return err
		}
		mac := hmac.New(newHash, k)
		mac.Write(p)
		return types.Bytes(mac.Sum(nil))
	}
}

// hexEncodeBinding renders bytes as lowercase hex, the form most providers put
// in their signature header.
//
//nolint:ireturn // a CEL UnaryBinding returns the ref.Val interface
func hexEncodeBinding(val ref.Val) ref.Val {
	b, err := celBytes(hexEncodeFuncName, "value", val)
	if err != nil {
		return err
	}
	return types.String(hex.EncodeToString(b))
}

// secureCompareBinding compares two values without a content-dependent early
// return.
//
// This is the function that makes the rest safe to offer. CEL's own == on
// strings stops at the first differing byte, and how long it took to stop is
// observable — which over enough requests is a way to learn the expected
// signature a byte at a time. hmac.Equal (subtle.ConstantTimeCompare underneath)
// does not. It does not hide the lengths, which is fine: for a fixed-width
// digest they are equal anyway.
//
//nolint:ireturn // a CEL BinaryBinding returns the ref.Val interface
func secureCompareBinding(lhs, rhs ref.Val) ref.Val {
	a, err := celBytes(secureCompareFuncName, "first argument", lhs)
	if err != nil {
		return err
	}
	b, err := celBytes(secureCompareFuncName, "second argument", rhs)
	if err != nil {
		return err
	}
	return types.Bool(hmac.Equal(a, b))
}

// celBytes takes a string or bytes argument as bytes, and reports anything else
// as an evaluation error naming the function and the argument that was wrong.
// The second return is a ref.Val rather than an error so a binding can return it
// directly, which is how cel-go surfaces a runtime failure.
//
//nolint:ireturn // the error half is a CEL error value, returned to the caller as-is
func celBytes(fn, arg string, val ref.Val) ([]byte, ref.Val) {
	switch v := val.Value().(type) {
	case string:
		return []byte(v), nil
	case []byte:
		return v, nil
	default:
		return nil, types.NewErr("%s: %s must be a string or bytes, got %v", fn, arg, val.Type())
	}
}
