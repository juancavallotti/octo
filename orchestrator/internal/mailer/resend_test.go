package mailer

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// capture records what the fake provider received, so a test can assert on the
// request as well as the outcome.
type capture struct {
	auth        string
	contentType string
	body        sendRequest
	raw         []byte
}

// newFakeProvider stands up a provider that answers with status/body and records the
// request. It returns the client under test and the capture it fills in.
func newFakeProvider(t *testing.T, status int, body string) (*Resend, *capture) {
	t.Helper()
	got := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth = r.Header.Get("Authorization")
		got.contentType = r.Header.Get("Content-Type")
		got.raw, _ = io.ReadAll(r.Body)
		_ = json.Unmarshal(got.raw, &got.body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return newResend(srv.URL), got
}

func testMessage() Message {
	return Message{
		APIKey:  "re_testkey1234",
		From:    "Acme <notifications@acme.com>",
		ReplyTo: "support@acme.com",
		To:      []string{"someone@example.com"},
		Subject: "hello",
		Text:    "body text",
	}
}

func TestSendSuccess(t *testing.T) {
	client, got := newFakeProvider(t, http.StatusOK, `{"id":"3f8c-abc"}`)

	id, err := client.Send(context.Background(), testMessage())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if id != "3f8c-abc" {
		t.Fatalf("id = %q, want %q", id, "3f8c-abc")
	}
	if got.auth != "Bearer re_testkey1234" {
		t.Fatalf("Authorization = %q", got.auth)
	}
	if got.contentType != "application/json" {
		t.Fatalf("Content-Type = %q", got.contentType)
	}
	if got.body.From != "Acme <notifications@acme.com>" {
		t.Fatalf("from = %q", got.body.From)
	}
	if len(got.body.To) != 1 || got.body.To[0] != "someone@example.com" {
		t.Fatalf("to = %v", got.body.To)
	}
	if got.body.Subject != "hello" || got.body.Text != "body text" {
		t.Fatalf("subject/text = %q / %q", got.body.Subject, got.body.Text)
	}
	if got.body.ReplyTo != "support@acme.com" {
		t.Fatalf("reply_to = %q", got.body.ReplyTo)
	}
}

// The optional fields must be absent rather than empty when unset: the provider
// treats an empty html as a body, and an empty reply_to as an address.
func TestSendOmitsUnsetOptionalFields(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Message)
		absent  string
		present string
	}{
		{
			name:    "text only omits html",
			mutate:  func(m *Message) { m.HTML = "" },
			absent:  `"html"`,
			present: `"text"`,
		},
		{
			name:    "html only omits text",
			mutate:  func(m *Message) { m.Text = ""; m.HTML = "<p>hi</p>" },
			absent:  `"text"`,
			present: `"html"`,
		},
		{
			name:    "no reply-to omits reply_to",
			mutate:  func(m *Message) { m.ReplyTo = "" },
			absent:  `"reply_to"`,
			present: `"from"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, got := newFakeProvider(t, http.StatusOK, `{"id":"x"}`)
			m := testMessage()
			tc.mutate(&m)

			if _, err := client.Send(context.Background(), m); err != nil {
				t.Fatalf("Send: %v", err)
			}
			if strings.Contains(string(got.raw), tc.absent) {
				t.Fatalf("body contains %s: %s", tc.absent, got.raw)
			}
			if !strings.Contains(string(got.raw), tc.present) {
				t.Fatalf("body missing %s: %s", tc.present, got.raw)
			}
		})
	}
}

func TestSendMapsProviderFailures(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    error
		message string
	}{
		{
			name:    "unauthorized is an invalid key",
			status:  http.StatusUnauthorized,
			body:    `{"message":"API key is invalid","name":"validation_error"}`,
			want:    ErrInvalidAPIKey,
			message: "API key is invalid",
		},
		{
			name:    "forbidden is an invalid key",
			status:  http.StatusForbidden,
			body:    `{"message":"restricted","name":"validation_error"}`,
			want:    ErrInvalidAPIKey,
			message: "restricted",
		},
		{
			// The one that matters most: the provider names the unverified domain, and
			// that is the entire fix. Losing it would leave the operator guessing.
			name:    "unverified domain keeps the provider wording",
			status:  http.StatusUnprocessableEntity,
			body:    `{"message":"The acme.com domain is not verified","name":"validation_error"}`,
			want:    ErrRejected,
			message: "acme.com",
		},
		{
			name:    "server error is unavailable",
			status:  http.StatusInternalServerError,
			body:    `{"message":"boom"}`,
			want:    ErrUnavailable,
			message: "boom",
		},
		{
			name:    "non-json failure falls back to the status",
			status:  http.StatusBadRequest,
			body:    `not json at all`,
			want:    ErrRejected,
			message: "400",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := newFakeProvider(t, tc.status, tc.body)

			_, err := client.Send(context.Background(), testMessage())
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("err = %q, want it to mention %q", err, tc.message)
			}
		})
	}
}

// A 2xx whose body is not the expected shape is a provider problem, not a caller
// one — it must not surface as success with an empty id.
func TestSendRejectsUnparseableSuccessBody(t *testing.T) {
	client, _ := newFakeProvider(t, http.StatusOK, `<html>maintenance</html>`)

	if _, err := client.Send(context.Background(), testMessage()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestSendCancelledContextIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	client := newResend(srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if _, err := client.Send(ctx, testMessage()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}
