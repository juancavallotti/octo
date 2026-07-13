package slack

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// depsFor returns BlockDeps resolving conn under the name "slack".
func depsFor(conn *Connector) core.BlockDeps {
	return core.BlockDeps{Connector: func(name string) (core.Connector, bool) {
		if name == "slack" {
			return conn, true
		}
		return nil, false
	}}
}

// signedMessage builds a message as the http source would deliver it: the parsed
// body, the raw body captured in a variable, and Slack's signature headers set
// to a valid signature for that raw body.
func signedMessage(t *testing.T, rawBody string) *types.Message {
	t.Helper()
	msg := blockMessage(t, nil)
	if err := msg.SetBodyJSON([]byte(rawBody)); err != nil {
		t.Fatalf("SetBodyJSON: %v", err)
	}
	// The verify block checks the timestamp against time.Now(), so sign with a
	// current timestamp to stay within the skew window.
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg.Variables.Set(defaultRawBodyVar, rawBody)
	msg.Variables.Set(defaultTimestampHeader, ts)
	msg.Variables.Set(defaultSignatureHeader, computeSig("secret", ts, []byte(rawBody)))
	return msg
}

// rawContentMessage builds a message as the http source would deliver it in
// native raw-content mode (rawBody: true): the raw bytes are the message Body via
// SetRawBody, with no rawBody variable set, and Slack's signature headers signed
// over those bytes.
func rawContentMessage(t *testing.T, contentType, rawBody string) *types.Message {
	t.Helper()
	msg := blockMessage(t, nil)
	msg.SetRawBody(contentType, rawBody)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg.Variables.Set(defaultTimestampHeader, ts)
	msg.Variables.Set(defaultSignatureHeader, computeSig("secret", ts, []byte(rawBody)))
	return msg
}

func TestVerifyReadsRawContentBody(t *testing.T) {
	proc, err := newVerify(types.Settings{"connector": "slack"}, blockDeps(t, "http://unused"))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}
	// No rawBody variable — the exact bytes live only in the raw-content Body.
	msg := rawContentMessage(t, "application/json", `{"type":"url_verification","challenge":"abc123"}`)

	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	// The verified bytes are parsed back into Body (raw-content mode cleared) so
	// the challenge is flagged and body.challenge is echoable downstream.
	if got, _ := out.Variables.Bool(challengeVar); !got {
		t.Errorf("%s = %v, want true", challengeVar, out.Variables[challengeVar])
	}
	if out.RawContent {
		t.Error("expected RawContent cleared after parsing the verified body")
	}
	body, ok := out.Body.(map[string]any)
	if !ok || body["challenge"] != "abc123" {
		t.Errorf("body = %v, want the parsed url_verification object", out.Body)
	}
}

func TestVerifyExplicitVarWinsOverRawContent(t *testing.T) {
	proc, err := newVerify(types.Settings{
		"connector":  "slack",
		"rawBodyVar": "slackRaw",
	}, blockDeps(t, "http://unused"))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}
	// The explicit variable holds the true signed bytes; the raw-content Body is a
	// decoy that would fail the signature if consumed.
	raw := `{"type":"event_callback"}`
	msg := blockMessage(t, nil)
	msg.SetRawBody("application/json", `{"tampered":true}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg.Variables.Set("slackRaw", raw)
	msg.Variables.Set(defaultTimestampHeader, ts)
	msg.Variables.Set(defaultSignatureHeader, computeSig("secret", ts, []byte(raw)))

	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
}

func TestVerifyRawContentNonJSON(t *testing.T) {
	proc, err := newVerify(types.Settings{"connector": "slack"}, blockDeps(t, "http://unused"))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}
	// Slack slash commands arrive form-encoded, not JSON: verification must still
	// pass and the raw body is left untouched (no JSON reparse).
	msg := rawContentMessage(t, "application/x-www-form-urlencoded", "token=abc&command=%2Fweather")

	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
}

func TestVerifyRejectsBadSignature(t *testing.T) {
	proc, err := newVerify(types.Settings{"connector": "slack"}, blockDeps(t, "http://unused"))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}
	msg := blockMessage(t, map[string]any{"type": "event_callback"})
	msg.Variables.Set(defaultRawBodyVar, "{}")
	msg.Variables.Set(defaultTimestampHeader, "1531420618")
	msg.Variables.Set(defaultSignatureHeader, "v0=deadbeef")

	if _, err := proc.Process(context.Background(), msg); err == nil {
		t.Error("expected an error for a bad signature")
	}
}

func TestVerifyFlagsChallenge(t *testing.T) {
	proc, err := newVerify(types.Settings{"connector": "slack"}, blockDeps(t, "http://unused"))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}
	msg := signedMessage(t, `{"type":"url_verification","challenge":"abc123"}`)

	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	// The block flags the handshake but leaves the body for the flow to echo.
	if got, _ := out.Variables.Bool(challengeVar); !got {
		t.Errorf("%s = %v, want true", challengeVar, out.Variables[challengeVar])
	}
	body, ok := out.Body.(map[string]any)
	if !ok || body["challenge"] != "abc123" {
		t.Errorf("body = %v, want the url_verification object left intact", out.Body)
	}
}

func TestVerifyRequiresSigningSecret(t *testing.T) {
	// A connector without a signing secret cannot verify requests.
	conn := startConnector(t, map[string]any{"botToken": "xoxb-test"})
	deps := depsFor(conn)
	if _, err := newVerify(types.Settings{"connector": "slack"}, deps); err == nil {
		t.Error("expected an error when the connector has no signing secret")
	}
}

func TestEventNormalizesAndFilters(t *testing.T) {
	raw := `{"type":"event_callback","team_id":"T1","event_id":"Ev1",` +
		`"event":{"type":"app_mention","user":"U1","channel":"C1","text":"hi","ts":"1.2","thread_ts":"1.0"}}`

	proc, err := newEvent(types.Settings{"eventTypes": []any{"app_mention"}}, blockDeps(t, "http://unused"))
	if err != nil {
		t.Fatalf("newEvent: %v", err)
	}

	msg := blockMessage(t, nil)
	if err := msg.SetBodyJSON([]byte(raw)); err != nil {
		t.Fatalf("SetBodyJSON: %v", err)
	}
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil {
		t.Fatal("expected the app_mention to pass the filter")
	}
	body := out.Body.(map[string]any)
	if body["type"] != "app_mention" || body["user"] != "U1" || body["channel"] != "C1" {
		t.Errorf("normalized body = %v", body)
	}
	if body["text"] != "hi" || body["threadTs"] != "1.0" || body["teamId"] != "T1" {
		t.Errorf("normalized body = %v", body)
	}
	if _, ok := body["raw"].(map[string]any); !ok {
		t.Errorf("expected raw event under body.raw, got %T", body["raw"])
	}
}

func TestEventDropsDisallowedType(t *testing.T) {
	raw := `{"type":"event_callback","event":{"type":"reaction_added","user":"U1"}}`
	proc, err := newEvent(types.Settings{"eventTypes": []any{"app_mention"}}, blockDeps(t, "http://unused"))
	if err != nil {
		t.Fatalf("newEvent: %v", err)
	}
	msg := blockMessage(t, nil)
	if err := msg.SetBodyJSON([]byte(raw)); err != nil {
		t.Fatalf("SetBodyJSON: %v", err)
	}
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out != nil {
		t.Errorf("expected a disallowed event type to be dropped, got %v", out.Body)
	}
}

func TestEventFilterPredicate(t *testing.T) {
	// A message from a bot carries bot_id; the filter drops it.
	raw := `{"type":"event_callback","event":{"type":"message","user":"U1","bot_id":"B1","text":"beep"}}`
	proc, err := newEvent(types.Settings{
		"eventTypes": []any{"message"},
		"filter":     "body.botId == null", // keep only human messages
	}, blockDeps(t, "http://unused"))
	if err != nil {
		t.Fatalf("newEvent: %v", err)
	}
	msg := blockMessage(t, nil)
	if err := msg.SetBodyJSON([]byte(raw)); err != nil {
		t.Fatalf("SetBodyJSON: %v", err)
	}
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out != nil {
		t.Errorf("expected the bot message to be filtered out, got %v", out.Body)
	}
}
