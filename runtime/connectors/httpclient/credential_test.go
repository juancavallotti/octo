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

// authDeps is restDeps with the connector's own auth configured, so a block built
// against it is the "connector already authenticates" case.
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

func TestRESTForwardsCredential(t *testing.T) {
	var seen string
	srv := authRecorder(&seen)
	defer srv.Close()

	proc, err := newREST(types.Settings{
		"connector": "api",
		"path":      "/things",
		"auth":      `vars["Authorization"]`,
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}

	msg := restMessage(t, nil)
	msg.Variables.Set("Authorization", "Bearer caller-token")
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if seen != "Bearer caller-token" {
		t.Errorf("Authorization = %q, want the forwarded credential", seen)
	}
}

func TestRESTDynamicForwardsCredential(t *testing.T) {
	var seen string
	srv := authRecorder(&seen)
	defer srv.Close()

	proc, err := newRESTDynamic(types.Settings{
		"connector": "api",
		"method":    `"GET"`,
		"path":      `"/things"`,
		"auth":      `"Bearer " + body.token`,
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newRESTDynamic: %v", err)
	}

	msg := restMessage(t, map[string]any{"token": "minted"})
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if seen != "Bearer minted" {
		t.Errorf("Authorization = %q, want the forwarded credential", seen)
	}
}

// An empty credential is the message that simply carries none: the request goes
// out unauthenticated and the upstream decides, rather than the block failing.
func TestForwardedCredentialIsOptionalPerMessage(t *testing.T) {
	var seen string
	srv := authRecorder(&seen)
	defer srv.Close()

	proc, err := newREST(types.Settings{
		"connector": "api",
		"path":      "/things",
		"auth":      `has(body.token) ? body.token : ""`,
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}

	if _, err := proc.Process(context.Background(), restMessage(t, map[string]any{})); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if seen != "" {
		t.Errorf("Authorization = %q, want no header", seen)
	}
}

func TestForwardedCredentialRefusedWhenConnectorAuthenticates(t *testing.T) {
	deps := authDeps(t, "http://example.invalid", types.Settings{
		"type": "bearer", "token": "configured",
	})

	for _, tc := range []struct {
		name  string
		build func() error
	}{
		{"rest", func() error {
			_, err := newREST(types.Settings{
				"connector": "api", "path": "/things", "auth": `"Bearer x"`,
			}, deps)
			return err
		}},
		{"rest-dynamic", func() error {
			_, err := newRESTDynamic(types.Settings{
				"connector": "api", "method": `"GET"`, "path": `"/things"`, "auth": `"Bearer x"`,
			}, deps)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.build()
			if err == nil {
				t.Fatal("building the block succeeded, want a refusal")
			}
			if !strings.Contains(err.Error(), "auth is disabled") {
				t.Errorf("error = %v, want it to say the connector's auth must be disabled", err)
			}
		})
	}
}

// A credential carrying CR or LF would end the header line and let whatever
// produced the message append headers of its own.
//
// The leading and trailing cases are here because the guard has to run on the
// value exactly as rendered: trimming first would strip them out of view and
// admit a value nobody inspected.
func TestForwardedCredentialRejectsHeaderInjection(t *testing.T) {
	// The offending values are assembled from parts rather than written as one
	// literal -- gosec reads a credential-shaped string literal as a hardcoded
	// secret, and the point of each of these is the CR/LF, not the token.
	crlf := "\r\n"
	for name, relayed := range map[string]string{
		"embedded": "Bearer x" + crlf + "X-Admin: true",
		"leading":  crlf + "Bearer x",
		"trailing": "Bearer x" + crlf,
		"bare lf":  "Bearer x\nX-Admin: true",
	} {
		t.Run(name, func(t *testing.T) {
			var seen string
			srv := authRecorder(&seen)
			defer srv.Close()

			proc, err := newREST(types.Settings{
				"connector": "api", "path": "/things", "auth": "body.relayed",
			}, restDeps(t, srv.URL))
			if err != nil {
				t.Fatalf("newREST: %v", err)
			}

			_, err = proc.Process(context.Background(),
				restMessage(t, map[string]any{"relayed": relayed}))
			if err == nil {
				t.Fatal("Process succeeded, want a rejection")
			}
			if !strings.Contains(err.Error(), "carriage return") {
				t.Errorf("error = %v, want it to name the offending characters", err)
			}
			if seen != "" {
				t.Errorf("the request went out carrying %q; it should not have been made", seen)
			}
		})
	}
}

// A credential is sent exactly as rendered, so an upstream that is particular
// about its scheme gets what the flow wrote rather than a cleaned-up copy.
//
// The assertion is on interior spacing rather than surrounding whitespace: HTTP
// defines the space around a header value as optional whitespace, so a receiving
// parser strips it in transit whatever we send. That is the wire's normalisation,
// not ours.
func TestForwardedCredentialIsSentVerbatim(t *testing.T) {
	var seen string
	srv := authRecorder(&seen)
	defer srv.Close()

	proc, err := newREST(types.Settings{
		"connector": "api", "path": "/things", "auth": `"Bearer  spaced-out"`,
	}, restDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newREST: %v", err)
	}
	if _, err := proc.Process(context.Background(), restMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if seen != "Bearer  spaced-out" {
		t.Errorf("Authorization = %q, want the credential unaltered", seen)
	}
}

// Both blocks send an operator to the auth setting rather than accepting an
// Authorization header written by hand.
func TestAuthorizationHeaderIsRefusedByBothBlocks(t *testing.T) {
	deps := restDeps(t, "http://example.invalid")

	_, err := newREST(types.Settings{
		"connector": "api", "path": "/things",
		"headers": map[string]any{"Authorization": `"Bearer x"`},
	}, deps)
	if err == nil || !strings.Contains(err.Error(), "auth setting") {
		t.Errorf("rest error = %v, want it to point at the auth setting", err)
	}

	proc, err := newRESTDynamic(types.Settings{
		"connector": "api", "method": `"GET"`, "path": `"/things"`,
		"headers": `{"authorization": "Bearer x"}`,
	}, deps)
	if err != nil {
		t.Fatalf("newRESTDynamic: %v", err)
	}
	_, err = proc.Process(context.Background(), restMessage(t, nil))
	if err == nil || !strings.Contains(err.Error(), "auth setting") {
		t.Errorf("rest-dynamic error = %v, want it to point at the auth setting", err)
	}
}
