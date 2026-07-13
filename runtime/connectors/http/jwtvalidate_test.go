package http

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// signRS256 mints an RS256 JWT with the given claims, signed by key. No kid is
// set; the static key set tries every configured key.
func signRS256(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	payload := enc.EncodeToString(payloadJSON)
	signingInput := header + "." + payload
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signingInput + "." + enc.EncodeToString(sig)
}

// pemPublicKey returns the PKIX PEM encoding of key's public part.
func pemPublicKey(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

const (
	testIssuer   = "https://issuer.test"
	testAudience = "my-api"
)

//nolint:ireturn // a test helper that returns the built MessageProcessor interface
func inlineJWTBlock(t *testing.T, key *rsa.PrivateKey) core.MessageProcessor {
	t.Helper()
	proc, err := newJWTValidate(types.Settings{
		"mode":       "inline",
		"issuer":     testIssuer,
		"audience":   testAudience,
		"algorithms": []any{"RS256"},
		"publicKey":  pemPublicKey(t, key),
	}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("build jwt-validate: %v", err)
	}
	return proc
}

func jwtMessage(t *testing.T, token string) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	if token != "" {
		msg.Variables.Set(defaultTokenHeader, "Bearer "+token)
	}
	return msg
}

func validClaims() map[string]any {
	return map[string]any{
		"iss": testIssuer,
		"aud": testAudience,
		"sub": "user-123",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
}

func TestJWTValidateAcceptsValidToken(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	proc := inlineJWTBlock(t, key)
	token := signRS256(t, key, validClaims())

	out, err := proc.Process(context.Background(), jwtMessage(t, token))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil || out.StopRequested() {
		t.Fatal("a valid token must pass through without stopping the flow")
	}
	if sub, _ := out.Variables.String("sub"); sub != "user-123" {
		t.Errorf("vars.sub = %q, want user-123", sub)
	}
	claims, ok := out.Variables[defaultClaimsVar].(map[string]any)
	if !ok || claims["iss"] != testIssuer {
		t.Errorf("vars.%s missing verified claims: %v", defaultClaimsVar, out.Variables[defaultClaimsVar])
	}
}

func TestJWTValidateRejects(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen other key: %v", err)
	}

	expired := validClaims()
	expired["exp"] = time.Now().Add(-time.Hour).Unix()
	wrongAud := validClaims()
	wrongAud["aud"] = "someone-else"

	tests := []struct {
		name  string
		token string
	}{
		{name: "missing token", token: ""},
		{name: "bad signature", token: signRS256(t, otherKey, validClaims())},
		{name: "wrong audience", token: signRS256(t, key, wrongAud)},
		{name: "expired", token: signRS256(t, key, expired)},
		{name: "garbage", token: "not.a.jwt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc := inlineJWTBlock(t, key)
			out, err := proc.Process(context.Background(), jwtMessage(t, tt.token))
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if out == nil || !out.StopRequested() {
				t.Fatal("an invalid token must reject and stop the flow")
			}
			if status, _ := out.Variables.Int(httpStatusVar); status != defaultRejectStatus {
				t.Errorf("status = %d, want %d", status, defaultRejectStatus)
			}
			body, ok := out.Body.(map[string]any)
			if !ok || body["error"] != "unauthorized" {
				t.Errorf("body = %v, want the unauthorized reject body", out.Body)
			}
		})
	}
}

func TestJWTValidateWWWAuthenticate(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	pub := pemPublicKey(t, key)
	const metadata = "https://rs.test/.well-known/oauth-protected-resource"

	build := func(extra types.Settings) core.MessageProcessor {
		s := types.Settings{"mode": "inline", "issuer": testIssuer, "audience": testAudience, "publicKey": pub}
		for k, v := range extra {
			s[k] = v
		}
		proc, err := newJWTValidate(s, core.BlockDeps{})
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		return proc
	}
	challengeFor := func(proc core.MessageProcessor, token string) (string, bool) {
		out, err := proc.Process(context.Background(), jwtMessage(t, token))
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		return out.Variables.String(wwwAuthenticateVar)
	}

	t.Run("default on with issuer, missing token omits error", func(t *testing.T) {
		ch, ok := challengeFor(build(types.Settings{"resourceMetadataUrl": metadata}), "")
		if !ok {
			t.Fatal("expected a WWW-Authenticate challenge by default when issuer is set")
		}
		if !strings.Contains(ch, `resource_metadata="`+metadata+`"`) {
			t.Errorf("challenge missing resource_metadata: %q", ch)
		}
		if !strings.Contains(ch, `realm="`+testIssuer+`"`) {
			t.Errorf("challenge missing realm: %q", ch)
		}
		if strings.Contains(ch, "invalid_token") {
			t.Errorf("missing-token challenge should not flag invalid_token: %q", ch)
		}
	})

	t.Run("invalid token flags invalid_token", func(t *testing.T) {
		ch, ok := challengeFor(build(nil), "not.a.jwt")
		if !ok || !strings.Contains(ch, `error="invalid_token"`) {
			t.Errorf("invalid-token challenge = %q, want error=invalid_token", ch)
		}
	})

	t.Run("disabled via checkbox", func(t *testing.T) {
		if ch, ok := challengeFor(build(types.Settings{"disableWwwAuthenticate": true}), ""); ok {
			t.Errorf("challenge should be disabled, got %q", ch)
		}
	})

	t.Run("off when no issuer configured", func(t *testing.T) {
		proc, err := newJWTValidate(types.Settings{"mode": "inline", "publicKey": pub}, core.BlockDeps{})
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		if ch, ok := challengeFor(proc, ""); ok {
			t.Errorf("challenge should be off without an issuer, got %q", ch)
		}
	})
}

func TestJWTValidateConfigErrors(t *testing.T) {
	tests := []struct {
		name     string
		settings types.Settings
	}{
		{name: "discover without issuer", settings: types.Settings{"mode": "discover"}},
		{name: "jwks without url", settings: types.Settings{"mode": "jwks"}},
		{name: "inline without key", settings: types.Settings{"mode": "inline"}},
		{name: "inline with both keys", settings: types.Settings{
			"mode": "inline", "publicKey": "x", "publicKeyResource": "y",
		}},
		{name: "unknown mode", settings: types.Settings{"mode": "nope"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newJWTValidate(tt.settings, core.BlockDeps{}); err == nil {
				t.Errorf("expected a config error for %s", tt.name)
			}
		})
	}
}

func TestParsePublicKeyRejectsGarbage(t *testing.T) {
	if _, err := parsePublicKey("not pem"); err == nil {
		t.Error("expected an error for non-PEM input")
	}
}
