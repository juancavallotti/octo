package mongodb

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

// withPassword merges a separately-configured password into the connection
// string, so only the password has to be a secret and the URI (host, user,
// database, replica set, TLS options) can live in plain config. An empty
// password leaves the URI untouched, which is the path every
// URI-with-inline-credentials config already takes.
//
// This is the database connector's dsn.go rule applied to MongoDB. There is no
// keyword-form branch to carry over: both mongodb:// and mongodb+srv:// are
// URLs, so net/url does the escaping and a password containing "@", "/" or ":"
// needs no hand-encoding in config.
//
// The password wins over one already embedded in the URI, with a warning: two
// sources for one credential is a mistake worth surfacing, but failing to start
// is the harsher answer to a config that still says unambiguously which value is
// meant to be live.
func withPassword(uri, password, connector string) (string, error) {
	if password == "" {
		return uri, nil
	}

	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("parse uri: %w", err)
	}
	if !strings.HasPrefix(parsed.Scheme, "mongodb") {
		return "", fmt.Errorf("uri scheme %q is not mongodb or mongodb+srv", parsed.Scheme)
	}
	// A password with no user to attach it to would produce mongodb://:secret@host,
	// which authenticates as nobody. Fail loudly rather than start a connector the
	// author believes is authenticated.
	username := parsed.User.Username()
	if username == "" {
		return "", fmt.Errorf("uri has no username; a password needs one")
	}
	if _, hadPassword := parsed.User.Password(); hadPassword {
		slog.Warn("mongodb connector: uri already carries a password; the password setting overrides it",
			"connector", connector)
	}
	parsed.User = url.UserPassword(username, password)
	return parsed.String(), nil
}
