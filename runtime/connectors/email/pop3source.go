// This file provides the pop3 source: it polls a mail drop and runs each new
// message through the flow, one at a time. POP3 has no flags to mark a message
// handled, so onSuccess decides where that record lives. With delete the drop
// is the record — a handled message is deleted once its flow completes. With
// keep the drop is never changed, and the source keeps the UIDLs it has handled
// in the KV store, starting from mail that arrives after it first runs.
package email

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// pop3SourceSettings configures the pop3 source bound to a flow.
type pop3SourceSettings struct {
	// How often to poll, e.g. 30s or 5m.
	Interval duration `json:"interval" octo:"label=Interval,type=string,default=1m"`
	// What to do with a message once its flow completes: keep it on the server
	// (tracking it in the KV store, starting from mail that arrives after the
	// source first runs), or delete it.
	OnSuccess string `json:"onSuccess" octo:"label=On success,type=enum,enum=keep|delete,default=keep"`
	// Most messages read in one poll, oldest first.
	MaxPerPoll int `json:"maxPerPoll" octo:"label=Max per poll,default=10"`
	// How long to wait for one message's flow before leaving the message for
	// the next poll.
	FlowTimeout duration `json:"flowTimeout" octo:"label=Flow timeout,type=string,default=5m"`
}

// pop3Source polls one mail drop for a flow.
type pop3Source struct {
	conn       *POP3Connector
	onSuccess  string
	maxPerPoll int
	handledKey string
	// account names the server and user, recorded with the handled set.
	account string
	// pendingBaseline is a first-run baseline not yet saved; see pollKeeping.
	pendingBaseline map[string]bool
	emitter         emitter
	poller          poller
}

// NewSource builds the pop3 source, validating its settings up front so a bad
// configuration fails at startup.
//
//nolint:ireturn // a SourceProvider returns the MessageSource interface
func (c *POP3Connector) NewSource(
	cfg types.SourceConfig, out chan<- *types.Message, _ core.SourceDeps,
) (core.MessageSource, error) {
	var set pop3SourceSettings
	if err := cfg.Settings.Decode(&set); err != nil {
		return nil, err
	}
	onSuccess, err := validatePOP3Action("pop3 source", "onSuccess", set.OnSuccess)
	if err != nil {
		return nil, err
	}
	if !c.claims.claim(pop3Type) {
		return nil, fmt.Errorf("pop3 connector %q already has a source; "+
			"read the mail drop from one flow and hand messages on with flow-ref", cfg.Connector)
	}
	key := leaderKey(pop3Type, cfg)
	s := &pop3Source{
		conn:       c,
		onSuccess:  onSuccess,
		maxPerPoll: positiveOr(set.MaxPerPoll, defaultMaxPerPoll),
		handledKey: key + "/handled",
		account:    c.ep.account(),
		emitter: emitter{
			out:         out,
			pending:     c.pending,
			flowTimeout: durationOr(set.FlowTimeout, defaultFlowTimeout),
		},
	}
	s.poller = poller{
		label:     "pop3",
		leaderKey: key,
		interval:  durationOr(set.Interval, defaultInterval),
		poll:      s.poll,
	}
	s.emitter.leading = s.poller.leading
	return s, nil
}

// Start begins polling.
func (s *pop3Source) Start(ctx context.Context) error { return s.poller.start(ctx) }

// Stop ends polling and waits for an in-flight poll.
func (s *pop3Source) Stop(context.Context) error {
	s.poller.stop()
	return nil
}

func (s *pop3Source) poll(ctx context.Context) error {
	if s.onSuccess == actionKeep {
		return s.pollKeeping(ctx)
	}
	return s.pollDeleting(ctx)
}

// handle parses one message and runs it through the flow. An unreadable
// message never reaches a flow: delete mode leaves it on the server, since
// nothing handled it, and keep mode records it so it is not read every poll.
func (s *pop3Source) handle(ctx context.Context, m pop3Message) (bool, error) {
	body, err := pop3Body(m)
	if err != nil {
		slog.Error("pop3 source: unreadable message", "error", err)
		return false, errUnreadable
	}
	return s.emitter.emit(ctx, body, map[string]any{keyUIDL: m.uidl})
}

// pollDeleting reads the oldest messages, runs each through the flow, and
// deletes the handled ones in a second session — the first is not held open
// while flows run.
func (s *pop3Source) pollDeleting(ctx context.Context) error {
	var messages []pop3Message
	err := s.conn.session(ctx, func(client *pop3Client) error {
		var rerr error
		_, messages, rerr = readDrop(client, pop3Query{limit: s.maxPerPoll})
		return rerr
	})
	if err != nil || len(messages) == 0 {
		return err
	}

	var handled []string
	var cause error
	for _, m := range messages {
		if !s.poller.leading() {
			cause = errLostLeadership
			break
		}
		ok, err := s.handle(ctx, m)
		if errors.Is(err, errUnreadable) {
			continue
		}
		if err != nil {
			cause = err
			break
		}
		if ok {
			handled = append(handled, m.uidl)
		}
	}
	if len(handled) == 0 {
		return cause
	}

	// A stopping source still deletes what it finished, so a completed flow is
	// never run again for the same message.
	ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.conn.ep.timeout)
	defer cancel()
	err = s.conn.session(ackCtx, func(client *pop3Client) error { return removeByUIDL(client, handled) })
	if cause != nil {
		return cause
	}
	return err
}

// pop3Handled is the keep-mode record: the UIDLs already handled in the
// account's drop. It is pruned to the drop's current listing every poll, so it
// never outgrows the drop. Account names the server and user it was taken on;
// a connector repointed at another account starts a fresh record.
type pop3Handled struct {
	Account string   `json:"account"`
	UIDLs   []string `json:"uidls"`
}

// pollKeeping reads messages not yet handled and records each one as its flow
// completes. With no record for this account yet, it records everything
// already in the drop and handles nothing: there is no telling which of those
// messages are new. That baseline is held until it is saved, so a failed save
// does not sweep mail that arrived since into it.
func (s *pop3Source) pollKeeping(ctx context.Context) error {
	kv := core.RuntimeServicesFromContext(ctx).KV()
	record, version, err := s.loadHandled(ctx, kv)
	if err != nil {
		return err
	}
	fresh := version == 0 || record.Account != s.account
	handled := map[string]bool{}
	if !fresh {
		for _, u := range record.UIDLs {
			handled[u] = true
		}
	}

	entries, messages, err := s.readUnhandled(ctx, handled, fresh)
	if err != nil {
		return err
	}
	if fresh {
		return s.saveBaseline(ctx, kv, entries, version)
	}

	for _, m := range messages {
		if !s.poller.leading() {
			return errLostLeadership
		}
		record, err := s.handledOrUnreadable(ctx, m)
		if err != nil {
			return err
		}
		if !record {
			continue
		}
		handled[m.uidl] = true
		if version, err = s.saveHandled(ctx, kv, entries, handled, version); err != nil {
			return err
		}
	}
	_, err = s.saveHandled(ctx, kv, entries, handled, version)
	return err
}

// readUnhandled lists the drop and downloads the oldest messages not yet
// handled; on a fresh record it only lists.
func (s *pop3Source) readUnhandled(
	ctx context.Context, handled map[string]bool, fresh bool,
) ([]pop3Entry, []pop3Message, error) {
	q := pop3Query{skip: func(u string) bool { return handled[u] }, limit: s.maxPerPoll}
	if fresh {
		q.limit, q.skip = 0, func(string) bool { return true }
	}
	var entries []pop3Entry
	var messages []pop3Message
	err := s.conn.session(ctx, func(client *pop3Client) error {
		var rerr error
		entries, messages, rerr = readDrop(client, q)
		return rerr
	})
	return entries, messages, err
}

// handledOrUnreadable runs m through the flow and reports whether keep mode
// should record it: its flow completed, or it can never reach one. An error is
// the source stopping.
func (s *pop3Source) handledOrUnreadable(ctx context.Context, m pop3Message) (bool, error) {
	ok, err := s.handle(ctx, m)
	if errors.Is(err, errUnreadable) {
		return true, nil
	}
	return ok, err
}

// saveBaseline records the drop's existing mail as handled, reusing a baseline
// an earlier poll could not save.
func (s *pop3Source) saveBaseline(ctx context.Context, kv core.KV, entries []pop3Entry, version int64) error {
	if s.pendingBaseline == nil {
		s.pendingBaseline = map[string]bool{}
		for _, e := range entries {
			s.pendingBaseline[e.uidl] = true
		}
		slog.Info("pop3 source: recording the drop's existing mail as handled", "messages", len(entries))
	}
	if _, err := s.saveHandled(ctx, kv, entries, s.pendingBaseline, version); err != nil {
		return err
	}
	s.pendingBaseline = nil
	return nil
}

func (s *pop3Source) loadHandled(ctx context.Context, kv core.KV) (pop3Handled, int64, error) {
	entry, ok, err := kv.Get(ctx, core.NamespaceSystem, s.handledKey)
	if err != nil {
		return pop3Handled{}, 0, fmt.Errorf("pop3 source: read handled set: %w", err)
	}
	if !ok {
		return pop3Handled{}, 0, nil
	}
	var record pop3Handled
	if err := json.Unmarshal(entry.Value, &record); err != nil {
		return pop3Handled{}, 0, fmt.Errorf("pop3 source: decode handled set: %w", err)
	}
	return record, entry.Version, nil
}

// saveHandled writes the handled UIDLs still present in entries, against the
// version last read, and returns the new version. Only the leader polls, so a
// version conflict means leadership moved mid-poll.
func (s *pop3Source) saveHandled(
	ctx context.Context, kv core.KV, entries []pop3Entry, handled map[string]bool, version int64,
) (int64, error) {
	record := pop3Handled{Account: s.account, UIDLs: []string{}}
	for _, e := range entries {
		if handled[e.uidl] {
			record.UIDLs = append(record.UIDLs, e.uidl)
		}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return 0, fmt.Errorf("pop3 source: encode handled set: %w", err)
	}
	next, err := kv.Set(ctx, core.NamespaceSystem, s.handledKey, raw, version)
	if errors.Is(err, core.ErrVersionConflict) {
		return 0, fmt.Errorf("pop3 source: handled set changed by another replica: %w", err)
	}
	if err != nil {
		return 0, fmt.Errorf("pop3 source: write handled set: %w", err)
	}
	return next, nil
}
