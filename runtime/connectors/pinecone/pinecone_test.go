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
		{"missing dimension", types.Settings{"apiKey": "key", "index": "docs"}, "dimension is required"},
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

// describeIndexServer serves a canned DescribeIndex control-plane response for
// the given dimension, and reports the index host as its own URL so the
// data-plane connection (never dialed in this test — see the package comment
// on grpc.NewClient's lazy connect) can still be constructed without error.
func describeIndexServer(t *testing.T, dimension int) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":      "docs",
			"host":      strings.TrimPrefix(srv.URL, "http://"),
			"dimension": dimension,
			"metric":    "cosine",
		})
	}))
	return srv
}

func TestStartValidatesDimension(t *testing.T) {
	srv := describeIndexServer(t, 1536)
	defer srv.Close()

	c := &Connector{}
	err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "pc",
		Settings: types.Settings{
			"apiKey": "key", "index": "docs", "dimension": 3072, "host": srv.URL,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "has dimension 1536, configured dimension is 3072") {
		t.Fatalf("expected a dimension-mismatch error, got %v", err)
	}
}

func TestStartSucceedsOnMatchingDimension(t *testing.T) {
	srv := describeIndexServer(t, 1536)
	defer srv.Close()

	c := &Connector{}
	err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "pc",
		Settings: types.Settings{
			"apiKey": "key", "index": "docs", "dimension": 1536, "host": srv.URL,
		},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if c.idxConn == nil {
		t.Fatal("expected an index connection to be set")
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestStartRejectsSparseIndex(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"docs","host":"`+strings.TrimPrefix(srv.URL, "http://")+`","metric":"dotproduct"}`)
	}))
	defer srv.Close()

	c := &Connector{}
	err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "pc",
		Settings: types.Settings{
			"apiKey": "key", "index": "docs", "dimension": 1536, "host": srv.URL,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "sparse indexes are not supported") {
		t.Fatalf("expected a sparse-index error, got %v", err)
	}
}
