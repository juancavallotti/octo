// This file provides the imap source: it polls a mailbox and runs each new
// message through the flow, one at a time, acting on the message only once its
// flow has completed. Which messages are new depends on onSuccess. With seen,
// move or delete the mailbox itself records what was handled, so a poll reads
// the unseen messages. With none the mailbox is never changed, so the source
// keeps a cursor — the highest UID handled — in the KV store instead.
package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// imapSourceSettings configures the imap source bound to a flow.
type imapSourceSettings struct {
	// Mailbox to poll.
	Mailbox string `json:"mailbox" octo:"label=Mailbox,default=INBOX"`
	// How often to poll, e.g. 30s or 5m.
	Interval duration `json:"interval" octo:"label=Interval,type=string,default=1m"`
	// What to do with a message once its flow completes: seen, move, delete,
	// or none (leave the mailbox untouched and track progress in the KV store,
	// starting from mail that arrives after the source first runs).
	OnSuccess string `json:"onSuccess" octo:"label=On success,type=enum,enum=seen|move|delete|none,default=seen"`
	// Mailbox to move handled messages to.
	MoveTo string `json:"moveTo" octo:"label=Move to,showIf=onSuccess=move"`
	// Most messages read in one poll, oldest first.
	MaxPerPoll int `json:"maxPerPoll" octo:"label=Max per poll,default=10"`
	// How long to wait for one message's flow before leaving the message for
	// the next poll.
	FlowTimeout duration `json:"flowTimeout" octo:"label=Flow timeout,type=string,default=5m"`
}

// imapSource polls one mailbox for a flow.
type imapSource struct {
	conn       *IMAPConnector
	mailbox    string
	onSuccess  string
	moveTo     string
	maxPerPoll int
	cursorKey  string
	emitter    emitter
	poller     poller
}

// NewSource builds the imap source, validating its settings up front so a bad
// configuration fails at startup.
//
//nolint:ireturn // a SourceProvider returns the MessageSource interface
func (c *IMAPConnector) NewSource(
	cfg types.SourceConfig, out chan<- *types.Message, _ core.SourceDeps,
) (core.MessageSource, error) {
	var set imapSourceSettings
	if err := cfg.Settings.Decode(&set); err != nil {
		return nil, err
	}
	onSuccess, err := validateAction("imap source", "onSuccess", set.OnSuccess, actionSeen, set.MoveTo)
	if err != nil {
		return nil, err
	}
	mailbox := canonicalMailbox(set.Mailbox)
	if !c.claims.claim(mailbox) {
		return nil, fmt.Errorf("imap connector %q: mailbox %q already has a source; "+
			"read it from one flow and hand messages on with flow-ref", cfg.Connector, mailbox)
	}
	s := &imapSource{
		conn:       c,
		mailbox:    mailbox,
		onSuccess:  onSuccess,
		moveTo:     set.MoveTo,
		maxPerPoll: positiveOr(set.MaxPerPoll, defaultMaxPerPoll),
		emitter: emitter{
			out:         out,
			pending:     c.pending,
			flowTimeout: durationOr(set.FlowTimeout, defaultFlowTimeout),
		},
	}
	// One key names the mailbox being read: the source campaigns for it and, with
	// onSuccess none, keeps its cursor under it. Mailboxes on one connector so
	// elect and progress independently.
	key := leaderKey(imapType, cfg) + "/" + mailbox
	s.cursorKey = key
	s.poller = poller{
		label:     "imap",
		leaderKey: key,
		interval:  durationOr(set.Interval, defaultInterval),
		poll:      s.poll,
	}
	return s, nil
}

// Start begins polling.
func (s *imapSource) Start(ctx context.Context) error { return s.poller.start(ctx) }

// Stop ends polling and waits for an in-flight poll.
func (s *imapSource) Stop(context.Context) error {
	s.poller.stop()
	return nil
}

// poll reads one batch, runs each message through the flow, and acts on the
// handled ones in a second session — the first is not held open while flows
// run, which can take far longer than a server keeps an idle session.
func (s *imapSource) poll(ctx context.Context) error {
	if s.onSuccess == actionNone {
		return s.pollWithCursor(ctx)
	}

	var batch imapBatch
	err := s.conn.session(ctx, func(client *imapclient.Client) error {
		if err := checkAction(client, s.onSuccess); err != nil {
			return err
		}
		var rerr error
		batch, rerr = read(client, imapQuery{mailbox: s.mailbox, unseen: true, limit: s.maxPerPoll})
		return rerr
	})
	if err != nil || len(batch.messages) == 0 {
		return err
	}

	var handled []imap.UID
	for _, m := range batch.messages {
		ok, err := s.handle(ctx, m)
		if errors.Is(err, errUnreadable) {
			continue
		}
		if err != nil {
			return s.ack(ctx, batch.uidValidity, handled, err)
		}
		if ok {
			handled = append(handled, m.uid)
		}
	}
	return s.ack(ctx, batch.uidValidity, handled, nil)
}

// errUnreadable reports a message whose header cannot be parsed. It never
// reaches a flow; seen, move and delete leave it in place (it is read again,
// and logged again, each poll), while none moves the cursor past it.
var errUnreadable = errors.New("unreadable message")

// handle parses one message and runs it through the flow.
func (s *imapSource) handle(ctx context.Context, m imapMessage) (bool, error) {
	body, err := imapBody(m, s.mailbox)
	if err != nil {
		slog.Error("imap source: unreadable message", "error", err)
		return false, errUnreadable
	}
	return s.emitter.emit(ctx, body, map[string]any{keyMailbox: s.mailbox, keyUID: int64(m.uid)})
}

// ack applies onSuccess to the handled messages and returns cause, or the ack
// error when there is no cause. It acks even when the poll was cut short, so a
// flow that completed is never run again for the same message. It uses a fresh
// context: a stopping source should still record what it finished.
func (s *imapSource) ack(ctx context.Context, uidValidity uint32, uids []imap.UID, cause error) error {
	if len(uids) == 0 {
		return cause
	}
	ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.conn.ep.timeout)
	defer cancel()
	err := s.conn.session(ackCtx, func(client *imapclient.Client) error {
		selected, err := client.Select(s.mailbox, nil).Wait()
		if err != nil {
			return fmt.Errorf("imap: select %q: %w", s.mailbox, err)
		}
		// The flows ran for as long as they took; if the mailbox was recreated
		// meanwhile its UIDs name other messages, and acting on them would mark,
		// move or delete the wrong mail. The handled messages are gone with the
		// old mailbox, so there is nothing left to act on.
		if selected.UIDValidity != uidValidity {
			slog.Warn("imap source: mailbox UIDVALIDITY changed while flows ran; not acting on stale UIDs",
				"mailbox", s.mailbox, "was", uidValidity, "now", selected.UIDValidity)
			return nil
		}
		return apply(client, uids, s.onSuccess, s.moveTo)
	})
	if cause != nil {
		return cause
	}
	return err
}

// imapCursor is the none-mode progress record: the highest UID handled, valid
// while the mailbox keeps its UIDVALIDITY.
type imapCursor struct {
	UIDValidity uint32   `json:"uidValidity"`
	LastUID     imap.UID `json:"lastUid"`
}

// pollWithCursor handles messages above the cursor in UID order, advancing the
// cursor after each one. It stops at the first message whose flow does not
// complete, so the cursor never passes a message that was not handled.
//
// With no cursor yet, or one from before the mailbox's UIDVALIDITY changed, it
// starts the cursor at the mailbox's current end: there is no telling which of
// the messages already there are new.
func (s *imapSource) pollWithCursor(ctx context.Context) error {
	kv := core.RuntimeServicesFromContext(ctx).KV()
	cursor, version, err := s.loadCursor(ctx, kv)
	if err != nil {
		return err
	}
	batch, uidNext, err := s.readAfter(ctx, cursor, version)
	if err != nil {
		return err
	}

	if version == 0 || cursor.UIDValidity != batch.uidValidity {
		start := imapCursor{UIDValidity: batch.uidValidity, LastUID: max(uidNext, 1) - 1}
		slog.Info("imap source: starting cursor at the mailbox's end",
			"mailbox", s.mailbox, "uidValidity", start.UIDValidity, "lastUid", start.LastUID)
		_, err := s.saveCursor(ctx, kv, start, version)
		return err
	}

	for _, m := range batch.messages {
		ok, err := s.handle(ctx, m)
		if errors.Is(err, errUnreadable) {
			ok, err = true, nil
		}
		if err != nil || !ok {
			return err
		}
		cursor.LastUID = m.uid
		if version, err = s.saveCursor(ctx, kv, cursor, version); err != nil {
			return err
		}
	}
	return nil
}

// readAfter reads the messages above the cursor, and the mailbox's UIDNEXT for
// placing a new cursor. It reads no messages when the cursor is missing
// (version 0) or belongs to another UIDVALIDITY.
func (s *imapSource) readAfter(ctx context.Context, cursor imapCursor, version int64) (imapBatch, imap.UID, error) {
	var batch imapBatch
	var uidNext imap.UID
	err := s.conn.session(ctx, func(client *imapclient.Client) error {
		selected, err := client.Select(s.mailbox, nil).Wait()
		if err != nil {
			return fmt.Errorf("imap: select %q: %w", s.mailbox, err)
		}
		uidNext = selected.UIDNext
		if version == 0 || cursor.UIDValidity != selected.UIDValidity {
			batch = imapBatch{uidValidity: selected.UIDValidity}
			return nil
		}
		batch, err = read(client, imapQuery{mailbox: s.mailbox, afterUID: cursor.LastUID, limit: s.maxPerPoll})
		return err
	})
	return batch, uidNext, err
}

func (s *imapSource) loadCursor(ctx context.Context, kv core.KV) (imapCursor, int64, error) {
	entry, ok, err := kv.Get(ctx, core.NamespaceSystem, s.cursorKey)
	if err != nil {
		return imapCursor{}, 0, fmt.Errorf("imap source: read cursor: %w", err)
	}
	if !ok {
		return imapCursor{}, 0, nil
	}
	var cursor imapCursor
	if err := json.Unmarshal(entry.Value, &cursor); err != nil {
		return imapCursor{}, 0, fmt.Errorf("imap source: decode cursor: %w", err)
	}
	return cursor, entry.Version, nil
}

// saveCursor writes the cursor against the version last read and returns the
// new one. Only the leader polls, so a version conflict means leadership moved
// mid-poll; the other replica now owns the cursor.
func (s *imapSource) saveCursor(ctx context.Context, kv core.KV, cursor imapCursor, version int64) (int64, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return 0, fmt.Errorf("imap source: encode cursor: %w", err)
	}
	next, err := kv.Set(ctx, core.NamespaceSystem, s.cursorKey, raw, version)
	if errors.Is(err, core.ErrVersionConflict) {
		return 0, fmt.Errorf("imap source: cursor changed by another replica: %w", err)
	}
	if err != nil {
		return 0, fmt.Errorf("imap source: write cursor: %w", err)
	}
	return next, nil
}

// canonicalMailbox resolves the mailbox setting, spelling INBOX the one way:
// IMAP treats that name case-insensitively and every other name as written.
func canonicalMailbox(setting string) string {
	mailbox := orDefault(strings.TrimSpace(setting), defaultMailbox)
	if strings.EqualFold(mailbox, defaultMailbox) {
		return defaultMailbox
	}
	return mailbox
}

func positiveOr(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func durationOr(d duration, fallback time.Duration) time.Duration {
	if d > 0 {
		return time.Duration(d)
	}
	return fallback
}
