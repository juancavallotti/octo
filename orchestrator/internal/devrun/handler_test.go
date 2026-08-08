package devrun

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/orchestrator/internal/kube"
)

// testHandler wires a Handler over the same fakes the service tests use, and returns a
// real mux to drive it through — so what these tests exercise is the routing table and
// the status codes, not a method that happens to share a name with a route.
func testHandler(t *testing.T, cluster *fakeCluster) (*http.ServeMux, *fakeSidecar) {
	t.Helper()
	svc, sidecar := testService(t, cluster, map[string]string{"int-1": networkedDefinition})
	mux := http.NewServeMux()
	NewHandler(svc).Register(mux)
	return mux, sidecar
}

// header is one request header, spelled as a pair so a call site reads as a list.
type header struct{ name, value string }

// call issues one request against the mux.
func call(mux *http.ServeMux, method, target, body string, headers ...header) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	for _, h := range headers {
		r.Header.Set(h.name, h.value)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

// decode unmarshals a response body, failing the test rather than the caller.
func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v\n%s", err, w.Body.String())
	}
	return out
}

// bearerFor is the dev-run token the last Apply minted — what that pod's sidecar would
// present.
func bearerFor(cluster *fakeCluster) header {
	return header{"Authorization", "Bearer " + cluster.lastSpec.DevRunToken}
}

// TestRunRouteCreatesThenAttaches: the status code carries the same distinction the body
// does, because "you joined a session" and "you started one" are different events.
func TestRunRouteCreatesThenAttaches(t *testing.T) {
	cluster := newFakeCluster()
	mux, _ := testHandler(t, cluster)

	w := call(mux, http.MethodPost, "/devruns", `{"userId":"u1","integrationId":"int-1"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("first Run: status = %d, want 201\n%s", w.Code, w.Body.String())
	}
	got := decode[map[string]any](t, w)
	if got["created"] != true {
		t.Errorf("created = %v, want true", got["created"])
	}
	// A run created a moment ago has no phase yet — the informer has not seen the
	// workload — and pending is what the editor should render for it.
	if got["status"] != kube.StatusPending {
		t.Errorf("status = %v, want %q for a run with no pod yet", got["status"], kube.StatusPending)
	}
	wantURL := "https://" + deriveLabel(secret, "u1", "int-1") + ".apps.example.com"
	if got["testUrl"] != wantURL {
		t.Errorf("testUrl = %v, want %q", got["testUrl"], wantURL)
	}

	w = call(mux, http.MethodPost, "/devruns", `{"userId":"u1","integrationId":"int-1"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("second Run: status = %d, want 200 (attached)\n%s", w.Code, w.Body.String())
	}
	if got := decode[map[string]any](t, w); got["created"] != false {
		t.Errorf("created = %v, want false on an attach", got["created"])
	}
}

func TestRunRouteRejectsBadInput(t *testing.T) {
	cluster := newFakeCluster()
	mux, _ := testHandler(t, cluster)

	for name, tc := range map[string]struct {
		body string
		want int
	}{
		// A misspelled field would otherwise start a run for the zero value of what the
		// caller meant to say.
		"unknown field":       {`{"userId":"u1","integrationId":"int-1","namespace":"ns"}`, http.StatusBadRequest},
		"not json":            {`nope`, http.StatusBadRequest},
		"no user":             {`{"integrationId":"int-1"}`, http.StatusBadRequest},
		"no integration":      {`{"userId":"u1"}`, http.StatusNotFound},
		"unknown integration": {`{"userId":"u1","integrationId":"nope"}`, http.StatusNotFound},
	} {
		if w := call(mux, http.MethodPost, "/devruns", tc.body); w.Code != tc.want {
			t.Errorf("%s: status = %d, want %d\n%s", name, w.Code, tc.want, w.Body.String())
		}
	}
}

// TestListRouteIsScopedAndAlwaysAList: another user sees nothing, and "nothing" is an
// empty array rather than null — a client that maps over the response should not have to
// defend against the difference.
func TestListRouteIsScopedAndAlwaysAList(t *testing.T) {
	cluster := newFakeCluster()
	mux, _ := testHandler(t, cluster)
	call(mux, http.MethodPost, "/devruns", `{"userId":"u1","integrationId":"int-1"}`)

	w := call(mux, http.MethodGet, "/devruns?userId=u1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\n%s", w.Code, w.Body.String())
	}
	if runs := decode[[]map[string]any](t, w); len(runs) != 1 {
		t.Fatalf("got %d runs, want 1: %v", len(runs), runs)
	}
	if w := call(mux, http.MethodGet, "/devruns?userId=u1&integrationId=other", ""); strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("body = %s, want []", w.Body.String())
	}
	if w := call(mux, http.MethodGet, "/devruns?userId=u2", ""); strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("another user's listing = %s, want []", w.Body.String())
	}
	if w := call(mux, http.MethodGet, "/devruns", ""); w.Code != http.StatusBadRequest {
		t.Errorf("unscoped listing: status = %d, want 400", w.Code)
	}
}

// TestStatusRoutePassesTheSidecarThrough: the sidecar's own document travels verbatim,
// because which generation is applied is the only way to tell "my save reached the pod"
// from "my save is still in Postgres".
func TestStatusRoutePassesTheSidecarThrough(t *testing.T) {
	cluster := newFakeCluster()
	mux, sidecar := testHandler(t, cluster)
	sidecar.status = json.RawMessage(`{"generation":"abc123","running":true}`)
	w := call(mux, http.MethodPost, "/devruns", `{"userId":"u1","integrationId":"int-1"}`)
	id := decode[map[string]any](t, w)["id"].(string)

	w = call(mux, http.MethodGet, "/devruns/"+id+"?userId=u1", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\n%s", w.Code, w.Body.String())
	}
	var got struct {
		Sidecar struct{ Generation string } `json:"sidecar"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Sidecar.Generation != "abc123" {
		t.Errorf("sidecar.generation = %q, want abc123", got.Sidecar.Generation)
	}

	if w := call(mux, http.MethodGet, "/devruns/"+id+"?userId=u2", ""); w.Code != http.StatusNotFound {
		t.Errorf("another user: status = %d, want 404", w.Code)
	}
}

func TestStopRoute(t *testing.T) {
	cluster := newFakeCluster()
	mux, _ := testHandler(t, cluster)
	id := decode[map[string]any](t, call(mux, http.MethodPost, "/devruns",
		`{"userId":"u1","integrationId":"int-1"}`))["id"].(string)

	if w := call(mux, http.MethodDelete, "/devruns/"+id+"?userId=u2", ""); w.Code != http.StatusNotFound {
		t.Fatalf("another user's stop: status = %d, want 404", w.Code)
	}
	if w := call(mux, http.MethodDelete, "/devruns/"+id+"?userId=u1", ""); w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204\n%s", w.Code, w.Body.String())
	}
	if _, still := cluster.runs[id]; still {
		t.Error("the workload is still there after a stop")
	}
	if w := call(mux, http.MethodGet, "/devruns/"+id+"?userId=u1", ""); w.Code != http.StatusNotFound {
		t.Errorf("status after stop = %d, want 404", w.Code)
	}
}

func TestReloadRouteForwardsToTheSidecar(t *testing.T) {
	cluster := newFakeCluster()
	mux, sidecar := testHandler(t, cluster)
	id := decode[map[string]any](t, call(mux, http.MethodPost, "/devruns",
		`{"userId":"u1","integrationId":"int-1"}`))["id"].(string)

	if w := call(mux, http.MethodPost, "/devruns/"+id+"/reload?userId=u1", ""); w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204\n%s", w.Code, w.Body.String())
	}
	if len(sidecar.reloads) != 1 {
		t.Fatalf("sidecar reloads = %d, want 1", len(sidecar.reloads))
	}
	if w := call(mux, http.MethodPost, "/devruns/"+id+"/reload?userId=u2", ""); w.Code != http.StatusNotFound {
		t.Errorf("another user's reload: status = %d, want 404", w.Code)
	}
}

// TestBundleRouteAuthorizesByToken covers the pod-facing half of the API, and the two
// statuses the sidecar branches on: 404 is terminal (it asks to be expired), 401 is not
// (it retries, loudly). Getting these the wrong way round is either a pod that deletes
// itself over a transient fault or one that serves a definition nobody can account for.
func TestBundleRouteAuthorizesByToken(t *testing.T) {
	cluster := newFakeCluster()
	mux, _ := testHandler(t, cluster)
	id := decode[map[string]any](t, call(mux, http.MethodPost, "/devruns",
		`{"userId":"u1","integrationId":"int-1"}`))["id"].(string)

	w := call(mux, http.MethodGet, "/devruns/"+id+"/bundle", "", bearerFor(cluster))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\n%s", w.Code, w.Body.String())
	}
	got := decode[Bundle](t, w)
	if !strings.Contains(got.Definition, devEnvResource) {
		t.Errorf("bundle definition does not declare %q:\n%s", devEnvResource, got.Definition)
	}
	if got.Generation == "" {
		t.Error("bundle carries no generation")
	}

	for name, tc := range map[string]struct {
		id      string
		headers []header
		want    int
	}{
		"no token":     {id, nil, http.StatusUnauthorized},
		"wrong token":  {id, []header{{"Authorization", "Bearer nope"}}, http.StatusUnauthorized},
		"wrong scheme": {id, []header{{"Authorization", cluster.lastSpec.DevRunToken}}, http.StatusUnauthorized},
		"unknown run":  {"00000000-0000-0000-0000-000000000000", []header{bearerFor(cluster)}, http.StatusNotFound},
	} {
		w := call(mux, http.MethodGet, "/devruns/"+tc.id+"/bundle", "", tc.headers...)
		if w.Code != tc.want {
			t.Errorf("%s: status = %d, want %d", name, w.Code, tc.want)
		}
	}
}

// TestBundleRouteAcceptsALowercaseScheme: RFC 7235 says the auth scheme is
// case-insensitive, and a sidecar rejected over its capitalisation would be a very hard
// afternoon.
func TestBundleRouteAcceptsALowercaseScheme(t *testing.T) {
	cluster := newFakeCluster()
	mux, _ := testHandler(t, cluster)
	id := decode[map[string]any](t, call(mux, http.MethodPost, "/devruns",
		`{"userId":"u1","integrationId":"int-1"}`))["id"].(string)

	w := call(mux, http.MethodGet, "/devruns/"+id+"/bundle", "",
		header{"Authorization", "bearer " + cluster.lastSpec.DevRunToken})
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestExpireRouteTearsTheRunDown(t *testing.T) {
	cluster := newFakeCluster()
	mux, _ := testHandler(t, cluster)
	id := decode[map[string]any](t, call(mux, http.MethodPost, "/devruns",
		`{"userId":"u1","integrationId":"int-1"}`))["id"].(string)

	if w := call(mux, http.MethodPost, "/devruns/"+id+"/expire", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated expire: status = %d, want 401", w.Code)
	}
	if _, still := cluster.runs[id]; !still {
		t.Fatal("an unauthenticated expire tore the run down")
	}
	if w := call(mux, http.MethodPost, "/devruns/"+id+"/expire", "", bearerFor(cluster)); w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204\n%s", w.Code, w.Body.String())
	}
	if _, still := cluster.runs[id]; still {
		t.Error("the workload is still there after an expire")
	}
}

func TestLogsRouteStreamsPlainText(t *testing.T) {
	cluster := newFakeCluster()
	cluster.logs = "config changed, reloading\n"
	mux, _ := testHandler(t, cluster)
	id := decode[map[string]any](t, call(mux, http.MethodPost, "/devruns",
		`{"userId":"u1","integrationId":"int-1"}`))["id"].(string)

	// Before a pod exists the run is a wait, not a 404: it exists, so the caller should
	// retry rather than start it again.
	if w := call(mux, http.MethodGet, "/devruns/"+id+"/logs?userId=u1", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("logs before a pod: status = %d, want 503", w.Code)
	}

	cluster.withPods(id, kube.PodStatus{Name: "octo-dev-pod", Phase: "Running", Ready: true})
	w := call(mux, http.MethodGet, "/devruns/"+id+"/logs?userId=u1&follow=1&tail=10", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\n%s", w.Code, w.Body.String())
	}
	if w.Body.String() != cluster.logs {
		t.Errorf("body = %q, want %q", w.Body.String(), cluster.logs)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}
	// Without this a proxy buffers the tail, which reads to a user as "the logs stopped".
	if w.Header().Get("X-Accel-Buffering") != "no" {
		t.Error("X-Accel-Buffering is not disabled")
	}
	want := "pod-logs octo-dev-pod/" + kube.RuntimeContainer + " follow=true tail=10"
	if !containsCall(cluster.calls, want) {
		t.Errorf("calls = %v, want one matching %q", cluster.calls, want)
	}
}

// TestRoutesAreDisabledWithoutASecret: an orchestrator that cannot derive identities has
// no dev-run feature rather than a degraded one, and every route says so the same way.
func TestRoutesAreDisabledWithoutASecret(t *testing.T) {
	svc := NewService(newFakeCluster(), fakeIntegrations{}, fakeResources{}, nil)
	mux := http.NewServeMux()
	NewHandler(svc).Register(mux)

	for _, target := range []struct {
		method, path string
		body         string
	}{
		{http.MethodPost, "/devruns", `{"userId":"u1","integrationId":"int-1"}`},
		{http.MethodGet, "/devruns?userId=u1", ""},
		{http.MethodGet, "/devruns/x?userId=u1", ""},
		{http.MethodDelete, "/devruns/x?userId=u1", ""},
		{http.MethodPost, "/devruns/x/reload?userId=u1", ""},
		{http.MethodGet, "/devruns/x/logs?userId=u1", ""},
		{http.MethodGet, "/devruns/x/bundle", ""},
		{http.MethodPost, "/devruns/x/expire", ""},
	} {
		w := call(mux, target.method, target.path, target.body)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", target.method, target.path, w.Code)
		}
	}
}
