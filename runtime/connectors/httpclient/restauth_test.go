package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// authDeps starts a connector with auth of its own, so a block built against it
// is the case where a block credential and a connector credential both exist.
func authDeps(t *testing.T, baseURL string, auth types.Settings) core.BlockDeps {
	t.Helper()
	conn := startConnector(t, types.Settings{"baseURL": baseURL, "auth": auth})
	return core.BlockDeps{Connector: func(name string) (core.Connector, bool) {
		if name == "api" {
			return conn, true
		}
		return nil, false
	}}
}

// authRecorder serves 200 and records the Authorization header it was sent.
func authRecorder(seen *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
}

// A flow acting on behalf of its caller puts the caller's credential on the
// request through headers. rest-dynamic used to refuse the name outright, which
// left that flow with nowhere to put it.
func TestRESTDynamicSendsRenderedAuthorization(t *testing.T) {
	var seen string
	srv := authRecorder(&seen)
	defer srv.Close()

	proc, err := newRESTDynamic(types.Settings{
		"connector": "api",
		"method":    `"GET"`,
		"path":      `"/me"`,
		"headers":   `{"Authorization": vars["Authorization"]}`,
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newRESTDynamic: %v", err)
	}

	msg := restMessage(t, nil)
	msg.Variables.Set("Authorization", "Bearer caller-token")
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if seen != "Bearer caller-token" {
		t.Errorf("Authorization = %q, want the caller's credential", seen)
	}
}

// The static block has always been able to do this; it stays that way.
func TestRESTSendsConfiguredAuthorization(t *testing.T) {
	var seen string
	srv := authRecorder(&seen)
	defer srv.Close()

	proc, err := newREST(types.Settings{
		"connector": "api",
		"path":      "/me",
		"headers":   map[string]any{"Authorization": `"Bearer " + body.token`},
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}

	if _, err := proc.Process(context.Background(),
		restMessage(t, map[string]any{"token": "minted"})); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if seen != "Bearer minted" {
		t.Errorf("Authorization = %q, want the block's credential", seen)
	}
}

// A block credential and a connector credential do not contend: the connector
// applies its own only when the request does not already carry one. That is what
// makes per-caller auth expressible without disturbing a connector configured for
// the deployment's own calls.
func TestBlockAuthorizationWinsOverConnectorAuth(t *testing.T) {
	var seen string
	srv := authRecorder(&seen)
	defer srv.Close()
	deps := authDeps(t, srv.URL, types.Settings{"type": "bearer", "token": "deployment-token"})

	withHeader, err := newRESTDynamic(types.Settings{
		"connector": "api", "method": `"GET"`, "path": `"/me"`,
		"headers": `{"Authorization": "Bearer caller-token"}`,
	}, deps)
	if err != nil {
		t.Fatalf("newRESTDynamic: %v", err)
	}
	if _, err := withHeader.Process(context.Background(), restMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if seen != "Bearer caller-token" {
		t.Errorf("Authorization = %q, want the block's credential to stand", seen)
	}

	// The same connector still applies its own to a block that sets none.
	seen = ""
	bare, err := newRESTDynamic(types.Settings{
		"connector": "api", "method": `"GET"`, "path": `"/me"`,
	}, deps)
	if err != nil {
		t.Fatalf("newRESTDynamic: %v", err)
	}
	if _, err := bare.Process(context.Background(), restMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if seen != "Bearer deployment-token" {
		t.Errorf("Authorization = %q, want the connector's credential", seen)
	}
}

// A header value carrying CR or LF would end the header line. Nothing in these
// blocks checks for it, because net/http refuses to write such a request at all --
// this pins that, so the guarantee is not merely assumed.
func TestRenderedHeaderCannotInjectALine(t *testing.T) {
	var seen string
	srv := authRecorder(&seen)
	defer srv.Close()

	proc, err := newRESTDynamic(types.Settings{
		"connector": "api", "method": `"GET"`, "path": `"/me"`,
		"headers": `{"Authorization": body.relayed}`,
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newRESTDynamic: %v", err)
	}

	crlf := "\r\n"
	_, err = proc.Process(context.Background(),
		restMessage(t, map[string]any{"relayed": "Bearer x" + crlf + "X-Admin: true"}))
	if err == nil {
		t.Fatal("Process succeeded; the request should not have been sent")
	}
	if !strings.Contains(err.Error(), "invalid header field value") {
		t.Errorf("error = %v, want net/http's rejection of the header value", err)
	}
	if seen != "" {
		t.Errorf("a request reached the upstream carrying %q", seen)
	}
}
