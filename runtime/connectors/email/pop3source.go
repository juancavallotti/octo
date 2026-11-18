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
	emitter    emitter
	poller     poller
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
	key := leaderKey(pop3Type, cfg)
	s := &pop3Source{
		conn:       c,
		onSuccess:  onSuccess,
		maxPerPoll: positiveOr(set.MaxPerPoll, defaultMaxPerPoll),
		handledKey: key + "/handled",
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
// message never reaches a flow and counts as handled, so it is deleted or
// recorded rather than retried forever.
func (s *pop3Source) handle(ctx context.Context, m pop3Message) (bool, error) {
	body, err := pop3Body(m)
	if err != nil {
		slog.Error("pop3 source: unreadable message", "error", err)
		return true, nil
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
		ok, err := s.handle(ctx, m)
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

// pop3Handled is the keep-mode record: the UIDLs already handled. It is pruned
// to the drop's current listing every poll, so it never outgrows the drop.
type pop3Handled struct {
	UIDLs []string `json:"uidls"`
}

// pollKeeping reads messages not yet handled and records each one as its flow
// completes. With no record yet, it records everything already in the drop and
// handles nothing: there is no telling which of those messages are new.
func (s *pop3Source) pollKeeping(ctx context.Context) error {
	kv := core.RuntimeServicesFromContext(ctx).KV()
	record, version, err := s.loadHandled(ctx, kv)
	if err != nil {
		return err
	}
	handled := map[string]bool{}
	for _, u := range record.UIDLs {
		handled[u] = true
	}

	var entries []pop3Entry
	var messages []pop3Message
	err = s.conn.session(ctx, func(client *pop3Client) error {
		q := pop3Query{skip: func(u string) bool { return handled[u] }, limit: s.maxPerPoll}
		if version == 0 {
			q.limit, q.skip = 0, func(string) bool { return true }
		}
		var rerr error
		entries, messages, rerr = readDrop(client, q)
		return rerr
	})
	if err != nil {
		return err
	}

	if version == 0 {
		for _, e := range entries {
			handled[e.uidl] = true
		}
		slog.Info("pop3 source: recorded the drop's existing mail as handled", "messages", len(entries))
		_, err := s.saveHandled(ctx, kv, entries, handled, version)
		return err
	}

	for _, m := range messages {
		ok, err := s.handle(ctx, m)
		if err != nil {
			return err
		}
		if !ok {
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
	record := pop3Handled{UIDLs: []string{}}
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
