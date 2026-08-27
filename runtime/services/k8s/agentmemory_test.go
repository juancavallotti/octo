package k8s

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

// memoryServer is a stand-in for the orchestrator's agent-memory routes, enough
// to exercise the client's request shaping and status handling. It is not a
// second implementation of the store — it answers what the routes answer.
type memoryServer struct {
	payload  []byte
	version  int64
	exists   bool
	turns    []map[string]any
	memories []map[string]any
	// notFound makes every route answer 404, which is what an orchestrator that
	// predates these routes does.
	notFound bool
	paths    []string
}

func (s *memoryServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// EscapedPath, not Path: the server decodes %2F back into a slash, so the
		// decoded form cannot tell an escaped key from one that really spans segments.
		s.paths = append(s.paths, r.Method+" "+r.URL.EscapedPath())
		if s.notFound {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/working") && r.Method == http.MethodGet:
			if !s.exists {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set(headerVersion, strconv.FormatInt(s.version, 10))
			w.Header().Set("X-Agent-Iteration", "3")
			w.Header().Set("X-Agent-Tokens", "42")
			_, _ = w.Write(s.payload)
		case strings.HasSuffix(r.URL.Path, "/working") && r.Method == http.MethodPut:
			expected, _ := strconv.ParseInt(r.Header.Get(headerVersion), 10, 64)
			current := int64(0)
			if s.exists {
				current = s.version
			}
			if expected != current {
				w.WriteHeader(http.StatusConflict)
				return
			}
			body, _ := io.ReadAll(r.Body)
			s.payload, s.version, s.exists = body, current+1, true
			w.Header().Set(headerVersion, strconv.FormatInt(s.version, 10))
		case strings.HasSuffix(r.URL.Path, "/turns"):
			var in struct {
				Turns []map[string]any `json:"turns"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			s.turns = append(s.turns, in.Turns...)
			_ = json.NewEncoder(w).Encode(map[string]int64{"version": int64(len(s.turns))})
		case strings.HasSuffix(r.URL.Path, "/memories") && r.Method == http.MethodGet:
			_ = json.NewEncoder(w).Encode(s.memories)
		case strings.Contains(r.URL.Path, "/memories/") && r.Method == http.MethodPut:
			w.Header().Set(headerVersion, "1")
			_ = json.NewEncoder(w).Encode(map[string]int64{"version": 1})
		case strings.HasSuffix(r.URL.Path, "/search"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"kind": "turn", "threadKey": "t1", "text": "an earlier answer", "seq": 2, "score": 0.5},
			})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// newTestMemory returns a client pointed at a stub orchestrator.
func newTestMemory(t *testing.T, srv *memoryServer) *agentMemory {
	t.Helper()
	ts := httptest.NewServer(srv.handler())
	t.Cleanup(ts.Close)
	m := newAgentMemory(ts.URL, "dep-1", "tok")
	t.Cleanup(m.close)
	return m
}

func memRef() core.MemoryRef {
	return core.MemoryRef{AgentID: "dr-octo", ThreadKey: "thread-1", UserID: "alice"}
}

// TestK8sMemoryWorkingRoundTrip covers the store-and-resume path and the headers
// the version and size ride on.
func TestK8sMemoryWorkingRoundTrip(t *testing.T) {
	srv := &memoryServer{}
	m := newTestMemory(t, srv)
	ctx := context.Background()

	if _, ok, err := m.LoadWorking(ctx, memRef()); err != nil || ok {
		t.Fatalf("a new conversation has no working memory (ok=%v err=%v)", ok, err)
	}
	v, err := m.SaveWorking(ctx, memRef(), core.WorkingMemory{
		Payload: []byte(`{"m":1}`), Iteration: 3, Tokens: 42,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if v != 1 {
		t.Errorf("a first write is version 1, got %d", v)
	}
	got, ok, err := m.LoadWorking(ctx, memRef())
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if string(got.Payload) != `{"m":1}` || got.Iteration != 3 || got.Tokens != 42 {
		t.Errorf("round trip lost something: %+v", got)
	}
}

// TestK8sMemoryVersionConflict checks the status the orchestrator uses for a
// stale write maps back to the sentinel the engine's retry loop knows.
func TestK8sMemoryVersionConflict(t *testing.T) {
	srv := &memoryServer{payload: []byte("a"), version: 5, exists: true}
	m := newTestMemory(t, srv)

	_, err := m.SaveWorking(context.Background(), memRef(), core.WorkingMemory{Version: 1})
	if !errors.Is(err, core.ErrVersionConflict) {
		t.Fatalf("want core.ErrVersionConflict, got %v", err)
	}
}

// TestK8sMemoryEscapesPathSegments checks that a thread key containing a slash
// addresses one conversation rather than inventing a route.
//
// It matters because thread keys are whatever a flow author's expression
// produces, and "user/42" is an entirely ordinary thing for one to produce. The
// assertion runs against a real ServeMux with the orchestrator's own pattern,
// because the interesting question is not whether the client escapes — it is
// whether the escaped form still routes to the single-segment wildcard and
// arrives back as the key that was sent.
func TestK8sMemoryEscapesPathSegments(t *testing.T) {
	var gotAgent, gotThread string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /deployments/{id}/agent-memory/{agentId}/threads/{threadKey}/working",
		func(w http.ResponseWriter, r *http.Request) {
			gotAgent, gotThread = r.PathValue("agentId"), r.PathValue("threadKey")
			w.WriteHeader(http.StatusNotFound)
		})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	m := newAgentMemory(ts.URL, "dep-1", "")
	defer m.close()
	ref := core.MemoryRef{AgentID: "dr octo", ThreadKey: "user/42", UserID: "alice"}
	if _, _, err := m.LoadWorking(context.Background(), ref); err != nil {
		t.Fatalf("load: %v", err)
	}
	if gotThread != "user/42" {
		t.Errorf("the thread key should arrive intact, got %q", gotThread)
	}
	if gotAgent != "dr octo" {
		t.Errorf("the agent id should arrive intact, got %q", gotAgent)
	}
}

// TestK8sMemoryAppendTurns checks the durable record reaches the orchestrator
// with its roles intact.
func TestK8sMemoryAppendTurns(t *testing.T) {
	srv := &memoryServer{}
	m := newTestMemory(t, srv)

	if _, err := m.AppendTurns(context.Background(), memRef(), []core.Turn{
		{Role: core.LLMRoleUser, Text: "a question"},
		{Role: core.LLMRoleAssistant, Text: "an answer"},
	}); err != nil {
		t.Fatalf("append: %v", err)
	}
	if len(srv.turns) != 2 {
		t.Fatalf("want 2 turns delivered, got %d", len(srv.turns))
	}
	if srv.turns[0]["role"] != "user" || srv.turns[1]["role"] != "assistant" {
		t.Errorf("roles did not survive the wire: %+v", srv.turns)
	}
}

// TestK8sMemorySearch checks results come back mapped, including which store
// each hit came from.
func TestK8sMemorySearch(t *testing.T) {
	srv := &memoryServer{}
	m := newTestMemory(t, srv)

	hits, err := m.Search(context.Background(), core.MemoryQuery{
		AgentID: "dr-octo", UserID: "alice", Text: "earlier",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 || hits[0].Kind != core.MemoryHitTurn || hits[0].Seq != 2 {
		t.Errorf("search result did not map: %+v", hits)
	}
}

// TestK8sMemoryDegradesWhenTheOrchestratorIsOlder is the whole reason the
// rollout has no ordering constraint.
//
// The runtime and the orchestrator are separate images with separate tags, so a
// pod built with this client can meet an orchestrator that predates the routes.
// The alternative to degrading is every agent run failing on a 404 from a store
// the runtime was told it had.
func TestK8sMemoryDegradesWhenTheOrchestratorIsOlder(t *testing.T) {
	srv := &memoryServer{notFound: true}
	m := newTestMemory(t, srv)
	ctx := context.Background()

	if !m.Enabled() {
		t.Fatal("the store should start enabled; it cannot know until it asks")
	}
	_, err := m.SaveWorking(ctx, memRef(), core.WorkingMemory{Payload: []byte("x")})
	if !errors.Is(err, core.ErrMemoryDisabled) {
		t.Fatalf("a 404 on a write means the routes are not served, got %v", err)
	}
	if m.Enabled() {
		t.Error("the store should latch off so the engine falls back rather than failing every run")
	}
}

// TestK8sMemoryDisabledWithoutAnOrchestrator covers the case where there is
// nothing to talk to at all.
func TestK8sMemoryDisabledWithoutAnOrchestrator(t *testing.T) {
	if newAgentMemory("", "dep-1", "tok").Enabled() {
		t.Error("no orchestrator URL means no store")
	}
	if newAgentMemory("http://example.invalid", "", "tok").Enabled() {
		t.Error("no deployment id means nothing to scope memory to")
	}
}

// TestK8sMemoryListingIsPlatformOnly pins the boundary: a pod knows a deployment,
// not an integration, and letting it enumerate would hand every pod a listing of
// every conversation on the installation.
func TestK8sMemoryListingIsPlatformOnly(t *testing.T) {
	srv := &memoryServer{}
	m := newTestMemory(t, srv)

	if _, _, err := m.ListThreads(context.Background(), "dr-octo", "alice", core.Page{}); err == nil {
		t.Error("listing conversations from a pod should be refused")
	}
	if _, _, _, err := m.ReadThread(context.Background(), memRef(), core.Page{}); err == nil {
		t.Error("reading a conversation from a pod should be refused")
	}
}

// TestK8sMemoryAuthorizes checks the bearer token rides on every request, since
// the routes are the same deployment-scoped surface KV authenticates on.
func TestK8sMemoryAuthorizes(t *testing.T) {
	var seen string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	m := newAgentMemory(ts.URL, "dep-1", "secret-token")
	defer m.close()
	_, _, _ = m.LoadWorking(context.Background(), memRef())

	if seen != "Bearer secret-token" {
		t.Errorf("want the bearer token on the request, got %q", seen)
	}
}
