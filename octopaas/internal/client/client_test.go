package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeBearer hands out "stale" until it is asked to refresh, then "fresh".
type fakeBearer struct{ refreshed int }

func (f *fakeBearer) Bearer(_ context.Context, refresh bool) (string, error) {

	if refresh {
		f.refreshed++
	}
	if f.refreshed > 0 {
		return "fresh", nil
	}
	return "stale", nil
}

// An expired OAuth token is refreshed and the request sent again, once: the
// person running the command never sees the 401.
func TestA401IsRetriedOnceWithAFreshCredential(t *testing.T) {

	var seen []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"a valid bearer token is required"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"u1","email":"ada@example.com","roles":["platform:admin"]}`))
	}))
	defer api.Close()

	bearer := &fakeBearer{}
	me, err := New(api.URL, bearer, "octopaas/test").Me(context.Background())
	if err != nil {
		t.Fatalf("the retry should have succeeded: %v", err)
	}
	if me.Email != "ada@example.com" {
		t.Errorf("me = %+v", me)
	}
	if len(seen) != 2 || bearer.refreshed != 1 {
		t.Errorf("expected exactly one retry with one refresh, saw %v and %d refreshes", seen, bearer.refreshed)
	}
}

// The API's {"error": "..."} becomes the message, and the status survives so a
// command can say something useful about a 403.
func TestRefusalsCarryTheAPIsMessage(t *testing.T) {

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"this account has not been provisioned on this platform"}`))
	}))
	defer api.Close()

	_, err := New(api.URL, &fakeBearer{}, "octopaas/test").ListIntegrations(context.Background())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("want a 403 *Error, got %v", err)
	}
	if !strings.Contains(err.Error(), "not been provisioned") {
		t.Errorf("the message should be the API's, got %q", err)
	}
}

// Path values are escaped as segments, and a file's path travels as a query
// parameter, slashes and all.
func TestPathsAndQueriesAreEscaped(t *testing.T) {

	var got string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.EscapedPath() + "?" + r.URL.RawQuery
		_, _ = w.Write([]byte(`{"path":"flows/a b.yaml","content":"x"}`))
	}))
	defer api.Close()

	_, err := New(api.URL, &fakeBearer{}, "octopaas/test").GetFile(context.Background(), "id/1", "flows/a b.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/v1/integrations/id%2F1/files/content?path=flows%2Fa+b.yaml" {
		t.Errorf("request was %s", got)
	}
}

// A deployment is open-ended in the spec, so whatever the orchestrator sent comes
// back out on -o json, fields the client never heard of included.
func TestDeploymentsKeepFieldsTheClientDoesNotKnow(t *testing.T) {

	in := `{"id":"d1","status":"running","replicas":2,"futureField":{"a":1}}`
	var d Deployment
	if err := json.Unmarshal([]byte(in), &d); err != nil {
		t.Fatal(err)
	}
	if d.Status != "running" || d.Replicas != 2 {
		t.Errorf("known fields should decode, got %+v", d)
	}
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Errorf("marshalled %s, want %s", out, in)
	}
}

func TestReadEvents(t *testing.T) {

	stream := ": a comment\n" +
		"event: open\ndata: {}\n\n" +
		"event: log\ndata: first line\n\n" +
		"event: log\ndata: two\ndata: parts\n\n" +
		"data: unnamed\n\n" +
		"event: log\ndata: cut off without a blank line"

	var got []Event
	if err := readEvents(strings.NewReader(stream), func(e Event) error {
		got = append(got, e)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	want := []Event{
		{"open", "{}"},
		{"log", "first line"},
		{"log", "two\nparts"},
		{"message", "unnamed"},
		{"log", "cut off without a blank line"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
