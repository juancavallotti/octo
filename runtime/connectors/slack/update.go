// This file provides the "slack-update-message" block: it edits a message the
// bot previously posted (chat.update), addressed by channel and timestamp. Text
// and optional rich blocks are CEL expressions evaluated per message.
package slack

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("slack-update-message", newUpdateMessage)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "slack-update-message",
		Label:       "Slack Update Message",
		Category:    core.CategoryProcessor,
		Description: "Edit a message the bot posted earlier (chat.update).",
		Config:      reflect.TypeFor[updateSettings](),
	})
}

// updateSettings is the slack-update-message block's typed configuration.
type updateSettings struct {
	// Name of the slack connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:slack"`
	// CEL expression for the message channel ID.
	Channel string `json:"channel" octo:"label=Channel,type=cel,required"`
	// CEL expression for the target message ts.
	Timestamp string `json:"timestamp" octo:"label=Timestamp,type=cel,required"`
	// CEL expression for the new text (required unless blocks is set).
	Text string `json:"text" octo:"label=Text,type=cel"`
	// CEL expression producing replacement Block Kit blocks (a list).
	Blocks string `json:"blocks" octo:"label=Blocks,type=cel"`
	// Turn a Slack API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
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
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// Process builds the chat.update payload and edits the message.
func (p *updateProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	payload, err := p.buildPayload(expr.MessageActivation(msg, p.env))
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
