// This file provides the "slack-update-message" block: it edits a message the
// bot previously posted (chat.update), addressed by channel and timestamp. Text
// and optional rich blocks are CEL expressions evaluated per message.
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
	core.MustRegisterBlock("slack-update-message", newUpdateMessage)
}

// updateSettings is the slack-update-message block's typed configuration.
type updateSettings struct {
	// Connector names the slack connector to update through (required).
	Connector string `json:"connector"`
	// Channel is a CEL expression producing the message's channel ID (required).
	Channel string `json:"channel"`
	// Timestamp is a CEL expression producing the target message's ts (required).
	Timestamp string `json:"timestamp"`
	// Text is a CEL expression producing the new message text. Required unless
	// Blocks is set.
	Text string `json:"text"`
	// Blocks is an optional CEL expression producing replacement Block Kit blocks.
	Blocks string `json:"blocks"`
	// FailOnError, when true (the default), turns a Slack API error into a flow
	// error.
	FailOnError *bool `json:"failOnError"`
}

// updateProcessor edits a previously posted message.
type updateProcessor struct {
	conn        *Connector
	channel     *expr.Program
	timestamp   *expr.Program
	text        *expr.Program
	blocks      *expr.Program
	failOnError bool
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newUpdateMessage(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg updateSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("slack-update-message: %w", err)
	}
	channel, err := compileRequired(deps.Resources, "slack-update-message", "channel", cfg.Channel)
	if err != nil {
		return nil, err
	}
	timestamp, err := compileRequired(deps.Resources, "slack-update-message", "timestamp", cfg.Timestamp)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Text) == "" && strings.TrimSpace(cfg.Blocks) == "" {
		return nil, errors.New("slack-update-message requires \"text\" or \"blocks\"")
	}
	text, err := compileOptional(deps.Resources, cfg.Text)
	if err != nil {
		return nil, fmt.Errorf("slack-update-message: compile text: %w", err)
	}
	blocks, err := compileOptional(deps.Resources, cfg.Blocks)
	if err != nil {
		return nil, fmt.Errorf("slack-update-message: compile blocks: %w", err)
	}
	return &updateProcessor{
		conn:        conn,
		channel:     channel,
		timestamp:   timestamp,
		text:        text,
		blocks:      blocks,
		failOnError: failOnErrorDefault(cfg.FailOnError),
		env:         envActivation(deps.Env),
	}, nil
}

// Process builds the chat.update payload and edits the message.
func (p *updateProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	payload, err := p.buildPayload(messageActivation(msg, p.env))
	if err != nil {
		return nil, err
	}
	if _, err := p.conn.Call(ctx, "chat.update", payload); err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	return msg, nil
}

// buildPayload evaluates the block's expressions into a chat.update payload.
func (p *updateProcessor) buildPayload(activation map[string]any) (map[string]any, error) {
	channel, err := p.channel.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("slack-update-message channel: %w", err)
	}
	ts, err := p.timestamp.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("slack-update-message timestamp: %w", err)
	}
	payload := map[string]any{fieldChannel: channel, "ts": ts}

	if p.text != nil {
		text, err := p.text.EvalString(activation)
		if err != nil {
			return nil, fmt.Errorf("slack-update-message text: %w", err)
		}
		payload["text"] = text
	}
	if p.blocks != nil {
		value, err := p.blocks.Eval(activation)
		if err != nil {
			return nil, fmt.Errorf("slack-update-message blocks: %w", err)
		}
		if value != nil {
			payload["blocks"] = value
		}
	}
	return payload, nil
}
