package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// The MCP protocol revisions this router speaks. One implementation serves all of
// them: the router covers the subset — initialize, ping, tools, resources,
// prompts — that has not changed across them.
const (
	// mcpProtocolVersion is what a client that names no version is answered with.
	// It is deliberately the oldest supported rather than the newest: a client
	// that omits the field has not told us what it can read, and the transport
	// spec makes the same conservative assumption for a missing
	// MCP-Protocol-Version header.
	mcpProtocolVersion = "2024-11-05"
	// mcpProtocolVersion2025March is supported but never chosen by the router
	// itself; it is here so a client asking for it is answered in kind.
	mcpProtocolVersion2025March = "2025-03-26"
	// mcpLatestProtocolVersion is what a request for a version the router does not
	// speak is answered with, per the spec's "respond with another protocol
	// version it supports (SHOULD be the latest version supported by the server)".
	mcpLatestProtocolVersion = "2025-06-18"
)

// defaultMCPServerName names the server when neither serverName nor the block
// name is set.
const defaultMCPServerName = "octo-mcp"

// mcpHTTPStatusVar mirrors the http source's httpStatus variable, so a
// notification can respond 202 with no body. mcpNotificationStatus is that code.
const (
	mcpHTTPStatusVar      = "httpStatus"
	mcpNotificationStatus = 202
)

// Repeated JSON keys/values in the MCP wire shape, named so they are written once.
const (
	mcpKeyName     = "name"
	mcpKeyText     = "text"
	mcpKeyMimeType = "mimeType"
)

// JSON-RPC 2.0 error codes (the subset the router emits).
const (
	jsonrpcParseError     = -32700
	jsonrpcInvalidParams  = -32602
	jsonrpcMethodNotFound = -32601
	jsonrpcInternalError  = -32603
)

// jsonrpcRequest is one incoming MCP request. A request with no id is a
// notification (acknowledged, not answered). Params is method-specific.
type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// jsonrpcResponse is one outgoing MCP response. Exactly one of Result or Error is
// set. ID echoes the request id (null for a request that could not be parsed).
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// mcpResource is one resource the router advertises and serves on resources/read.
// Exactly one of uri and template is set: uri is one fixed document, template a
// family of them reached by matching a concrete uri back against it.
type mcpResource struct {
	uri         string
	template    *uriTemplate
	name        string
	description string
	mimeType    string
	resource    string
}

// mcpToolMeta is the MCP-only metadata a tool declared: display title, behavior
// hints, and the schema of what it returns. It is keyed by tool name beside the
// core.LLMTool list rather than folded into it, so the LLM abstraction stays
// about LLM calls.
type mcpToolMeta struct {
	title        string
	annotations  map[string]any
	outputSchema json.RawMessage
}

// mcpPrompt is one prompt the router advertises and renders on prompts/get.
type mcpPrompt struct {
	name        string
	description string
	arguments   []types.MCPPromptArg
	resource    string
}

// mcpRouter is a composite that turns a flow into a stateless MCP server. It sits
// in front of an HTTP source: each request body is one MCP JSON-RPC request and
// the router's output body is the JSON-RPC response (which the source returns).
// It advertises its tool flows as MCP tools, its template resources as MCP
// resources, and its template resources as MCP prompts, and routes tools/call to
// the matching flow. Unlike the ai-agent it calls no LLM.
type mcpRouter struct {
	serverName string
	tools      []core.LLMTool
	branches   map[string]*Flow
	toolMeta   map[string]mcpToolMeta
	resources  []mcpResource
	templates  []mcpResource
	prompts    []mcpPrompt
	registry   *expr.TemplateRegistry
	env        map[string]any
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) mcpRouter(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if err := allowSlots(cfg, blockKindMCPRouter,
		"tools", "resources", "prompts", "serverName"); err != nil {
		return nil, err
	}
	if len(cfg.Tools) == 0 && len(cfg.Resources) == 0 && len(cfg.Prompts) == 0 {
		return nil, errors.New("mcp-router block requires at least one tool, resource, or prompt")
	}

	branches, tools, err := b.agentTools(blockKindMCPRouter, cfg.Tools)
	if err != nil {
		return nil, err
	}
	toolMeta, err := buildMCPToolMeta(cfg.Tools)
	if err != nil {
		return nil, err
	}
	resources, templates, err := buildMCPResources(cfg.Resources)
	if err != nil {
		return nil, err
	}
	prompts, err := buildMCPPrompts(cfg.Prompts)
	if err != nil {
		return nil, err
	}

	serverName := cfg.ServerName
	if serverName == "" {
		serverName = cfg.Name
	}
	if serverName == "" {
		serverName = defaultMCPServerName
	}

	return &mcpRouter{
		serverName: serverName,
		tools:      tools,
		branches:   branches,
		toolMeta:   toolMeta,
		resources:  resources,
		templates:  templates,
		prompts:    prompts,
		registry:   expr.NewTemplateRegistry(b.deps.Resources),
		env:        expr.EnvActivation(b.deps.Env),
	}, nil
}

// buildMCPResources validates the resource configs and splits them into the fixed
// resources and the templated ones, which are advertised on different methods.
func buildMCPResources(configs []types.MCPResourceConfig) (fixed, templated []mcpResource, err error) {
	seen := make(map[string]bool, len(configs))
	for i := range configs {
		res, key, err := buildMCPResource(i, configs[i])
		if err != nil {
			return nil, nil, err
		}
		if seen[key] {
			return nil, nil, fmt.Errorf("mcp-router resource %q is defined more than once", key)
		}
		seen[key] = true
		if res.template != nil {
			templated = append(templated, res)
			continue
		}
		fixed = append(fixed, res)
	}
	return fixed, templated, nil
}

// buildMCPResource validates one resource config and returns it alongside the key
// it is deduplicated by — its uri or its uriTemplate, whichever it declared.
func buildMCPResource(i int, c types.MCPResourceConfig) (mcpResource, string, error) {
	switch {
	case c.URI == "" && c.URITemplate == "":
		return mcpResource{}, "", fmt.Errorf("mcp-router resource %d requires a uri or a uriTemplate", i)
	case c.URI != "" && c.URITemplate != "":
		return mcpResource{}, "", fmt.Errorf(
			"mcp-router resource %q declares both a uri and a uriTemplate: it is either one fixed "+
				"document or a family of them", c.URI)
	}

	key := c.URI
	if key == "" {
		key = c.URITemplate
	}
	switch {
	case c.Name == "":
		return mcpResource{}, "", fmt.Errorf("mcp-router resource %q requires a name", key)
	case c.Resource == "":
		return mcpResource{}, "", fmt.Errorf("mcp-router resource %q requires a resource", key)
	}

	res := mcpResource{
		uri: c.URI, name: c.Name, description: c.Description,
		mimeType: c.MimeType, resource: c.Resource,
	}
	if res.mimeType == "" {
		res.mimeType = "text/plain"
	}
	if c.URITemplate != "" {
		tpl, err := parseURITemplate(c.URITemplate)
		if err != nil {
			return mcpResource{}, "", fmt.Errorf("mcp-router resource %q: %w", c.Name, err)
		}
		res.template = &tpl
	}
	return res, key, nil
}

// buildMCPPrompts validates the prompt configs and builds the advertised set,
// rejecting duplicates and missing fields.
func buildMCPPrompts(configs []types.MCPPromptConfig) ([]mcpPrompt, error) {
	prompts := make([]mcpPrompt, 0, len(configs))
	seen := make(map[string]bool, len(configs))
	for i := range configs {
		c := configs[i]
		switch {
		case c.Name == "":
			return nil, fmt.Errorf("mcp-router prompt %d requires a name", i)
		case c.Resource == "":
			return nil, fmt.Errorf("mcp-router prompt %q requires a resource", c.Name)
		case seen[c.Name]:
			return nil, fmt.Errorf("mcp-router prompt %q is defined more than once", c.Name)
		}
		seen[c.Name] = true
		prompts = append(prompts, mcpPrompt{
			name: c.Name, description: c.Description, arguments: c.Arguments, resource: c.Resource,
		})
	}
	return prompts, nil
}

// Process handles one MCP JSON-RPC request: it decodes the request body, and
// either acknowledges a notification with an empty 202 or dispatches the method
// and writes the JSON-RPC response as the message body.
func (m *mcpRouter) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	raw, err := msg.BodyJSON()
	if err != nil {
		return m.writeResponse(msg, errorResponse(nil, jsonrpcParseError, "could not read request body"))
	}
	var req jsonrpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return m.writeResponse(msg, errorResponse(nil, jsonrpcParseError, "invalid JSON-RPC request"))
	}

	// A request with no id is a notification: acknowledge with an empty 202 body.
	if len(req.ID) == 0 {
		slog.Debug("mcp-router notification", "method", req.Method)
		msg.SetRawBody("application/json", "")
		msg.Variables.Set(mcpHTTPStatusVar, mcpNotificationStatus)
		return msg, nil
	}

	slog.Info("mcp-router request", "method", req.Method)
	return m.writeResponse(msg, m.dispatch(ctx, req, msg))
}

// dispatch routes a request to its handler, returning method-not-found for any
// method the router does not implement.
func (m *mcpRouter) dispatch(ctx context.Context, req jsonrpcRequest, msg *types.Message) jsonrpcResponse {
	switch req.Method {
	case "initialize":
		return m.initialize(req)
	case "ping":
		return okResponse(req.ID, map[string]any{})
	case "tools/list":
		return okResponse(req.ID, map[string]any{"tools": m.toolList()})
	case "tools/call":
		return m.callTool(ctx, req, msg)
	case "resources/list":
		return okResponse(req.ID, map[string]any{"resources": m.resourceList()})
	case "resources/templates/list":
		return okResponse(req.ID, map[string]any{"resourceTemplates": m.resourceTemplateList()})
	case "resources/read":
		return m.readResource(ctx, req, msg)
	case "prompts/list":
		return okResponse(req.ID, map[string]any{"prompts": m.promptList()})
	case "prompts/get":
		return m.getPrompt(ctx, req, msg)
	default:
		return errorResponse(req.ID, jsonrpcMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	}
}

// initialize reports the router's capabilities and server info, on a protocol
// version negotiated with the client.
func (m *mcpRouter) initialize(req jsonrpcRequest) jsonrpcResponse {
	requested := ""
	if len(req.Params) > 0 {
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &params) == nil {
			requested = params.ProtocolVersion
		}
	}
	return okResponse(req.ID, map[string]any{
		"protocolVersion": negotiateProtocolVersion(requested),
		"capabilities":    m.capabilities(),
		"serverInfo":      map[string]any{mcpKeyName: m.serverName, "version": core.Version},
	})
}

// negotiateProtocolVersion answers with the client's own version when the router
// speaks it, the conservative default when the client named none, and the latest
// supported otherwise.
//
// The last case is the one that matters: echoing back a version the router has
// never heard of — which is what it used to do — is a false agreement, and worse
// than a mismatch. Answering with a version the router does speak lets a client
// that cannot read it disconnect rather than proceed on the lie.
func negotiateProtocolVersion(requested string) string {
	switch requested {
	case "":
		return mcpProtocolVersion
	case mcpProtocolVersion, mcpProtocolVersion2025March, mcpLatestProtocolVersion:
		return requested
	default:
		return mcpLatestProtocolVersion
	}
}

// capabilities reports only what this router can actually serve.
//
// Advertising a capability nothing is configured for is what sends a client to
// prompts/list on a tools-only server, and then makes it log the error it gets
// back. The builder already requires at least one of the three, so this is never
// empty.
//
// Neither subscribe nor listChanged is claimed. The router is stateless — one
// POST is one request and one response — so there is no channel to notify a
// client over; the default for both is false, and omitting them says so.
func (m *mcpRouter) capabilities() map[string]any {
	caps := make(map[string]any)
	if len(m.tools) > 0 {
		caps["tools"] = map[string]any{}
	}
	// Templated resources count: a router with only templates still answers
	// resources/list (empty), resources/templates/list, and resources/read.
	if len(m.resources) > 0 || len(m.templates) > 0 {
		caps["resources"] = map[string]any{}
	}
	if len(m.prompts) > 0 {
		caps["prompts"] = map[string]any{}
	}
	return caps
}

// toolList advertises the tool flows with their JSON-Schema input, plus whatever
// MCP metadata each declared.
func (m *mcpRouter) toolList() []map[string]any {
	tools := make([]map[string]any, 0, len(m.tools))
	for _, t := range m.tools {
		entry := map[string]any{
			mcpKeyName:    t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		}
		meta := m.toolMeta[t.Name]
		if meta.title != "" {
			entry["title"] = meta.title
		}
		if len(meta.annotations) > 0 {
			entry["annotations"] = meta.annotations
		}
		if meta.outputSchema != nil {
			entry["outputSchema"] = meta.outputSchema
		}
		tools = append(tools, entry)
	}
	return tools
}

// buildMCPToolMeta collects the router-only tool metadata, keyed by tool name.
//
// It is held here rather than on core.LLMTool because that type is the LLM
// abstraction the provider connectors speak: an output schema and a
// destructiveHint mean nothing to a model call, and widening it would push MCP's
// vocabulary onto every caller of it.
func buildMCPToolMeta(configs []types.ToolConfig) (map[string]mcpToolMeta, error) {
	metas := make(map[string]mcpToolMeta, len(configs))
	for i := range configs {
		c := configs[i]
		meta := mcpToolMeta{title: c.Title, annotations: toolAnnotations(c.Annotations)}
		if schema := strings.TrimSpace(c.OutputSchema); schema != "" {
			if !json.Valid([]byte(schema)) {
				return nil, fmt.Errorf("mcp-router tool %q: outputSchema is not valid JSON", c.Name)
			}
			meta.outputSchema = json.RawMessage(schema)
		}
		metas[c.Name] = meta
	}
	return metas, nil
}

// toolAnnotations renders the declared hints, leaving out the ones the author did
// not state. Omitting an unstated hint matters: the protocol's defaults differ
// per hint, so writing them all out would assert things nobody claimed.
func toolAnnotations(a *types.ToolAnnotations) map[string]any {
	if a == nil {
		return nil
	}
	out := make(map[string]any)
	for key, hint := range map[string]*bool{
		"readOnlyHint":    a.ReadOnlyHint,
		"destructiveHint": a.DestructiveHint,
		"idempotentHint":  a.IdempotentHint,
		"openWorldHint":   a.OpenWorldHint,
	} {
		if hint != nil {
			out[key] = *hint
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// callTool routes a tools/call to the named flow branch: the arguments become the
// message body and the branch's output body is returned as the tool text result.
// An unknown tool or a branch failure is reported as an isError tool result (an
// application error the client sees), not a protocol error.
func (m *mcpRouter) callTool(ctx context.Context, req jsonrpcRequest, msg *types.Message) jsonrpcResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		return errorResponse(req.ID, jsonrpcInvalidParams, "tools/call requires a name")
	}
	flow, ok := m.branches[params.Name]
	if !ok {
		return okResponse(req.ID, toolCallResult(fmt.Sprintf("unknown tool %q", params.Name), true))
	}
	content, _, errMsg := dispatchToolBranch(ctx, flow, params.Arguments, msg)
	if errMsg != "" {
		return okResponse(req.ID, toolCallResult(errMsg, true))
	}
	result := toolCallResult(content, false)
	m.addStructuredContent(result, params.Name, content)
	return okResponse(req.ID, result)
}

// addStructuredContent attaches the branch's result as data, for a tool that
// declared an output schema.
//
// The text block stays either way: the spec requires a tool returning structured
// content to also serialize it into a TextContent block, so a client that does
// not read structuredContent still gets the answer.
//
// Content that does not parse as a JSON object is left alone rather than
// reported as an error. The declaration is a promise about the branch, and
// failing the call at the protocol level would turn a mismatched promise into an
// outage for a tool that otherwise answered.
func (m *mcpRouter) addStructuredContent(result map[string]any, tool, content string) {
	if m.toolMeta[tool].outputSchema == nil {
		return
	}
	var structured map[string]any
	if err := json.Unmarshal([]byte(content), &structured); err != nil {
		slog.Debug("mcp-router tool declares an outputSchema but did not return a JSON object",
			"tool", tool, "error", err)
		return
	}
	result["structuredContent"] = structured
}

// toolCallResult wraps text as an MCP tool result content block.
func toolCallResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": mcpKeyText, mcpKeyText: text}},
		"isError": isError,
	}
}

// resourceList advertises the configured resources.
func (m *mcpRouter) resourceList() []map[string]any {
	out := make([]map[string]any, 0, len(m.resources))
	for _, r := range m.resources {
		entry := map[string]any{"uri": r.uri, mcpKeyName: r.name, mcpKeyMimeType: r.mimeType}
		if r.description != "" {
			entry["description"] = r.description
		}
		out = append(out, entry)
	}
	return out
}

// resourceTemplateList advertises the templated resources. They are deliberately
// absent from resourceList: a template is not a document a client can read, it is
// a shape a client fills in, and the spec gives each its own method for exactly
// that reason.
func (m *mcpRouter) resourceTemplateList() []map[string]any {
	out := make([]map[string]any, 0, len(m.templates))
	for _, r := range m.templates {
		entry := map[string]any{"uriTemplate": r.template.raw, mcpKeyName: r.name, mcpKeyMimeType: r.mimeType}
		if r.description != "" {
			entry["description"] = r.description
		}
		out = append(out, entry)
	}
	return out
}

// readResource renders the resource the uri names and returns its contents.
func (m *mcpRouter) readResource(ctx context.Context, req jsonrpcRequest, msg *types.Message) jsonrpcResponse {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.URI == "" {
		return errorResponse(req.ID, jsonrpcInvalidParams, "resources/read requires a uri")
	}
	res, values, ok := m.matchResource(params.URI)
	if !ok {
		return errorResponse(req.ID, jsonrpcInvalidParams, fmt.Sprintf("unknown resource %q", params.URI))
	}
	target, err := m.resourceMessage(msg, values)
	if err != nil {
		return errorResponse(req.ID, jsonrpcInternalError, fmt.Sprintf("read resource %q: %v", params.URI, err))
	}
	text, err := m.render(ctx, res.resource, target)
	if err != nil {
		return errorResponse(req.ID, jsonrpcInternalError, fmt.Sprintf("read resource %q: %v", params.URI, err))
	}
	// The concrete uri the client asked for, not the template it matched: the
	// contents have to be addressable by what was read.
	return okResponse(req.ID, map[string]any{
		"contents": []map[string]any{{"uri": params.URI, mcpKeyMimeType: res.mimeType, mcpKeyText: text}},
	})
}

// matchResource finds the resource a concrete uri names, and what its template
// variables took if it matched one.
//
// Fixed uris are tried first — they are exact, cheap and unambiguous — and only
// then the templates, in declaration order, first match winning. That order is
// the contract: a fixed resource always shadows a template that would also match
// it, so an author can carve one special case out of a family.
func (m *mcpRouter) matchResource(uri string) (mcpResource, map[string]any, bool) {
	if res, ok := m.findResource(uri); ok {
		return res, nil, true
	}
	for _, res := range m.templates {
		if values, ok := res.template.match(uri); ok {
			return res, values, true
		}
	}
	return mcpResource{}, nil, false
}

// resourceMessage is the message a resource's template renders against: the
// request's own variables, with the values a uri template extracted as the body.
//
// The variables are the half that is easy to overlook and the half that matters.
// A jwt-validate in front of the router leaves the caller's claims in vars.jwt,
// and a resource that renders per-caller data needs them — a body-only message
// would make the router's own front door invisible to the thing it protects.
//
// A resource with no template variables renders against the request message
// untouched, which is what it has always done.
func (m *mcpRouter) resourceMessage(msg *types.Message, values map[string]any) (*types.Message, error) {
	if len(values) == 0 {
		return msg, nil
	}
	// Carrying the variables over rather than cloning is the same trade the agent
	// events path makes: the body is being replaced anyway, so deep-copying it
	// first would be work thrown away.
	out, err := types.NewMessage(msg.CorrelationID)
	if err != nil {
		// Falling back to the request message would render the resource against
		// the JSON-RPC envelope instead of the extracted variables, and answer 200
		// with content built from the wrong data. An error is the honest reply.
		return nil, err
	}
	for name, v := range msg.Variables {
		out.Variables.Set(name, v)
	}
	out.SetBody(values)
	return out, nil
}

// promptList advertises the configured prompts and their argument metadata.
func (m *mcpRouter) promptList() []map[string]any {
	out := make([]map[string]any, 0, len(m.prompts))
	for _, p := range m.prompts {
		entry := map[string]any{mcpKeyName: p.name}
		if p.description != "" {
			entry["description"] = p.description
		}
		if len(p.arguments) > 0 {
			args := make([]map[string]any, 0, len(p.arguments))
			for _, a := range p.arguments {
				arg := map[string]any{mcpKeyName: a.Name, "required": a.Required}
				if a.Description != "" {
					arg["description"] = a.Description
				}
				args = append(args, arg)
			}
			entry["arguments"] = args
		}
		out = append(out, entry)
	}
	return out
}

// getPrompt renders the named prompt's template, exposing the supplied arguments
// to the template as the message body (body.<arg>), and returns it as a user
// message.
func (m *mcpRouter) getPrompt(ctx context.Context, req jsonrpcRequest, msg *types.Message) jsonrpcResponse {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		return errorResponse(req.ID, jsonrpcInvalidParams, "prompts/get requires a name")
	}
	prompt, ok := m.findPrompt(params.Name)
	if !ok {
		return errorResponse(req.ID, jsonrpcInvalidParams, fmt.Sprintf("unknown prompt %q", params.Name))
	}
	// A fresh message, so the body is the arguments rather than the JSON-RPC
	// envelope — but carrying the request's variables, so a prompt template can
	// read what a jwt-validate in front of the router left behind, exactly as a
	// resource's template can.
	argMsg, err := types.NewMessage(msg.CorrelationID)
	if err != nil {
		return errorResponse(req.ID, jsonrpcInternalError, err.Error())
	}
	for name, v := range msg.Variables {
		argMsg.Variables.Set(name, v)
	}
	if len(params.Arguments) > 0 {
		if err := argMsg.SetBodyJSON(params.Arguments); err != nil {
			return errorResponse(req.ID, jsonrpcInvalidParams, fmt.Sprintf("invalid arguments: %v", err))
		}
	}
	text, err := m.render(ctx, prompt.resource, argMsg)
	if err != nil {
		return errorResponse(req.ID, jsonrpcInternalError, fmt.Sprintf("get prompt %q: %v", prompt.name, err))
	}
	result := map[string]any{
		"messages": []map[string]any{{
			"role":    "user",
			"content": map[string]any{"type": mcpKeyText, mcpKeyText: text},
		}},
	}
	if prompt.description != "" {
		result["description"] = prompt.description
	}
	return okResponse(req.ID, result)
}

// render loads and renders a template resource against the message.
func (m *mcpRouter) render(ctx context.Context, id string, msg *types.Message) (string, error) {
	tpl, err := m.registry.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return tpl.Render(expr.MessageActivation(msg, m.env))
}

func (m *mcpRouter) findResource(uri string) (mcpResource, bool) {
	for _, r := range m.resources {
		if r.uri == uri {
			return r, true
		}
	}
	return mcpResource{}, false
}

func (m *mcpRouter) findPrompt(name string) (mcpPrompt, bool) {
	for _, p := range m.prompts {
		if p.name == name {
			return p, true
		}
	}
	return mcpPrompt{}, false
}

// writeResponse encodes the JSON-RPC response as the message body and returns the
// message so the HTTP source serves it.
func (m *mcpRouter) writeResponse(msg *types.Message, resp jsonrpcResponse) (*types.Message, error) {
	encoded, err := json.Marshal(resp)
	if err != nil {
		return nil, fmt.Errorf("mcp-router: encode response: %w", err)
	}
	if err := msg.SetBodyJSON(encoded); err != nil {
		return nil, fmt.Errorf("mcp-router: set response body: %w", err)
	}
	return msg, nil
}

// okResponse builds a success response echoing the request id.
func okResponse(id json.RawMessage, result any) jsonrpcResponse {
	return jsonrpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

// errorResponse builds an error response; a nil id (unparseable request) becomes
// JSON null, as the JSON-RPC spec requires.
func errorResponse(id json.RawMessage, code int, message string) jsonrpcResponse {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	return jsonrpcResponse{JSONRPC: "2.0", ID: id, Error: &jsonrpcError{Code: code, Message: message}}
}
