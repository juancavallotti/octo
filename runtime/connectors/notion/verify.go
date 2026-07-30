// This file provides the "notion-verify-request" block: it authenticates an
// inbound Notion webhook delivered over the http connector. It verifies the HMAC
// signature over the exact request bytes using the notion connector's
// verification token, and aborts on a bad signature. It sources those bytes from
// either the http source's rawBodyVar variable or its native raw-content mode
// (rawBody: true, msg.RawBody()); in raw-content mode it then parses the verified
// bytes back into Body so downstream body.* access keeps working. When the payload
// is Notion's one-time subscription handshake (a bare {verification_token}) it
// sets a marker variable so the flow can branch and log the token — and, while no
// token is known yet, accepts that one request unsigned and captures the token, so
// a fresh subscription can be bootstrapped without one already in hand.
package notion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerVerify() {
	core.MustRegisterBlock("notion-verify-request", newVerify)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "notion-verify-request",
		Label:    "Notion Verify Request",
		Category: core.CategoryProcessor,
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
	// No verificationToken is not a build error: a brand-new subscription has no
	// token until Notion's handshake delivers one, and this block is what receives
	// it. Refusing to build would make that handshake unreachable.
	return &verifyProcessor{
		conn:               conn,
		sigVar:             orDefault(cfg.SignatureHeader, defaultSignatureHeader),
		rawBodyVar:         orDefault(cfg.RawBodyVar, defaultRawBodyVar),
		rawBodyVarExplicit: cfg.RawBodyVar != "",
	}, nil
}

// Process verifies the signature and, for the subscription handshake, sets the
// verification marker variable so the flow can branch and capture the token. It
// aborts when the signature is missing or invalid — except for the one request that
// cannot be checked, the handshake that bootstraps a subscription (see
// bootstrapToken).
func (p *verifyProcessor) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	sig, _ := msg.Variables.String(p.sigVar)
	raw := p.resolveRawBody(msg)

	if !p.bootstrapToken(raw) && !p.conn.VerifySignature(sig, raw) {
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

// bootstrapToken captures the token from Notion's one-time subscription handshake
// and reports whether it did, in which case the caller skips signature
// verification. That is the chicken-and-egg this solves: the handshake is the only
// way to learn the token, so it cannot itself be checked against one.
//
// The exemption is deliberately narrow. It applies only while no token is known —
// neither configured nor already captured — so the window is exactly the bootstrap
// window and closes for good the moment a token exists. Every later request,
// handshake or event, must carry a valid signature. It also applies only to a body
// that is nothing but the handshake, so an event payload cannot smuggle itself
// through by carrying a verification_token field.
func (p *verifyProcessor) bootstrapToken(raw []byte) bool {
	if p.conn.VerificationToken() != "" {
		return false
	}
	token, ok := handshakeToken(raw)
	if !ok {
		return false
	}

	p.conn.CaptureVerificationToken(token)
	slog.Info("notion-verify-request: captured the webhook verification token from Notion's handshake; "+
		"paste it into Notion to confirm the subscription, and set it as the connector's verificationToken "+
		"(e.g. NOTION_VERIFICATION_TOKEN) so it survives a restart",
		"verificationToken", token)
	return true
}

// handshakeToken returns the token of Notion's subscription handshake payload — a
// JSON object carrying a verification_token and nothing else. Any other shape,
// including an event that merely has the field, is not a handshake.
func handshakeToken(raw []byte) (string, bool) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil || len(body) != 1 {
		return "", false
	}
	token, _ := body[verificationField].(string)
	return token, token != ""
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
