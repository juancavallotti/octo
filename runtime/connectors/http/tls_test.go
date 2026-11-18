package http

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/certs"
	"github.com/juancavallotti/octo/runtime/core/certs/certstest"
	"github.com/juancavallotti/octo/runtime/types"
)

// tlsServices is a RuntimeServices stand-in that serves resources from a map and,
// when cert is set, provisions a server certificate.
type tlsServices struct {
	core.RuntimeServices
	resources map[string][]byte
	cert      *certs.Source
}

//nolint:ireturn // satisfies core.RuntimeServices
func (s tlsServices) Resources() core.ResourceLoader { return mapLoader(s.resources) }

func (s tlsServices) ServerCertificate() (*certs.Source, error) { return s.cert, nil }

type mapLoader map[string][]byte

func (m mapLoader) Load(_ context.Context, _ core.ResourceKind, id string) ([]byte, error) {
	if b, ok := m[id]; ok {
		return b, nil
	}
	return nil, core.ErrResourceNotFound
}

// startTLS starts a connector on loopback with ctx's runtime services, serves a
// /ping route answering {"ok":true}, and returns it.
func startTLS(ctx context.Context, t *testing.T, settings map[string]any) *Connector {
	t.Helper()
	settings["host"] = "127.0.0.1"
	settings["port"] = 0
	c := &Connector{}
	if err := c.Start(ctx, types.ConnectorConfig{Settings: settings}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	out := newSource(t, c, map[string]any{"path": "/ping"})
	go func() {
		for msg := range out {
			msg.Body = map[string]any{"ok": true}
			core.DefaultEventBus().Publish(types.FlowEvent{
				Kind: types.FlowEventCompleted, EventID: msg.EventID, Result: msg,
			})
		}
	}()
	return c
}

func httpsClient(t *testing.T, certPEM []byte) *http.Client {
	t.Helper()
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: certstest.Pool(t, certPEM), MinVersion: tls.VersionTLS12},
	}}
}

func get(t *testing.T, client *http.Client, url string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestTLSOnMainPort(t *testing.T) {
	certPEM, keyPEM := certstest.SelfSigned(t, "127.0.0.1")
	c := startTLS(context.Background(), t, map[string]any{
		"tls": map[string]any{"cert": string(certPEM), "key": string(keyPEM)},
	})
	if c.secureLn != nil {
		t.Fatal("a tls block without a port opened a second listener")
	}
	if got := get(t, httpsClient(t, certPEM), "https://"+c.ln.Addr().String()+"/ping"); got != http.StatusOK {
		t.Fatalf("status = %d", got)
	}
}

func TestTLSDualPorts(t *testing.T) {
	certPEM, keyPEM := certstest.SelfSigned(t, "127.0.0.1")
	c := startTLS(context.Background(), t, map[string]any{
		"tls": map[string]any{"cert": string(certPEM), "key": string(keyPEM), "port": 0},
	})
	if got := get(t, http.DefaultClient, "http://"+c.ln.Addr().String()+"/ping"); got != http.StatusOK {
		t.Fatalf("plain status = %d", got)
	}
	if got := get(t, httpsClient(t, certPEM), "https://"+c.secureLn.Addr().String()+"/ping"); got != http.StatusOK {
		t.Fatalf("tls status = %d", got)
	}
}

func TestTLSSources(t *testing.T) {
	certPEM, keyPEM := certstest.SelfSigned(t, "127.0.0.1")
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key")
	for path, data := range map[string][]byte{certFile: certPEM, keyFile: keyPEM} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	provided, err := certs.FromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	svc := tlsServices{resources: map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM}, cert: provided}

	cases := map[string]map[string]any{
		"files":     {"certFile": certFile, "keyFile": keyFile},
		"resources": {"certResource": "tls.crt", "keyResource": "tls.key"},
		"provider":  {"enabled": true},
	}
	for name, tlsSet := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := core.ContextWithRuntimeServices(context.Background(), svc)
			c := startTLS(ctx, t, map[string]any{"tls": tlsSet})
			if got := get(t, httpsClient(t, certPEM), "https://"+c.ln.Addr().String()+"/ping"); got != http.StatusOK {
				t.Fatalf("status = %d", got)
			}
		})
	}
}

// TestTLSFromHTTPSPortEnv covers enablement without a tls block: $HTTPS_PORT plus
// a provisioned certificate adds the HTTPS listener next to plain HTTP.
func TestTLSFromHTTPSPortEnv(t *testing.T) {
	certPEM, keyPEM := certstest.SelfSigned(t, "127.0.0.1")
	provided, err := certs.FromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	t.Setenv(envHTTPSPort, strconv.Itoa(port))

	ctx := core.ContextWithRuntimeServices(context.Background(), tlsServices{cert: provided})
	c := startTLS(ctx, t, map[string]any{})
	if c.secureLn == nil {
		t.Fatal("HTTPS_PORT with a provisioned certificate did not open the HTTPS listener")
	}
	if got := get(t, httpsClient(t, certPEM), "https://127.0.0.1:"+strconv.Itoa(port)+"/ping"); got != http.StatusOK {
		t.Fatalf("tls status = %d", got)
	}
	if got := get(t, http.DefaultClient, "http://"+c.ln.Addr().String()+"/ping"); got != http.StatusOK {
		t.Fatalf("plain status = %d", got)
	}
}

func TestTLSFromHTTPSPortEnvWithoutCertificateServesHTTP(t *testing.T) {
	t.Setenv(envHTTPSPort, strconv.Itoa(freePort(t)))
	ctx := core.ContextWithRuntimeServices(context.Background(), tlsServices{})
	c := startTLS(ctx, t, map[string]any{})
	if c.secureLn != nil || c.lnTLS {
		t.Fatal("served TLS without a certificate")
	}
}

func TestTLSSettingsErrors(t *testing.T) {
	certPEM, keyPEM := certstest.SelfSigned(t, "127.0.0.1")
	cases := map[string]struct {
		tls  map[string]any
		want string
	}{
		"two sources": {
			tls:  map[string]any{"cert": string(certPEM), "key": string(keyPEM), "certFile": "a", "keyFile": "b"},
			want: "one certificate source",
		},
		"half pair":      {tls: map[string]any{"cert": string(certPEM)}, want: "must be set together"},
		"no certificate": {tls: map[string]any{"enabled": true}, want: "no certificate"},
		"port only":      {tls: map[string]any{"port": 0}, want: "no certificate"},
		"bad version": {
			tls:  map[string]any{"cert": string(certPEM), "key": string(keyPEM), "minVersion": "1.1"},
			want: "minVersion",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := &Connector{}
			err := c.Start(context.Background(), types.ConnectorConfig{Settings: map[string]any{
				"host": "127.0.0.1", "port": 0, "tls": tc.tls,
			}})
			if err == nil {
				_ = c.Stop(context.Background())
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// TestStopReleasesBothTLSPorts pins that a dual-port connector frees both ports,
// whether or not it ever served.
func TestStopReleasesBothTLSPorts(t *testing.T) {
	certPEM, keyPEM := certstest.SelfSigned(t, "127.0.0.1")
	settings := map[string]any{
		"host": "127.0.0.1", "port": freePort(t),
		"tls": map[string]any{"cert": string(certPEM), "key": string(keyPEM), "port": freePort(t)},
	}
	for range 2 {
		c := &Connector{}
		if err := c.Start(context.Background(), types.ConnectorConfig{Settings: settings}); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if err := c.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	}
}
