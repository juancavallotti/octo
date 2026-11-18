// This file holds the small settings types the email connectors and blocks
// decode, and binding a block to its connector.
package email

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
)

// duration decodes either a Go duration string ("5s") or a numeric nanosecond
// count from settings, since settings round-trip through JSON.
type duration time.Duration

// UnmarshalJSON parses a duration from a quoted string ("250ms") or a number.
func (d *duration) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == "" {
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		parsed, err := time.ParseDuration(strings.Trim(s, `"`))
		if err != nil {
			return fmt.Errorf("parse duration: %w", err)
		}
		*d = duration(parsed)
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("parse duration: %w", err)
	}
	*d = duration(n)
	return nil
}

// resolveConnector binds a block to the connector named name, which must be of
// the connector type T (the smtp, imap or pop3 connector).
//
//nolint:ireturn // T is always a concrete connector pointer, never an interface
func resolveConnector[T core.Connector](block, name string, deps core.BlockDeps) (T, error) {
	var zero T
	if strings.TrimSpace(name) == "" {
		return zero, fmt.Errorf("%s requires a \"connector\" setting", block)
	}
	if deps.Connector == nil {
		return zero, fmt.Errorf("%s: connector %q requested but no connectors are available", block, name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return zero, fmt.Errorf("%s: connector %q is not configured", block, name)
	}
	conn, ok := connector.(T)
	if !ok {
		return zero, fmt.Errorf("%s: connector %q is the wrong type for this block", block, name)
	}
	return conn, nil
}
