package redisx

import "strings"

// redactURL removes url from err's message.
//
// go-redis puts the string it could not parse into its error, and that string may
// be a URL with a password in it — which would then reach the pod log, where it
// outlives the process and is readable by anyone who can read logs. Substituting
// a placeholder keeps the reason ("invalid port", "unsupported scheme") while
// dropping the value.
func redactURL(err error, url string) error {
	if url == "" || !strings.Contains(err.Error(), url) {
		return err
	}
	return redacted(strings.ReplaceAll(err.Error(), url, "<redis-url>"))
}

// redacted is an error whose message has already been rewritten. A distinct type
// so nothing unwraps back to the original and reprints it.
type redacted string

func (e redacted) Error() string { return string(e) }
