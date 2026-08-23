// This file provides the "rest-dynamic" block: a request whose method, path,
// query, headers and body are all decided per message rather than at build time.
//
// The rest block freezes method and path, which is right when a flow calls a known
// endpoint and only the values vary. It is the wrong shape when the endpoint itself
// is data — a caller relaying a request it was handed, or an agent that decides
// which route to call from a description of the API. There is no way to express
// "GET /integrations/{the id I just learned}" with a static path.
//
// The other difference is query and headers. rest takes a map of expressions, one
// per known key. This takes a single expression per map, evaluated to the whole
// map, because a caller that does not know the path usually does not know the
// parameter names either.
package httpclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerRESTDynamic() {
	core.MustRegisterBlock("rest-dynamic", newRESTDynamic)

	// Group ("Integration") and Icon ("Globe") are inherited from the package
	// ExtensionMeta registered in httpclient.go.
	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "rest-dynamic",
		Label:    "Dynamic REST Call",
		Category: core.CategoryProcessor,
		Description: "Make an HTTP request through an http-client connector, with the method, " +
			"path, query, headers and body all built from expressions.",
		Config: reflect.TypeFor[restDynamicSettings](),
	})
}

// allowedMethods is the closed set a rendered method must belong to.
// http.NewRequest accepts any token that lexes as one, so without this a rendered
// method is whatever produced it — and what produces it here is, by design, data.
var allowedMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost,
	http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions,
}

// restDynamicSettings is the rest-dynamic block's typed configuration.
type restDynamicSettings struct {
	// Name of the http-client connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:http-client"`
	// CEL expression for the HTTP method.
	Method string `json:"method" octo:"label=Method,type=cel,required"`
	// CEL expression for the path, resolved against the connector's base URL.
	Path string `json:"path" octo:"label=Path,type=cel,required"`
	// CEL expression evaluating to a map of query parameters.
	Query string `json:"query" octo:"label=Query,type=cel"`
	// CEL expression evaluating to a map of request headers.
	Headers string `json:"headers" octo:"label=Headers,type=cel"`
	// CEL expression for the request body.
	Body string `json:"body" octo:"label=Body,type=cel"`
	// How the body is sent. raw sends the evaluated body as-is: a string verbatim,
	// anything else JSON-encoded. multipart treats it as a parts map -- what
	// body.parts holds and multipart()/addPart() build -- and renders a
	// multipart/form-data body, naming its own boundary in the Content-Type header.
	BodyType string `json:"bodyType" octo:"label=Body type,type=enum,enum=raw|multipart,default=raw"`
	// Methods this block may issue. Empty allows any of the standard set.
	AllowMethods []string `json:"allowMethods" octo:"label=Allowed methods,type=string-list"`
	// Path prefix this block may call under. Empty allows the whole base URL.
	PathPrefix string `json:"pathPrefix" octo:"label=Path prefix"`
	// Turn a 400+ status into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
	// Variable to store the response status code in.
	StatusVar string `json:"statusVar" octo:"label=Status variable,default=statusCode"`
}

// dynamicProcessor builds a request entirely from expressions, then runs it
// through the connector exactly as the rest block does.
type dynamicProcessor struct {
	conn         *Connector
	method       *expr.Program
	path         *expr.Program
	query        *expr.Program // nil when no query expression is configured
	headers      *expr.Program // nil when no header expression is configured
	body         *expr.Program // nil when no body expression is configured
	bodyType     string
	allowMethods []string // empty allows any of allowedMethods
	pathPrefix   string
	failOnError  bool
	statusVar    string
	env          map[string]any
}

// newRESTDynamic builds the processor, compiling every expression once so a bad
// one fails at startup rather than on the message that first reaches it.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newRESTDynamic(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg restDynamicSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}

	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Method) == "" {
		return nil, fmt.Errorf("rest-dynamic block: method is required")
	}
	if strings.TrimSpace(cfg.Path) == "" {
		return nil, fmt.Errorf("rest-dynamic block: path is required")
	}

	bodyType, err := resolveBodyType(cfg.BodyType)
	if err != nil {
		return nil, fmt.Errorf("rest-dynamic block: %w", err)
	}

	processor := &dynamicProcessor{
		conn:        conn,
		bodyType:    bodyType,
		pathPrefix:  cfg.PathPrefix,
		failOnError: true,
		statusVar:   defaultStatusVar,
		env:         expr.EnvActivation(deps.Env),
	}
	if err := processor.compileExpressions(cfg, deps); err != nil {
		return nil, err
	}

	processor.allowMethods, err = normalizeAllowMethods(cfg.AllowMethods)
	if err != nil {
		return nil, err
	}
	if cfg.StatusVar != "" {
		processor.statusVar = cfg.StatusVar
	}
	if cfg.FailOnError != nil {
		processor.failOnError = *cfg.FailOnError
	}
	return processor, nil
}

// compileExpressions compiles every configured expression into the processor,
// naming the setting in any error so a bad one says which it was. Compiling at
// build time is what makes a malformed expression fail at startup rather than on
// the message that first reaches it.
func (p *dynamicProcessor) compileExpressions(cfg restDynamicSettings, deps core.BlockDeps) error {
	targets := map[string]**expr.Program{
		"method": &p.method, "path": &p.path,
		"query": &p.query, "headers": &p.headers, "body": &p.body,
	}
	for name, source := range map[string]string{
		"method": cfg.Method, "path": cfg.Path,
		"query": cfg.Query, "headers": cfg.Headers, "body": cfg.Body,
	} {
		if source == "" {
			continue
		}
		program, err := expr.CompileMessage(deps.Resources, source)
		if err != nil {
			return fmt.Errorf("rest-dynamic block: compile %s: %w", name, err)
		}
		*targets[name] = program
	}
	return nil
}

// normalizeAllowMethods upper-cases the configured methods and rejects anything
// that is not an HTTP method, so a typo fails at startup rather than silently
// forbidding every request the block was meant to allow.
func normalizeAllowMethods(configured []string) ([]string, error) {
	var normalized []string
	for _, method := range configured {
		upper := strings.ToUpper(strings.TrimSpace(method))
		if !slices.Contains(allowedMethods, upper) {
			return nil, fmt.Errorf(
				"rest-dynamic block: allowMethods contains %q, which is not an HTTP method", method)
		}
		normalized = append(normalized, upper)
	}
	return normalized, nil
}

// Process renders the request, runs it, and folds the response into the message —
// the same tail as the rest block, over a request nothing knew the shape of until
// this message arrived.
func (p *dynamicProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)

	method, err := p.buildMethod(activation)
	if err != nil {
		return nil, err
	}
	target, err := p.buildTarget(activation)
	if err != nil {
		return nil, err
	}
	body, err := buildBodyFrom(p.body, activation, "rest-dynamic", p.bodyType)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, target, body.reader)
	if err != nil {
		return nil, fmt.Errorf("rest-dynamic build request: %w", err)
	}
	if err := p.applyHeaders(req, activation); err != nil {
		return nil, err
	}
	applyBodyContentType(req, body)

	return exchange(p.conn, req, target, msg, responseOptions{
		block: "rest-dynamic", statusVar: p.statusVar, failOnError: p.failOnError,
	})
}

// buildMethod renders the method and checks it against the standard set and any
// configured allow-list.
func (p *dynamicProcessor) buildMethod(activation map[string]any) (string, error) {
	rendered, err := p.method.EvalString(activation)
	if err != nil {
		return "", fmt.Errorf("rest-dynamic method: %w", err)
	}
	method := strings.ToUpper(strings.TrimSpace(rendered))
	if !slices.Contains(allowedMethods, method) {
		return "", fmt.Errorf("rest-dynamic method: %q is not an HTTP method", rendered)
	}
	if len(p.allowMethods) > 0 && !slices.Contains(p.allowMethods, method) {
		return "", fmt.Errorf("rest-dynamic method: %s is not allowed by this block", method)
	}
	return method, nil
}

// buildTarget renders the path, checks it, and appends the rendered query.
func (p *dynamicProcessor) buildTarget(activation map[string]any) (string, error) {
	rendered, err := p.path.EvalString(activation)
	if err != nil {
		return "", fmt.Errorf("rest-dynamic path: %w", err)
	}
	path := strings.TrimSpace(rendered)
	if err := p.checkPath(path); err != nil {
		return "", err
	}

	values, err := p.buildQuery(activation)
	if err != nil {
		return "", err
	}
	if len(values) == 0 {
		return path, nil
	}
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + values.Encode(), nil
}

// checkPath refuses the shapes a rendered path must not take.
//
// The connector's base URL is already a hard boundary: Connector.resolveURL starts
// from the configured base and takes only the path, query and fragment from the
// block's URL, so no value here can send the request to another host. But it does
// that silently — "http://evil.com/x" becomes "{base}/x" with no complaint — and a
// caller who wrote a full URL deserves to be told it was not honoured rather than
// to have a different request made on their behalf.
//
// A ".." segment is the one that matters. joinPath concatenates without cleaning,
// so "../.." escapes a base path prefix like "/v1" and reaches routes the connector
// was configured to sit beneath. Nothing legitimate needs it in a path built for an
// API call, and here the path is data.
//
// Every check runs against the parsed path rather than the rendered string, which
// gets two things right that scanning the raw value does not: url.Parse
// percent-decodes, so "%2e%2e" is caught alongside the literal "..", and the query
// is separated out, so a parameter value that happens to contain ".." is not
// mistaken for traversal it cannot perform.
func (p *dynamicProcessor) checkPath(path string) error {
	if path == "" {
		return fmt.Errorf("rest-dynamic path: evaluated to empty")
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return fmt.Errorf("rest-dynamic path: %q does not parse: %w", path, err)
	}
	if parsed.Scheme != "" || parsed.Host != "" {
		return fmt.Errorf("rest-dynamic path: %q is a full URL; "+
			"the host comes from the connector's base URL and a path cannot change it", path)
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == ".." {
			return fmt.Errorf("rest-dynamic path: %q contains a %q segment", path, "..")
		}
	}
	if p.pathPrefix != "" && !underPrefix(parsed.Path, p.pathPrefix) {
		return fmt.Errorf("rest-dynamic path: %q is outside the allowed prefix %q", path, p.pathPrefix)
	}
	return nil
}

// underPrefix reports whether path sits at or beneath prefix, on a segment
// boundary. A plain string prefix would let "/integrations" admit
// "/integrations-internal": the setting reads as a path boundary, so it has to be
// one, or it restricts less than the operator who wrote it believes.
func underPrefix(path, prefix string) bool {
	trimmed := strings.TrimSuffix(prefix, "/")
	return path == trimmed || strings.HasPrefix(path, trimmed+"/")
}

// buildQuery evaluates the query expression to a map of parameters.
func (p *dynamicProcessor) buildQuery(activation map[string]any) (url.Values, error) {
	entries, err := evalStringMap(p.query, activation, "query")
	if err != nil {
		return nil, err
	}
	if entries == nil {
		return nil, nil
	}
	values := url.Values{}
	for name, value := range entries {
		values.Set(name, value)
	}
	return values, nil
}

// applyHeaders evaluates the header expression to a map and sets each entry.
//
// Authorization is refused. The connector only applies its configured credential
// when the request does not already carry one, so a rendered Authorization header
// does not sit alongside the connector's auth — it replaces it. On this block the
// header names are themselves data, which would put the choice of credential in the
// hands of whatever produced the message. The connector's auth setting is where a
// credential belongs, and it is the one place an operator can see it.
func (p *dynamicProcessor) applyHeaders(req *http.Request, activation map[string]any) error {
	entries, err := evalStringMap(p.headers, activation, "headers")
	if err != nil {
		return err
	}
	for name, value := range entries {
		if http.CanonicalHeaderKey(name) == "Authorization" {
			return fmt.Errorf("rest-dynamic headers: %q is set by the connector, not by this block; "+
				"configure it under the http-client connector's auth", name)
		}
		req.Header.Set(name, value)
	}
	return nil
}

// evalStringMap evaluates one expression expected to produce a map, rendering each
// value as a string the way EvalString does — verbatim for a string, compact JSON
// for anything else. A nil program yields no entries.
func evalStringMap(program *expr.Program, activation map[string]any, what string) (map[string]string, error) {
	if program == nil {
		return nil, nil
	}
	value, err := program.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("rest-dynamic %s: %w", what, err)
	}
	if value == nil {
		return nil, nil
	}
	entries, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("rest-dynamic %s: expression produced %T, want a map", what, value)
	}

	out := make(map[string]string, len(entries))
	for name, raw := range entries {
		rendered, err := renderValue(raw)
		if err != nil {
			return nil, fmt.Errorf("rest-dynamic %s %q: %w", what, name, err)
		}
		out[name] = rendered
	}
	return out, nil
}
