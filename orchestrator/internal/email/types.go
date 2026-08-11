package email

import (
	"net/mail"
	"strings"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

const (
	// settingsKey is this feature's row in site_settings.
	settingsKey = "email"
	// providerResend is the only provider today. Stored as a discriminator so a
	// second one can be added without guessing what existing rows meant.
	providerResend = "resend"

	// maxNameLen bounds the display name, and maxSubjectLen the subject — the latter
	// is RFC 5322's line limit, which is the point at which a subject stops being a
	// subject.
	maxNameLen    = 200
	maxSubjectLen = 998
	// minAPIKeyLen is short enough to accept any real provider key and long enough
	// that the stored last4 is not most of the secret.
	minAPIKeyLen = 8
	// maxRecipients bounds one send. This is a transactional mailer for notifications
	// the platform itself raises; the cap is what keeps it from quietly becoming a
	// bulk sender.
	maxRecipients = 50
)

// stored is the jsonb shape in site_settings.value. Unexported: nothing outside this
// package sees the ciphertext.
type stored struct {
	Provider  string                   `json:"provider"`
	FromEmail string                   `json:"fromEmail"`
	FromName  string                   `json:"fromName,omitempty"`
	ReplyTo   string                   `json:"replyTo,omitempty"`
	APIKey    sitesettings.SecretField `json:"apiKey,omitzero"`
	UpdatedAt time.Time                `json:"updatedAt"`
}

// Settings is the read model. It carries no key material by construction — there is
// no field through which an accidental change could leak one.
type Settings struct {
	FromEmail  string
	FromName   string
	ReplyTo    string
	Configured bool
	Last4      string
	UpdatedAt  *time.Time
}

// Update is one save of the settings. APIKey is a pointer because "not supplied" and
// "supplied empty" are different instructions; see sitesettings.SecretField.Apply.
type Update struct {
	APIKey    *string
	FromEmail string
	FromName  string
	ReplyTo   string
}

// TestSend is the admin page's "send a test" request. It carries the whole draft
// rather than reading storage, so what is verified is what is on screen — including
// a key that has not been saved yet.
type TestSend struct {
	To        string
	APIKey    *string
	FromEmail string
	FromName  string
	ReplyTo   string
}

// SendRequest is one message from a platform-internal caller. It deliberately has no
// key or from-address field: the identity a send goes out under is defined once, in
// the stored settings, and is not something a caller gets to choose.
type SendRequest struct {
	To      []string
	Subject string
	Text    string
	HTML    string
}

// normalized returns the request with its header-bound fields trimmed. Validation
// accepts a padded address (it trims before parsing), so without this the provider
// would receive the untrimmed original and reject an address that passed our own
// check — a failure that would read as the provider's fault. The bodies are left
// alone: leading whitespace in a message body is content, not formatting.
func (r SendRequest) normalized() SendRequest {
	to := make([]string, len(r.To))
	for i, addr := range r.To {
		to[i] = strings.TrimSpace(addr)
	}
	r.To = to
	r.Subject = strings.TrimSpace(r.Subject)
	return r
}

// toSettings maps the stored row to the read model.
func (s stored) toSettings() Settings {
	out := Settings{
		FromEmail:  s.FromEmail,
		FromName:   s.FromName,
		ReplyTo:    s.ReplyTo,
		Configured: s.APIKey.Configured(),
		Last4:      s.APIKey.Last4,
	}
	if !s.UpdatedAt.IsZero() {
		t := s.UpdatedAt
		out.UpdatedAt = &t
	}
	return out
}

// from composes the envelope sender: "Name <address>" when a display name is set,
// the bare address otherwise.
func (s stored) from() string {
	if s.FromName == "" {
		return s.FromEmail
	}
	return (&mail.Address{Name: s.FromName, Address: s.FromEmail}).String()
}

// validAddress reports whether v is a bare email address. It requires the parsed
// address to equal the input, so "Acme <a@b.c>" is rejected: the display name belongs
// in fromName, and accepting it in both places is how the two disagree later.
func validAddress(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > 254 {
		return false
	}
	addr, err := mail.ParseAddress(v)
	return err == nil && addr.Address == v
}

// validHeaderText reports whether v is safe to place in a mail header: within len,
// and free of the carriage returns and newlines that split one header into two.
func validHeaderText(v string, maxLen int) bool {
	return len(v) <= maxLen && !strings.ContainsAny(v, "\r\n")
}

// validateUpdate checks a save before anything is written or encrypted.
func validateUpdate(u Update) error {
	if !validAddress(u.FromEmail) {
		return ErrInvalidFromEmail
	}
	if strings.TrimSpace(u.ReplyTo) != "" && !validAddress(u.ReplyTo) {
		return ErrInvalidReplyTo
	}
	if !validHeaderText(u.FromName, maxNameLen) {
		return ErrInvalidFromName
	}
	return validateSuppliedKey(u.APIKey)
}

// validateSuppliedKey checks a key the caller supplied. A nil or cleared key is not a
// key to validate — those mean "keep" and "remove".
func validateSuppliedKey(key *string) error {
	if key == nil {
		return nil
	}
	if trimmed := strings.TrimSpace(*key); trimmed != "" && len(trimmed) < minAPIKeyLen {
		return ErrInvalidAPIKey
	}
	return nil
}

// validateSendRequest checks a platform-internal send. Everything here is checked
// before the provider is touched, so a malformed call costs nothing.
func validateSendRequest(r SendRequest) error {
	if len(r.To) == 0 || len(r.To) > maxRecipients {
		return ErrInvalidRecipients
	}
	for _, to := range r.To {
		if !validAddress(to) {
			return ErrInvalidRecipients
		}
	}
	if strings.TrimSpace(r.Subject) == "" || !validHeaderText(r.Subject, maxSubjectLen) {
		return ErrInvalidSubject
	}
	if strings.TrimSpace(r.Text) == "" && strings.TrimSpace(r.HTML) == "" {
		return ErrEmptyBody
	}
	return nil
}
