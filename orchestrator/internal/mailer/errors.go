package mailer

import "errors"

var (
	// ErrInvalidAPIKey is a 401/403 from the provider: the key is wrong, revoked, or
	// belongs to another account. It is distinguished from the rest because it is the
	// one failure an operator fixes by pasting a different value into the form.
	ErrInvalidAPIKey = errors.New("email provider rejected the api key")
	// ErrRejected is any other 4xx — most often a from-address on a domain the
	// provider has not verified. The provider's own message is wrapped in, because it
	// names the domain and that is the whole of the fix.
	ErrRejected = errors.New("email provider rejected the message")
	// ErrUnavailable is a transport failure, a timeout, or a 5xx: nothing about the
	// request was necessarily wrong, and retrying later may work.
	ErrUnavailable = errors.New("email provider unavailable")
)
