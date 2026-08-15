package expr

import (
	"strings"
	"testing"
)

// The known-answer vector every case below is built on. It is the one RFC 4231
// and every provider's own documentation use, so a wrong implementation fails
// here rather than in production against a real webhook.
const (
	testKey     = "key"
	testPayload = "The quick brown fox jumps over the lazy dog"
	// HMAC-SHA256(key, payload), hex.
	wantSha256 = "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8"
	// HMAC-SHA1(key, payload), hex.
	wantSha1 = "de7c9b85b8b78aa6bc8a7a36f70a90701c9db4d9"
)

// evalString compiles and evaluates a message expression against an empty
// activation, which is all these functions need.
func evalString(t *testing.T, expression string) string {
	t.Helper()
	prog, err := CompileMessage(nil, expression)
	if err != nil {
		t.Fatalf("compile %q: %v", expression, err)
	}
	out, err := prog.EvalString(messageActivation(nil))
	if err != nil {
		t.Fatalf("eval %q: %v", expression, err)
	}
	return out
}

func evalBool(t *testing.T, expression string) bool {
	t.Helper()
	prog, err := CompileMessage(nil, expression)
	if err != nil {
		t.Fatalf("compile %q: %v", expression, err)
	}
	out, err := prog.Eval(messageActivation(nil))
	if err != nil {
		t.Fatalf("eval %q: %v", expression, err)
	}
	b, ok := out.(bool)
	if !ok {
		t.Fatalf("eval %q returned %T, want bool", expression, out)
	}
	return b
}

func TestHmacKnownAnswers(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"sha256", `hexEncode(hmacSha256("key", "` + testPayload + `"))`, wantSha256},
		{"sha1", `hexEncode(hmacSha1("key", "` + testPayload + `"))`, wantSha1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalString(t, tc.expr); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestHmacAcceptsBytes covers the reason the arguments are dynamically typed: a
// payload reaches an expression as a string in one scheme and as bytes in
// another, and an author should not have to convert.
func TestHmacAcceptsBytes(t *testing.T) {
	got := evalString(t, `hexEncode(hmacSha256(b"key", b"`+testPayload+`"))`)
	if got != wantSha256 {
		t.Errorf("bytes arguments should hash the same as strings: got %q, want %q", got, wantSha256)
	}
}

// TestHmacIsBase64Renderable proves the split between computing and rendering:
// Shopify and Twilio put the same digest in the header base64-encoded, and that
// already works through ext.Encoders with no new function.
func TestHmacIsBase64Renderable(t *testing.T) {
	got := evalString(t, `base64.encode(hmacSha256("key", "`+testPayload+`"))`)
	if got == "" || strings.Contains(got, " ") {
		t.Errorf("base64.encode of an hmac should render: got %q", got)
	}
}

func TestSecureCompare(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want bool
	}{
		{"equal strings", `secureCompare("abc", "abc")`, true},
		{"different strings", `secureCompare("abc", "abd")`, false},
		{"different lengths", `secureCompare("abc", "abcd")`, false},
		{"an empty signature never passes", `secureCompare("", "abc")`, false},
		{"two empties are equal", `secureCompare("", "")`, true},
		{"a string and the same bytes", `secureCompare("abc", b"abc")`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalBool(t, tc.expr); got != tc.want {
				t.Errorf("%s = %v, want %v", tc.expr, got, tc.want)
			}
		})
	}
}

// TestVerifyAGitHubSignature is the whole feature in one expression, written the
// way a flow would: the prefix is compared as part of the signature rather than
// stripped off the incoming one, so a request that omits it fails.
func TestVerifyAGitHubSignature(t *testing.T) {
	signature := "sha256=" + wantSha256
	verify := func(header string) string {
		return `secureCompare("` + header + `", "sha256=" + hexEncode(hmacSha256("key", "` + testPayload + `")))`
	}
	if !evalBool(t, verify(signature)) {
		t.Error("a correctly signed payload should verify")
	}
	if evalBool(t, verify("sha256=deadbeef")) {
		t.Error("a wrong signature must not verify")
	}
	if evalBool(t, verify(wantSha256)) {
		t.Error("a signature missing its scheme prefix must not verify")
	}
	if evalBool(t, verify("")) {
		t.Error("a missing signature must not verify")
	}
}

// TestCryptoArgumentErrors: a bad argument fails at evaluation, naming the
// function and which argument was wrong. It compiles fine, because the arguments
// are dynamically typed.
func TestCryptoArgumentErrors(t *testing.T) {
	cases := map[string]string{
		"hmacSha256":    `hmacSha256(1, "x")`,
		"hexEncode":     `hexEncode(1)`,
		"secureCompare": `secureCompare("a", 1)`,
	}
	for fn, expression := range cases {
		t.Run(fn, func(t *testing.T) {
			prog, err := CompileMessage(nil, expression)
			if err != nil {
				t.Fatalf("compile %q: %v", expression, err)
			}
			if _, err := prog.Eval(messageActivation(nil)); err == nil {
				t.Errorf("%q should fail at evaluation", expression)
			} else if !strings.Contains(err.Error(), fn) {
				t.Errorf("error %q should name %q", err, fn)
			}
		})
	}
}
