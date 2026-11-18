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
	"sync"
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

// fakeServices is a RuntimeServices with fixed leadership and an in-memory KV,
// to drive the sources' leader gating and cursor in tests.
type fakeServices struct {
	leader bool
	kv     *memKV
}

//nolint:ireturn // implements core.RuntimeServices, which returns interfaces
func (f fakeServices) LeaderElection() core.LeaderElection { return fakeElection{leader: f.leader} }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Leases() core.Leases { return core.NoopLeases() }

//nolint:ireturn // implements core.RuntimeServices
func (f fakeServices) KV() core.KV { return f.kv }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Secrets() core.SecretStore { return nil }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Queues() core.Queues { return core.NoopQueues() }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Topics() core.Topics { return core.NoopTopics() }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Resources() core.ResourceLoader { return core.NoopResourceLoader{} }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) Traces() core.TracePublisher { return core.NoopTracer() }

//nolint:ireturn // implements core.RuntimeServices
func (fakeServices) AgentMemory() core.AgentMemory { return core.NoopAgentMemory() }
func (fakeServices) Close() error                  { return nil }

type fakeElection struct{ leader bool }

//nolint:ireturn // implements core.LeaderElection
func (f fakeElection) Acquire(context.Context, string) (core.Leadership, error) {
	return fakeLeadership(f), nil
}

type fakeLeadership struct{ leader bool }

func (f fakeLeadership) IsLeader() bool { return f.leader }
func (fakeLeadership) Close() error     { return nil }

// memKV is a versioned in-memory KV.
type memKV struct {
	mu      sync.Mutex
	entries map[string]core.Entry
}

func newMemKV() *memKV { return &memKV{entries: map[string]core.Entry{}} }

func (m *memKV) Get(_ context.Context, ns, key string) (core.Entry, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[ns+"/"+key]
	return e, ok, nil
}

func (m *memKV) Set(_ context.Context, ns, key string, value []byte, expected int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur := m.entries[ns+"/"+key]
	if cur.Version != expected {
		return 0, core.ErrVersionConflict
	}
	next := core.Entry{Value: append([]byte(nil), value...), Version: cur.Version + 1}
	m.entries[ns+"/"+key] = next
	return next.Version, nil
}

func (m *memKV) Delete(_ context.Context, ns, key string, _ int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, ns+"/"+key)
	return nil
}

// servicesCtx returns a context carrying fake services.
func servicesCtx(t *testing.T, leader bool, kv *memKV) context.Context {
	t.Helper()
	return core.ContextWithRuntimeServices(t.Context(), fakeServices{leader: leader, kv: kv})
}

// fakeFlow drains a source's output, records each message's subject, and
// publishes the outcome outcome(subject) picks on the flow-event bus.
type fakeFlow struct {
	mu       sync.Mutex
	subjects []string
}

func runFakeFlow(t *testing.T, out chan *types.Message, outcome func(subject string) types.FlowEventKind) *fakeFlow {
	t.Helper()
	f := &fakeFlow{}
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case msg := <-out:
				body, _ := msg.Body.(map[string]any)
				subject, _ := body[keySubject].(string)
				f.mu.Lock()
				f.subjects = append(f.subjects, subject)
				f.mu.Unlock()
				core.DefaultEventBus().Publish(types.FlowEvent{Kind: outcome(subject), EventID: msg.EventID})
			case <-done:
				return
			}
		}
	}()
	return f
}

func (f *fakeFlow) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.subjects...)
}

func completeAll(string) types.FlowEventKind { return types.FlowEventCompleted }

func typesConfig(settings map[string]any) types.ConnectorConfig {
	return types.ConnectorConfig{Settings: settings}
}

//nolint:ireturn // returns the KV the context's services carry
func servicesKV(ctx context.Context) core.KV {
	return core.RuntimeServicesFromContext(ctx).KV()
}
