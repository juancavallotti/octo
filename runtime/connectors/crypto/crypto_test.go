package crypto

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// A 32-byte key, which every algorithm here accepts, in each encoding the
// connector reads.
const (
	testKeyUTF8   = "0123456789abcdef0123456789abcdef"
	testKeyBase64 = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
)

// startedConnector builds and starts a connector over the given settings, which
// is the only way to reach a usable cipher.
func startedConnector(t *testing.T, settings types.Settings) *Connector {
	t.Helper()

	connector := &Connector{}
	cfg := types.ConnectorConfig{Name: "vault", Type: "crypto", Settings: settings}
	if err := connector.Start(context.Background(), cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}
	return connector
}

func TestConnectorServesACipherPerAlgorithm(t *testing.T) {
	for _, algorithm := range []string{"", algorithmAESGCM, algorithmChaCha} {
		t.Run(algorithm, func(t *testing.T) {
			connector := startedConnector(t, types.Settings{
				"algorithm": algorithm,
				"key":       testKeyBase64,
			})

			cipher, err := connector.Cipher()
			if err != nil {
				t.Fatalf("Cipher: %v", err)
			}
			sealed, err := cipher.Seal([]byte("secret"))
			if err != nil {
				t.Fatalf("Seal: %v", err)
			}
			opened, err := cipher.Open(sealed)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if string(opened) != "secret" {
				t.Errorf("got %q, want %q", opened, "secret")
			}
		})
	}
}

func TestCipherBeforeStart(t *testing.T) {
	if _, err := (&Connector{}).Cipher(); err == nil {
		t.Error("an unstarted connector served a cipher")
	}
}

func TestStopReleasesNothing(t *testing.T) {
	connector := startedConnector(t, types.Settings{"key": testKeyBase64})
	if err := connector.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

// A key of the wrong length is the mistake an operator actually makes, and it has
// to be caught at startup rather than on the first message.
func TestStartRejectsBadSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings types.Settings
		wants    string
	}{
		{
			name:     "no key",
			settings: types.Settings{},
			wants:    "requires a key",
		},
		{
			name:     "key too short",
			settings: types.Settings{"key": base64.StdEncoding.EncodeToString([]byte("short"))},
			wants:    "key is 5 bytes",
		},
		{
			name:     "key is not base64",
			settings: types.Settings{"key": "not base64!"},
			wants:    "not valid base64",
		},
		{
			name:     "key is not hex",
			settings: types.Settings{"key": "zz", "keyEncoding": keyEncodingHex},
			wants:    "not valid hex",
		},
		{
			name:     "unknown key encoding",
			settings: types.Settings{"key": testKeyUTF8, "keyEncoding": "rot13"},
			wants:    "keyEncoding",
		},
		{
			name:     "unknown algorithm",
			settings: types.Settings{"algorithm": "enigma", "key": testKeyBase64},
			wants:    "is not one of",
		},
		{
			name:     "chacha with a short key",
			settings: types.Settings{"algorithm": algorithmChaCha, "key": "0123456789abcdef", "keyEncoding": keyEncodingUTF8},
			wants:    "key is 16 bytes",
		},
		{
			name:     "rsa with no keys",
			settings: types.Settings{"algorithm": algorithmRSA},
			wants:    "public key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := types.ConnectorConfig{Name: "vault", Type: "crypto", Settings: tc.settings}
			err := (&Connector{}).Start(context.Background(), cfg)
			if err == nil {
				t.Fatal("Start accepted the settings")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error %q does not mention %q", err, tc.wants)
			}
		})
	}
}

// Every encoding has to arrive at the same bytes, since the same key gets written
// three different ways depending on where it came from.
func TestDecodeKeyAcrossEncodings(t *testing.T) {
	tests := []struct {
		encoding string
		key      string
	}{
		{keyEncodingBase64, testKeyBase64},
		{keyEncodingHex, hex.EncodeToString([]byte(testKeyUTF8))},
		{keyEncodingUTF8, testKeyUTF8},
	}

	for _, tc := range tests {
		t.Run(tc.encoding, func(t *testing.T) {
			got, err := decodeKey(tc.key, tc.encoding)
			if err != nil {
				t.Fatalf("decodeKey: %v", err)
			}
			if string(got) != testKeyUTF8 {
				t.Errorf("got %q, want %q", got, testKeyUTF8)
			}
		})
	}
}
