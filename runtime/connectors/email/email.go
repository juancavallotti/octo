// Package email connects flows to mail servers. It registers one connector per
// protocol: smtp sends mail through the smtp-send block, while imap and pop3
// receive it — each through a polling source that runs every new message through
// a flow, and through a fetch block that retrieves messages when a flow asks.
//
// Every connector speaks its protocol in clear text, upgraded with STARTTLS, or
// over implicit TLS, chosen by its security setting. A received message reaches
// a flow as the JSON shape parse.go builds; attachments carry their bytes base64
// encoded, in the same part shape the multipart CEL functions use, so a received
// attachment can be handed to smtp-send unchanged.
package email

import (
	"github.com/juancavallotti/octo/runtime/core"
)

// displayName is the palette group the email connectors and blocks share.
const displayName = "Email"

// init is this module's manifest: the one place that says what importing this
// package puts into the runtime, in a deterministic order. Each connector and
// block's own registration lives beside it as a registerX function called here.
func init() {
	// Package-level editor defaults: every email connector and block shares the
	// Email palette group and icon unless it sets its own.
	core.RegisterExtension(core.ExtensionMeta{Group: displayName, Icon: "Mail"})

	registerSMTP()
	registerSMTPSend()
	registerIMAP()
	registerIMAPFetch()
}
