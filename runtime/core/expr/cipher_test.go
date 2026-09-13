package expr

import (
	"strings"
	"testing"
)

// A 32-byte key, spelled the way an operator would put one in an env var, plus
// the plaintext every round-trip below carries.
const (
	testCipherKey       = "0123456789abcdef0123456789abcdef"
	testCipherPlaintext = "the launch codes are 0000"
)

// A round-trip is the only assertion available for a to* function: the nonce is
// fresh per call, so there is no fixed ciphertext to compare against.
func TestCipherRoundTrip(t *testing.T) {
	cases := []struct{ name, to, from string }{
		{"aes-gcm", toAesFuncName, fromAesFuncName},
		{"chacha20-poly1305", toChachaFuncName, fromChachaFuncName},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expression := `string(` + tc.from + `(` + tc.to +
				`("` + testCipherPlaintext + `", "` + testCipherKey + `"), "` + testCipherKey + `"))`
			if got := evalString(t, expression); got != testCipherPlaintext {
				t.Errorf("got %q, want %q", got, testCipherPlaintext)
			}
		})
	}
}

// The rendered form is what actually travels, so the base64 hop an author writes
// has to survive the round-trip too.
func TestCipherRoundTripThroughBase64(t *testing.T) {
	expression := `string(fromAes(base64.decode(base64.encode(toAes("` + testCipherPlaintext +
		`", "` + testCipherKey + `"))), "` + testCipherKey + `"))`
	if got := evalString(t, expression); got != testCipherPlaintext {
		t.Errorf("got %q, want %q", got, testCipherPlaintext)
	}
}

// Sealing twice must not produce the same bytes, or equal plaintexts would be
// visible as equal ciphertexts.
func TestToAesIsNotDeterministic(t *testing.T) {
	expression := `hexEncode(toAes("` + testCipherPlaintext + `", "` + testCipherKey + `"))`
	first, second := evalString(t, expression), evalString(t, expression)
	if first == second {
		t.Error("sealing the same plaintext twice produced identical bytes")
	}
}

// The two algorithms are not interchangeable, and mixing them has to fail rather
// than return something plausible.
func TestCipherAlgorithmsDoNotInterchange(t *testing.T) {
	expression := `string(fromChacha(toAes("` + testCipherPlaintext + `", "` + testCipherKey +
		`"), "` + testCipherKey + `"))`
	prog, err := CompileMessage(nil, expression)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := prog.Eval(messageActivation(nil)); err == nil {
		t.Error("a value sealed with aes-gcm opened as chacha20-poly1305")
	}
}

func TestCipherErrors(t *testing.T) {
	cases := []struct {
		name       string
		expression string
		wants      string
	}{
		{
			"wrong key length",
			`toAes("secret", "short")`,
			"key is 5 bytes",
		},
		{
			"data is neither string nor bytes",
			`toAes(42, "` + testCipherKey + `")`,
			"data must be a string or bytes",
		},
		{
			"key is neither string nor bytes",
			`toAes("secret", 42)`,
			"key must be a string or bytes",
		},
		{
			"value too short to have been sealed",
			`fromAes("nope", "` + testCipherKey + `")`,
			"too short",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prog, err := CompileMessage(nil, tc.expression)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			_, err = prog.Eval(messageActivation(nil))
			if err == nil {
				t.Fatalf("%q evaluated without error", tc.expression)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error %q does not mention %q", err, tc.wants)
			}
		})
	}
}
