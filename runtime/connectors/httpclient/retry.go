package httpclient

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// resolveRetry applies defaults to the configured retry policy: at least one
// attempt and a positive backoff cap.
func resolveRetry(set retrySettings) (attempts int, maxBackoff time.Duration) {
	attempts = set.MaxAttempts
	if attempts <= 0 {
		attempts = defaultMaxAttempts
	}
	maxBackoff = time.Duration(set.MaxBackoff)
	if maxBackoff <= 0 {
		maxBackoff = defaultMaxBackoff
	}
	return attempts, maxBackoff
}

// maxDrainBytes bounds how much of a to-be-retried response body we read before
// closing it. A 429 body is small; draining it lets the underlying connection be
// reused rather than discarded.
const maxDrainBytes = 4 << 10 // 4 KiB

// doWithRetry executes req, re-attempting on 429 (Too Many Requests) up to
// c.maxAttempts times. Between attempts it waits for the duration advertised by
// the Retry-After header, falling back to exponential backoff, capped at
// c.maxBackoff. Transport errors are returned immediately (not retried). The
// request body is replayed via req.GetBody, which net/http populates for the
// buffered readers the rest block uses; if a body cannot be replayed, retrying
// stops and the 429 response is returned as-is.
func (c *Connector) doWithRetry(req *http.Request) (*http.Response, error) {
	attempts := c.maxAttempts
	if attempts < 1 {
		attempts = 1
	}

	for attempt := 1; ; attempt++ {
		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("http-client do %s %s: %w", req.Method, req.URL.Redacted(), err)
		}

		if resp.StatusCode != http.StatusTooManyRequests || attempt >= attempts {
			return resp, nil
		}

		// A body we cannot rewind can't be replayed safely; stop retrying and let
		// the caller see the 429.
		if req.Body != nil && req.GetBody == nil {
			return resp, nil
		}

		wait := c.retryWait(resp.Header.Get("Retry-After"), attempt)
		drainAndClose(resp.Body)

		if req.GetBody != nil {
			body, gErr := req.GetBody()
			if gErr != nil {
				return nil, fmt.Errorf("http-client retry %s %s: rewind body: %w",
					req.Method, req.URL.Redacted(), gErr)
			}
			req.Body = body
		}

		slog.Debug("http client retrying after 429",
			"method", req.Method, "url", req.URL.Redacted(),
			"attempt", attempt, "wait", wait)

		select {
		case <-time.After(wait):
		case <-req.Context().Done():
			return nil, fmt.Errorf("http-client retry %s %s: %w",
				req.Method, req.URL.Redacted(), req.Context().Err())
		}
	}
}

// retryWait picks the wait before the next attempt: the Retry-After value when it
// parses, otherwise exponential backoff (base * 2^(attempt-1)). The result is
// capped at c.maxBackoff.
func (c *Connector) retryWait(retryAfter string, attempt int) time.Duration {
	wait, ok := parseRetryAfter(retryAfter)
	if !ok {
		wait = defaultBaseBackoff << (attempt - 1)
	}
	if wait > c.maxBackoff {
		wait = c.maxBackoff
	}
	if wait < 0 {
		wait = 0
	}
	return wait
}

// parseRetryAfter parses a Retry-After header value, which is either a number of
// delay-seconds or an HTTP-date. It returns the delay and whether parsing
// succeeded; a past or malformed date yields (0,false) so the caller falls back
// to backoff.
func parseRetryAfter(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d, true
		}
		return 0, true // a date at/in the past means "retry now"
	}
	return 0, false
}

// drainAndClose reads (up to a bound) and closes a response body so the
// connection can be reused before the next attempt.
func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxDrainBytes))
	_ = body.Close()
}
