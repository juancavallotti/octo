// This file provides the "imap" connector and the mailbox operations its source
// and its fetch block share: open a session, find messages, download them, and
// act on them once they are handled. Each poll or fetch is its own session, so
// the connector keeps no connection between them.
package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

const (
	imapType = "imap"
	// defaultMailbox is the mailbox read when none is named.
	defaultMailbox = "INBOX"
	// keyUID and keyMailbox are added to each parsed IMAP message, so a later
	// step can name the message on the server.
	keyUID     = "uid"
	keyMailbox = "mailbox"
)

// The IMAP ports: 143 for clear text and STARTTLS, 993 for implicit TLS.
const (
	imapPlainPort = 143
	imapTLSPort   = 993
)

// imapProtocol reads over implicit TLS unless told otherwise.
var imapProtocol = protocol{
	name:            imapType,
	defaultSecurity: securityTLS,
	plainPort:       imapPlainPort,
	startTLSPort:    imapPlainPort,
	tlsPort:         imapTLSPort,
}

// The actions applied to a message once it is handled. keep is POP3's word for
// none: leave the message where it is.
const (
	actionNone   = "none"
	actionKeep   = "keep"
	actionSeen   = "seen"
	actionMove   = "move"
	actionDelete = "delete"
)

func registerIMAP() {
	core.MustRegisterConnector(imapType, func() core.Connector {
		return &IMAPConnector{}
	})
	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     imapType,
		Label:    "IMAP",
		Icon:     iconInbox,
		Settings: reflect.TypeFor[imapSettings](),
		Sources: []core.SourceMeta{{
			Type:     imapType,
			Label:    "IMAP Mailbox",
			Icon:     iconInbox,
			Settings: reflect.TypeFor[imapSourceSettings](),
		}},
	})
}

// imapSettings is the imap connector's configuration.
type imapSettings struct {
	// IMAP server host name.
	Host string `json:"host" octo:"label=Host,required"`
	// Server port; defaults to 143 for none and starttls, 993 for tls.
	Port int `json:"port" octo:"label=Port"`
	// How the connection is secured: none, starttls (upgrade before logging
	// in), or tls (implicit TLS).
	Security string `json:"security" octo:"label=Security,type=enum,enum=none|starttls|tls,default=tls"`
	// Accept any server certificate. Disables verification; only for lab
	// servers with self-signed certificates.
	TLSSkipVerify bool `json:"tlsSkipVerify" octo:"label=Skip TLS verification"`
	// User to log in as.
	Username string `json:"username" octo:"label=Username,required"`
	// Password for the user, usually a ${ENV} reference.
	Password string `json:"password" octo:"label=Password"`
	// Bounds one session: a whole poll or fetch, from dial to logout.
	Timeout duration `json:"timeout" octo:"label=Timeout,type=string,default=60s"`
}

// IMAPConnector reads a mailbox on an IMAP server. It is safe for concurrent
// use: every session dials its own connection and the endpoint is read-only
// after Start. Flows bound to it receive mail through its imap source.
type IMAPConnector struct {
	ep      endpoint
	pending *pendingFlows
	claims  sourceClaims
}

// Start decodes and validates the settings and starts correlating flow outcomes
// for the connector's sources.
func (c *IMAPConnector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set imapSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.Username) == "" {
		return errors.New("imap connector requires a \"username\" setting")
	}
	ep, err := newEndpoint(imapProtocol, serverSettings{
		host:          set.Host,
		port:          set.Port,
		security:      set.Security,
		tlsSkipVerify: set.TLSSkipVerify,
		username:      set.Username,
		password:      set.Password,
		timeout:       set.Timeout,
	})
	if err != nil {
		return err
	}
	c.ep = ep
	c.pending = newPendingFlows()
	return nil
}

// Stop stops correlating flow outcomes, releasing any source parked on one.
func (c *IMAPConnector) Stop(context.Context) error {
	if c.pending != nil {
		c.pending.close()
	}
	return nil
}

// session runs fn on a logged-in client and logs out after it.
func (c *IMAPConnector) session(ctx context.Context, fn func(*imapclient.Client) error) error {
	ctx, cancel := c.ep.bound(ctx)
	defer cancel()
	conn, err := c.ep.dial(ctx)
	if err != nil {
		return err
	}
	opts := &imapclient.Options{
		TLSConfig:   c.ep.tls,
		WordDecoder: &mime.WordDecoder{CharsetReader: charset.Reader},
	}
	var client *imapclient.Client
	if c.ep.security == securityStartTLS {
		client, err = imapclient.NewStartTLS(conn, opts)
		if err != nil {
			_ = conn.Close()
			return fmt.Errorf("imap: starttls with %s: %w", c.ep.addr(), err)
		}
	} else {
		client = imapclient.New(conn, opts)
	}
	defer func() { _ = client.Close() }()

	if err := client.Login(c.ep.username, c.ep.password).Wait(); err != nil {
		return fmt.Errorf("imap: log in as %q: %w", c.ep.username, err)
	}
	if err := fn(client); err != nil {
		return err
	}
	// The work is done once fn returns — an EXPUNGE is final — so a goodbye the
	// server never answers is no reason to report the session as failed and
	// make the caller drop what it read.
	if err := client.Logout().Wait(); err != nil {
		slog.Debug("imap: logout after a successful session failed", "server", c.ep.addr(), "error", err)
	}
	return nil
}

// imapQuery says which messages of a mailbox to read.
type imapQuery struct {
	mailbox string
	// unseen limits the search to messages without the \Seen flag.
	unseen bool
	// since limits the search to messages received on or after that date.
	since time.Time
	// afterUID limits the search to messages above that UID.
	afterUID imap.UID
	// limit caps how many messages are read; zero reads every match.
	limit int
	// newest takes the highest UIDs when limit cuts the matches; otherwise the
	// lowest — oldest first, the order a queue drains in.
	newest bool
}

// imapMessage is one downloaded message.
type imapMessage struct {
	uid imap.UID
	raw []byte
}

// imapBatch is the result of reading a mailbox: its UIDVALIDITY, which says
// whether UIDs from an earlier session still name the same messages, its
// UIDNEXT, and the messages in ascending UID order.
type imapBatch struct {
	uidValidity uint32
	uidNext     imap.UID
	messages    []imapMessage
}

// read selects the mailbox, searches it, and downloads the matching messages.
// Messages are fetched with BODY.PEEK so reading never marks them seen: what
// happens to a message is decided by apply, after it is handled.
func read(client *imapclient.Client, q imapQuery) (imapBatch, error) {
	selected, err := client.Select(q.mailbox, nil).Wait()
	if err != nil {
		return imapBatch{}, fmt.Errorf("imap: select %q: %w", q.mailbox, err)
	}
	return readSelected(client, q, selected)
}

// readSelected is read on a mailbox the caller already selected, so metadata it
// took from that SELECT and the messages read here describe the same mailbox.
func readSelected(client *imapclient.Client, q imapQuery, selected *imap.SelectData) (imapBatch, error) {
	batch := imapBatch{uidValidity: selected.UIDValidity, uidNext: selected.UIDNext}
	if selected.NumMessages == 0 {
		return batch, nil
	}

	criteria := &imap.SearchCriteria{Since: q.since}
	if q.unseen {
		criteria.NotFlag = []imap.Flag{imap.FlagSeen}
	}
	if q.afterUID > 0 {
		criteria.UID = []imap.UIDSet{{imap.UIDRange{Start: q.afterUID + 1}}}
	}
	found, err := client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return imapBatch{}, fmt.Errorf("imap: search %q: %w", q.mailbox, err)
	}
	uids := selectUIDs(found.AllUIDs(), q)
	if len(uids) == 0 {
		return batch, nil
	}

	section := &imap.FetchItemBodySection{Peek: true}
	fetched, err := client.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{
		UID:         true,
		BodySection: []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return imapBatch{}, fmt.Errorf("imap: fetch from %q: %w", q.mailbox, err)
	}
	for _, m := range fetched {
		batch.messages = append(batch.messages, imapMessage{uid: m.UID, raw: m.FindBodySection(section)})
	}
	slices.SortFunc(batch.messages, func(a, b imapMessage) int { return int(a.uid) - int(b.uid) })
	return batch, nil
}

// selectUIDs orders the search result and applies the cursor and limit. The
// cursor is re-checked here because "N:*" matches the last message even when N
// is past it.
func selectUIDs(found []imap.UID, q imapQuery) []imap.UID {
	uids := make([]imap.UID, 0, len(found))
	for _, uid := range found {
		if uid > q.afterUID {
			uids = append(uids, uid)
		}
	}
	slices.Sort(uids)
	if q.limit > 0 && len(uids) > q.limit {
		if q.newest {
			uids = uids[len(uids)-q.limit:]
		} else {
			uids = uids[:q.limit]
		}
	}
	return uids
}

// apply acts on handled messages: marks them seen, moves them to moveTo, or
// deletes them. none leaves them as they are. The mailbox must be selected, and
// checkAction must have passed for this session.
//
// Nothing here may touch a message it was not given. A move without the MOVE
// extension copies first and removes the originals only once the copy is known
// to have succeeded — go-imap's own fallback pipelines all three commands and
// would delete a message whose copy failed. Removal always uses UID EXPUNGE, so
// a message someone else flagged \Deleted is left for them.
func apply(client *imapclient.Client, uids []imap.UID, action, moveTo string) error {
	if len(uids) == 0 || action == actionNone {
		return nil
	}
	set := imap.UIDSetNum(uids...)
	switch action {
	case actionSeen:
		return storeFlag(client, set, imap.FlagSeen)
	case actionMove:
		if client.Caps().Has(imap.CapMove) {
			if _, err := client.Move(set, moveTo).Wait(); err != nil {
				return fmt.Errorf("imap: move to %q: %w", moveTo, err)
			}
			return nil
		}
		if _, err := client.Copy(set, moveTo).Wait(); err != nil {
			return fmt.Errorf("imap: copy to %q: %w", moveTo, err)
		}
		return remove(client, set)
	case actionDelete:
		return remove(client, set)
	default:
		return fmt.Errorf("imap: unknown action %q", action)
	}
}

// remove deletes exactly the messages in set: flag them \Deleted, then UID
// EXPUNGE them.
func remove(client *imapclient.Client, set imap.UIDSet) error {
	if err := storeFlag(client, set, imap.FlagDeleted); err != nil {
		return err
	}
	if err := client.UIDExpunge(set).Close(); err != nil {
		return fmt.Errorf("imap: expunge: %w", err)
	}
	return nil
}

// checkAction reports whether the server can carry out action without touching
// other messages, so a poll or fetch fails before any flow runs rather than
// after — a source whose action then failed would read the same mail again and
// run its flows twice. Removing messages needs UID EXPUNGE (UIDPLUS, part of
// IMAP4rev2); a plain EXPUNGE would also remove whatever else is flagged
// \Deleted. A move needs MOVE, or UID EXPUNGE for the copy-and-remove fallback.
func checkAction(client *imapclient.Client, action string) error {
	caps := client.Caps()
	targeted := caps.Has(imap.CapUIDPlus)
	switch {
	case action == actionDelete && !targeted:
		return errors.New("imap: the server lacks UIDPLUS, so delete cannot remove only the handled messages; " +
			"use seen or move")
	case action == actionMove && !caps.Has(imap.CapMove) && !targeted:
		return errors.New("imap: the server supports neither MOVE nor UIDPLUS, so move cannot remove only the " +
			"handled messages; use seen")
	}
	return nil
}

func storeFlag(client *imapclient.Client, set imap.UIDSet, flag imap.Flag) error {
	err := client.Store(set, &imap.StoreFlags{
		Op:     imap.StoreFlagsAdd,
		Silent: true,
		Flags:  []imap.Flag{flag},
	}, nil).Close()
	if err != nil {
		return fmt.Errorf("imap: flag %s: %w", flag, err)
	}
	return nil
}

// validateAction checks an IMAP action setting, resolving the empty value to
// fallback, and requires a target mailbox for move.
func validateAction(owner, field, action, fallback, moveTo string) (string, error) {
	if action == "" {
		action = fallback
	}
	switch action {
	case actionNone, actionSeen, actionDelete:
		return action, nil
	case actionMove:
		if strings.TrimSpace(moveTo) == "" {
			return "", fmt.Errorf("%s: %s move requires a \"moveTo\" mailbox", owner, field)
		}
		return action, nil
	default:
		return "", fmt.Errorf("%s: unknown %s %q (want none, seen, move or delete)", owner, field, action)
	}
}

// imapBody parses a downloaded message into the body shape, adding its UID and
// mailbox.
func imapBody(m imapMessage, mailbox string) (map[string]any, error) {
	body, err := parseMessage(m.raw)
	if err != nil {
		return nil, fmt.Errorf("imap: message %d in %q: %w", m.uid, mailbox, err)
	}
	body[keyUID] = int64(m.uid)
	body[keyMailbox] = mailbox
	return body, nil
}
