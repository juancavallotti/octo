// This file provides the Odoo 19 client: the JSON-2 API, where every call is
// POST /json/2/<model>/<method> with the method's arguments as named keys of a
// JSON body, the API key as a bearer token, and failures as an HTTP error status.
package odoo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// json2Client calls Odoo's JSON-2 API.
type json2Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
	// database is sent as X-Odoo-Database when set; a server hosting a single
	// database (or filtering by host name) does not need it.
	database string
}

func (c *json2Client) call(ctx context.Context, req Request) (any, error) {
	raw, err := json.Marshal(json2Body(req))
	if err != nil {
		return nil, fmt.Errorf("odoo %s.%s: encode request: %w", req.Model, req.Method, err)
	}

	endpoint := c.baseURL + "/json/2/" + url.PathEscape(req.Model) + "/" + url.PathEscape(req.Method)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("odoo %s.%s: build request: %w", req.Model, req.Method, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "bearer "+c.apiKey)
	if c.database != "" {
		httpReq.Header.Set("X-Odoo-Database", c.database)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("odoo %s.%s: %w", req.Model, req.Method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return decodeJSON2(req, resp)
}

// json2Body lays a Request out as a JSON-2 body: the method's arguments by name,
// beside ids for a recordset call and the context when one is set.
func json2Body(req Request) map[string]any {
	body := make(map[string]any, len(req.Params)+2)
	for k, v := range req.Params {
		body[k] = v
	}
	if req.IDs != nil {
		body["ids"] = req.IDs
	}
	if req.Context != nil {
		body["context"] = req.Context
	}
	return body
}

// decodeJSON2 reads a JSON-2 response: the method's return value on success, an
// *Error built from the exception body on an error status.
func decodeJSON2(req Request, resp *http.Response) (any, error) {
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("odoo %s.%s: read response: %w", req.Model, req.Method, err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		var se serverError
		if json.Unmarshal(payload, &se) != nil || (se.Name == "" && se.Message == "") {
			se.Message = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, snippet(payload))
		}
		return nil, &Error{Model: req.Model, Method: req.Method, Name: se.Name, Message: se.Message}
	}
	var result any
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("odoo %s.%s: decode response: %w", req.Model, req.Method, err)
	}
	return result, nil
}
