// This file provides credential forwarding: the seam that lets a rest or
// rest-dynamic block put a credential it obtained at runtime -- a token relayed
// from the inbound request, minted by an earlier block, or read out of the
// message -- on the outbound request.
//
// It exists because the connector's auth settings can only describe a credential
// that is known when the service starts. A flow acting on behalf of its caller
// has no such credential: the only one that will do arrives with the message. The
// connector's own auth stays the place a *configured* credential belongs, so
// forwarding is available only when that is switched off -- otherwise the two
// would silently contend for one Authorization header and the message would win,
// which is the failure the rest-dynamic header refusal was written to prevent.
package httpclient

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
)

// authConfigured reports whether the connector carries a credential of its own.
func (c *Connector) authConfigured() bool { return c.auth.Type != authNone }

// compileCredential compiles a block's auth expression, refusing it when the
// connector already authenticates. block names the caller for the error.
//
// The refusal is at build time rather than at request time because it is a
// configuration mistake, not a runtime condition: the two credentials are both
// written down, and which of them reaches the upstream should not be something an
// operator discovers from the first message.
func compileCredential(source string, conn *Connector, block string, res core.ResourceLoader) (*expr.Program, error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil
	}
	if conn.authConfigured() {
		return nil, fmt.Errorf(
			"%s block: auth forwards a credential per message, but the connector authenticates with %q; "+
				"a connector credential is not something a message may replace, so forward one only "+
				"through a connector whose auth is disabled", block, conn.auth.Type)
	}
	program, err := expr.CompileMessage(res, source)
	if err != nil {
		return nil, fmt.Errorf("%s block: compile auth: %w", block, err)
	}
	return program, nil
}

// applyCredential renders the credential and sets it as the request's
// Authorization header. A nil program, a null result, or an empty one sets
// nothing -- which is what makes the credential optional on a flow where only
// some messages carry one, rather than every uncredentialed message failing here
// instead of at the upstream that gets to decide.
//
// The rendered value is the whole header value, scheme included, so forwarding an
// inbound header is the identity case and nothing has to guess at a scheme.
func applyCredential(program *expr.Program, req *http.Request, activation map[string]any, block string) error {
	if program == nil {
		return nil
	}
	value, err := program.Eval(activation)
	if err != nil {
		return fmt.Errorf("%s auth: %w", block, err)
	}
	if value == nil {
		return nil
	}
	rendered, err := renderValue(value)
	if err != nil {
		return fmt.Errorf("%s auth: %w", block, err)
	}
	rendered = strings.TrimSpace(rendered)
	if rendered == "" {
		return nil
	}
	// The value is data by construction. A CR or LF in it would end the header
	// line and let whatever produced the message append headers of its own to the
	// upstream request.
	if strings.ContainsAny(rendered, "\r\n") {
		return fmt.Errorf("%s auth: the credential contains a carriage return or line feed", block)
	}
	req.Header.Set("Authorization", rendered)
	return nil
}

// refuseAuthorizationHeader rejects an Authorization header written through a
// block's headers setting, naming the auth setting that does the job properly.
//
// Both blocks refuse it, for the same reason: the connector applies its own
// credential only when the request does not already carry one, so an Authorization
// header here does not sit alongside the connector's auth -- it replaces it, with
// no indication that it did. auth is the one seam where that replacement is
// declared, and where it is checked against the connector actually having none.
func refuseAuthorizationHeader(name, block string) error {
	if http.CanonicalHeaderKey(name) != "Authorization" {
		return nil
	}
	return fmt.Errorf("%s headers: %q is not set through headers; "+
		"configure a fixed credential under the http-client connector's auth, "+
		"or forward one per message with this block's auth setting", block, name)
}
