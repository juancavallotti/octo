package httpclient

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/engine"
	"github.com/juancavallotti/octo/runtime/internal/testkit"
	"github.com/juancavallotti/octo/runtime/types"
)

// collector is the body flow's one block: it records every line it is run on,
// and stops the stream once it has seen stopAfter of them, when that is set.
type collector struct {
	lines     []string
	stopAfter int
}

// buildStream builds a rest-stream block through the engine, the way the runtime
// would, with "collect" registered as the leaf its body runs.
//
//nolint:ireturn // a test helper that returns the built MessageProcessor interface
func buildStream(t *testing.T, baseURL string, settings types.Settings, c *collector) core.MessageProcessor {
	t.Helper()
	reg := testkit.Registry()
	reg.MustRegister("collect", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return testkit.ProcessorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			c.lines = append(c.lines, msg.Variables["line"].(string))
			if c.stopAfter > 0 && len(c.lines) >= c.stopAfter {
				msg.RequestStop()
			}
			return msg, nil
		}), nil
	})
	settings["connector"] = "api"
	settings["body"] = map[string]any{"process": []any{map[string]any{"type": "collect"}}}

	flow, err := engine.BuildRoot(types.FlowConfig{
		Name:    "test",
		Process: []types.BlockConfig{{Type: "rest-stream", Settings: settings}},
	}, reg, testkit.Scheduler(), nil, restDeps(t, baseURL))
	if err != nil {
		t.Fatalf("build rest-stream: %v", err)
	}
	return flow.Blocks[0].Processor
}

// lineServer writes each line and flushes it, the way a log tail does.
func lineServer(t *testing.T, status int, lines ...string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		for _, line := range lines {
			_, _ = fmt.Fprintln(w, line)
			w.(http.Flusher).Flush()
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStreamRunsTheBodyOncePerLineInOrder(t *testing.T) {
	srv := lineServer(t, http.StatusOK, "one", "two", "three")
	var c collector
	proc := buildStream(t, srv.URL, types.Settings{"method": `"GET"`, "path": `"/logs"`}, &c)

	out, err := proc.Process(context.Background(), restMessage(t, nil))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got := strings.Join(c.lines, ","); got != "one,two,three" {
		t.Errorf("lines = %q, want one,two,three", got)
	}
	// The loop variable is the body's, and does not outlive the stream.
	if _, ok := out.Variables["line"]; ok {
		t.Error("the line variable leaked past the stream")
	}
}

// A stop in the body is how a relay notices its caller has gone, so it has to
// end the read there rather than drain the upstream.
func TestAStopInTheBodyEndsTheStream(t *testing.T) {
	srv := lineServer(t, http.StatusOK, "one", "two", "three")
	c := collector{stopAfter: 1}
	proc := buildStream(t, srv.URL, types.Settings{"method": `"GET"`, "path": `"/logs"`}, &c)

	out, err := proc.Process(context.Background(), restMessage(t, nil))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(c.lines) != 1 {
		t.Errorf("the body ran on %d lines, want 1", len(c.lines))
	}
	if !out.StopRequested() {
		t.Error("the stop did not reach the enclosing flow")
	}
}

// A refusal is a short answer rather than a stream: folded like rest-dynamic's,
// with no line run through the body.
func TestARefusalIsFoldedAndReadsNoLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"error": "no such pod"}`)
	}))
	t.Cleanup(srv.Close)
	var c collector
	proc := buildStream(t, srv.URL, types.Settings{
		"method": `"GET"`, "path": `"/logs"`, "failOnError": false,
	}, &c)

	out, err := proc.Process(context.Background(), restMessage(t, nil))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(c.lines) != 0 {
		t.Errorf("the body ran on a refusal: %v", c.lines)
	}
	if out.Variables["statusCode"] != http.StatusNotFound {
		t.Errorf("statusCode = %v, want 404", out.Variables["statusCode"])
	}
	if body, _ := out.Body.(map[string]any); body["error"] != "no such pod" {
		t.Errorf("body = %v, want the upstream's error", out.Body)
	}
}

// maxDuration is the bound the flow asked for, so an upstream that is still
// talking when it passes ends the stream cleanly rather than failing the flow.
func TestMaxDurationEndsAnOpenStreamWithoutAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "hello")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	var c collector
	proc := buildStream(t, srv.URL, types.Settings{
		"method": `"GET"`, "path": `"/logs"`, "maxDuration": "100ms",
	}, &c)

	start := time.Now()
	if _, err := proc.Process(context.Background(), restMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(c.lines) != 1 {
		t.Errorf("lines = %v, want the one written before the bound", c.lines)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the stream outlived its bound by %v", elapsed)
	}
}

// A stream must not be cut off by the connector's timeout, which is meant for
// requests that end: a healthy log tail outlives any sensible one.
func TestTheConnectorTimeoutDoesNotCutAStreamOff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		for _, line := range []string{"early", "late"} {
			_, _ = fmt.Fprintln(w, line)
			w.(http.Flusher).Flush()
			time.Sleep(300 * time.Millisecond)
		}
	}))
	t.Cleanup(srv.Close)

	conn := startConnector(t, types.Settings{"baseURL": srv.URL, "timeout": "100ms"})
	reg := testkit.Registry()
	var lines []string
	reg.MustRegister("collect", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return testkit.ProcessorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			lines = append(lines, msg.Variables["line"].(string))
			return msg, nil
		}), nil
	})
	flow, err := engine.BuildRoot(types.FlowConfig{
		Name: "test",
		Process: []types.BlockConfig{{Type: "rest-stream", Settings: types.Settings{
			"connector": "api", "method": `"GET"`, "path": `"/logs"`,
			"body": map[string]any{"process": []any{map[string]any{"type": "collect"}}},
		}}},
	}, reg, testkit.Scheduler(), nil, core.BlockDeps{Connector: func(string) (core.Connector, bool) {
		return conn, true
	}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if _, err := flow.Blocks[0].Processor.Process(context.Background(), restMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if strings.Join(lines, ",") != "early,late" {
		t.Errorf("lines = %v, want both, though the stream ran past the connector timeout", lines)
	}
}
