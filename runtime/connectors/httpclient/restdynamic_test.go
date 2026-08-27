package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// newDynamic builds a rest-dynamic processor against baseURL, failing the test if
// it does not build — construction errors have their own tests.
func newDynamic(t *testing.T, baseURL string, settings types.Settings) *dynamicProcessor {
	t.Helper()
	if _, ok := settings["connector"]; !ok {
		settings["connector"] = "api"
	}
	proc, err := newRESTDynamic(settings, restDeps(t, baseURL))
	if err != nil {
		t.Fatalf("newRESTDynamic: %v", err)
	}
	return proc.(*dynamicProcessor)
}

// echo records what the server actually received, which is the thing every test
// here is really asserting on.
type echo struct {
	method string
	path   string
	query  string
	header http.Header
	body   string
}

func echoServer(t *testing.T, got *echo, status int, respond string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got.method, got.path, got.query = r.Method, r.URL.Path, r.URL.RawQuery
		got.header, got.body = r.Header.Clone(), string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respond)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The point of the block: neither the method nor the path is known until the
// message arrives.
func TestDynamicBuildsMethodAndPathFromTheMessage(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{"ok": true}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method": "body.method",
		"path":   `"/integrations/" + body.id`,
	})

	out, err := proc.Process(context.Background(), restMessage(t, map[string]any{
		"method": "delete", "id": "abc-123",
	}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	if got.method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE (rendered and upper-cased)", got.method)
	}
	if got.path != "/integrations/abc-123" {
		t.Errorf("path = %q, want /integrations/abc-123", got.path)
	}
	if status, _ := out.Variables.Int("statusCode"); status != 200 {
		t.Errorf("statusCode = %v, want 200", status)
	}
}

// The other half of the shape: one expression for the whole map, because a caller
// that does not know the path rarely knows the parameter names.
func TestDynamicQueryAndHeadersComeFromWholeMaps(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method":  `"GET"`,
		"path":    `"/things"`,
		"query":   "body.query",
		"headers": `{"X-Trace": body.trace}`,
	})

	_, err := proc.Process(context.Background(), restMessage(t, map[string]any{
		"query": map[string]any{"limit": "10", "q": "octo"},
		"trace": "t-1",
	}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	if !strings.Contains(got.query, "limit=10") || !strings.Contains(got.query, "q=octo") {
		t.Errorf("query = %q, want both parameters", got.query)
	}
	if h := got.header.Get("X-Trace"); h != "t-1" {
		t.Errorf("X-Trace = %q, want t-1", h)
	}
}

// A map value that is not a string still has to reach the wire, rendered the way
// EvalString renders one: compact JSON.
func TestDynamicRendersNonStringMapValues(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method": `"GET"`,
		"path":   `"/things"`,
		"query":  `{"limit": 10, "deep": {"a": 1}}`,
	})

	if _, err := proc.Process(context.Background(), restMessage(t, map[string]any{})); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if !strings.Contains(got.query, "limit=10") {
		t.Errorf("query = %q, want a number rendered as its digits", got.query)
	}
	if !strings.Contains(got.query, "deep=%7B%22a%22%3A1%7D") {
		t.Errorf("query = %q, want a nested value rendered as compact JSON", got.query)
	}
}

func TestDynamicSendsABody(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusCreated, `{"id": "new"}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method": `"POST"`,
		"path":   `"/integrations"`,
		"body":   "body.payload",
	})

	if _, err := proc.Process(context.Background(), restMessage(t, map[string]any{
		"payload": map[string]any{"name": "demo"},
	})); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if !strings.Contains(got.body, `"name":"demo"`) {
		t.Errorf("body = %q, want the payload JSON-encoded", got.body)
	}
	if ct := got.header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json by default", ct)
	}
}

// A rendered full URL is silently rewritten to the base host by the connector.
// Failing loudly is the difference between "that was not honoured" and "a
// different request was made on your behalf".
func TestDynamicRejectsAFullURL(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method": `"GET"`,
		"path":   "body.path",
	})

	_, err := proc.Process(context.Background(), restMessage(t, map[string]any{
		"path": "http://evil.example.com/steal",
	}))
	if err == nil {
		t.Fatal("want an error for a path that is a full URL")
	}
	if !strings.Contains(err.Error(), "full URL") {
		t.Errorf("error = %v, want it to name the problem", err)
	}
	if got.method != "" {
		t.Error("want no request made at all")
	}
}

// joinPath concatenates without cleaning, so ".." escapes a base path prefix.
func TestDynamicRejectsDotDotSegments(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method": `"GET"`,
		"path":   "body.path",
	})

	_, err := proc.Process(context.Background(), restMessage(t, map[string]any{
		"path": "/v1/../../admin",
	}))
	if err == nil {
		t.Fatal("want an error for a path containing a .. segment")
	}
	if got.method != "" {
		t.Error("want no request made at all")
	}
}

func TestDynamicRejectsANonMethod(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method": "body.method",
		"path":   `"/things"`,
	})

	_, err := proc.Process(context.Background(), restMessage(t, map[string]any{
		"method": "TRACE ME",
	}))
	if err == nil {
		t.Fatal("want an error for a method outside the standard set")
	}
	if got.method != "" {
		t.Error("want no request made at all")
	}
}

// The lever an operator reaches for: the block is configured with what it may do,
// and the expression cannot talk its way past it.
func TestDynamicAllowMethodsRestrictsWhatMayBeIssued(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method":       "body.method",
		"path":         `"/things"`,
		"allowMethods": []any{"GET"},
	})

	if _, err := proc.Process(context.Background(), restMessage(t, map[string]any{"method": "GET"})); err != nil {
		t.Fatalf("an allowed method should pass: %v", err)
	}
	if got.method != http.MethodGet {
		t.Errorf("method = %q, want the allowed GET to reach the server", got.method)
	}

	got = echo{}
	_, err := proc.Process(context.Background(), restMessage(t, map[string]any{"method": "DELETE"}))
	if err == nil {
		t.Fatal("want a disallowed method refused")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("error = %v, want it to say the block does not allow it", err)
	}
	if got.method != "" {
		t.Error("want no request made at all")
	}
}

func TestDynamicPathPrefixRestrictsWhereItMayCall(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method":     `"GET"`,
		"path":       "body.path",
		"pathPrefix": "/integrations",
	})

	if _, err := proc.Process(context.Background(), restMessage(t, map[string]any{
		"path": "/integrations/abc",
	})); err != nil {
		t.Fatalf("a path under the prefix should pass: %v", err)
	}

	got = echo{}
	if _, err := proc.Process(context.Background(), restMessage(t, map[string]any{
		"path": "/secrets",
	})); err == nil {
		t.Fatal("want a path outside the prefix refused")
	}
	if got.method != "" {
		t.Error("want no request made at all")
	}
}

// An allow-list that silently accepted a typo would be worse than none: it would
// read as a restriction while restricting nothing.
func TestDynamicRejectsANonsenseAllowMethodsAtBuildTime(t *testing.T) {
	_, err := newRESTDynamic(types.Settings{
		"connector":    "api",
		"method":       `"GET"`,
		"path":         `"/things"`,
		"allowMethods": []any{"GET", "FETCH"},
	}, restDeps(t, "http://example.invalid"))
	if err == nil {
		t.Fatal("want an error for an allowMethods entry that is not an HTTP method")
	}
	if !strings.Contains(err.Error(), "FETCH") {
		t.Errorf("error = %v, want it to name the offending entry", err)
	}
}

func TestDynamicRequiresMethodAndPath(t *testing.T) {
	for _, missing := range []string{"method", "path"} {
		settings := types.Settings{"connector": "api", "method": `"GET"`, "path": `"/things"`}
		delete(settings, missing)

		if _, err := newRESTDynamic(settings, restDeps(t, "http://example.invalid")); err == nil {
			t.Errorf("want an error when %s is absent", missing)
		}
	}
}

func TestDynamicHonoursFailOnError(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusNotFound, `{"error": "nope"}`)

	settings := func(fail bool) types.Settings {
		return types.Settings{
			"method": `"GET"`, "path": `"/things"`, "failOnError": fail,
		}
	}

	if _, err := newDynamic(t, srv.URL, settings(true)).
		Process(context.Background(), restMessage(t, map[string]any{})); err == nil {
		t.Error("want a 404 to fail the message by default")
	}

	out, err := newDynamic(t, srv.URL, settings(false)).
		Process(context.Background(), restMessage(t, map[string]any{}))
	if err != nil {
		t.Fatalf("want a 404 folded in as data when failOnError is false: %v", err)
	}
	if status, _ := out.Variables.Int("statusCode"); status != 404 {
		t.Errorf("statusCode = %v, want 404 recorded", status)
	}
	body, ok := out.Body.(map[string]any)
	if !ok || body["error"] != "nope" {
		t.Errorf("body = %#v, want the error response folded in", out.Body)
	}
}

// The connector's base path has to survive, or a connector configured to sit under
// a prefix would be bypassed by every call this block makes.
func TestDynamicKeepsTheConnectorBasePath(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL+"/v1", types.Settings{
		"method": `"GET"`,
		"path":   `"/things"`,
	})

	if _, err := proc.Process(context.Background(), restMessage(t, map[string]any{})); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got.path != "/v1/things" {
		t.Errorf("path = %q, want the connector's /v1 prefix kept", got.path)
	}
}

func TestDynamicRejectsAQueryThatIsNotAMap(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method": `"GET"`,
		"path":   `"/things"`,
		"query":  `"limit=10"`,
	})

	_, err := proc.Process(context.Background(), restMessage(t, map[string]any{}))
	if err == nil {
		t.Fatal("want an error when the query expression is not a map")
	}
	if !strings.Contains(err.Error(), "want a map") {
		t.Errorf("error = %v, want it to say what shape was expected", err)
	}
}

// A prefix that reads as a path boundary has to be one. "/integrations" admitting
// "/integrations-internal" would restrict less than the operator who wrote it
// believes, which is worse than not restricting at all.
func TestDynamicPathPrefixMatchesOnSegmentBoundaries(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method":     `"GET"`,
		"path":       "body.path",
		"pathPrefix": "/integrations",
	})

	for _, path := range []string{"/integrations-internal/secrets", "/integrationsfoo"} {
		got = echo{}
		if _, err := proc.Process(context.Background(), restMessage(t, map[string]any{
			"path": path,
		})); err == nil {
			t.Errorf("want %q refused: it shares a string prefix, not a path one", path)
		}
		if got.method != "" {
			t.Errorf("want no request made for %q", path)
		}
	}

	// The prefix itself, and anything beneath it, still pass.
	for _, path := range []string{"/integrations", "/integrations/abc"} {
		if _, err := proc.Process(context.Background(), restMessage(t, map[string]any{
			"path": path,
		})); err != nil {
			t.Errorf("want %q allowed: %v", path, err)
		}
	}
}

// url.Parse percent-decodes, so the encoded form is caught by the same scan. An
// upstream that decodes before routing would otherwise be reached by it.
func TestDynamicRejectsPercentEncodedTraversal(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method": `"GET"`,
		"path":   "body.path",
	})

	_, err := proc.Process(context.Background(), restMessage(t, map[string]any{
		"path": "/v1/%2e%2e/%2e%2e/admin",
	}))
	if err == nil {
		t.Fatal("want an encoded .. segment refused")
	}
	if got.method != "" {
		t.Error("want no request made at all")
	}
}

// The traversal scan looks at the path, not the whole rendered string, so a query
// value that happens to contain ".." is not mistaken for traversal it cannot do.
func TestDynamicAllowsDotDotInsideAQueryValue(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method": `"GET"`,
		"path":   `"/search"`,
		"query":  `{"q": "../etc/passwd"}`,
	})

	if _, err := proc.Process(context.Background(), restMessage(t, map[string]any{})); err != nil {
		t.Fatalf("a query value is not a path: %v", err)
	}
	if got.path != "/search" {
		t.Errorf("path = %q, want /search", got.path)
	}
	if !strings.Contains(got.query, "%2Fetc%2Fpasswd") {
		t.Errorf("query = %q, want the value passed through encoded", got.query)
	}
}

// A rendered Authorization header is sent, under either spelling. The connector
// applies its own credential only when the request carries none, so this replaces
// it rather than sitting alongside it -- which is the point, and the only way a
// flow can call an API as the caller who asked it to.
func TestDynamicSendsARenderedAuthorizationHeader(t *testing.T) {
	var got echo
	srv := echoServer(t, &got, http.StatusOK, `{}`)

	proc := newDynamic(t, srv.URL, types.Settings{
		"method":  `"GET"`,
		"path":    `"/things"`,
		"headers": "body.headers",
	})

	for _, name := range []string{"Authorization", "authorization"} {
		got = echo{}
		_, err := proc.Process(context.Background(), restMessage(t, map[string]any{
			"headers": map[string]any{name: "Bearer caller"},
		}))
		if err != nil {
			t.Errorf("Process for %q: %v", name, err)
			continue
		}
		if got.header.Get("Authorization") != "Bearer caller" {
			t.Errorf("Authorization for %q = %q, want the rendered credential",
				name, got.header.Get("Authorization"))
		}
	}
}
