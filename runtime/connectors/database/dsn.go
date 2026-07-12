package database

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

const (
	// driverSQLite has no notion of a password, so supplying one is a configuration
	// mistake rather than a no-op.
	driverSQLite = "sqlite"
	// passwordKey is the Postgres keyword/value DSN key holding the password.
	passwordKey = "password"
)

// withPassword merges a separately-configured password into the DSN, so only the
// password has to be a secret and the DSN (host, user, database, sslmode) can live
// in plain config. An empty password leaves the DSN untouched, which is the path
// every DSN-with-inline-credentials config already takes.
//
// The password wins over one already embedded in the DSN, with a warning: two
// sources for one credential is a mistake worth surfacing, but failing to start is
// the harsher answer to a config that still says unambiguously which value is
// meant to be live.
func withPassword(driver, dsn, password, connector string) (string, error) {
	if password == "" {
		return dsn, nil
	}
	if driver == driverSQLite {
		return "", fmt.Errorf("driver %q takes no password; put the file path in dsn", driverSQLite)
	}

	if isURLDSN(dsn) {
		return urlWithPassword(dsn, password, connector)
	}
	return keywordsWithPassword(dsn, password, connector), nil
}

// isURLDSN reports whether the DSN is the URL form (postgres://user@host/db)
// rather than the keyword form (host=... user=...). pgx accepts both.
func isURLDSN(dsn string) bool {
	return strings.Contains(dsn, "://")
}

// urlWithPassword sets the password on a URL-form DSN, letting net/url escape it,
// so a password containing "@", "/" or ":" needs no hand-encoding in config.
func urlWithPassword(dsn, password, connector string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse dsn: %w", err)
	}
	if _, hadPassword := parsed.User.Password(); hadPassword {
		warnOverride(connector)
	}
	parsed.User = url.UserPassword(parsed.User.Username(), password)
	return parsed.String(), nil
}

// keywordsWithPassword replaces the password key of a keyword/value DSN, or
// appends it when there is none.
func keywordsWithPassword(dsn, password, connector string) string {
	fields := strings.Fields(dsn)
	kept := make([]string, 0, len(fields)+1)
	for _, field := range fields {
		if strings.HasPrefix(field, passwordKey+"=") {
			warnOverride(connector)
			continue
		}
		kept = append(kept, field)
	}
	return strings.Join(append(kept, passwordKey+"="+quoteKeywordValue(password)), " ")
}

// quoteKeywordValue renders a keyword/value DSN value: single-quoted, with
// backslashes and quotes escaped, so a password with spaces or quotes survives.
func quoteKeywordValue(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(value)
	return "'" + escaped + "'"
}

// warnOverride reports a DSN that already carries a password alongside the
// separately-configured one, naming which of the two the connector uses.
func warnOverride(connector string) {
	slog.Warn("database connector: dsn already carries a password; the password setting overrides it",
		"connector", connector)
}
