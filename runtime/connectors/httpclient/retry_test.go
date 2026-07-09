package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juancavallotti/octo/types"
)

// retrySettings with tiny backoff so tests never actually sleep long.
func fastRetry(maxAttempts int) map[string]any {
	return map[string]any{"maxAttempts": maxAttempts, "maxBackoff": "5ms"}
}

func TestRetryOn429ThenSuccess(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":"slow down"}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := startConnector(t, types.Settings{"baseURL": srv.URL, "retry": fastRetry(3)})

	status, body := get(t, c, "/things")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 after retries", status)
	}
	if body != `{"ok":true}` {
		t.Errorf("body = %q, want success body", body)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (two 429s then success)", got)
	}
}

func TestRetryExhaustedReturns429(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := startConnector(t, types.Settings{"baseURL": srv.URL, "retry": fastRetry(3)})

	status, _ := get(t, c, "/things")
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 after exhausting attempts", status)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (maxAttempts)", got)
	}
}

func TestRetryReplaysBody(t *testing.T) {
	var calls atomic.Int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if calls.Add(1) < 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := startConnector(t, types.Settings{"baseURL": srv.URL, "retry": fastRetry(3)})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"/submit", strings.NewReader(`{"item":"widget"}`))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(bodies) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(bodies))
	}
	for i, b := range bodies {
		if b != `{"item":"widget"}` {
			t.Errorf("request %d body = %q, want the payload re-sent verbatim", i, b)
		}
	}
}

func TestNoRetryWhenMaxAttemptsOne(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := startConnector(t, types.Settings{"baseURL": srv.URL, "retry": fastRetry(1)})

	status, _ := get(t, c, "/things")
	if status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", status)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want exactly 1 (retry disabled)", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Duration
		ok    bool
	}{
		{name: "seconds", value: "5", want: 5 * time.Second, ok: true},
		{name: "zero", value: "0", want: 0, ok: true},
		{name: "negative", value: "-3", want: 0, ok: false},
		{name: "empty", value: "", want: 0, ok: false},
		{name: "garbage", value: "soon", want: 0, ok: false},
		{name: "past date", value: "Mon, 02 Jan 2006 15:04:05 GMT", want: 0, ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tt.value)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != tt.want {
				t.Errorf("duration = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRetryAfterFutureDate verifies an HTTP-date in the future yields a positive
// wait (capped later by maxBackoff at the call site).
func TestRetryAfterFutureDate(t *testing.T) {
	future := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	got, ok := parseRetryAfter(future)
	if !ok {
		t.Fatalf("ok = false, want true for a future date")
	}
	if got <= 0 || got > 3*time.Second {
		t.Errorf("duration = %v, want ~2s", got)
	}
}
