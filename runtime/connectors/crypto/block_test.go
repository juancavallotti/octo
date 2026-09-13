package crypto

import (
	"context"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// deps wires the blocks to one started crypto connector named "vault", which is
// what a flow does through its connector list.
func deps(t *testing.T) core.BlockDeps {
	t.Helper()

	connector := startedConnector(t, types.Settings{"key": testKeyBase64})
	return core.BlockDeps{
		Connector: func(name string) (core.Connector, bool) {
			if name != "vault" {
				return nil, false
			}
			return connector, true
		},
	}
}

// message builds a message with the given body.
func message(t *testing.T, body any) *types.Message {
	t.Helper()

	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = body
	return msg
}

// The round-trip through both blocks is the property that matters: anything
// encrypt writes, decrypt has to read back.
func TestEncryptDecryptRoundTripsTheBody(t *testing.T) {
	for _, encoding := range []string{"", encodingBase64, encodingHex} {
		t.Run(encoding, func(t *testing.T) {
			d := deps(t)
			settings := types.Settings{"crypto": "vault", "encoding": encoding}

			encrypt, err := newEncrypt(settings, d)
			if err != nil {
				t.Fatalf("newEncrypt: %v", err)
			}
			decrypt, err := newDecrypt(settings, d)
			if err != nil {
				t.Fatalf("newDecrypt: %v", err)
			}

			msg := message(t, "the launch codes are 0000")
			sealed, err := encrypt.Process(context.Background(), msg)
			if err != nil {
				t.Fatalf("encrypt: %v", err)
			}
			if sealed.Body == "the launch codes are 0000" {
				t.Fatal("the body is still the plaintext")
			}

			opened, err := decrypt.Process(context.Background(), sealed)
			if err != nil {
				t.Fatalf("decrypt: %v", err)
			}
			if opened.Body != "the launch codes are 0000" {
				t.Errorf("got %v, want the plaintext back", opened.Body)
			}
		})
	}
}

// A target variable leaves the body alone, which is how a flow keeps the original
// message and carries the ciphertext beside it.
func TestEncryptToTargetVariableLeavesTheBody(t *testing.T) {
	d := deps(t)
	encrypt, err := newEncrypt(types.Settings{
		"crypto": "vault",
		"value":  "body.ssn",
		"target": "sealedSsn",
	}, d)
	if err != nil {
		t.Fatalf("newEncrypt: %v", err)
	}

	msg := message(t, map[string]any{"ssn": "078-05-1120"})
	out, err := encrypt.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	sealed, ok := out.Variables["sealedSsn"].(string)
	if !ok {
		t.Fatalf("the target variable holds %T, want a string", out.Variables["sealedSsn"])
	}
	if sealed == "078-05-1120" {
		t.Error("the target variable holds the plaintext")
	}
	body, ok := out.Body.(map[string]any)
	if !ok || body["ssn"] != "078-05-1120" {
		t.Errorf("the body should be untouched, got %v", out.Body)
	}

	decrypt, err := newDecrypt(types.Settings{
		"crypto": "vault",
		"value":  "vars.sealedSsn",
		"target": "ssn",
	}, d)
	if err != nil {
		t.Fatalf("newDecrypt: %v", err)
	}
	out, err = decrypt.Process(context.Background(), out)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if out.Variables["ssn"] != "078-05-1120" {
		t.Errorf("got %v, want the plaintext back", out.Variables["ssn"])
	}
}

// A structured body is encrypted as its JSON, so what comes back is that JSON
// text — which fromJson turns back into a body.
func TestEncryptStructuredBodyRoundTripsAsJSON(t *testing.T) {
	d := deps(t)
	encrypt, err := newEncrypt(types.Settings{"crypto": "vault"}, d)
	if err != nil {
		t.Fatalf("newEncrypt: %v", err)
	}
	decrypt, err := newDecrypt(types.Settings{"crypto": "vault"}, d)
	if err != nil {
		t.Fatalf("newDecrypt: %v", err)
	}

	sealed, err := encrypt.Process(context.Background(), message(t, map[string]any{"id": "7"}))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	opened, err := decrypt.Process(context.Background(), sealed)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if opened.Body != `{"id":"7"}` {
		t.Errorf("got %v, want the body's JSON", opened.Body)
	}
}

// Two connectors with different keys must not read each other's ciphertext, and
// the failure has to surface as an error rather than as garbled plaintext.
func TestDecryptRejectsAnotherKeysCiphertext(t *testing.T) {
	sealer := deps(t)
	other := core.BlockDeps{
		Connector: func(string) (core.Connector, bool) {
			return startedConnector(t, types.Settings{
				"key":         "fedcba9876543210fedcba9876543210",
				"keyEncoding": keyEncodingUTF8,
			}), true
		},
	}

	encrypt, err := newEncrypt(types.Settings{"crypto": "vault"}, sealer)
	if err != nil {
		t.Fatalf("newEncrypt: %v", err)
	}
	decrypt, err := newDecrypt(types.Settings{"crypto": "vault"}, other)
	if err != nil {
		t.Fatalf("newDecrypt: %v", err)
	}

	sealed, err := encrypt.Process(context.Background(), message(t, "secret"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := decrypt.Process(context.Background(), sealed); err == nil {
		t.Error("a value sealed under one key opened under another")
	}
}

// Reading a value that was never encrypted is a different mistake from reading one
// sealed with the wrong key, and it is reported as such.
func TestDecryptRejectsAValueInTheWrongEncoding(t *testing.T) {
	decrypt, err := newDecrypt(types.Settings{"crypto": "vault", "encoding": encodingHex}, deps(t))
	if err != nil {
		t.Fatalf("newDecrypt: %v", err)
	}

	_, err = decrypt.Process(context.Background(), message(t, "not hex at all"))
	if err == nil {
		t.Fatal("a value that is not hex was decrypted")
	}
	if !strings.Contains(err.Error(), "encoding") {
		t.Errorf("error %q should name the encoding", err)
	}
}

// A misconfigured block must fail while the flow is being built, not on the first
// message that reaches it.
func TestBlockRejectsBadSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings types.Settings
		wants    string
	}{
		{
			name:     "no connector named",
			settings: types.Settings{},
			wants:    "requires a crypto connector",
		},
		{
			name:     "connector not configured",
			settings: types.Settings{"crypto": "missing"},
			wants:    "is not configured",
		},
		{
			name:     "unknown encoding",
			settings: types.Settings{"crypto": "vault", "encoding": "rot13"},
			wants:    "encoding",
		},
		{
			name:     "bad expression",
			settings: types.Settings{"crypto": "vault", "value": "body."},
			wants:    "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := newEncrypt(tc.settings, deps(t))
			if err == nil {
				t.Fatal("newEncrypt accepted the settings")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error %q does not mention %q", err, tc.wants)
			}
		})
	}
}

// Without a connector list at all — a block built outside a running flow — the
// error says that, rather than reporting the connector as missing.
func TestBlockWithoutAnyConnectors(t *testing.T) {
	_, err := newEncrypt(types.Settings{"crypto": "vault"}, core.BlockDeps{})
	if err == nil {
		t.Fatal("newEncrypt built without any connectors")
	}
	if !strings.Contains(err.Error(), "no connectors are available") {
		t.Errorf("got %q", err)
	}
}

// A connector of some other type referenced by name is caught by the type
// assertion, not by a message that fails later.
func TestBlockRejectsANonCryptoConnector(t *testing.T) {
	d := core.BlockDeps{
		Connector: func(string) (core.Connector, bool) { return notACipher{}, true },
	}
	_, err := newEncrypt(types.Settings{"crypto": "vault"}, d)
	if err == nil {
		t.Fatal("newEncrypt accepted a connector that is not a crypto connector")
	}
	if !strings.Contains(err.Error(), "does not provide a cipher") {
		t.Errorf("got %q", err)
	}
}

// notACipher stands in for any other connector a flow might have declared.
type notACipher struct{}

func (notACipher) Start(context.Context, types.ConnectorConfig) error { return nil }

func (notACipher) Stop(context.Context) error { return nil }
