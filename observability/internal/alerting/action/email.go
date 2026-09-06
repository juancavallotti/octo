package action

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

const (
	// sendTimeout bounds one send. Matched to the orchestrator handler's own
	// budget, so neither side gives up on the other.
	sendTimeout = 20 * time.Second

	// maxRecipients mirrors the orchestrator's cap. Checked here as well so a
	// watch with too many recipients is refused at the form rather than at the
	// moment it fires.
	maxRecipients = 50

	// sendPath is the orchestrator's platform-internal send. Deliberately not a
	// provider API: the Resend key lives encrypted in site_settings and is
	// decrypted by the one service that owns it, so this process holds no
	// credential at all and a compromise of it leaks no way to send mail as this
	// installation.
	sendPath = "/email/send"
)

// Mailer sends through the orchestrator.
type Mailer struct {
	baseURL string
	client  *http.Client
}

// NewMailer returns a mailer against the orchestrator at baseURL. An empty
// address is a Mailer that cannot send, which is what a process with no
// ORCHESTRATOR_URL has.
func NewMailer(baseURL string) *Mailer {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}
	return &Mailer{baseURL: baseURL, client: &http.Client{Timeout: sendTimeout}}
}

// EmailParams names who hears about a watch.
type EmailParams struct {
	To []string `json:"to"`
}

type emailAction struct {
	params EmailParams
	mail   *Mailer
}

func newEmailAction(spec alerting.ActionSpec, mailer *Mailer) (Deliverer, error) {
	var p EmailParams
	if err := decodeParams(spec.Params, &p); err != nil {
		return nil, err
	}
	if len(p.To) == 0 {
		return nil, fmt.Errorf("action: %w: an email action needs a recipient", alerting.ErrInvalidParams)
	}
	if len(p.To) > maxRecipients {
		return nil, fmt.Errorf("action: %w: %d recipients exceeds the limit of %d",
			alerting.ErrInvalidParams, len(p.To), maxRecipients)
	}
	for _, address := range p.To {
		if _, err := mail.ParseAddress(strings.TrimSpace(address)); err != nil {
			return nil, fmt.Errorf("action: %w: %q is not an address", alerting.ErrInvalidParams, address)
		}
	}
	return &emailAction{params: p, mail: mailer}, nil
}

// sendRequest mirrors the orchestrator's sendRequestBody. There is no from or
// apiKey field on purpose: the identity a send goes out under is defined once, in
// the stored settings, and that is what stops this being a relay.
type sendRequest struct {
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

// Deliver posts the notification to the orchestrator.
func (a *emailAction) Deliver(ctx context.Context, n alerting.Notification) error {
	body, err := json.Marshal(sendRequest{
		To:      a.params.To,
		Subject: subjectFor(n),
		Text:    bodyFor(n),
	})
	if err != nil {
		return fmt.Errorf("action: encode an alert email: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.mail.baseURL+sendPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("action: build an alert email request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := a.mail.client.Do(req)
	if err != nil {
		return fmt.Errorf("action: send an alert email: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= http.StatusBadRequest {
		// The orchestrator's own reason, carried through. "Email is not
		// configured" and "the provider was unreachable" want quite different
		// things done about them, and a bare status code says neither.
		return fmt.Errorf("action: send an alert email: %s: %s", res.Status, readReason(res))
	}
	return nil
}

// readReason pulls the error envelope out of a failed response, falling back to
// nothing rather than to a wall of HTML.
func readReason(res *http.Response) string {
	var envelope struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&envelope); err != nil {
		return ""
	}
	return envelope.Error
}

// subjectFor is what lands in an inbox, and it is written to be readable in a
// notification list where only the first few words survive.
func subjectFor(n alerting.Notification) string {
	prefix := "[octo]"
	if n.Severity != "" && !n.Resolved() {
		prefix = "[octo " + n.Severity + "]"
	}
	return prefix + " " + n.Headline()
}

// bodyFor renders the outcomes as the sentence the incident page renders, so
// somebody reading the mail and somebody reading the UI are reading the same
// account of what happened.
func bodyFor(n alerting.Notification) string {
	var b strings.Builder
	b.WriteString(n.Headline() + ".\n\n")
	fmt.Fprintf(&b, "%d of %d conditions matched (%s).\n", n.Matched, n.Total, n.Combinator)
	if n.Degraded {
		b.WriteString("At least one condition could not be evaluated.\n")
	}
	b.WriteString("\n")
	for _, o := range n.Outcomes {
		fmt.Fprintf(&b, "%s %s\n", mark(o), o.Label)
		fmt.Fprintf(&b, "    observed %s against a threshold of %s%s\n",
			number(o.Observed), plain(o.Threshold), baselineOf(o))
		if o.Reason != "" {
			fmt.Fprintf(&b, "    (%s)\n", o.Reason)
		}
	}
	if !n.WindowFrom.IsZero() {
		fmt.Fprintf(&b, "\nOver %s to %s.\n",
			n.WindowFrom.UTC().Format(time.RFC3339), n.WindowTo.UTC().Format(time.RFC3339))
	}
	return b.String()
}

func mark(o alerting.Outcome) string {
	switch o.Truth {
	case "true":
		return "[x]"
	case "false":
		return "[ ]"
	default:
		return "[?]"
	}
}

func baselineOf(o alerting.Outcome) string {
	if o.Baseline == nil {
		return ""
	}
	return ", against a baseline of " + plain(*o.Baseline)
}

func number(v *float64) string {
	if v == nil {
		return "nothing"
	}
	return plain(*v)
}

func plain(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.4f", v), "0"), ".")
}
