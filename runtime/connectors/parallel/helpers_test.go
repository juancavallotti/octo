package parallel

import (
	"encoding/base64"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// webhookKey is the raw HMAC key every verification test signs with, and
// webhookSecret is how Parallel would hand it over: base64 key material behind
// the whsec_ prefix. Built rather than written out, so the relationship between
// the two is visible in the test rather than assumed.
const webhookKey = "test-webhook-key"

var webhookSecret = secretPrefix + base64.StdEncoding.EncodeToString([]byte(webhookKey))

// blockDeps returns BlockDeps resolving a parallel connector pointed at baseURL
// under the name "parallel".
func blockDeps(t *testing.T, baseURL string) core.BlockDeps {
	t.Helper()
	conn := startConnector(t, map[string]any{
		"apiKey":        "pk-test",
		"webhookSecret": webhookSecret,
		"apiBaseURL":    baseURL,
	})
	return depsFor(conn)
}

// depsFor returns BlockDeps resolving conn under the name "parallel".
func depsFor(conn *Connector) core.BlockDeps {
	return core.BlockDeps{Connector: func(name string) (core.Connector, bool) {
		if name == "parallel" {
			return conn, true
		}
		return nil, false
	}}
}

func blockMessage(t *testing.T, body any) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = body
	return msg
}

func TestResolveConnectorErrors(t *testing.T) {
	deps := blockDeps(t, "")
	cases := []struct {
		name string
		conn string
		deps core.BlockDeps
	}{
		{"empty name", "", deps},
		{"unknown name", "nope", deps},
		{"no connectors available", "parallel", core.BlockDeps{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolveConnector(tc.conn, tc.deps); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestToStringSlice(t *testing.T) {
	got, err := toStringSlice("one query")
	if err != nil || len(got) != 1 || got[0] != "one query" {
		t.Errorf("bare string = %v, %v; want a one-element list", got, err)
	}
	got, err = toStringSlice([]any{"a", "b"})
	if err != nil || len(got) != 2 {
		t.Errorf("list = %v, %v; want two elements", got, err)
	}
	if _, err := toStringSlice([]any{"a", 1}); err == nil {
		t.Error("expected an error for a non-string element")
	}
	if _, err := toStringSlice(42); err == nil {
		t.Error("expected an error for a non-list value")
	}
}

func TestPutOptionalOmitsZero(t *testing.T) {
	payload := map[string]any{}
	putOptional(payload, "max_results", 0)
	putOptional(payload, "mode", "")
	putOptional(payload, "processor", "base")
	if _, ok := payload["max_results"]; ok {
		t.Error("a zero int should be omitted so Parallel's own default applies")
	}
	if _, ok := payload["mode"]; ok {
		t.Error("an empty string should be omitted")
	}
	if payload["processor"] != "base" {
		t.Errorf("processor = %v, want base", payload["processor"])
	}
}

func TestDeliverDefaultsToBody(t *testing.T) {
	result := map[string]any{"results": []any{}}

	msg := blockMessage(t, map[string]any{"in": true})
	if out := deliver(msg, "", result); out.Body == nil {
		t.Error("an empty resultVar should put the result in the body")
	}

	msg = blockMessage(t, map[string]any{"in": true})
	out := deliver(msg, "hits", result)
	if out.Variables["hits"] == nil {
		t.Error("a named resultVar should hold the result")
	}
	if body, _ := out.Body.(map[string]any); body["in"] != true {
		t.Error("a named resultVar should leave the body alone")
	}
}

func TestOrDefault(t *testing.T) {
	if orDefault("", "fallback") != "fallback" {
		t.Error("an empty value should fall back")
	}
	if orDefault("set", "fallback") != "set" {
		t.Error("a set value should win")
	}
}

func TestFailOnErrorDefaultsToTrue(t *testing.T) {
	if !failOnErrorDefault(nil) {
		t.Error("an unset failOnError should default to true")
	}
	explicit := false
	if failOnErrorDefault(&explicit) {
		t.Error("an explicit false should be honoured")
	}
}
