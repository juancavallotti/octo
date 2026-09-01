// This file provides the "parallel-verify-request" block: it authenticates an
// inbound Parallel webhook delivered over the http connector. Parallel follows
// the Standard Webhooks spec, so the signature covers the id, the timestamp and
// the exact request bytes together, and the block aborts on anything that does
// not verify.
//
// It sources those bytes from either the http source's rawBodyVar variable or its
// native raw-content mode (rawBody: true, msg.RawBody()); in raw-content mode it
// then parses the verified bytes back into Body so downstream body.* access keeps
// working.
//
// There is no handshake to bootstrap, unlike notion: Parallel issues the secret
// in its dashboard, so a request that cannot be verified is simply not one Octo
// has any reason to trust.
package parallel

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerVerify() {
	core.MustRegisterBlock("parallel-verify-request", newVerify)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "parallel-verify-request",
		Label:    "Parallel Verify Request",
		Category: core.CategoryProcessor,
		Description: "Verify an inbound Parallel webhook signature (Standard Webhooks) and abort " +
			"the flow on a bad or stale one.",
		Config: reflect.TypeFor[verifySettings](),
	})
}

const (
	// defaultIDHeader, defaultTimestampHeader and defaultSignatureHeader are the
	// variables the http source lands Parallel's webhook headers in when
	// configured to copy them; they match the Standard Webhooks header names.
	defaultIDHeader        = "webhook-id"
	defaultTimestampHeader = "webhook-timestamp"
	defaultSignatureHeader = "webhook-signature"
	// defaultRawBodyVar matches the http source's default rawBodyVar.
	defaultRawBodyVar = "rawBody"
)

// verifySettings is the parallel-verify-request block's typed configuration.
type verifySettings struct {
	// Name of the parallel connector to use; its webhookSecret is required.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:parallel"`
	// Variable holding the webhook's unique id.
	IDHeader string `json:"idHeader" octo:"label=ID variable,default=webhook-id"`
	// Variable holding the webhook's unix timestamp.
	TimestampHeader string `json:"timestampHeader" octo:"label=Timestamp variable,default=webhook-timestamp"`
	// Variable holding the webhook's signature.
	SignatureHeader string `json:"signatureHeader" octo:"label=Signature variable,default=webhook-signature"`
	// Variable holding the exact request body; must match the http source's
	// rawBodyVar. Optional when the http source uses raw-content mode (rawBody:
	// true) — the block then reads the raw body directly and re-parses it into body.
	RawBodyVar string `json:"rawBodyVar" octo:"label=Raw body variable,default=rawBody"`
}

// verifyProcessor authenticates an inbound Parallel webhook.
type verifyProcessor struct {
	conn       *Connector
	idVar      string
	tsVar      string
	sigVar     string
	rawBodyVar string
	// rawBodyVarExplicit records whether rawBodyVar was configured (vs defaulted),
	// so an explicit variable takes precedence over native raw-content mode.
	rawBodyVarExplicit bool
}

// newVerify builds the processor. A missing webhook secret is a build error, as
// in slack: without it no request can ever be authenticated, and a flow that
// cannot authenticate its callbacks should not start.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newVerify(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg verifySettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("parallel-verify-request: %w", err)
	}
	if !conn.HasWebhookSecret() {
		return nil, errors.New("parallel-verify-request requires the parallel connector's webhookSecret")
	}
	return &verifyProcessor{
		conn:               conn,
		idVar:              orDefault(cfg.IDHeader, defaultIDHeader),
		tsVar:              orDefault(cfg.TimestampHeader, defaultTimestampHeader),
		sigVar:             orDefault(cfg.SignatureHeader, defaultSignatureHeader),
		rawBodyVar:         orDefault(cfg.RawBodyVar, defaultRawBodyVar),
		rawBodyVarExplicit: cfg.RawBodyVar != "",
	}, nil
}

// Process verifies the signature over the id, timestamp and exact request bytes,
// and aborts when it is missing, invalid, or stale.
func (p *verifyProcessor) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	id, _ := msg.Variables.String(p.idVar)
	ts, _ := msg.Variables.String(p.tsVar)
	sig, _ := msg.Variables.String(p.sigVar)
	raw := p.resolveRawBody(msg)

	if !p.conn.VerifySignature(id, ts, sig, raw, time.Now()) {
		return nil, errors.New("parallel-verify-request: invalid request signature")
	}

	// In native raw-content mode Body is the {contentType, rawData} envelope, not
	// the Parallel payload. Parse the verified bytes into Body so downstream
	// body.* access works the same as with rawBodyVar. SetBodyJSON leaves
	// raw-content mode on the way, which is what makes this a normal JSON message.
	//
	// The error is returned rather than swallowed, which is where this parts ways
	// with slack: Slack posts form-encoded slash commands, so a parse failure
	// there is expected and best-effort is right. Every Parallel webhook is JSON,
	// so bytes that carry a valid signature and still will not parse mean
	// something is wrong, and a flow reading body.data would otherwise walk into
	// the raw envelope instead.
	if _, _, ok := msg.RawBody(); ok {
		if err := msg.SetBodyJSON(raw); err != nil {
			return nil, fmt.Errorf("parallel-verify-request: parse verified body: %w", err)
		}
	}
	return msg, nil
}

// resolveRawBody returns the exact request bytes for signature verification,
// preferring in order: an explicitly-configured rawBodyVar variable, the
// message's native raw-content body (rawBody: true on the http source), then the
// default rawBody variable. This lets the block verify requests whether the http
// source captured the bytes into a variable or sourced them as raw content.
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
