// Package client calls the Octo public API (api.{domain}).
//
// It is written by hand against api/static/openapi.yaml rather than generated:
// the API is small, and a hand-written client can name things the way a CLI
// wants to show them. routes.go is the table of everything it can call, and
// specdrift_test.go is what notices when the spec and the table disagree.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Bearer hands out the credential a request carries. refresh asks for a new one
// because the last was refused; a source that cannot refresh (an API key)
// returns the same one and the request fails as it would have.
type Bearer interface {
	Bearer(ctx context.Context, refresh bool) (string, error)
}

// Progress is told how many bytes of a body have moved, and of how many. total
// is -1 when the other side did not say.
type Progress func(done, total int64)

// Error is the API refusing a request, with the status and the message it gave.
type Error struct {
	Status  int
	Message string
	// Challenge is the WWW-Authenticate the API answered a 401 with.
	Challenge string
}

func (e *Error) Error() string {

	if e.Message == "" {
		return fmt.Sprintf("the API answered %d %s", e.Status, http.StatusText(e.Status))
	}
	return fmt.Sprintf("%s (%d)", e.Message, e.Status)
}

// StatusOf is the HTTP status behind err, or 0 when err is not the API's answer.
func StatusOf(err error) int {

	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

// Client talks to one installation's public API.
type Client struct {
	base      string
	http      *http.Client
	bearer    Bearer
	userAgent string
}

// New is a client for the API at base (https://api.octopaas.dev). bearer may be
// nil for the routes that need no credential.
func New(base string, bearer Bearer, userAgent string) *Client {

	//no overall timeout: a bundle can take a while and a log stream runs for up
	//to half an hour. The context bounds a call, and the transport bounds how
	//long we wait for the API to start answering.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 90 * time.Second

	return &Client{
		base:      strings.TrimRight(base, "/"),
		http:      &http.Client{Transport: transport},
		bearer:    bearer,
		userAgent: userAgent,
	}
}

// Base is the API's root URL.
func (c *Client) Base() string {
	return c.base
}

// request is one call: the route, its path values in order, and what to send.
type request struct {
	route       route
	path        []string
	query       url.Values
	body        []byte
	contentType string
	accept      string
	upload      Progress
	anonymous   bool
}

// do sends r and returns the response for the caller to read, or the API's
// refusal as an *Error. A 401 on a credential that can be refreshed is retried
// once with a fresh one, which is why bodies are bytes: they can be sent twice.
func (c *Client) do(ctx context.Context, r request) (*http.Response, error) {

	resp, err := c.send(ctx, r, false)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized && c.bearer != nil && !r.anonymous {
		_ = resp.Body.Close()
		if resp, err = c.send(ctx, r, true); err != nil {
			return nil, err
		}
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		return nil, refusal(resp)
	}
	return resp, nil
}

func (c *Client) send(ctx context.Context, r request, refresh bool) (*http.Response, error) {

	target := c.base + r.route.expand(r.path...)
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}

	var body io.Reader
	if r.body != nil {
		body = bytes.NewReader(r.body)
		if r.upload != nil {
			body = &counting{r: body, total: int64(len(r.body)), progress: r.upload}
		}
	}
	req, err := http.NewRequestWithContext(ctx, r.route.method, target, body)
	if err != nil {
		return nil, fmt.Errorf("build %s %s: %w", r.route.method, target, err)
	}
	if r.body != nil {
		req.ContentLength = int64(len(r.body))
		req.Header.Set("Content-Type", r.contentType)
	}
	if r.accept != "" {
		req.Header.Set("Accept", r.accept)
	}
	//some edges refuse a library's default user agent outright, and it is nice
	//for the platform's logs to know who is calling anyway.
	req.Header.Set("User-Agent", c.userAgent)

	if c.bearer != nil && !r.anonymous {
		token, err := c.bearer.Bearer(ctx, refresh)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach %s: %w", c.base, err)
	}
	return resp, nil
}

// refusal reads the API's {"error": "..."} into an *Error, falling back to the
// first line of whatever else it sent.
func refusal(resp *http.Response) error {

	ret := &Error{Status: resp.StatusCode, Challenge: resp.Header.Get("WWW-Authenticate")}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))

	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &body) == nil && body.Error != "" {
		ret.Message = body.Error
		return ret
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
	if len(line) > 200 {
		line = line[:200] + "…"
	}
	ret.Message = line
	return ret
}

// call sends r and decodes a JSON answer into out. A nil out discards it.
func (c *Client) call(ctx context.Context, r request, out any) error {

	resp, err := c.do(ctx, r)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("read the answer to %s %s: %w", r.route.method, r.route.path, err)
	}
	return nil
}

// jsonBody marshals v for a request.
func jsonBody(r request, v any) (request, error) {

	data, err := json.Marshal(v)
	if err != nil {
		return r, fmt.Errorf("encode the request: %w", err)
	}
	r.body, r.contentType = data, "application/json"
	return r, nil
}

// download sends r and copies the answer into w, reporting progress.
func (c *Client) download(ctx context.Context, r request, w io.Writer, progress Progress) (int64, error) {

	resp, err := c.do(ctx, r)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	var src io.Reader = resp.Body
	if progress != nil {
		src = &counting{r: resp.Body, total: resp.ContentLength, progress: progress}
	}
	n, err := io.Copy(w, src)
	if err != nil {
		return n, fmt.Errorf("download %s: %w", r.route.path, err)
	}
	return n, nil
}

// counting reports progress as a body is read.
type counting struct {
	r        io.Reader
	done     int64
	total    int64
	progress Progress
}

func (c *counting) Read(p []byte) (int, error) {

	n, err := c.r.Read(p)
	c.done += int64(n)
	c.progress(c.done, c.total)
	return n, err
}
