// This file provides the "smtp" connector: an SMTP server the smtp-send block
// submits mail through. The connector keeps no connection open — every send is
// its own session — so it holds only the resolved endpoint.
package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/smtp"
	"os"
	"reflect"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// smtpType is the name a flow declares the SMTP connector under.
const smtpType = "smtp"

// The SMTP ports: 25 is relay in clear text, 587 is submission upgraded with
// STARTTLS, and 465 is submission over implicit TLS.
const (
	smtpPlainPort    = 25
	smtpStartTLSPort = 587
	smtpTLSPort      = 465
)

// smtpProtocol submits on 587 with STARTTLS unless told otherwise.
var smtpProtocol = protocol{
	name:            smtpType,
	defaultSecurity: securityStartTLS,
	plainPort:       smtpPlainPort,
	startTLSPort:    smtpStartTLSPort,
	tlsPort:         smtpTLSPort,
}

func registerSMTP() {
	core.MustRegisterConnector(smtpType, func() core.Connector {
		return &SMTPConnector{}
	})
	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     smtpType,
		Label:    "SMTP",
		Icon:     iconSend,
		Settings: reflect.TypeFor[smtpSettings](),
	})
}

// smtpSettings is the smtp connector's configuration.
type smtpSettings struct {
	// SMTP server host name.
	Host string `json:"host" octo:"label=Host,required"`
	// Server port; defaults to 25, 587 or 465 for none, starttls and tls.
	Port int `json:"port" octo:"label=Port"`
	// How the connection is secured: none, starttls (upgrade before
	// authenticating), or tls (implicit TLS).
	Security string `json:"security" octo:"label=Security,type=enum,enum=none|starttls|tls,default=starttls"`
	// Accept any server certificate. Disables verification; only for lab
	// servers with self-signed certificates.
	TLSSkipVerify bool `json:"tlsSkipVerify" octo:"label=Skip TLS verification"`
	// User to authenticate as; leave empty for a server that needs no login.
	Username string `json:"username" octo:"label=Username"`
	// Password for the user, usually a ${ENV} reference.
	Password string `json:"password" octo:"label=Password"`
	// Bounds one send, from dial to goodbye.
	Timeout duration `json:"timeout" octo:"label=Timeout,type=string,default=60s"`
	// Host name the connector introduces itself with (EHLO); defaults to this
	// machine's host name. Servers receiving on port 25 usually reject a name
	// that is not fully qualified.
	HeloName string `json:"heloName" octo:"label=HELO name"`
}

// SMTPConnector submits mail to an SMTP server. It is safe for concurrent use:
// each send dials its own session and the endpoint is read-only after Start.
type SMTPConnector struct {
	ep   endpoint
	helo string
}

// Start decodes and validates the settings.
func (c *SMTPConnector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set smtpSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	ep, err := newEndpoint(smtpProtocol, serverSettings{
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
	c.helo = heloName(set.HeloName)
	return nil
}

// Stop releases nothing: no connection outlives a send.
func (c *SMTPConnector) Stop(context.Context) error { return nil }

// Send submits one message in its own session: it dials, upgrades with STARTTLS
// when configured, authenticates when a username is set, and hands the server
// the envelope and the already-formatted message.
//
// net/smtp's PLAIN auth refuses to send credentials over an unencrypted
// connection to anything but localhost, so security none with a username only
// works against a local relay — the protection is kept on purpose.
func (c *SMTPConnector) Send(ctx context.Context, from string, recipients []string, data []byte) error {
	if len(recipients) == 0 {
		return errors.New("smtp: no recipients")
	}
	conn, err := c.ep.dial(ctx)
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, c.ep.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp: greeting from %s: %w", c.ep.addr(), err)
	}
	defer func() { _ = client.Close() }()

	// net/smtp introduces itself as "localhost" unless told otherwise, which a
	// server checking HELO names refuses.
	if err := client.Hello(c.helo); err != nil {
		return fmt.Errorf("smtp: EHLO %s: %w", c.helo, err)
	}
	if err := c.prepare(client); err != nil {
		return err
	}
	if err := c.submit(client, from, recipients, data); err != nil {
		return err
	}
	// The message is the server's once DATA is accepted; a connection lost on
	// the way out does not undo that, and reporting it as a failure would invite
	// sending the message again.
	if err := client.Quit(); err != nil {
		slog.Debug("smtp: quit after a successful send failed", "server", c.ep.addr(), "error", err)
	}
	return nil
}

// heloName resolves the EHLO name: the setting, else this machine's host name,
// else localhost as net/smtp would have used.
func heloName(setting string) string {
	if name := strings.TrimSpace(setting); name != "" {
		return name
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return "localhost"
}

// prepare upgrades the session and authenticates, as the endpoint asks.
func (c *SMTPConnector) prepare(client *smtp.Client) error {
	if c.ep.security == securityStartTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("smtp: %s does not offer STARTTLS", c.ep.addr())
		}
		if err := client.StartTLS(c.ep.tls); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}
	if c.ep.username == "" {
		return nil
	}
	if err := client.Auth(smtp.PlainAuth("", c.ep.username, c.ep.password, c.ep.host)); err != nil {
		return fmt.Errorf("smtp: authenticate as %q: %w", c.ep.username, err)
	}
	return nil
}

// submit sends the envelope and the message body.
func (c *SMTPConnector) submit(client *smtp.Client, from string, recipients []string, data []byte) error {
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("smtp: MAIL FROM %q: %w", from, err)
	}
	for _, rcpt := range recipients {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("smtp: RCPT TO %q: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp: DATA: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		_ = w.Close()
		return fmt.Errorf("smtp: write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp: message rejected: %w", err)
	}
	return nil
}
