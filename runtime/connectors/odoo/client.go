// This file holds what the version-specific clients share: the request a block
// builds, the interface each client implements, and the error both report.
package odoo

import (
	"context"
	"fmt"
	"strings"
)

// Request is one call on an Odoo model. IDs nil calls the method on the model
// itself (search_read, create); IDs non-nil — even empty — calls it on that
// recordset (read, write, unlink, action_confirm). Params are the method's
// arguments by name, and Context is Odoo's call context (lang, tz,
// allowed_company_ids, ...).
type Request struct {
	Model   string
	Method  string
	IDs     []int64
	Params  map[string]any
	Context map[string]any
}

// caller is one protocol's way of performing a Request.
type caller interface {
	call(ctx context.Context, req Request) (any, error)
}

// Error is a failure the Odoo server reported. Name is the Python exception
// (odoo.exceptions.ValidationError, odoo.exceptions.AccessDenied, ...) and
// Message its text; the server's traceback is dropped, since it describes the
// server and not the call.
type Error struct {
	Model   string
	Method  string
	Name    string
	Message string
}

func (e *Error) Error() string {
	target := e.Model + "." + e.Method
	switch {
	case e.Name != "" && e.Message != "":
		return fmt.Sprintf("odoo %s: %s: %s", target, e.Name, e.Message)
	case e.Message != "":
		return fmt.Sprintf("odoo %s: %s", target, e.Message)
	default:
		return fmt.Sprintf("odoo %s: %s", target, e.Name)
	}
}

// serverError is the error object both protocols carry: JSON-2 as the whole
// response body, JSON-RPC under error.data.
type serverError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

// snippet trims a non-JSON response body (a proxy's HTML error page) to
// something that fits in an error message.
func snippet(body []byte) string {
	const maxLen = 200
	s := strings.TrimSpace(string(body))
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
