package notion

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/types"
)

// signedMessage builds a message as the http source would deliver it: the parsed
// body, the raw body captured in a variable, and Notion's signature header set to
// a valid signature for that raw body.
func signedMessage(t *testing.T, rawBody string) *types.Message {
	t.Helper()
	msg := blockMessage(t, nil)
	if err := msg.SetBodyJSON([]byte(rawBody)); err != nil {
		t.Fatalf("SetBodyJSON: %v", err)
	}
	msg.Variables.Set(defaultRawBodyVar, rawBody)
	msg.Variables.Set(defaultSignatureHeader, computeSig(verifyToken, []byte(rawBody)))
	return msg
}

func TestVerifyReadsRawContentBody(t *testing.T) {
	proc, err := newVerify(types.Settings{"connector": "notion"}, blockDeps(t))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}
	// No rawBody variable — the exact bytes live only in the raw-content Body.
	raw := `{"verification_token":"secret_abc123"}`
	msg := blockMessage(t, nil)
	msg.SetRawBody("application/json", raw)
	msg.Variables.Set(defaultSignatureHeader, computeSig(verifyToken, []byte(raw)))

	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	// The verified bytes are parsed back into Body (raw-content mode cleared) so the
	// handshake is flagged and the token is captured downstream.
	if got, _ := out.Variables.String(verificationVar); got != "secret_abc123" {
		t.Errorf("%s = %q, want secret_abc123", verificationVar, got)
	}
	if out.RawContent {
		t.Error("expected RawContent cleared after parsing the verified body")
	}
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	proc, err := newVerify(types.Settings{"connector": "notion"}, blockDeps(t))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}
	msg := blockMessage(t, map[string]any{"type": "page.created"})
	msg.Variables.Set(defaultRawBodyVar, "{}")
	msg.Variables.Set(defaultSignatureHeader, "sha256=deadbeef")

	if _, err := proc.Process(context.Background(), msg); err == nil {
		t.Error("expected an error for a bad signature")
	}
}

func TestVerifyFlagsHandshake(t *testing.T) {
	proc, err := newVerify(types.Settings{"connector": "notion"}, blockDeps(t))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}
	msg := signedMessage(t, `{"verification_token":"secret_xyz"}`)

	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got, _ := out.Variables.String(verificationVar); got != "secret_xyz" {
		t.Errorf("%s = %q, want secret_xyz", verificationVar, got)
	}
}

func TestVerifyPassesEventThrough(t *testing.T) {
	proc, err := newVerify(types.Settings{"connector": "notion"}, blockDeps(t))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}
	msg := signedMessage(t, `{"type":"page.content_updated","entity":{"id":"p1","type":"page"}}`)

	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	// A normal event is not a handshake, so no verification marker is set.
	if _, ok := out.Variables.String(verificationVar); ok {
		t.Error("did not expect the verification marker for a normal event")
	}
}

func TestVerifyRequiresVerificationToken(t *testing.T) {
	// A connector without a verification token cannot verify requests.
	conn := startConnector(t, map[string]any{"token": "ntn-test"})
	deps := depsFor(conn)
	if _, err := newVerify(types.Settings{"connector": "notion"}, deps); err == nil {
		t.Error("expected an error when the connector has no verification token")
	}
}
