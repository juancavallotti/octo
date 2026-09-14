package expr

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"

	"github.com/juancavallotti/octo/runtime/core/cryptox"
)

// The message functions that keep a value secret rather than merely signed.
//
// A to* function seals its first argument under the key given as its second and
// returns the sealed bytes; the matching from* function reverses it. Both
// directions return bytes because rendering is a separate step — base64.encode
// and hexEncode already exist, and which one a counterparty expects is not
// something these functions should decide.
//
// The key is an argument. A binding cannot read the activation and has no path to
// a connector, so there is nowhere else for it to come from, and that is the
// right shape: an expression that encrypts says out loud which key it used.
// `env.NAME` is where an operator-held key is normally spelled.
//
// Asymmetric encryption is deliberately not here. A PEM keypair is not something
// to write into an expression, and parsing one per evaluation would be a silent
// cost with nothing to show for it.
const (
	toAesFuncName      = "toAes"
	fromAesFuncName    = "fromAes"
	toChachaFuncName   = "toChacha"
	fromChachaFuncName = "fromChacha"
)

func registerCipherExtension() {
	RegisterMessageExtension(func(MessageContext) []cel.EnvOption { return cipherOptions() })
}

// cipherOptions declares the encryption functions. Like the signing functions
// they operate purely on their arguments (no activation access), so plain binary
// functions suffice, and both arguments are DynType so a payload or a key may be
// a string or bytes without the author converting first.
func cipherOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cipherFunction(toAesFuncName, cryptox.NewAESGCM, seal),
		cipherFunction(fromAesFuncName, cryptox.NewAESGCM, open),
		cipherFunction(toChachaFuncName, cryptox.NewChaCha20Poly1305, seal),
		cipherFunction(fromChachaFuncName, cryptox.NewChaCha20Poly1305, open),
	}
}

// cipherBuilder makes a cipher from a key, and cipherDirection is the half of it
// that one function exposes.
type (
	cipherBuilder   func([]byte) (cryptox.Cipher, error)
	cipherDirection func(cryptox.Cipher, []byte) ([]byte, error)
)

func seal(c cryptox.Cipher, data []byte) ([]byte, error) { return c.Seal(data) }

func open(c cryptox.Cipher, data []byte) ([]byte, error) { return c.Open(data) }

// cipherFunction declares one function over one algorithm and one direction.
func cipherFunction(name string, build cipherBuilder, run cipherDirection) cel.EnvOption {
	return cel.Function(name,
		cel.Overload(name+"_dyn_dyn_bytes",
			[]*cel.Type{cel.DynType, cel.DynType}, cel.BytesType,
			cel.BinaryBinding(cipherBinding(name, build, run))))
}

// cipherBinding builds the cipher from the key on every evaluation. Key setup is
// a few microseconds and holding derived ciphers in a cache would mean holding
// key material for the life of the process, which is a worse trade than doing the
// arithmetic again.
func cipherBinding(name string, build cipherBuilder, run cipherDirection) func(data, key ref.Val) ref.Val {
	//nolint:ireturn // a CEL BinaryBinding returns the ref.Val interface
	return func(data, key ref.Val) ref.Val {
		d, celErr := celBytes(name, "data", data)
		if celErr != nil {
			return celErr
		}
		k, celErr := celBytes(name, "key", key)
		if celErr != nil {
			return celErr
		}

		cipher, err := build(k)
		if err != nil {
			return types.NewErr("%s: %v", name, err)
		}
		out, err := run(cipher, d)
		if err != nil {
			return types.NewErr("%s: %v", name, err)
		}
		return types.Bytes(out)
	}
}
