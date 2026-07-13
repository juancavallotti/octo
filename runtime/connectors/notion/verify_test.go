package notion

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
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
	proc, err := newVerify(types.Settings{"connector": "notion"}, blockDeps(t, ""))
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
	proc, err := newVerify(types.Settings{"connector": "notion"}, blockDeps(t, ""))
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
	proc, err := newVerify(types.Settings{"connector": "notion"}, blockDeps(t, ""))
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
	proc, err := newVerify(types.Settings{"connector": "notion"}, blockDeps(t, ""))
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

// unsignedMessage builds a message as the http source would deliver it, with no
// signature header — the shape Notion's handshake arrives in when the subscription
// is still being set up.
func unsignedMessage(t *testing.T, rawBody string) *types.Message {
	t.Helper()
	msg := blockMessage(t, nil)
	if err := msg.SetBodyJSON([]byte(rawBody)); err != nil {
		t.Fatalf("SetBodyJSON: %v", err)
	}
	msg.Variables.Set(defaultRawBodyVar, rawBody)
	return msg
}

// A connector with no verification token must still build: the handshake that
// delivers the token runs through this very block.
func TestVerifyBuildsWithoutVerificationToken(t *testing.T) {
	conn := startConnector(t, map[string]any{"token": "ntn-test"})
	if _, err := newVerify(types.Settings{"connector": "notion"}, depsFor(conn)); err != nil {
		t.Errorf("newVerify without a verification token: %v", err)
	}
}

// The bootstrap: with no token known, an unsigned handshake is accepted, its token
// captured, and real events then verify against it without a restart.
func TestVerifyBootstrapsHandshakeWithoutToken(t *testing.T) {
	const handshake = "secret_bootstrapped"
	conn := startConnector(t, map[string]any{"token": "ntn-test"})
	proc, err := newVerify(types.Settings{"connector": "notion"}, depsFor(conn))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}

	out, err := proc.Process(context.Background(), unsignedMessage(t, `{"verification_token":"`+handshake+`"}`))
	if err != nil {
		t.Fatalf("Process(handshake): %v", err)
	}
	if got, _ := out.Variables.String(verificationVar); got != handshake {
		t.Errorf("%s = %q, want %q", verificationVar, got, handshake)
	}
	if got := conn.VerificationToken(); got != handshake {
		t.Errorf("connector token = %q, want the captured %q", got, handshake)
	}

	// The captured token now authenticates real events, with no restart.
	raw := `{"type":"page.content_updated","entity":{"id":"p1","type":"page"}}`
	event := blockMessage(t, nil)
	if err := event.SetBodyJSON([]byte(raw)); err != nil {
		t.Fatalf("SetBodyJSON: %v", err)
	}
	event.Variables.Set(defaultRawBodyVar, raw)
	event.Variables.Set(defaultSignatureHeader, computeSig(handshake, []byte(raw)))

	if _, err := proc.Process(context.Background(), event); err != nil {
		t.Errorf("Process(event signed with the captured token): %v", err)
	}
}

// The bootstrap window closes once a token is known: a handshake is no longer a
// free pass, so nobody can push a token into a running, configured service.
func TestVerifyRejectsUnsignedHandshakeOnceTokenIsKnown(t *testing.T) {
	proc, err := newVerify(types.Settings{"connector": "notion"}, blockDeps(t, ""))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}

	msg := unsignedMessage(t, `{"verification_token":"attacker_token"}`)
	if _, err := proc.Process(context.Background(), msg); err == nil {
		t.Error("expected an unsigned handshake to be rejected once a token is configured")
	}
}

// The exemption covers only a body that is nothing but the handshake — an event
// cannot smuggle itself through by carrying a verification_token field.
func TestVerifyRejectsUnsignedEventCarryingAToken(t *testing.T) {
	conn := startConnector(t, map[string]any{"token": "ntn-test"})
	proc, err := newVerify(types.Settings{"connector": "notion"}, depsFor(conn))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}

	msg := unsignedMessage(t, `{"verification_token":"x","type":"page.content_updated"}`)
	if _, err := proc.Process(context.Background(), msg); err == nil {
		t.Error("expected an unsigned event carrying a verification_token to be rejected")
	}
	if got := conn.VerificationToken(); got != "" {
		t.Errorf("connector captured %q from a non-handshake body", got)
	}
}

// With no token known, a request that is not a handshake still has nothing to be
// checked against, so it is rejected.
func TestVerifyRejectsEventWithoutToken(t *testing.T) {
	conn := startConnector(t, map[string]any{"token": "ntn-test"})
	proc, err := newVerify(types.Settings{"connector": "notion"}, depsFor(conn))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}

	msg := unsignedMessage(t, `{"type":"page.content_updated","entity":{"id":"p1"}}`)
	if _, err := proc.Process(context.Background(), msg); err == nil {
		t.Error("expected an event to be rejected while no verification token is known")
	}
}
