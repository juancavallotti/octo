// This file provides the "imap-fetch" block: it reads messages from a mailbox
// when the flow reaches it, rather than when mail arrives, and replaces the body
// with them. What happens to the messages on the server is the block's onFetch
// action, applied in the same session right after they are read — there is no
// flow outcome to wait for, so a flow that must not lose mail when a later step
// fails leaves onFetch at none and acts on the messages itself.
package email

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

const (
	imapFetchType = "imap-fetch"
	// searchUnseen and searchAll are the imap-fetch search modes.
	searchUnseen = "unseen"
	searchAll    = "all"
	// defaultFetchLimit caps a fetch block's read when it sets no limit.
	defaultFetchLimit = 10
)

func registerIMAPFetch() {
	core.MustRegisterBlock(imapFetchType, newIMAPFetch)
	core.RegisterBlockMeta(core.BlockMeta{
		Type:        imapFetchType,
		Label:       "Fetch IMAP Mail",
		Category:    core.CategoryProcessor,
		Icon:        iconInbox,
		Description: "Read messages from an IMAP mailbox into the body, as a list.",
		Config:      reflect.TypeFor[imapFetchSettings](),
	})
}

// imapFetchSettings is the imap-fetch block's configuration.
type imapFetchSettings struct {
	// Name of the imap connector to read through.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector:imap"`
	// Mailbox to read.
	Mailbox string `json:"mailbox" octo:"label=Mailbox,default=INBOX"`
	// Which messages to read: unseen ones, or all of them.
	Search string `json:"search" octo:"label=Search,type=enum,enum=unseen|all,default=unseen"`
	// CEL expression for the earliest date to read from: a timestamp or an
	// RFC 3339 / YYYY-MM-DD string. Only the date counts, as IMAP searches by day.
	Since string `json:"since" octo:"label=Since,type=cel"`
	// Most messages to read; the newest are kept when more match.
	Limit int `json:"limit" octo:"label=Limit,default=10"`
	// What to do with the messages once read: none, seen, move, or delete.
	OnFetch string `json:"onFetch" octo:"label=On fetch,type=enum,enum=none|seen|move|delete,default=none"`
	// Mailbox to move read messages to.
	MoveTo string `json:"moveTo" octo:"label=Move to,showIf=onFetch=move"`
}

// imapFetchProcessor reads a mailbox per flow message.
type imapFetchProcessor struct {
	conn    *IMAPConnector
	mailbox string
	unseen  bool
	since   *expr.Program
	limit   int
	onFetch string
	moveTo  string
	env     expr.Env
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newIMAPFetch(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg imapFetchSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	conn, err := resolveConnector[*IMAPConnector](imapFetchType, cfg.Connector, deps)
	if err != nil {
		return nil, err
	}
	search := cfg.Search
	if search == "" {
		search = searchUnseen
	}
	if search != searchUnseen && search != searchAll {
		return nil, fmt.Errorf("imap-fetch: unknown search %q (want unseen or all)", cfg.Search)
	}
	onFetch, err := validateAction(imapFetchType, "onFetch", cfg.OnFetch, actionNone, cfg.MoveTo)
	if err != nil {
		return nil, err
	}
	limit := cfg.Limit
	if limit <= 0 {
		limit = defaultFetchLimit
	}
	c := compiler{res: deps.Resources, block: imapFetchType}
	since := c.optional("since", cfg.Since)
	if c.err != nil {
		return nil, c.err
	}

	return &imapFetchProcessor{
		conn:    conn,
		mailbox: orDefault(strings.TrimSpace(cfg.Mailbox), defaultMailbox),
		unseen:  search == searchUnseen,
		since:   since,
		limit:   limit,
		onFetch: onFetch,
		moveTo:  cfg.MoveTo,
		env:     expr.EnvActivation(deps.Env),
	}, nil
}

// Process reads the mailbox, applies onFetch, and sets the body to the messages
// in ascending UID order.
func (p *imapFetchProcessor) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	e := evaluator{act: expr.MessageActivation(msg, p.env)}
	since := e.timestamp("since", p.since)
	if e.err != nil {
		return nil, fmt.Errorf("imap-fetch: %w", e.err)
	}
	q := imapQuery{mailbox: p.mailbox, unseen: p.unseen, since: since, limit: p.limit, newest: true}

	// Every message is parsed before onFetch runs, inside the session: a message
	// that cannot be parsed fails the fetch with the mailbox still untouched,
	// rather than after its batch was already moved or deleted.
	var out []any
	err := p.conn.session(ctx, func(client *imapclient.Client) error {
		if err := checkAction(client, p.onFetch); err != nil {
			return err
		}
		batch, err := read(client, q)
		if err != nil {
			return err
		}
		out = make([]any, 0, len(batch.messages))
		uids := make([]imap.UID, 0, len(batch.messages))
		for _, m := range batch.messages {
			body, err := imapBody(m, p.mailbox)
			if err != nil {
				return err
			}
			out = append(out, body)
			uids = append(uids, m.uid)
		}
		return apply(client, uids, p.onFetch, p.moveTo)
	})
	if err != nil {
		return nil, fmt.Errorf("imap-fetch: %w", err)
	}
	msg.SetBody(out)
	return msg, nil
}

// timestamp evaluates to a time: a CEL timestamp, or a string in RFC 3339 or
// YYYY-MM-DD form. Null and the empty string are the zero time.
func (e *evaluator) timestamp(field string, p *expr.Program) time.Time {
	switch v := e.eval(field, p).(type) {
	case nil:
		return time.Time{}
	case time.Time:
		return v
	case string:
		if strings.TrimSpace(v) == "" {
			return time.Time{}
		}
		for _, layout := range []string{time.RFC3339, time.DateOnly} {
			if t, err := time.Parse(layout, v); err == nil {
				return t
			}
		}
		e.fail(fmt.Errorf("%s: %q is not an RFC 3339 or YYYY-MM-DD date", field, v))
	default:
		e.fail(fmt.Errorf("%s must be a timestamp or a date string, got %T", field, v))
	}
	return time.Time{}
}

// orDefault returns value when it is non-empty, otherwise fallback.
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
