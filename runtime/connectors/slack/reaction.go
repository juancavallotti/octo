// This file provides the "slack-add-reaction" block: it adds an emoji reaction
// to a message (reactions.add), typically to acknowledge an event. Channel,
// message timestamp, and emoji name are CEL expressions evaluated per message.
package slack

import (
	"context"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("slack-add-reaction", newAddReaction)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "slack-add-reaction",
		Label:       "Slack Add Reaction",
		Category:    "processor",
		Description: "Add an emoji reaction to a message (reactions.add).",
		Config:      reflect.TypeFor[reactionSettings](),
	})
}

// reactionSettings is the slack-add-reaction block's typed configuration.
type reactionSettings struct {
	// Name of the slack connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:slack"`
	// CEL expression for the message channel ID.
	Channel string `json:"channel" octo:"label=Channel,type=cel,required"`
	// CEL expression for the target message ts.
	Timestamp string `json:"timestamp" octo:"label=Timestamp,type=cel,required"`
	// CEL expression for the reaction name without colons (e.g. white_check_mark).
	Emoji string `json:"emoji" octo:"label=Emoji,type=cel,required"`
	// Turn a Slack API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

// reactionProcessor adds an emoji reaction to a message.
type reactionProcessor struct {
	conn        *Connector
	channel     *expr.Program
	timestamp   *expr.Program
	emoji       *expr.Program
	failOnError bool
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newAddReaction(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg reactionSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("slack-add-reaction: %w", err)
	}
	channel, err := compileRequired(deps.Resources, "slack-add-reaction", "channel", cfg.Channel)
	if err != nil {
		return nil, err
	}
	timestamp, err := compileRequired(deps.Resources, "slack-add-reaction", "timestamp", cfg.Timestamp)
	if err != nil {
		return nil, err
	}
	emoji, err := compileRequired(deps.Resources, "slack-add-reaction", "emoji", cfg.Emoji)
	if err != nil {
		return nil, err
	}
	return &reactionProcessor{
		conn:        conn,
		channel:     channel,
		timestamp:   timestamp,
		emoji:       emoji,
		failOnError: failOnErrorDefault(cfg.FailOnError),
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// Process evaluates the target and reaction and calls reactions.add.
func (p *reactionProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)

	channel, err := p.channel.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("slack-add-reaction channel: %w", err)
	}
	timestamp, err := p.timestamp.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("slack-add-reaction timestamp: %w", err)
	}
	emoji, err := p.emoji.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("slack-add-reaction emoji: %w", err)
	}

	payload := map[string]any{fieldChannel: channel, "timestamp": timestamp, "name": emoji}
	if _, err := p.conn.Call(ctx, "reactions.add", payload); err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	return msg, nil
}
