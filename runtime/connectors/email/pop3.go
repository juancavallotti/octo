// This file provides the "pop3" connector and a POP3 client small enough to
// own: USER/PASS, UIDL, RETR, DELE, QUIT and STLS are all a mail drop needs.
// POP3 has no flags and no folders — a message is either left on the server or
// deleted — and deletions only take effect when a session ends with QUIT, so a
// session that fails part-way leaves the mailbox exactly as it found it.
//
// Message numbers are only valid within one session; a message is named across
// sessions by its UIDL, which is what the source records and acts on.
package email

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

const (
	pop3Type = "pop3"
	// keyUIDL is added to each parsed POP3 message: the id that names it on the
	// server across sessions.
	keyUIDL = "uidl"
)

// The POP3 ports: 110 for clear text and STLS, 995 for implicit TLS.
const (
	pop3PlainPort = 110
	pop3TLSPort   = 995
)

// pop3Protocol reads over implicit TLS unless told otherwise.
var pop3Protocol = protocol{
	name:            pop3Type,
	defaultSecurity: securityTLS,
	plainPort:       pop3PlainPort,
	startTLSPort:    pop3PlainPort,
	tlsPort:         pop3TLSPort,
}

func registerPOP3() {
	core.MustRegisterConnector(pop3Type, func() core.Connector {
		return &POP3Connector{}
	})
	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     pop3Type,
		Label:    "POP3",
		Icon:     iconInbox,
		Settings: reflect.TypeFor[pop3Settings](),
		Sources: []core.SourceMeta{{
			Type:     pop3Type,
			Label:    "POP3 Mail Drop",
			Icon:     iconInbox,
			Settings: reflect.TypeFor[pop3SourceSettings](),
		}},
	})
}

// pop3Settings is the pop3 connector's configuration.
type pop3Settings struct {
	// POP3 server host name.
	Host string `json:"host" octo:"label=Host,required"`
	// Server port; defaults to 110 for none and starttls, 995 for tls.
	Port int `json:"port" octo:"label=Port"`
	// How the connection is secured: none, starttls (upgrade with STLS before
	// logging in), or tls (implicit TLS).
	Security string `json:"security" octo:"label=Security,type=enum,enum=none|starttls|tls,default=tls"`
	// Accept any server certificate. Disables verification; only for lab
	// servers with self-signed certificates.
	TLSSkipVerify bool `json:"tlsSkipVerify" octo:"label=Skip TLS verification"`
	// User to log in as.
	Username string `json:"username" octo:"label=Username,required"`
	// Password for the user, usually a ${ENV} reference.
	Password string `json:"password" octo:"label=Password"`
	// Bounds one session: a whole poll or fetch, from dial to QUIT.
	Timeout duration `json:"timeout" octo:"label=Timeout,type=string,default=60s"`
}

// POP3Connector reads a mail drop on a POP3 server. It is safe for concurrent
// use: every session dials its own connection and the endpoint is read-only
// after Start. Flows bound to it receive mail through its pop3 source.
type POP3Connector struct {
	ep      endpoint
	pending *pendingFlows
	claims  sourceClaims
}

// Start decodes and validates the settings and starts correlating flow outcomes
// for the connector's sources.
func (c *POP3Connector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set pop3Settings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.Username) == "" {
		return errors.New("pop3 connector requires a \"username\" setting")
	}
	ep, err := newEndpoint(pop3Protocol, serverSettings{
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
func (c *POP3Connector) Stop(context.Context) error {
	if c.pending != nil {
		c.pending.close()
	}
	return nil
}

// session runs fn on a logged-in client and ends with QUIT, which commits any
// deletions fn made. When fn fails the connection is dropped without QUIT, so
// the server discards them.
func (c *POP3Connector) session(ctx context.Context, fn func(*pop3Client) error) error {
	client, err := c.login(ctx)
	if err != nil {
		return err
	}
	if err := fn(client); err != nil {
		_ = client.close()
		return err
	}
	if err := client.cmd("QUIT"); err != nil {
		_ = client.close()
		return fmt.Errorf("pop3: quit: %w", err)
	}
	return client.close()
}

// login dials, upgrades with STLS when configured, and authenticates.
func (c *POP3Connector) login(ctx context.Context) (*pop3Client, error) {
	conn, err := c.ep.dial(ctx)
	if err != nil {
		return nil, err
	}
	client := &pop3Client{conn: conn, tp: textproto.NewConn(conn)}
	if err := client.response(); err != nil {
		_ = client.close()
		return nil, fmt.Errorf("pop3: greeting from %s: %w", c.ep.addr(), err)
	}
	if c.ep.security == securityStartTLS {
		if err := client.startTLS(ctx, c.ep.tls); err != nil {
			_ = client.close()
			return nil, err
		}
	}
	if err := client.cmd("USER %s", c.ep.username); err != nil {
		_ = client.close()
		return nil, fmt.Errorf("pop3: log in as %q: %w", c.ep.username, err)
	}
	if err := client.cmd("PASS %s", c.ep.password); err != nil {
		_ = client.close()
		return nil, fmt.Errorf("pop3: log in as %q: %w", c.ep.username, err)
	}
	return client, nil
}

// pop3Client speaks POP3 over one connection.
type pop3Client struct {
	conn net.Conn
	tp   *textproto.Conn
}

// pop3Entry is one message in a session's listing.
type pop3Entry struct {
	num  int
	uidl string
}

// response reads a status line: nil for +OK, or an error carrying the text
// after -ERR.
func (c *pop3Client) response() error {
	line, err := c.tp.ReadLine()
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if strings.HasPrefix(line, "+OK") {
		return nil
	}
	return fmt.Errorf("server said %q", strings.TrimSpace(strings.TrimPrefix(line, "-ERR")))
}

// cmd sends a command and reads its status line.
func (c *pop3Client) cmd(format string, args ...any) error {
	if err := c.tp.PrintfLine(format, args...); err != nil {
		return fmt.Errorf("send command: %w", err)
	}
	return c.response()
}

// startTLS upgrades the connection with STLS.
func (c *pop3Client) startTLS(ctx context.Context, cfg *tls.Config) error {
	if err := c.cmd("STLS"); err != nil {
		return fmt.Errorf("pop3: STLS: %w", err)
	}
	tlsConn := tls.Client(c.conn, cfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("pop3: tls handshake: %w", err)
	}
	c.conn = tlsConn
	c.tp = textproto.NewConn(tlsConn)
	return nil
}

// list returns the messages in the drop with their UIDLs, in message-number
// order — the order they arrived in.
func (c *pop3Client) list() ([]pop3Entry, error) {
	if err := c.cmd("UIDL"); err != nil {
		return nil, fmt.Errorf("pop3: UIDL: %w", err)
	}
	lines, err := c.tp.ReadDotLines()
	if err != nil {
		return nil, fmt.Errorf("pop3: UIDL: %w", err)
	}
	entries := make([]pop3Entry, 0, len(lines))
	for _, line := range lines {
		numStr, uidl, ok := strings.Cut(strings.TrimSpace(line), " ")
		num, err := strconv.Atoi(numStr)
		if !ok || err != nil {
			return nil, fmt.Errorf("pop3: malformed UIDL line %q", line)
		}
		entries = append(entries, pop3Entry{num: num, uidl: strings.TrimSpace(uidl)})
	}
	slices.SortFunc(entries, func(a, b pop3Entry) int { return a.num - b.num })
	return entries, nil
}

// retrieve downloads message num.
func (c *pop3Client) retrieve(num int) ([]byte, error) {
	if err := c.cmd("RETR %d", num); err != nil {
		return nil, fmt.Errorf("pop3: RETR %d: %w", num, err)
	}
	raw, err := io.ReadAll(c.tp.DotReader())
	if err != nil {
		return nil, fmt.Errorf("pop3: RETR %d: %w", num, err)
	}
	return raw, nil
}

// remove marks message num for deletion when the session QUITs.
func (c *pop3Client) remove(num int) error {
	if err := c.cmd("DELE %d", num); err != nil {
		return fmt.Errorf("pop3: DELE %d: %w", num, err)
	}
	return nil
}

func (c *pop3Client) close() error {
	if err := c.tp.Close(); err != nil {
		return fmt.Errorf("pop3: close: %w", err)
	}
	return nil
}

// pop3Message is one downloaded message.
type pop3Message struct {
	uidl string
	raw  []byte
}

// pop3Query says which messages of the drop to read.
type pop3Query struct {
	// skip excludes messages by UIDL; nil excludes none.
	skip func(uidl string) bool
	// limit caps how many messages are read; zero reads every match.
	limit int
	// newest takes the latest messages when limit cuts the matches; otherwise
	// the earliest.
	newest bool
}

// readDrop lists the drop and downloads the matching messages, returning the
// full listing alongside them so a caller can reconcile what it recorded.
func readDrop(client *pop3Client, q pop3Query) ([]pop3Entry, []pop3Message, error) {
	entries, err := client.list()
	if err != nil {
		return nil, nil, err
	}
	picked := make([]pop3Entry, 0, len(entries))
	for _, e := range entries {
		if q.skip == nil || !q.skip(e.uidl) {
			picked = append(picked, e)
		}
	}
	if q.limit > 0 && len(picked) > q.limit {
		if q.newest {
			picked = picked[len(picked)-q.limit:]
		} else {
			picked = picked[:q.limit]
		}
	}
	messages := make([]pop3Message, 0, len(picked))
	for _, e := range picked {
		raw, err := client.retrieve(e.num)
		if err != nil {
			return nil, nil, err
		}
		messages = append(messages, pop3Message{uidl: e.uidl, raw: raw})
	}
	return entries, messages, nil
}

// removeByUIDL marks the messages with the given UIDLs for deletion; a UIDL no
// longer in the drop is skipped, since someone else already removed it.
func removeByUIDL(client *pop3Client, uidls []string) error {
	entries, err := client.list()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if slices.Contains(uidls, e.uidl) {
			if err := client.remove(e.num); err != nil {
				return err
			}
		}
	}
	return nil
}

// validatePOP3Action checks a POP3 action setting, resolving the empty value
// to keep.
func validatePOP3Action(owner, field, action string) (string, error) {
	switch action {
	case "", actionKeep:
		return actionKeep, nil
	case actionDelete:
		return actionDelete, nil
	default:
		return "", fmt.Errorf("%s: unknown %s %q (want keep or delete)", owner, field, action)
	}
}

// pop3Body parses a downloaded message into the body shape, adding its UIDL.
func pop3Body(m pop3Message) (map[string]any, error) {
	body, err := parseMessage(m.raw)
	if err != nil {
		return nil, fmt.Errorf("pop3: message %q: %w", m.uidl, err)
	}
	body[keyUIDL] = m.uidl
	return body, nil
}
