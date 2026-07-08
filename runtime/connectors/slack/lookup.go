// This file provides the "slack-lookup-user" block: it resolves a Slack user and
// folds the returned user object into a variable, so a flow can enrich a message
// with the user's id and profile. It looks up by email (users.lookupByEmail, the
// default) or by Slack user id (users.info), selected with the "by" setting.
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
	core.MustRegisterBlock("slack-lookup-user", newLookupUser)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "slack-lookup-user",
		Label:    "Slack Lookup User",
		Category: "processor",
		Description: "Resolve a Slack user into a variable — by email (users.lookupByEmail, the " +
			"default) or by Slack user id (users.info), selected with `by`.",
		Config: reflect.TypeFor[lookupSettings](),
	})
}

// defaultUserVar names the variable the looked-up user object is stored in.
const defaultUserVar = "slackUser"

// Lookup modes for the "by" setting.
const (
	lookupByEmail = "email"
	lookupByID    = "id"
)

// lookupSettings is the slack-lookup-user block's typed configuration.
type lookupSettings struct {
	// Name of the slack connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:slack"`
	// How to resolve the user: 'email' (users.lookupByEmail, the default) or 'id'
	// (users.info).
	By string `json:"by" octo:"label=Look up by,type=enum,enum=email|id,default=email"`
	// CEL expression for the email address to resolve. Required when 'by' is 'email'.
	Email string `json:"email" octo:"label=Email,type=cel"`
	// CEL expression for the Slack user id to resolve (e.g. U123). Required when
	// 'by' is 'id'.
	User string `json:"user" octo:"label=User ID,type=cel"`
	// Variable to store the user object in.
	ResultVar string `json:"resultVar" octo:"label=Result variable,default=slackUser"`
	// Turn a Slack API error into a flow error.
	FailOnError *bool `json:"failOnError" octo:"label=Fail on error,default=true"`
}

// lookupProcessor resolves a user (by email or id) and stores the result. method
// is the Slack Web API method to call and param the request field the resolved
// expression fills, so Process is method-agnostic.
type lookupProcessor struct {
	conn        *Connector
	method      string
	param       string
	arg         *expr.Program
	resultVar   string
	failOnError bool
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newLookupUser(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg lookupSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector(cfg.Connector, deps)
	if err != nil {
		return nil, fmt.Errorf("slack-lookup-user: %w", err)
	}

	block := &lookupProcessor{
		conn:        conn,
		resultVar:   orDefault(cfg.ResultVar, defaultUserVar),
		failOnError: failOnErrorDefault(cfg.FailOnError),
		env:         expr.EnvActivation(deps.Env),
	}

	switch orDefault(cfg.By, lookupByEmail) {
	case lookupByEmail:
		arg, argErr := compileRequired(deps.Resources, "slack-lookup-user", "email", cfg.Email)
		if argErr != nil {
			return nil, argErr
		}
		block.method, block.param, block.arg = "users.lookupByEmail", "email", arg
	case lookupByID:
		arg, argErr := compileRequired(deps.Resources, "slack-lookup-user", "user", cfg.User)
		if argErr != nil {
			return nil, argErr
		}
		block.method, block.param, block.arg = "users.info", "user", arg
	default:
		return nil, fmt.Errorf("slack-lookup-user: unknown by %q (want %q or %q)", cfg.By, lookupByEmail, lookupByID)
	}
	return block, nil
}

// Process resolves the argument (email or user id) and folds the returned user
// object into the result variable.
func (p *lookupProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	arg, err := p.arg.EvalString(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("slack-lookup-user %s: %w", p.param, err)
	}
	resp, err := p.conn.Call(ctx, p.method, map[string]any{p.param: arg})
	if err != nil {
		return onCallError(msg, err, p.failOnError)
	}
	msg.Variables.Set(p.resultVar, resp["user"])
	return msg, nil
}
