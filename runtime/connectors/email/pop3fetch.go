// This file provides the "pop3-fetch" block: it reads messages from a POP3
// drop when the flow reaches it and replaces the body with them. With onFetch
// delete the messages are removed in the same session, right after they are
// read; a flow that must not lose mail when a later step fails keeps them.
package email

import (
	"context"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

const pop3FetchType = "pop3-fetch"

func registerPOP3Fetch() {
	core.MustRegisterBlock(pop3FetchType, newPOP3Fetch)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        pop3FetchType,
		Label:       "Fetch POP3 Mail",
		Category:    core.CategoryProcessor,
		Icon:        iconInbox,
		Description: "Read messages from a POP3 mail drop into the body, as a list.",
		Config:      reflect.TypeFor[pop3FetchSettings](),
	})
}

// pop3FetchSettings is the pop3-fetch block's configuration.
type pop3FetchSettings struct {
	// Name of the pop3 connector to read through.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:pop3"`
	// Most messages to read; the newest are kept when more are waiting.
	Limit int `json:"limit" octo:"label=Limit,default=10"`
	// What to do with the messages once read: keep them on the server, or delete them.
	OnFetch string `json:"onFetch" octo:"label=On fetch,type=enum,enum=keep|delete,default=keep"`
}

// pop3FetchProcessor reads the drop per flow message.
type pop3FetchProcessor struct {
	conn    *POP3Connector
	limit   int
	onFetch string
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newPOP3Fetch(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg pop3FetchSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector[*POP3Connector](pop3FetchType, cfg.Connector, deps)
	if err != nil {
		return nil, err
	}
	onFetch, err := validatePOP3Action(pop3FetchType, "onFetch", cfg.OnFetch)
	if err != nil {
		return nil, err
	}
	return &pop3FetchProcessor{
		conn:    conn,
		limit:   positiveOr(cfg.Limit, defaultFetchLimit),
		onFetch: onFetch,
	}, nil
}

// Process reads the drop, deletes what was read when asked, and sets the body
// to the messages in arrival order.
func (p *pop3FetchProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	// Every message is parsed before anything is deleted, inside the session: a
	// message that cannot be parsed fails the fetch and the session ends without
	// QUIT, so the server deletes nothing.
	var out []any
	err := p.conn.session(ctx, func(client *pop3Client) error {
		_, read, err := readDrop(client, pop3Query{limit: p.limit, newest: true})
		if err != nil {
			return err
		}
		out = make([]any, 0, len(read))
		uidls := make([]string, 0, len(read))
		for _, m := range read {
			body, err := pop3Body(m)
			if err != nil {
				return err
			}
			out = append(out, body)
			uidls = append(uidls, m.uidl)
		}
		if p.onFetch != actionDelete {
			return nil
		}
		return removeByUIDL(client, uidls)
	})
	if err != nil {
		return nil, fmt.Errorf("pop3-fetch: %w", err)
	}
	msg.SetBody(out)
	return msg, nil
}
