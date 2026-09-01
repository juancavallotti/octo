package tavily

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// jsonStub answers every request with body and records the request it was given.
func jsonStub(t *testing.T, got *map[string]any, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const extractResponse = `{"results":[{"url":"https://example.com","raw_content":"hi"}],"failed_results":[]}`

func TestExtractAcceptsSingleURLAndList(t *testing.T) {
	cases := []struct {
		name string
		urls string
		body any
		want []any
	}{
		{"literal string", `"https://example.com"`, nil, []any{"https://example.com"}},
		{"literal list", `["https://a.test","https://b.test"]`, nil, []any{"https://a.test", "https://b.test"}},
		{"from the message", "body.links", map[string]any{"links": []any{"https://c.test"}}, []any{"https://c.test"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			srv := jsonStub(t, &got, extractResponse)

			proc, err := newExtract(types.Settings{
				"connector": "tavily",
				"urls":      tc.urls,
			}, blockDeps(t, srv.URL))
			if err != nil {
				t.Fatalf("newExtract: %v", err)
			}
			if _, err := proc.Process(context.Background(), blockMessage(t, tc.body)); err != nil {
				t.Fatalf("Process: %v", err)
			}
			urls, _ := got["urls"].([]any)
			if len(urls) != len(tc.want) {
				t.Fatalf("urls = %v, want %v", got["urls"], tc.want)
			}
			for i, want := range tc.want {
				if urls[i] != want {
					t.Errorf("urls[%d] = %v, want %v", i, urls[i], want)
				}
			}
		})
	}
}

func TestExtractSendsConfiguredOptions(t *testing.T) {
	var got map[string]any
	srv := jsonStub(t, &got, extractResponse)

	proc, err := newExtract(types.Settings{
		"connector":       "tavily",
		"urls":            `"https://example.com"`,
		"query":           "body.intent",
		"extractDepth":    "advanced",
		"format":          "text",
		"chunksPerSource": 5,
		"includeImages":   true,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newExtract: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, map[string]any{
		"intent": "pricing",
	})); err != nil {
		t.Fatalf("Process: %v", err)
	}

	want := map[string]any{
		"query":             "pricing",
		"extract_depth":     "advanced",
		"format":            "text",
		"chunks_per_source": float64(5),
		"include_images":    true,
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("request %s = %v, want %v", key, got[key], value)
		}
	}
}

func TestExtractOmitsUnsetOptions(t *testing.T) {
	var got map[string]any
	srv := jsonStub(t, &got, extractResponse)

	proc, err := newExtract(types.Settings{
		"connector": "tavily",
		"urls":      `"https://example.com"`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newExtract: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err != nil {
		t.Fatalf("Process: %v", err)
	}
	for _, key := range []string{"query", "extract_depth", "format", "chunks_per_source", "include_images"} {
		if _, ok := got[key]; ok {
			t.Errorf("%s should be omitted when unset, got %v", key, got[key])
		}
	}
}

func TestExtractRequiresURLs(t *testing.T) {
	if _, err := newExtract(types.Settings{"connector": "tavily"}, blockDeps(t, "")); err == nil {
		t.Error("expected an error when urls is not set")
	}
}

func TestExtractRejectsEmptyURLList(t *testing.T) {
	var got map[string]any
	srv := jsonStub(t, &got, extractResponse)

	proc, err := newExtract(types.Settings{
		"connector": "tavily",
		"urls":      "body.links",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newExtract: %v", err)
	}
	_, err = proc.Process(context.Background(), blockMessage(t, map[string]any{"links": []any{}}))
	if err == nil {
		t.Error("expected an error when urls evaluates to an empty list")
	}
}

func TestExtractPartialFailure(t *testing.T) {
	const partial = `{"results":[{"url":"https://a.test"}],` +
		`"failed_results":[{"url":"https://b.test","error":"timeout"}]}`

	t.Run("tolerated by default", func(t *testing.T) {
		var got map[string]any
		srv := jsonStub(t, &got, partial)

		proc, err := newExtract(types.Settings{
			"connector": "tavily",
			"urls":      `["https://a.test","https://b.test"]`,
		}, blockDeps(t, srv.URL))
		if err != nil {
			t.Fatalf("newExtract: %v", err)
		}
		out, err := proc.Process(context.Background(), blockMessage(t, nil))
		if err != nil {
			t.Fatalf("Process: %v", err)
		}
		body, _ := out.Body.(map[string]any)
		if failed, _ := body["failed_results"].([]any); len(failed) != 1 {
			t.Error("the response should still carry failed_results for the flow to inspect")
		}
	})

	t.Run("fails when failOnPartial is set", func(t *testing.T) {
		var got map[string]any
		srv := jsonStub(t, &got, partial)

		proc, err := newExtract(types.Settings{
			"connector":     "tavily",
			"urls":          `["https://a.test","https://b.test"]`,
			"failOnPartial": true,
		}, blockDeps(t, srv.URL))
		if err != nil {
			t.Fatalf("newExtract: %v", err)
		}
		_, err = proc.Process(context.Background(), blockMessage(t, nil))
		if err == nil {
			t.Fatal("expected a partial extraction to fail the flow")
		}
		if !strings.Contains(err.Error(), "1 of 2") {
			t.Errorf("error = %q, want it to count the failures", err)
		}
	})

	t.Run("failOnPartial yields to failOnError", func(t *testing.T) {
		var got map[string]any
		srv := jsonStub(t, &got, partial)

		failOnError := false
		proc, err := newExtract(types.Settings{
			"connector":     "tavily",
			"urls":          `["https://a.test","https://b.test"]`,
			"failOnPartial": true,
			"failOnError":   &failOnError,
		}, blockDeps(t, srv.URL))
		if err != nil {
			t.Fatalf("newExtract: %v", err)
		}
		msg := blockMessage(t, map[string]any{"in": true})
		out, err := proc.Process(context.Background(), msg)
		if err != nil {
			t.Fatalf("Process should tolerate the partial failure: %v", err)
		}
		if body, _ := out.Body.(map[string]any); body["in"] != true {
			t.Error("a tolerated failure should leave the message unchanged")
		}
	})
}
