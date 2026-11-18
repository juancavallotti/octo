// This file holds what the three email connectors share about reaching a server:
// the security modes, the port each mode defaults to, and dialing a connection
// that is plain or wrapped in implicit TLS. STARTTLS is not handled here — each
// protocol negotiates the upgrade with its own command, so the protocol client
// performs it on the plain connection this file returns.
package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// The security modes a connector's "security" setting accepts. none speaks the
// protocol in clear text, starttls connects in clear text and upgrades before
// authenticating, and tls negotiates TLS before the protocol says anything.
const (
	securityNone     = "none"
	securityStartTLS = "starttls"
	securityTLS      = "tls"
)

// defaultTimeout bounds one server session — dial, authenticate, the work, and
// the goodbye — when a connector sets no timeout of its own.
const defaultTimeout = 60 * time.Second

// protocol describes what differs between the three servers when dialing: the
// name used in errors, the security mode applied when the setting is empty, and
// the well-known port for each mode.
type protocol struct {
	name            string
	defaultSecurity string
	plainPort       int
	startTLSPort    int
	tlsPort         int
}

// endpoint is a resolved server address with its security mode, credentials and
// session bound. It is built once in a connector's Start and read-only after, so
// concurrent sessions share it safely.
type endpoint struct {
	proto    protocol
	host     string
	port     int
	security string
	username string
	password string
	timeout  time.Duration
	tls      *tls.Config
}

// serverSettings is the part of every email connector's settings that says how
// to reach the server. Each connector declares its own settings struct — the
// schema needs per-protocol defaults — and hands its fields here.
type serverSettings struct {
	host          string
	port          int
	security      string
	tlsSkipVerify bool
	username      string
	password      string
	timeout       duration
}

// newEndpoint validates the server settings for proto and resolves the defaults:
// the security mode when none is set, and the port the mode is served on.
func newEndpoint(proto protocol, set serverSettings) (endpoint, error) {
	host := strings.TrimSpace(set.host)
	if host == "" {
		return endpoint{}, fmt.Errorf("%s connector requires a \"host\" setting", proto.name)
	}

	security := strings.ToLower(strings.TrimSpace(set.security))
	if security == "" {
		security = proto.defaultSecurity
	}
	port := set.port
	switch security {
	case securityNone:
		port = orDefaultPort(port, proto.plainPort)
	case securityStartTLS:
		port = orDefaultPort(port, proto.startTLSPort)
	case securityTLS:
		port = orDefaultPort(port, proto.tlsPort)
	default:
		return endpoint{}, fmt.Errorf("%s connector: unknown security %q (want none, starttls or tls)",
			proto.name, set.security)
	}

	timeout := time.Duration(set.timeout)
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	return endpoint{
		proto:    proto,
		host:     host,
		port:     port,
		security: security,
		username: set.username,
		password: set.password,
		timeout:  timeout,
		tls: &tls.Config{
			ServerName: host,
			MinVersion: tls.VersionTLS12,
			// Opt-in for lab servers with self-signed certificates; the setting
			// documents that it disables verification.
			InsecureSkipVerify: set.tlsSkipVerify, //nolint:gosec // explicit user setting
		},
	}, nil
}

func orDefaultPort(port, fallback int) int {
	if port > 0 {
		return port
	}
	return fallback
}

// addr is the host:port the endpoint dials.
func (e endpoint) addr() string {
	return net.JoinHostPort(e.host, strconv.Itoa(e.port))
}

// dial opens a connection to the server, wrapped in TLS when the security mode
// is implicit TLS. The whole session that follows is bounded by the endpoint's
// timeout, so a stalled server cannot hold the caller past it; the deadline is
// also cut short by ctx's own, and cancelling ctx closes the connection, so a
// stopping source or flow is not held for the rest of the timeout.
func (e endpoint) dial(ctx context.Context) (net.Conn, error) {
	deadline := time.Now().Add(e.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	dialer := net.Dialer{Deadline: deadline}
	conn, err := dialer.DialContext(ctx, "tcp", e.addr())
	if err != nil {
		return nil, fmt.Errorf("%s: dial %s: %w", e.proto.name, e.addr(), err)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s: set deadline: %w", e.proto.name, err)
	}
	conn = closeOnCancel(ctx, conn)
	if e.security != securityTLS {
		return conn, nil
	}

	tlsConn := tls.Client(conn, e.tls)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s: tls handshake with %s: %w", e.proto.name, e.addr(), err)
	}
	return tlsConn, nil
}

// cancelConn closes its connection when a context is cancelled, until the
// connection is closed normally.
type cancelConn struct {
	net.Conn
	stop func() bool
}

// closeOnCancel ties conn to ctx: cancelling ctx closes conn, which unblocks
// whatever read or write the session is parked on.
func closeOnCancel(ctx context.Context, conn net.Conn) net.Conn {
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	return &cancelConn{Conn: conn, stop: stop}
}

// Close stops watching the context and closes the connection.
func (c *cancelConn) Close() error {
	c.stop()
	return c.Conn.Close() //nolint:wrapcheck // a net.Conn's Close error is the caller's to read as is
}
