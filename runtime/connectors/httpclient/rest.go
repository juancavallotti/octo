// This file provides the "rest" block: a processor that runs a single HTTP
// request through an http-client connector and folds the response into the
// message body. Method and path are static; query parameters, headers, and the
// request body come from CEL expressions evaluated per message. The response
// status is stored in a variable (default vars.statusCode), and by default a
// non-2xx/3xx status fails the message.
//
// The block lives in the connector's package and binds to it by concrete type.
package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/core/expr"
	"github.com/juancavallotti/eip-go/types"
)

func init() {
	core.MustRegisterBlock("rest", newREST)
}

const defaultStatusVar = "statusCode"

// exprVars are the names a rest expression can reference, matching the other
// CEL-driven blocks.
var exprVars = []string{"body", "vars", "eventID", "correlationID", "env"}

// restSettings is the rest block's typed configuration.
type restSettings struct {
	// Connector names the http-client connector to call through (required).
	Connector string `json:"connector"`
	// Method is the HTTP method (default GET).
	Method string `json:"method"`
	// Path is appended to the connector's base URL.
	Path string `json:"path"`
	// Query maps parameter names to CEL expressions evaluated per message.
	Query map[string]string `json:"query"`
	// Headers maps header names to CEL expressions evaluated per message.
	Headers map[string]string `json:"headers"`
	// Body is a CEL expression producing the request body (write methods). A
	// string result is sent as-is; any other value is JSON-encoded.
	Body string `json:"body"`
	// FailOnError, when true (the default), turns a status >= 400 into an error.
	// It is a pointer so an explicit false is distinguishable from unset.
	FailOnError *bool `json:"failOnError"`
	// StatusVar names the variable the response status is stored in (default
	// "statusCode").
	StatusVar string `json:"statusVar"`
}

// processor builds and runs the request, then folds the response into the body.
type processor struct {
	conn        *Connector
	method      string
	path        string
	query       map[string]*expr.Program
	headers     map[string]*expr.Program
	body        *expr.Program
	failOnError bool
	statusVar   string
	env         map[string]any
}

// newREST builds a rest processor, resolving its connector and compiling the
// query/header/body expressions once so a bad reference or expression fails at
// startup rather than at runtime.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newREST(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg restSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}

	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, err
	}

	query, err := compileMap(cfg.Query)
	if err != nil {
		return nil, err
	}
	headers, err := compileMap(cfg.Headers)
	if err != nil {
		return nil, err
	}
	var body *expr.Program
	if cfg.Body != "" {
		body, err = expr.Compile(cfg.Body, exprVars...)
		if err != nil {
			return nil, err
		}
	}

	method := strings.ToUpper(strings.TrimSpace(cfg.Method))
	if method == "" {
		method = http.MethodGet
	}
	statusVar := cfg.StatusVar
	if statusVar == "" {
		statusVar = defaultStatusVar
	}
	failOnError := true
	if cfg.FailOnError != nil {
		failOnError = *cfg.FailOnError
	}

	return &processor{
		conn:        conn,
		method:      method,
		path:        cfg.Path,
		query:       query,
		headers:     headers,
		body:        body,
		failOnError: failOnError,
		statusVar:   statusVar,
		env:         envActivation(deps.Env),
	}, nil
}

// resolveConnector binds the block to its http-client connector by name.
func resolveConnector(name string, deps core.BlockDeps) (*Connector, error) {
	if name == "" {
		return nil, fmt.Errorf("rest block: connector is required")
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("rest block: connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("rest block: http-client connector %q is not configured", name)
	}
	conn, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("rest block: connector %q is not an http-client", name)
	}
	return conn, nil
}

// compileMap compiles each value of a name->expression map into a program.
func compileMap(in map[string]string) (map[string]*expr.Program, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]*expr.Program, len(in))
	for name, e := range in {
		program, err := expr.Compile(e, exprVars...)
		if err != nil {
			return nil, fmt.Errorf("rest block: compile %q: %w", name, err)
		}
		out[name] = program
	}
	return out, nil
}

// Process builds the request from the message, executes it through the
// connector, stores the status in a variable, and folds the response body into
// the message body.
func (p *processor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := messageActivation(msg, p.env)

	target, err := p.buildURL(activation)
	if err != nil {
		return nil, err
	}
	bodyReader, hasBody, err := p.buildBody(activation)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, p.method, target, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("rest build request: %w", err)
	}
	if err := p.applyHeaders(req, activation); err != nil {
		return nil, err
	}
	if hasBody && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.conn.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rest request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("rest read response: %w", err)
	}

	msg.Variables.Set(p.statusVar, resp.StatusCode)

	if p.failOnError && resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("rest request to %s returned %d: %s", target, resp.StatusCode, snippet(respBody))
	}

	if err := foldResponse(msg, respBody); err != nil {
		return nil, err
	}
	return msg, nil
}

// buildURL renders the query expressions and assembles "path?query".
func (p *processor) buildURL(activation map[string]any) (string, error) {
	if len(p.query) == 0 {
		return p.path, nil
	}
	values := url.Values{}
	for name, program := range p.query {
		value, err := program.EvalString(activation)
		if err != nil {
			return "", fmt.Errorf("rest query %q: %w", name, err)
		}
		values.Set(name, value)
	}
	sep := "?"
	if strings.Contains(p.path, "?") {
		sep = "&"
	}
	return p.path + sep + values.Encode(), nil
}

// buildBody renders the body expression, returning a reader and whether a body
// was produced. A string result is sent verbatim; any other value is JSON-encoded.
func (p *processor) buildBody(activation map[string]any) (io.Reader, bool, error) {
	if p.body == nil {
		return nil, false, nil
	}
	value, err := p.body.Eval(activation)
	if err != nil {
		return nil, false, fmt.Errorf("rest body: %w", err)
	}
	if s, ok := value.(string); ok {
		return strings.NewReader(s), true, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false, fmt.Errorf("rest encode body: %w", err)
	}
	return bytes.NewReader(raw), true, nil
}

// applyHeaders renders and sets each configured request header.
func (p *processor) applyHeaders(req *http.Request, activation map[string]any) error {
	for name, program := range p.headers {
		value, err := program.EvalString(activation)
		if err != nil {
			return fmt.Errorf("rest header %q: %w", name, err)
		}
		req.Header.Set(name, value)
	}
	return nil
}

// foldResponse writes the response body into the message: JSON when it parses as
// such (normalized to decoded-JSON kinds), the raw string otherwise, or null for
// an empty body.
func foldResponse(msg *types.Message, body []byte) error {
	if len(bytes.TrimSpace(body)) == 0 {
		msg.Body = nil
		return nil
	}
	if json.Valid(body) {
		return msg.SetBodyJSON(body)
	}
	msg.Body = string(body)
	return nil
}

// messageActivation maps a message (and the block's resolved env) onto the
// variables an expression can reference.
func messageActivation(msg *types.Message, env map[string]any) map[string]any {
	return map[string]any{
		"body":          msg.Body,
		"vars":          map[string]any(msg.Variables),
		"eventID":       msg.EventID,
		"correlationID": msg.CorrelationID,
		"env":           env,
	}
}

// envActivation materializes a resolved env map into the form CEL expects once
// at build time, so it is shared across every message the block processes.
func envActivation(env map[string]string) map[string]any {
	out := make(map[string]any, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}

// snippet returns a short, single-line preview of a response body for errors.
func snippet(body []byte) string {
	const maxLen = 200
	s := strings.TrimSpace(string(body))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}
