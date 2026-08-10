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
	"reflect"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerREST() {
	core.MustRegisterBlock("rest", newREST)

	// Group ("Integration") and Icon ("Globe") are inherited from the package
	// ExtensionMeta registered in httpclient.go.
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "rest",
		Label:       "REST Call",
		Category:    core.CategoryProcessor,
		Description: "Make an HTTP request through an http-client connector.",
		Config:      reflect.TypeFor[restSettings](),
	})
}

const defaultStatusVar = "statusCode"

// restSettings is the rest block's typed configuration.
type restSettings struct {
	// Name of the http-client connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:http-client"`
	// HTTP method.
	Method string `json:"method" octo:"label=Method,type=enum,enum=GET|POST|PUT|PATCH|DELETE|HEAD,default=GET"`
	// Path appended to the connector base URL.
	Path string `json:"path" octo:"label=Path"`
	// Query params; each value is a CEL expression.
	Query map[string]string `json:"query" octo:"label=Query"`
	// Request headers; each value is a CEL expression.
	Headers map[string]string `json:"headers" octo:"label=Headers"`
	// CEL expression for the request body.
	Body string `json:"body" octo:"label=Body,type=cel"`
	// Turn a 400+ status into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
	// Variable to store the response status code in.
	StatusVar string `json:"statusVar" octo:"label=Status variable,default=statusCode"`
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

	query, err := compileMap(deps.Resources, cfg.Query)
	if err != nil {
		return nil, err
	}
	headers, err := compileMap(deps.Resources, cfg.Headers)
	if err != nil {
		return nil, err
	}
	var body *expr.Program
	if cfg.Body != "" {
		body, err = expr.CompileMessage(deps.Resources, cfg.Body)
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
		env:         expr.EnvActivation(deps.Env),
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

// compileMap compiles each value of a name->expression map into a message program.
func compileMap(res core.ResourceLoader, in map[string]string) (map[string]*expr.Program, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]*expr.Program, len(in))
	for name, e := range in {
		program, err := expr.CompileMessage(res, e)
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
	activation := expr.MessageActivation(msg, p.env)

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
		return nil, fmt.Errorf("rest request to %s returned %d: %s",
			requestedURL(req, target), resp.StatusCode, snippet(respBody))
	}

	if err := foldResponse(msg, respBody, resp.Header.Get("Content-Type")); err != nil {
		return nil, err
	}
	return msg, nil
}

// requestedURL names the URL the call actually went to. Connector.Do resolves
// the block's path against the connector's base URL in place, so once it has
// returned req.URL is the absolute URL — and that is the one worth reporting.
// The configured path on its own ("things") names neither the host nor the base
// it was joined to, which leaves a reader of "rest request to things returned
// 400" with no indication a base URL was even involved.
//
// Userinfo is dropped whole rather than passed through URL.Redacted(), which
// masks the password but keeps the username. That is the right trade for the
// debug logs next door, where the username tells you which credential was used;
// it is the wrong one here, because a block error travels — into the flow's
// error handling, the shipped logs, and the traces UI — and the host and path
// are the whole diagnostic value. The username adds nothing a reader cannot get
// from the connector name.
//
// Falls back to the configured target for the one path where Do returns before
// resolving anything: an unstarted connector.
func requestedURL(req *http.Request, target string) string {
	if req.URL == nil || req.URL.Host == "" {
		return target
	}
	clean := *req.URL
	clean.User = nil
	return clean.String()
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
// such (normalized to decoded-JSON kinds), a raw-content body carrying the
// response's Content-Type otherwise, or null for an empty body.
func foldResponse(msg *types.Message, body []byte, contentType string) error {
	if len(bytes.TrimSpace(body)) == 0 {
		msg.Body = nil
		return nil
	}
	if json.Valid(body) {
		return msg.SetBodyJSON(body)
	}
	msg.SetRawBody(contentType, string(body))
	return nil
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
