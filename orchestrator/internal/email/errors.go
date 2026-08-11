package email

import "errors"

var (
	// ErrInvalidFromEmail is returned when the from address is missing or is not a
	// bare address (a display name belongs in fromName).
	ErrInvalidFromEmail = errors.New("invalid from address")
	// ErrInvalidReplyTo is returned when a supplied reply-to is not an address.
	ErrInvalidReplyTo = errors.New("invalid reply-to address")
	// ErrInvalidFromName is returned when the display name is too long or contains
	// a line break, which would split the header it lands in.
	ErrInvalidFromName = errors.New("invalid from name")
	// ErrInvalidAPIKey is returned when a supplied key is too short to be real.
	ErrInvalidAPIKey = errors.New("invalid api key")
	// ErrInvalidRecipient is returned when a test send's recipient is not an address.
	ErrInvalidRecipient = errors.New("invalid recipient address")
	// ErrInvalidRecipients is returned when a send has no recipients, too many, or
	// one that is not an address.
	ErrInvalidRecipients = errors.New("invalid recipients")
	// ErrInvalidSubject is returned when a subject is empty, too long, or contains a
	// line break.
	ErrInvalidSubject = errors.New("invalid subject")
	// ErrEmptyBody is returned when a send supplies neither a text nor an HTML body.
	ErrEmptyBody = errors.New("message body is empty")
)
