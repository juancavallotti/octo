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
	"reflect"
	"strings"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("slack-send-message", newSendMessage)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "slack-send-message",
		Label:       "Slack Send Message",
		Category:    core.CategoryProcessor,
		Description: "Post a message to a Slack channel or user through a slack connector.",
		Config:      reflect.TypeFor[sendSettings](),
	})
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
	// Name of the slack connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:slack"`
	// CEL expression for the channel ID or user ID (Slack DMs a user).
	Target string `json:"target" octo:"label=Target,type=cel,required"`
	// CEL expression for the message text (required unless blocks is set).
	Text string `json:"text" octo:"label=Text,type=cel"`
	// CEL expression for a parent message ts, to reply in-thread.
	ThreadTS string `json:"threadTs" octo:"label=Thread timestamp,type=cel"`
	// CEL expression producing Block Kit blocks (a list).
	Blocks string `json:"blocks" octo:"label=Blocks,type=cel"`
	// Turn a Slack API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
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
