package email

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// selfSignedTLS returns a server TLS config with a throwaway certificate for
// 127.0.0.1, which clients accept through tlsSkipVerify.
func selfSignedTLS(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	}
}

// listen opens a loopback listener, wrapped in TLS when implicit is set, and
// closes it with the test.
func listen(t *testing.T, cfg *tls.Config, implicit bool) net.Listener {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if implicit {
		ln = tls.NewListener(ln, cfg)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// hostPort splits a listener address into the host and port settings.
func hostPort(t *testing.T, ln net.Listener) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split address: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return host, port
}

// startConnector starts c with settings and stops it with the test.
func startConnector(t *testing.T, c core.Connector, settings map[string]any) {
	t.Helper()
	if err := c.Start(context.Background(), types.ConnectorConfig{Settings: settings}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
}

// depsWith returns BlockDeps that resolve conn under name.
func depsWith(name string, conn core.Connector) core.BlockDeps {
	return core.BlockDeps{Connector: func(n string) (core.Connector, bool) {
		if n == name {
			return conn, true
		}
		return nil, false
	}}
}

func newMessage(t *testing.T, body any) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = body
	return msg
}
