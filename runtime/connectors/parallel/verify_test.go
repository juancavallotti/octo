package parallel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/types"
)

// webhookID and webhookBody are what every signature here is made for.
const (
	webhookID   = "msg_1"
	webhookBody = `{"type":"task_run.status","data":{"run_id":"r1","status":"completed"}}`
)

// signedAt returns the Standard Webhooks signature header for webhookBody at ts.
// It signs the same string the connector does, so a mismatch is a real
// disagreement and not a test artifact. Cases that vary the id or the bytes vary
// them on the *message*, leaving the signature made for these ones — which is the
// whole point: both are inside the signed string.
func signedAt(ts int64, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	_, _ = mac.Write([]byte(webhookID + "." + strconv.FormatInt(ts, 10) + "." + webhookBody))
	return signatureVersion + "," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// webhookMessage builds the message the http source would have produced for a
// webhook: the three headers copied into variables, the exact bytes in rawBody,
// and the parsed body alongside them.
func webhookMessage(t *testing.T, id string, ts int64, body, signature string) *types.Message {
	t.Helper()
	msg := blockMessage(t, map[string]any{"type": "task_run.status"})
	msg.Variables.Set(defaultIDHeader, id)
	msg.Variables.Set(defaultTimestampHeader, strconv.FormatInt(ts, 10))
	msg.Variables.Set(defaultSignatureHeader, signature)
	msg.Variables.Set(defaultRawBodyVar, body)
	return msg
}

func TestVerifyAcceptsAValidSignature(t *testing.T) {
	proc, err := newVerify(types.Settings{"connector": "parallel"}, blockDeps(t, ""))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}

	ts := time.Now().Unix()
	msg := webhookMessage(t, webhookID, ts, webhookBody, signedAt(ts, webhookKey))
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
}

func TestVerifyRejects(t *testing.T) {
	ts := time.Now().Unix()
	valid := signedAt(ts, webhookKey)

	cases := []struct {
		name      string
		id        string
		ts        int64
		body      string
		signature string
	}{
		{"no signature", webhookID, ts, webhookBody, ""},
		{"a signature from a different key", webhookID, ts, webhookBody,
			signedAt(ts, "someone-elses-key")},
		// The id and the timestamp are inside the signed string, so replaying a
		// valid signature under either changed does not verify.
		{"a valid signature presented under a different id", "msg_2", ts, webhookBody, valid},
		{"a valid signature over different bytes", webhookID, ts,
			`{"type":"task_run.status","data":{"run_id":"r999","status":"completed"}}`, valid},
		{"an unversioned signature", webhookID, ts, webhookBody,
			base64.StdEncoding.EncodeToString([]byte("whatever"))},
		{"an unknown signature version", webhookID, ts, webhookBody, "v2," + valid[3:]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			proc, err := newVerify(types.Settings{"connector": "parallel"}, blockDeps(t, ""))
			if err != nil {
				t.Fatalf("newVerify: %v", err)
			}
			msg := webhookMessage(t, tc.id, tc.ts, tc.body, tc.signature)
			if _, err := proc.Process(context.Background(), msg); err == nil {
				t.Error("expected the request to be rejected")
			}
		})
	}
}

// A captured signature stays valid forever without a replay window, which is the
// reason the timestamp is signed at all.
func TestVerifyRejectsAStaleTimestamp(t *testing.T) {
	conn := startConnector(t, map[string]any{"apiKey": "pk-test", "webhookSecret": webhookSecret})
	now := time.Now()

	for _, offset := range []time.Duration{-2 * maxTimestampSkew, 2 * maxTimestampSkew} {
		ts := now.Add(offset).Unix()
		sig := signedAt(ts, webhookKey)
		if conn.VerifySignature(webhookID, strconv.FormatInt(ts, 10), sig, []byte(webhookBody), now) {
			t.Errorf("a signature %v out of date should be rejected", offset)
		}
	}

	// Inside the window it still verifies, so the bound is a replay limit and not
	// a clock-precision requirement.
	ts := now.Add(-maxTimestampSkew / 2).Unix()
	sig := signedAt(ts, webhookKey)
	if !conn.VerifySignature(webhookID, strconv.FormatInt(ts, 10), sig, []byte(webhookBody), now) {
		t.Error("a signature inside the skew window should be accepted")
	}
}

// While a secret is rotated Parallel sends both signatures, space-delimited.
// Either one authenticating the request is enough.
func TestVerifyAcceptsARotatedSignatureList(t *testing.T) {
	conn := startConnector(t, map[string]any{"apiKey": "pk-test", "webhookSecret": webhookSecret})
	now := time.Now()
	ts := now.Unix()
	tsStr := strconv.FormatInt(ts, 10)
	mine := signedAt(ts, webhookKey)
	other := signedAt(ts, "the-incoming-key")

	for _, header := range []string{mine + " " + other, other + " " + mine} {
		if !conn.VerifySignature(webhookID, tsStr, header, []byte(webhookBody), now) {
			t.Errorf("a list containing our signature should verify: %q", header)
		}
	}
	if conn.VerifySignature(webhookID, tsStr, other+" "+other, []byte(webhookBody), now) {
		t.Error("a list with none of our signatures must not verify")
	}
}

func TestVerifyRejectsMalformedTimestamps(t *testing.T) {
	conn := startConnector(t, map[string]any{"apiKey": "pk-test", "webhookSecret": webhookSecret})
	now := time.Now()
	sig := signedAt(now.Unix(), webhookKey)

	for _, ts := range []string{"", "not-a-number", "2026-08-31T00:00:00Z"} {
		if conn.VerifySignature(webhookID, ts, sig, []byte(webhookBody), now) {
			t.Errorf("timestamp %q should be rejected", ts)
		}
	}
	if conn.VerifySignature("", strconv.FormatInt(now.Unix(), 10), sig, []byte(webhookBody), now) {
		t.Error("a missing webhook id should be rejected")
	}
}

func TestVerifyRequiresWebhookSecret(t *testing.T) {
	conn := startConnector(t, map[string]any{"apiKey": "pk-test"})
	_, err := newVerify(types.Settings{"connector": "parallel"}, depsFor(conn))
	if err == nil {
		t.Fatal("expected the block to refuse to build without a webhookSecret")
	}

	// And the connector itself never verifies anything without one.
	now := time.Now()
	ts := strconv.FormatInt(now.Unix(), 10)
	if conn.VerifySignature(webhookID, ts, signedAt(now.Unix(), ""), []byte(webhookBody), now) {
		t.Error("a connector with no secret must not verify a signature")
	}
}

func TestVerifyReadsRawContentBody(t *testing.T) {
	proc, err := newVerify(types.Settings{"connector": "parallel"}, blockDeps(t, ""))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}

	ts := time.Now().Unix()
	msg := blockMessage(t, nil)
	msg.Variables.Set(defaultIDHeader, webhookID)
	msg.Variables.Set(defaultTimestampHeader, strconv.FormatInt(ts, 10))
	msg.Variables.Set(defaultSignatureHeader, signedAt(ts, webhookKey))
	msg.SetRawBody("application/json", webhookBody)

	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	// The verified bytes are re-parsed, so downstream body.* works the same as it
	// would with rawBodyVar.
	body, ok := out.Body.(map[string]any)
	if !ok || body["type"] != "task_run.status" {
		t.Fatalf("body = %v, want the parsed webhook payload", out.Body)
	}
	data, _ := body["data"].(map[string]any)
	if data["run_id"] != "r1" {
		t.Errorf("body.data.run_id = %v, want r1", data["run_id"])
	}
}

func TestVerifyExplicitVarWinsOverRawContent(t *testing.T) {
	proc, err := newVerify(types.Settings{
		"connector":  "parallel",
		"rawBodyVar": "captured",
	}, blockDeps(t, ""))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}

	ts := time.Now().Unix()
	msg := blockMessage(t, nil)
	msg.Variables.Set(defaultIDHeader, webhookID)
	msg.Variables.Set(defaultTimestampHeader, strconv.FormatInt(ts, 10))
	msg.Variables.Set(defaultSignatureHeader, signedAt(ts, webhookKey))
	msg.Variables.Set("captured", webhookBody)
	// Raw content carries different bytes; the configured variable must win, or
	// the signature would be checked against something the flow did not choose.
	msg.SetRawBody("application/json", `{"type":"task_run.status","data":{"run_id":"tampered"}}`)

	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
}

func TestVerifyCustomHeaderVariables(t *testing.T) {
	proc, err := newVerify(types.Settings{
		"connector":       "parallel",
		"idHeader":        "Webhook-Id",
		"timestampHeader": "Webhook-Timestamp",
		"signatureHeader": "Webhook-Signature",
	}, blockDeps(t, ""))
	if err != nil {
		t.Fatalf("newVerify: %v", err)
	}

	ts := time.Now().Unix()
	msg := blockMessage(t, map[string]any{"type": "task_run.status"})
	msg.Variables.Set("Webhook-Id", webhookID)
	msg.Variables.Set("Webhook-Timestamp", strconv.FormatInt(ts, 10))
	msg.Variables.Set("Webhook-Signature", signedAt(ts, webhookKey))
	msg.Variables.Set(defaultRawBodyVar, webhookBody)

	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
}
