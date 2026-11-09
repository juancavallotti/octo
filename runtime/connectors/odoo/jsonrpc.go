// This file provides the Odoo 18 client: the legacy JSON-RPC endpoint (POST
// /jsonrpc), which Odoo 14 through 18 serve and Odoo 19 still accepts. A call is
// the object service's execute_kw(db, uid, key, model, method, args, kwargs),
// authenticated by a user id the common service's authenticate returns for the
// login and API key. Failures arrive as HTTP 200 with an error member.
package odoo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// positionalParams names, per method, the arguments execute_kw must receive
// positionally after the ids. Odoo's call dispatch reads them from args — the
// batch-create decorator indexes args[0] and fails on a vals_list passed by
// name — while JSON-2 takes every argument by name. Any argument not listed here
// is passed by name, which every other ORM method accepts.
var positionalParams = map[string][]string{
	"create": {"vals_list"},
}

// jsonrpcClient calls Odoo's legacy JSON-RPC endpoint.
type jsonrpcClient struct {
	http     *http.Client
	baseURL  string
	apiKey   string
	database string
	username string

	// mu guards uid, the user id authenticate returned, learned on the first call
	// and reused for every later one. A failed authentication is not cached, so a
	// key fixed on the server takes effect without a restart.
	mu  sync.Mutex
	uid int64
}

func (c *jsonrpcClient) call(ctx context.Context, req Request) (any, error) {
	uid, err := c.userID(ctx, req)
	if err != nil {
		return nil, err
	}
	args, kwargs := splitParams(req)
	return c.rpc(ctx, req, "object", "execute_kw",
		[]any{c.database, uid, c.apiKey, req.Model, req.Method, args, kwargs})
}

// splitParams lays a Request out as execute_kw's args and kwargs: the ids first
// when calling a recordset, then any argument the method needs positionally,
// then everything else by name, the context included.
func splitParams(req Request) ([]any, map[string]any) {
	args := []any{}
	if req.IDs != nil {
		args = append(args, req.IDs)
	}
	kwargs := make(map[string]any, len(req.Params)+1)
	for k, v := range req.Params {
		kwargs[k] = v
	}
	for _, name := range positionalParams[req.Method] {
		v, ok := kwargs[name]
		if !ok {
			break
		}
		args = append(args, v)
		delete(kwargs, name)
	}
	if req.Context != nil {
		kwargs["context"] = req.Context
	}
	return args, kwargs
}

// userID returns the cached user id, authenticating first when there is none.
func (c *jsonrpcClient) userID(ctx context.Context, req Request) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.uid != 0 {
		return c.uid, nil
	}
	result, err := c.rpc(ctx, req, "common", "authenticate",
		[]any{c.database, c.username, c.apiKey, map[string]any{}})
	if err != nil {
		return 0, err
	}
	// authenticate answers false, not an error, for a wrong login or key.
	uid, ok := result.(float64)
	if !ok || uid <= 0 {
		return 0, &Error{
			Model: req.Model, Method: req.Method,
			Name: "odoo.exceptions.AccessDenied", Message: "authentication failed for user " + c.username,
		}
	}
	c.uid = int64(uid)
	return c.uid, nil
}

// rpcEnvelope is a JSON-RPC response: a result, or an error whose data member
// carries the Odoo exception.
type rpcEnvelope struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string      `json:"message"`
		Data    serverError `json:"data"`
	} `json:"error"`
}

// rpc performs one JSON-RPC call on service.method. req only labels errors.
func (c *jsonrpcClient) rpc(ctx context.Context, req Request, service, method string, args []any) (any, error) {
	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "call",
		"params":  map[string]any{"service": service, "method": method, "args": args},
	})
	if err != nil {
		return nil, fmt.Errorf("odoo %s.%s: encode request: %w", req.Model, req.Method, err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/jsonrpc", bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("odoo %s.%s: build request: %w", req.Model, req.Method, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("odoo %s.%s: %w", req.Model, req.Method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("odoo %s.%s: read response: %w", req.Model, req.Method, err)
	}

	var env rpcEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, &Error{
			Model: req.Model, Method: req.Method,
			Message: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, snippet(payload)),
		}
	}
	if env.Error != nil {
		se := env.Error.Data
		if se.Message == "" {
			se.Message = env.Error.Message
		}
		return nil, &Error{Model: req.Model, Method: req.Method, Name: se.Name, Message: se.Message}
	}
	var result any
	if len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, &result); err != nil {
			return nil, fmt.Errorf("odoo %s.%s: decode response: %w", req.Model, req.Method, err)
		}
	}
	return result, nil
}
