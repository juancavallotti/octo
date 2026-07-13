package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// mcpProtocolVersion is the MCP protocol version the router reports when a client
// does not request one in initialize.
const mcpProtocolVersion = "2024-11-05"

// mcpServerVersion is the version the router reports in its initialize serverInfo.
const mcpServerVersion = "0.1.0"

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
	mcpKeyName = "name"
	mcpKeyText = "text"
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
type mcpResource struct {
	uri         string
	name        string
	description string
	mimeType    string
	resource    string
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
	resources  []mcpResource
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
	resources, err := buildMCPResources(cfg.Resources)
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
		resources:  resources,
		prompts:    prompts,
		registry:   expr.NewTemplateRegistry(b.deps.Resources),
		env:        expr.EnvActivation(b.deps.Env),
	}, nil
}

// buildMCPResources validates the resource configs and builds the advertised set,
// rejecting duplicates and missing fields and defaulting the mime type.
func buildMCPResources(configs []types.MCPResourceConfig) ([]mcpResource, error) {
	resources := make([]mcpResource, 0, len(configs))
	seen := make(map[string]bool, len(configs))
	for i := range configs {
		c := configs[i]
		switch {
		case c.URI == "":
			return nil, fmt.Errorf("mcp-router resource %d requires a uri", i)
		case c.Name == "":
			return nil, fmt.Errorf("mcp-router resource %q requires a name", c.URI)
		case c.Resource == "":
			return nil, fmt.Errorf("mcp-router resource %q requires a resource", c.URI)
		case seen[c.URI]:
			return nil, fmt.Errorf("mcp-router resource uri %q is defined more than once", c.URI)
		}
		seen[c.URI] = true
		mimeType := c.MimeType
		if mimeType == "" {
			mimeType = "text/plain"
		}
		resources = append(resources, mcpResource{
			uri: c.URI, name: c.Name, description: c.Description, mimeType: mimeType, resource: c.Resource,
		})
	}
	return resources, nil
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
	case "resources/read":
		return m.readResource(ctx, req, msg)
	case "prompts/list":
		return okResponse(req.ID, map[string]any{"prompts": m.promptList()})
	case "prompts/get":
		return m.getPrompt(ctx, req)
	default:
		return errorResponse(req.ID, jsonrpcMethodNotFound, fmt.Sprintf("method %q not found", req.Method))
	}
}

// initialize reports the router's capabilities and server info, echoing the
// client's requested protocol version when it supplies one.
func (m *mcpRouter) initialize(req jsonrpcRequest) jsonrpcResponse {
	version := mcpProtocolVersion
	if len(req.Params) > 0 {
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &params) == nil && params.ProtocolVersion != "" {
			version = params.ProtocolVersion
		}
	}
	return okResponse(req.ID, map[string]any{
		"protocolVersion": version,
		"capabilities": map[string]any{
			"tools":     map[string]any{},
			"resources": map[string]any{},
			"prompts":   map[string]any{},
		},
		"serverInfo": map[string]any{mcpKeyName: m.serverName, "version": mcpServerVersion},
	})
}

// toolList advertises the tool flows with their JSON-Schema input.
func (m *mcpRouter) toolList() []map[string]any {
	tools := make([]map[string]any, 0, len(m.tools))
	for _, t := range m.tools {
		tools = append(tools, map[string]any{
			mcpKeyName:    t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return tools
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
	return okResponse(req.ID, toolCallResult(content, false))
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
		entry := map[string]any{"uri": r.uri, mcpKeyName: r.name, "mimeType": r.mimeType}
		if r.description != "" {
			entry["description"] = r.description
		}
		out = append(out, entry)
	}
	return out
}

// readResource renders the named resource's template and returns its contents.
func (m *mcpRouter) readResource(ctx context.Context, req jsonrpcRequest, msg *types.Message) jsonrpcResponse {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.URI == "" {
		return errorResponse(req.ID, jsonrpcInvalidParams, "resources/read requires a uri")
	}
	res, ok := m.findResource(params.URI)
	if !ok {
		return errorResponse(req.ID, jsonrpcInvalidParams, fmt.Sprintf("unknown resource %q", params.URI))
	}
	text, err := m.render(ctx, res.resource, msg)
	if err != nil {
		return errorResponse(req.ID, jsonrpcInternalError, fmt.Sprintf("read resource %q: %v", res.uri, err))
	}
	return okResponse(req.ID, map[string]any{
		"contents": []map[string]any{{"uri": res.uri, "mimeType": res.mimeType, mcpKeyText: text}},
	})
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
func (m *mcpRouter) getPrompt(ctx context.Context, req jsonrpcRequest) jsonrpcResponse {
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
	argMsg, err := types.NewMessage("")
	if err != nil {
		return errorResponse(req.ID, jsonrpcInternalError, err.Error())
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
