// This file provides the "slack-verify-request" block: it authenticates an
// inbound Slack request delivered over the http connector. It verifies the HMAC
// signature over the exact request bytes using the slack connector's signing
// secret, and aborts on a bad or stale signature. It sources those bytes from
// either the http source's rawBodyVar variable or its native raw-content mode
// (rawBody: true, msg.RawBody()); in raw-content mode it then parses the verified
// bytes back into Body so downstream body.* access keeps working. When the
// payload is Slack's URL-verification handshake it sets a marker variable so the
// flow can branch, echo the challenge back, and skip event handling (see the
// sample).
package slack

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("slack-verify-request", newVerify)
}

const (
	// defaultSignatureHeader and defaultTimestampHeader are the variables the
	// http source lands Slack's signature headers in when configured to copy
	// them; they match Slack's header names.
	defaultSignatureHeader = "X-Slack-Signature"
	defaultTimestampHeader = "X-Slack-Request-Timestamp"
	// defaultRawBodyVar matches the http source's default rawBodyVar.
	defaultRawBodyVar = "rawBody"
	// challengeVar is set to true when the request is a URL-verification
	// handshake, so the flow can branch and echo body.challenge.
	challengeVar = "slackChallenge"
	// urlVerificationType is the "type" of Slack's URL-verification payload.
	urlVerificationType = "url_verification"
)

// verifySettings is the slack-verify-request block's typed configuration.
type verifySettings struct {
	// Connector names the slack connector whose signing secret verifies the
	// request (required).
	Connector string `json:"connector"`
	// SignatureHeader names the variable holding Slack's signature (default
	// "X-Slack-Signature").
	SignatureHeader string `json:"signatureHeader"`
	// TimestampHeader names the variable holding Slack's request timestamp
	// (default "X-Slack-Request-Timestamp").
	TimestampHeader string `json:"timestampHeader"`
	// RawBodyVar names the variable holding the exact request body (default
	// "rawBody"); it must match the http source's rawBodyVar.
	RawBodyVar string `json:"rawBodyVar"`
}

// verifyProcessor authenticates an inbound Slack request and prepares the
// URL-verification challenge response.
type verifyProcessor struct {
	conn       *Connector
	sigVar     string
	tsVar      string
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
		return nil, fmt.Errorf("slack-verify-request: %w", err)
	}
	if conn.SigningSecret() == "" {
		return nil, errors.New("slack-verify-request requires the slack connector's signingSecret")
	}
	return &verifyProcessor{
		conn:               conn,
		sigVar:             orDefault(cfg.SignatureHeader, defaultSignatureHeader),
		tsVar:              orDefault(cfg.TimestampHeader, defaultTimestampHeader),
		rawBodyVar:         orDefault(cfg.RawBodyVar, defaultRawBodyVar),
		rawBodyVarExplicit: cfg.RawBodyVar != "",
	}, nil
}

// Process verifies the signature and, for a URL-verification handshake, sets the
// challenge marker variable so the flow can branch and echo body.challenge. It
// aborts when the signature is missing, invalid, or stale.
func (p *verifyProcessor) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	sig, _ := msg.Variables.String(p.sigVar)
	ts, _ := msg.Variables.String(p.tsVar)
	raw := p.resolveRawBody(msg)

	if !p.conn.VerifySignature(sig, ts, raw, time.Now()) {
		return nil, errors.New("slack-verify-request: invalid request signature")
	}

	// In native raw-content mode Body is the {contentType, rawData} envelope, not
	// the Slack payload. Parse the verified bytes into Body (best-effort: Slack
	// slash commands are form-encoded, not JSON) so the challenge branch below and
	// downstream body.* access work the same as with rawBodyVar. SetBodyJSON leaves
	// RawContent set, so clear it to make this a normal JSON message.
	if _, _, ok := msg.RawBody(); ok {
		if err := msg.SetBodyJSON(raw); err == nil {
			msg.RawContent = false
		}
	}

	if body, ok := msg.Body.(map[string]any); ok {
		if t, _ := body["type"].(string); t == urlVerificationType {
			msg.Variables.Set(challengeVar, true)
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
