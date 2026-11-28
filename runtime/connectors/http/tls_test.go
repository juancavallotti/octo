package http

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
	quit := make(chan struct{})
	var worker sync.WaitGroup
	worker.Go(func() {
		for {
			select {
			case <-quit:
				return
			case msg := <-out:
				msg.Body = map[string]any{"ok": true}
				core.DefaultEventBus().Publish(types.FlowEvent{
					Kind: types.FlowEventCompleted, EventID: msg.EventID, Result: msg,
				})
			}
		}
	})
	t.Cleanup(func() {
		close(quit)
		worker.Wait()
	})
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
	status, _ := getProto(t, client, url)
	return status
}

// getProto issues a GET and returns the status and the HTTP major version the
// response came back on.
func getProto(t *testing.T, client *http.Client, url string) (int, int) {
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
	return resp.StatusCode, resp.ProtoMajor
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

// TestTLSDualPortsServesHTTP2 pins that the HTTPS port answers HTTP/2 even when
// the plain accept loop starts first, the order in which net/http would otherwise
// skip setting HTTP/2 up. It drives the two loops itself to fix that order.
func TestTLSDualPortsServesHTTP2(t *testing.T) {
	certPEM, keyPEM := certstest.SelfSigned(t, "127.0.0.1")
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{Settings: map[string]any{
		"host": "127.0.0.1", "port": 0,
		"tls": map[string]any{"cert": string(certPEM), "key": string(keyPEM), "port": 0},
	}}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	plainUp := make(chan struct{})
	c.serveOnce.Do(func() {
		go func() {
			defer close(c.serving)
			var wg sync.WaitGroup
			wg.Go(func() { c.serveOn(c.ln, false) })
			<-plainUp
			wg.Go(func() { c.serveOn(c.secureLn, true) })
			wg.Wait()
		}()
	})
	// Any answer on the plain port proves its Serve ran before ServeTLS started.
	get(t, http.DefaultClient, "http://"+c.ln.Addr().String()+"/")
	close(plainUp)

	client := &http.Client{Transport: &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig:   &tls.Config{RootCAs: certstest.Pool(t, certPEM), MinVersion: tls.VersionTLS12},
	}}
	if _, proto := getProto(t, client, "https://"+c.secureLn.Addr().String()+"/"); proto != 2 {
		t.Fatalf("served HTTP/%d, want HTTP/2", proto)
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
	// The provider holds a pair of its own, so a case that wrongly falls back to
	// it fails the handshake against the certificate that case expects.
	providerCertPEM, providerKeyPEM := certstest.SelfSigned(t, "127.0.0.1")
	provided, err := certs.FromPEM(providerCertPEM, providerKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	svc := tlsServices{resources: map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM}, cert: provided}

	cases := map[string]struct {
		tls   map[string]any
		trust []byte
	}{
		"files":     {tls: map[string]any{"certFile": certFile, "keyFile": keyFile}, trust: certPEM},
		"resources": {tls: map[string]any{"certResource": "tls.crt", "keyResource": "tls.key"}, trust: certPEM},
		"provider":  {tls: map[string]any{"enabled": true}, trust: providerCertPEM},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := core.ContextWithRuntimeServices(context.Background(), svc)
			c := startTLS(ctx, t, map[string]any{"tls": tc.tls})
			if got := get(t, httpsClient(t, tc.trust), "https://"+c.ln.Addr().String()+"/ping"); got != http.StatusOK {
				t.Fatalf("status = %d", got)
			}
		})
	}
}

// TestTLSFromHTTPSPortEnv covers enablement without a tls block: $HTTPS_PORT plus
// a provisioned certificate moves the one listener of a connector on the injected
// port to HTTPS on that port. startTLS sets port 0, so $HTTP_PORT=0 makes that the
// injected port, as `port: ${HTTP_PORT}` would.
func TestTLSFromHTTPSPortEnv(t *testing.T) {
	certPEM, keyPEM := certstest.SelfSigned(t, "127.0.0.1")
	provided, err := certs.FromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	t.Setenv(envHTTPPort, "0")
	t.Setenv(envHTTPSPort, strconv.Itoa(port))

	ctx := core.ContextWithRuntimeServices(context.Background(), tlsServices{cert: provided})
	c := startTLS(ctx, t, map[string]any{})
	if c.secureLn != nil || !c.lnTLS {
		t.Fatal("HTTPS_PORT must move the one listener to HTTPS, not add a second")
	}
	if got := c.ln.Addr().(*net.TCPAddr).Port; got != port {
		t.Fatalf("listening on %d, want HTTPS_PORT %d", got, port)
	}
	if got := get(t, httpsClient(t, certPEM), "https://127.0.0.1:"+strconv.Itoa(port)+"/ping"); got != http.StatusOK {
		t.Fatalf("tls status = %d", got)
	}
}

func TestTLSFromHTTPSPortEnvWithoutCertificateServesHTTP(t *testing.T) {
	t.Setenv(envHTTPPort, "0")
	t.Setenv(envHTTPSPort, strconv.Itoa(freePort(t)))
	ctx := core.ContextWithRuntimeServices(context.Background(), tlsServices{})
	c := startTLS(ctx, t, map[string]any{})
	if c.secureLn != nil || c.lnTLS {
		t.Fatal("served TLS without a certificate")
	}
}

// TestHTTPSPortEnvIgnoresLiteralPort pins that $HTTPS_PORT leaves a connector
// bound to a port of its own on plain HTTP, even with a certificate available.
func TestHTTPSPortEnvIgnoresLiteralPort(t *testing.T) {
	certPEM, keyPEM := certstest.SelfSigned(t, "127.0.0.1")
	provided, err := certs.FromPEM(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(envHTTPPort, strconv.Itoa(freePort(t)))
	t.Setenv(envHTTPSPort, strconv.Itoa(freePort(t)))

	ctx := core.ContextWithRuntimeServices(context.Background(), tlsServices{cert: provided})
	c := startTLS(ctx, t, map[string]any{})
	if c.secureLn != nil || c.lnTLS {
		t.Fatal("HTTPS_PORT moved a connector that sets its own port")
	}
	if got := get(t, http.DefaultClient, "http://"+c.ln.Addr().String()+"/ping"); got != http.StatusOK {
		t.Fatalf("plain status = %d", got)
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
// whether or not it ever served. The first generation binds OS-assigned ports and
// the second re-binds exactly those, so no other listener can hold them between.
func TestStopReleasesBothTLSPorts(t *testing.T) {
	certPEM, keyPEM := certstest.SelfSigned(t, "127.0.0.1")
	start := func(port, tlsPort int) *Connector {
		t.Helper()
		c := &Connector{}
		if err := c.Start(context.Background(), types.ConnectorConfig{Settings: map[string]any{
			"host": "127.0.0.1", "port": port,
			"tls": map[string]any{"cert": string(certPEM), "key": string(keyPEM), "port": tlsPort},
		}}); err != nil {
			t.Fatalf("Start: %v", err)
		}
		return c
	}
	first := start(0, 0)
	port := first.ln.Addr().(*net.TCPAddr).Port
	tlsPort := first.secureLn.Addr().(*net.TCPAddr).Port
	if err := first.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	second := start(port, tlsPort)
	if err := second.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
