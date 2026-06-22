package http

import (
	"context"
	"reflect"
	"testing"

	"github.com/juancavallotti/octo/types"
)

func TestNewSourceRejectsBadConfig(t *testing.T) {
	c, _ := startConnector(t, nil)
	out := make(chan *types.Message, 1)

	if _, err := c.NewSource(types.SourceConfig{Settings: map[string]any{}}, out); err == nil {
		t.Error("expected an error for a missing path")
	}
}

func TestNewSourceRejectsDuplicateRoute(t *testing.T) {
	c, _ := startConnector(t, map[string]any{"basePath": "/api"})
	out := make(chan *types.Message, 1)
	settings := map[string]any{"path": "/orders/{id}"}

	if _, err := c.NewSource(types.SourceConfig{Settings: settings}, out); err != nil {
		t.Fatalf("first NewSource: %v", err)
	}
	if _, err := c.NewSource(types.SourceConfig{Settings: settings}, out); err == nil {
		t.Error("expected an error registering the same route twice")
	}
}

func TestParsePathParams(t *testing.T) {
	tests := []struct {
		path string
		want []string
	}{
		{path: "/orders", want: nil},
		{path: "/orders/{id}", want: []string{"id"}},
		{path: "/orders/{id}/items/{itemId}", want: []string{"id", "itemId"}},
		{path: "/files/{rest...}", want: []string{"rest"}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := parsePathParams(tt.path); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePathParams(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestNormalizeBasePath(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "/", want: ""},
		{in: "/api/v1", want: "/api/v1"},
		{in: "/api/v1/", want: "/api/v1"},
		{in: "api/v1", want: "/api/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := normalizeBasePath(tt.in); got != tt.want {
				t.Errorf("normalizeBasePath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSourceStopUnblocksShutdown(t *testing.T) {
	c, _ := startConnector(t, nil)
	out := make(chan *types.Message)
	src, err := c.NewSource(types.SourceConfig{Settings: map[string]any{"path": "/x"}}, out)
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	if err := src.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// With no in-flight requests, Stop must return promptly.
	done := make(chan error, 1)
	go func() { done <- src.Stop(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop: %v", err)
		}
	case <-context.Background().Done():
	}
}
