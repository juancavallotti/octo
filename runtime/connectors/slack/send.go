// This file provides the "slack-send-message" block: it posts a message to a
// Slack channel or user through a slack connector (chat.postMessage). The target
// is a channel ID or a user ID — Slack opens a DM when given a user. Target,
// text, an optional thread timestamp, and optional rich blocks are CEL
// expressions evaluated per message. On success the posted message's channel and
// timestamp are folded into variables so a later block can react to or update it.
package slack

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("slack-send-message", newSendMessage)
}

const (
	// sendChannelVar and sendTSVar name the variables the posted message's
	// channel and timestamp are stored in, so a later reaction/update can target
	// the same message.
	sendChannelVar = "slackChannel"
	sendTSVar      = "slackTs"
)

// sendSettings is the slack-send-message block's typed configuration.
type sendSettings struct {
	// Connector names the slack connector to send through (required).
	Connector string `json:"connector"`
	// Target is a CEL expression producing a channel ID or user ID (required).
	Target string `json:"target"`
	// Text is a CEL expression producing the message text. Required unless Blocks
	// is set.
	Text string `json:"text"`
	// ThreadTS is an optional CEL expression producing a parent message ts to
	// reply in-thread.
	ThreadTS string `json:"threadTs"`
	// Blocks is an optional CEL expression producing Slack Block Kit blocks (a
	// list); when set it is sent alongside text.
	Blocks string `json:"blocks"`
	// FailOnError, when true (the default), turns a Slack API error into a flow
	// error. It is a pointer so an explicit false is distinguishable from unset.
	FailOnError *bool `json:"failOnError"`
}

// sendProcessor posts a message and folds the result into variables.
type sendProcessor struct {
	conn        *Connector
	target      *expr.Program
	text        *expr.Program
	threadTS    *expr.Program
	blocks      *expr.Program
	failOnError bool
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newSendMessage(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg sendSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("slack-send-message: %w", err)
	}
	if strings.TrimSpace(cfg.Target) == "" {
		return nil, errors.New("slack-send-message requires a \"target\" expression")
	}
	if strings.TrimSpace(cfg.Text) == "" && strings.TrimSpace(cfg.Blocks) == "" {
		return nil, errors.New("slack-send-message requires \"text\" or \"blocks\"")
	}

	target, err := expr.CompileMessage(deps.Resources, cfg.Target)
	if err != nil {
		return nil, fmt.Errorf("slack-send-message: compile target: %w", err)
	}
	text, err := compileOptional(deps.Resources, cfg.Text)
	if err != nil {
		return nil, fmt.Errorf("slack-send-message: compile text: %w", err)
	}
	threadTS, err := compileOptional(deps.Resources, cfg.ThreadTS)
	if err != nil {
		return nil, fmt.Errorf("slack-send-message: compile threadTs: %w", err)
	}
	blocks, err := compileOptional(deps.Resources, cfg.Blocks)
	if err != nil {
		return nil, fmt.Errorf("slack-send-message: compile blocks: %w", err)
	}

	failOnError := true
	if cfg.FailOnError != nil {
		failOnError = *cfg.FailOnError
	}

	return &sendProcessor{
		conn:        conn,
		target:      target,
		text:        text,
		threadTS:    threadTS,
		blocks:      blocks,
		failOnError: failOnError,
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// Process builds the chat.postMessage payload, posts it, and folds the posted
// channel and timestamp into variables. On a Slack error it aborts when
// failOnError is set, otherwise it passes the message through unchanged.
func (p *sendProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	payload, err := p.buildPayload(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, err
	}

	resp, err := p.conn.Call(ctx, "chat.postMessage", payload)
	if err != nil {
		if p.failOnError {
			return nil, err
		}
		return msg, nil
	}

	if ch, ok := resp["channel"].(string); ok {
		msg.Variables.Set(sendChannelVar, ch)
	}
	if ts, ok := resp["ts"].(string); ok {
		msg.Variables.Set(sendTSVar, ts)
	}
	return msg, nil
}

// buildPayload evaluates the block's expressions into a chat.postMessage payload.
func (p *sendProcessor) buildPayload(activation map[string]any) (map[string]any, error) {
	channel, err := p.target.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("slack-send-message target: %w", err)
	}
	if channel = strings.TrimSpace(channel); channel == "" {
		return nil, errors.New("slack-send-message: target evaluated to empty")
	}
	payload := map[string]any{fieldChannel: channel}

	if p.text != nil {
		text, err := p.text.EvalString(activation)
		if err != nil {
			return nil, fmt.Errorf("slack-send-message text: %w", err)
		}
		payload["text"] = text
	}
	if p.threadTS != nil {
		ts, err := p.threadTS.EvalString(activation)
		if err != nil {
			return nil, fmt.Errorf("slack-send-message threadTs: %w", err)
		}
		if ts = strings.TrimSpace(ts); ts != "" {
			payload["thread_ts"] = ts
		}
	}
	if p.blocks != nil {
		value, err := p.blocks.Eval(activation)
		if err != nil {
			return nil, fmt.Errorf("slack-send-message blocks: %w", err)
		}
		if value != nil {
			payload["blocks"] = value
		}
	}
	return payload, nil
}
