package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// captureServer returns an httptest server that records the request path and
// decoded body and replies with the given JSON response.
func captureServer(t *testing.T, response string, path *string, body *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*path = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLookupUserFoldsResult(t *testing.T) {
	var path string
	var body map[string]any
	srv := captureServer(t, `{"ok":true,"user":{"id":"U1","name":"ada"}}`, &path, &body)

	proc, err := newLookupUser(types.Settings{
		"connector": "slack",
		"email":     "body.email",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newLookupUser: %v", err)
	}
	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{"email": "ada@x.io"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if path != "/users.lookupByEmail" {
		t.Errorf("path = %q, want /users.lookupByEmail", path)
	}
	if body["email"] != "ada@x.io" {
		t.Errorf("email = %v, want ada@x.io", body["email"])
	}
	user, ok := out.Variables[defaultUserVar].(map[string]any)
	if !ok || user["id"] != "U1" {
		t.Errorf("%s = %v, want the user object", defaultUserVar, out.Variables[defaultUserVar])
	}
}

func TestLookupUserByID(t *testing.T) {
	var path string
	var body map[string]any
	srv := captureServer(t, `{"ok":true,"user":{"id":"U9","name":"grace"}}`, &path, &body)

	proc, err := newLookupUser(types.Settings{
		"connector": "slack",
		"by":        "id",
		"user":      "body.userId",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newLookupUser: %v", err)
	}
	out, err := proc.Process(context.Background(), blockMessage(t, map[string]any{"userId": "U9"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if path != "/users.info" {
		t.Errorf("path = %q, want /users.info", path)
	}
	if body["user"] != "U9" {
		t.Errorf("user = %v, want U9", body["user"])
	}
	user, ok := out.Variables[defaultUserVar].(map[string]any)
	if !ok || user["id"] != "U9" {
		t.Errorf("%s = %v, want the user object", defaultUserVar, out.Variables[defaultUserVar])
	}
}

func TestLookupUserBuildValidation(t *testing.T) {
	deps := blockDeps(t, "http://unused")
	tests := []struct {
		name string
		raw  types.Settings
	}{
		{name: "email mode without email", raw: types.Settings{"connector": "slack"}},
		{name: "id mode without user", raw: types.Settings{"connector": "slack", "by": "id"}},
		{name: "unknown by", raw: types.Settings{"connector": "slack", "by": "name", "email": `"a@x.io"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newLookupUser(tt.raw, deps); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}

func TestAddReactionSendsTarget(t *testing.T) {
	var path string
	var body map[string]any
	srv := captureServer(t, `{"ok":true}`, &path, &body)

	proc, err := newAddReaction(types.Settings{
		"connector": "slack",
		"channel":   "body.channel",
		"timestamp": "body.ts",
		"emoji":     `"white_check_mark"`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newAddReaction: %v", err)
	}
	_, err = proc.Process(context.Background(), blockMessage(t, map[string]any{"channel": "C1", "ts": "1.2"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if path != "/reactions.add" {
		t.Errorf("path = %q, want /reactions.add", path)
	}
	if body["channel"] != "C1" || body["timestamp"] != "1.2" || body["name"] != "white_check_mark" {
		t.Errorf("payload = %v", body)
	}
}

func TestUpdateMessageSendsPayload(t *testing.T) {
	var path string
	var body map[string]any
	srv := captureServer(t, `{"ok":true,"ts":"1.2"}`, &path, &body)

	proc, err := newUpdateMessage(types.Settings{
		"connector": "slack",
		"channel":   "body.channel",
		"timestamp": "body.ts",
		"text":      `"edited"`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newUpdateMessage: %v", err)
	}
	_, err = proc.Process(context.Background(), blockMessage(t, map[string]any{"channel": "C1", "ts": "1.2"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if path != "/chat.update" {
		t.Errorf("path = %q, want /chat.update", path)
	}
	if body["channel"] != "C1" || body["ts"] != "1.2" || body["text"] != "edited" {
		t.Errorf("payload = %v", body)
	}
}

func TestUpdateMessageRequiresContent(t *testing.T) {
	deps := blockDeps(t, "http://unused")
	_, err := newUpdateMessage(types.Settings{
		"connector": "slack", "channel": `"C1"`, "timestamp": `"1.2"`,
	}, deps)
	if err == nil {
		t.Error("expected an error when both text and blocks are missing")
	}
}

func TestLookupUserFailOnError(t *testing.T) {
	var path string
	var body map[string]any
	srv := captureServer(t, `{"ok":false,"error":"users_not_found"}`, &path, &body)

	// failOnError=false passes the message through despite the Slack error.
	proc, err := newLookupUser(types.Settings{
		"connector": "slack", "email": `"nobody@x.io"`, "failOnError": false,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newLookupUser: %v", err)
	}
	out, err := proc.Process(context.Background(), blockMessage(t, nil))
	if err != nil {
		t.Errorf("Process with failOnError=false returned error: %v", err)
	}
	if out == nil {
		t.Error("expected the message to pass through with failOnError=false")
	}
}
