// This file provides the "notion-verify-request" block: it authenticates an
// inbound Notion webhook delivered over the http connector. It verifies the HMAC
// signature over the exact request bytes using the notion connector's
// verification token, and aborts on a bad signature. It sources those bytes from
// either the http source's rawBodyVar variable or its native raw-content mode
// (rawBody: true, msg.RawBody()); in raw-content mode it then parses the verified
// bytes back into Body so downstream body.* access keeps working. When the payload
// is Notion's one-time subscription handshake (a bare {verification_token}) it
// sets a marker variable so the flow can branch and capture the token.
package notion

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("notion-verify-request", newVerify)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "notion-verify-request",
		Label:    "Notion Verify Request",
		Category: "processor",
		Description: "Verify an inbound Notion webhook signature and capture the one-time " +
			"subscription handshake token.",
		Config: reflect.TypeFor[verifySettings](),
	})
}

const (
	// defaultSignatureHeader is the variable the http source lands Notion's
	// signature header in when configured to copy it; it matches Notion's header
	// name.
	defaultSignatureHeader = "X-Notion-Signature"
	// defaultRawBodyVar matches the http source's default rawBodyVar.
	defaultRawBodyVar = "rawBody"
	// verificationVar is set when the request is Notion's subscription handshake,
	// so the flow can branch and capture body.verification_token.
	verificationVar = "notionVerification"
	// verificationField is the key Notion's handshake payload carries the token in.
	verificationField = "verification_token"
)

// verifySettings is the notion-verify-request block's typed configuration.
type verifySettings struct {
	// Name of the notion connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:notion"`
	// Variable holding Notion's request signature.
	SignatureHeader string `json:"signatureHeader" octo:"label=Signature variable,default=X-Notion-Signature"`
	// Variable holding the exact request body; must match the http source's
	// rawBodyVar. Optional when the http source uses raw-content mode (rawBody:
	// true) — the block then reads the raw body directly and re-parses it into body.
	RawBodyVar string `json:"rawBodyVar" octo:"label=Raw body variable,default=rawBody"`
}

// verifyProcessor authenticates an inbound Notion webhook and flags the
// subscription handshake.
type verifyProcessor struct {
	conn       *Connector
	sigVar     string
	rawBodyVar string
	// rawBodyVarExplicit records whether rawBodyVar was configured (vs defaulted),
	// so an explicit variable takes precedence over native raw-content mode.
	rawBodyVarExplicit bool
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newVerify(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg verifySettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("notion-verify-request: %w", err)
	}
	if conn.VerificationToken() == "" {
		return nil, errors.New("notion-verify-request requires the notion connector's verificationToken")
	}
	return &verifyProcessor{
		conn:               conn,
		sigVar:             orDefault(cfg.SignatureHeader, defaultSignatureHeader),
		rawBodyVar:         orDefault(cfg.RawBodyVar, defaultRawBodyVar),
		rawBodyVarExplicit: cfg.RawBodyVar != "",
	}, nil
}

// Process verifies the signature and, for the subscription handshake, sets the
// verification marker variable so the flow can branch and capture the token. It
// aborts when the signature is missing or invalid.
func (p *verifyProcessor) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	sig, _ := msg.Variables.String(p.sigVar)
	raw := p.resolveRawBody(msg)

	if !p.conn.VerifySignature(sig, raw) {
		return nil, errors.New("notion-verify-request: invalid request signature")
	}

	// In native raw-content mode Body is the {contentType, rawData} envelope, not
	// the Notion payload. Parse the verified bytes into Body so the handshake branch
	// below and downstream body.* access work the same as with rawBodyVar.
	// SetBodyJSON leaves RawContent set, so clear it to make this a normal message.
	if _, _, ok := msg.RawBody(); ok {
		if err := msg.SetBodyJSON(raw); err == nil {
			msg.RawContent = false
		}
	}

	if body, ok := msg.Body.(map[string]any); ok {
		if token, _ := body[verificationField].(string); token != "" {
			msg.Variables.Set(verificationVar, token)
		}
	}
	return msg, nil
}

// resolveRawBody returns the exact request bytes for signature verification,
// preferring in order: an explicitly-configured rawBodyVar variable, the message's
// native raw-content body (rawBody: true on the http source), then the default
// rawBody variable. This lets the block verify requests whether the http source
// captured the bytes into a variable or sourced them as raw content.
func (p *verifyProcessor) resolveRawBody(msg *types.Message) []byte {
	if p.rawBodyVarExplicit {
		if raw, ok := msg.Variables.String(p.rawBodyVar); ok && raw != "" {
			return []byte(raw)
		}
	}
	if _, rawData, ok := msg.RawBody(); ok {
		return []byte(rawData)
	}
	raw, _ := msg.Variables.String(p.rawBodyVar)
	return []byte(raw)
}
