package pinecone

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestStartRequiresSettings(t *testing.T) {
	cases := []struct {
		name     string
		settings types.Settings
		want     string
	}{
		{"missing apiKey", types.Settings{"index": "docs", "dimension": 3}, "apiKey is required"},
		{"missing index", types.Settings{"apiKey": "key", "dimension": 3}, "index is required"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c := &Connector{}
			err := c.Start(context.Background(), types.ConnectorConfig{Name: "pc", Settings: tt.settings})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %v", tt.want, err)
			}
		})
	}
}

// controlPlane points the SDK at a stub control plane for the rest of the test.
// PINECONE_CONTROLLER_HOST is the SDK's own variable, not one this connector
// invented — the same seam the LLM samples use for a base URL, so the connector
// keeps exactly one host setting and it is the one users actually have.
func controlPlane(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("PINECONE_CONTROLLER_HOST", srv.URL)
	return srv
}

// describeIndexServer serves a canned DescribeIndex control-plane response for
// the given dimension, and reports the index host as its own URL so the
// data-plane connection (never dialed in this test — see the package comment
// on grpc.NewClient's lazy connect) can still be constructed without error.
func describeIndexServer(t *testing.T, dimension int) {
	t.Helper()
	var srv *httptest.Server
	srv = controlPlane(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":      "docs",
			"host":      strings.TrimPrefix(srv.URL, "http://"),
			"dimension": dimension,
			"metric":    "cosine",
		})
	})
}

func TestStartValidatesDimension(t *testing.T) {
	describeIndexServer(t, 1536)

	c := &Connector{}
	err := c.Start(context.Background(), types.ConnectorConfig{
		Name:     "pc",
		Settings: types.Settings{"apiKey": "key", "index": "docs", "dimension": 3072},
	})
	if err == nil || !strings.Contains(err.Error(), "has dimension 1536, configured dimension is 3072") {
		t.Fatalf("expected a dimension-mismatch error, got %v", err)
	}
}

func TestStartSucceedsOnMatchingDimension(t *testing.T) {
	describeIndexServer(t, 1536)

	c := &Connector{}
	err := c.Start(context.Background(), types.ConnectorConfig{
		Name:     "pc",
		Settings: types.Settings{"apiKey": "key", "index": "docs", "dimension": 1536},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if c.idxConn.Load() == nil {
		t.Fatal("expected an index connection to be set")
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestStartRejectsSparseIndex(t *testing.T) {
	var srv *httptest.Server
	srv = controlPlane(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"docs","host":"`+strings.TrimPrefix(srv.URL, "http://")+`","metric":"dotproduct"}`)
	})

	c := &Connector{}
	err := c.Start(context.Background(), types.ConnectorConfig{
		Name:     "pc",
		Settings: types.Settings{"apiKey": "key", "index": "docs", "dimension": 1536},
	})
	if err == nil || !strings.Contains(err.Error(), "sparse indexes are not supported") {
		t.Fatalf("expected a sparse-index error, got %v", err)
	}
}

// A configured host is the whole index address, so there is nothing left to ask
// the control plane. The stub fails the test if it is called at all: this is the
// property the sample's suite rests on — startup that touches no network.
func TestStartWithHostSkipsTheLookup(t *testing.T) {
	controlPlane(t, func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("the control plane was called (%s %s) despite a configured host", r.Method, r.URL.Path)
	})

	c := &Connector{}
	err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "pc",
		Settings: types.Settings{
			"apiKey": "key", "index": "docs", "dimension": 1536,
			"host": "docs-abc123.svc.aped-4627-b74a.pinecone.io",
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if c.idxConn.Load() == nil {
		t.Fatal("expected an index connection to be set")
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

// After Stop the connection is gone, and a message still in flight has to be
// told so rather than dereferencing it. Stopping twice is a no-op, not a double
// close.
func TestIndexConnectionAfterStop(t *testing.T) {
	describeIndexServer(t, 1536)

	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name:     "pc",
		Settings: types.Settings{"apiKey": "key", "index": "docs", "dimension": 1536},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := c.IndexConnection(""); err != nil {
		t.Fatalf("index connection while started: %v", err)
	}

	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if _, err := c.IndexConnection("tenant-a"); err == nil {
		t.Error("expected an error from a namespaced connection after Stop, got nil")
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("second stop: %v", err)
	}
}
