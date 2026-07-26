package httpclient

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/types"
)

// countingServer is a test server that counts the TCP connections it accepts, so
// a test can assert connections *per request* rather than guessing from timing.
// The symptom this file guards is invisible from inside the process: every
// request succeeds either way, and only the socket count tells them apart.
type countingServer struct {
	*httptest.Server
	conns atomic.Int64
}

func newCountingServer(t *testing.T, handler http.HandlerFunc) *countingServer {
	t.Helper()
	cs := &countingServer{}
	cs.Server = httptest.NewUnstartedServer(handler)
	cs.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			cs.conns.Add(1)
		}
	}
	cs.Start()
	t.Cleanup(cs.Close)
	return cs
}

// TestConcurrentRequestsReuseConnections is the regression check for the pooling
// bug: the connector built its client as &http.Client{Timeout: t}, leaving
// Transport nil. That means http.DefaultTransport, whose MaxIdleConnsPerHost is
// 2 — so past two concurrent requests to the same host, every further connection
// was closed instead of parked and the next request dialled again. Measured
// against a real backend that was ~1 new TCP connection per request, worsening
// as concurrency rose.
func TestConcurrentRequestsReuseConnections(t *testing.T) {
	const (
		concurrency = 16
		perWorker   = 8
		total       = concurrency * perWorker
	)
	// The handler holds each request open long enough that the workers genuinely
	// overlap. Against an instant local handler they mostly serialize, the peak
	// concurrency never approaches the worker count, and an idle pool of two is
	// enough to hide the bug — which is exactly why it took a 70ms backend under
	// load to expose it in the first place.
	srv := newCountingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(10 * time.Millisecond)
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	})
	c := startConnector(t, types.Settings{"baseURL": srv.URL})

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				if status, _ := get(t, c, "/thing"); status != http.StatusOK {
					t.Errorf("status = %d, want 200", status)
					return
				}
			}
		}()
	}
	wg.Wait()

	// The pool cannot hold fewer connections than the peak concurrency, so the
	// floor is roughly `concurrency`. What must not happen is a connection per
	// request: with the old idle pool of 2 this lands near `total`.
	conns := srv.conns.Load()
	if conns > concurrency*2 {
		t.Errorf("%d connections for %d requests at concurrency %d — connections are not being reused",
			conns, total, concurrency)
	}
	t.Logf("%d connections for %d requests", conns, total)
}

// TestDisableKeepAlivesOptsOut: the escape hatch has to actually opt out, since
// the whole point of it is an upstream that mishandles persistent connections.
func TestDisableKeepAlivesOptsOut(t *testing.T) {
	srv := newCountingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":true}`)
	})
	c := startConnector(t, types.Settings{
		"baseURL": srv.URL,
		"pool":    map[string]any{"disableKeepAlives": true},
	})

	const requests = 4
	for i := 0; i < requests; i++ {
		if status, _ := get(t, c, "/thing"); status != http.StatusOK {
			t.Fatalf("status = %d, want 200", status)
		}
	}
	if conns := srv.conns.Load(); conns != requests {
		t.Errorf("connections = %d, want %d (one per request with keep-alives off)", conns, requests)
	}
}

// TestTruncatedResponseStillReusesConnection covers the other way pooling fails
// even with a correct pool: net/http returns a connection to the idle pool only
// once its body reaches EOF, and maxResponseBytes stops the caller short of it
// by design. Every oversized response would otherwise cost a connection.
func TestTruncatedResponseStillReusesConnection(t *testing.T) {
	big := strings.Repeat("x", 4096)
	srv := newCountingServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, big)
	})
	c := startConnector(t, types.Settings{
		"baseURL":          srv.URL,
		"maxResponseBytes": 16,
	})

	const requests = 5
	for i := 0; i < requests; i++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/big", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("Do: %v", err)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("ReadAll: %v", err)
		}
		_ = resp.Body.Close()
		if len(body) != 16 {
			t.Fatalf("body = %d bytes, want the 16-byte cap", len(body))
		}
	}

	if conns := srv.conns.Load(); conns != 1 {
		t.Errorf("connections = %d for %d truncated responses, want 1 — a body abandoned "+
			"short of EOF is never returned to the pool", conns, requests)
	}
}
