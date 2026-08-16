// Package dotenv holds the single .env parser shared across the tree. octo core
// loads .env files through it, and dolphin resolves its --env-file with it, so
// there is exactly one set of quoting and comment rules for every tool to agree on.
package dotenv

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// needsQuoting are the characters that would change how Parse reads a value if it
// were written bare: whitespace and the comment marker end (or hide) the value,
// and a quote character at the edges would be stripped as a wrapper.
const needsQuoting = " \t\n\r#\"'"

// Parse parses the contents of a .env file into a name->value map. Each
// non-blank line is a KEY=VALUE assignment; blank lines and lines beginning with
// '#' are ignored, an optional leading "export " is dropped, surrounding whitespace
// is trimmed, and a value wrapped in matching single or double quotes is unquoted.
// A non-empty, non-comment line without '=' is a parse error so typos surface early.
func Parse(data []byte) (map[string]string, error) {
	values := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		raw = strings.TrimPrefix(raw, "export ")

		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, fmt.Errorf("parse .env line %d: missing '=' in %q", line, raw)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("parse .env line %d: empty variable name", line)
		}
		values[key] = unquote(strings.TrimSpace(value))
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read .env: %w", err)
	}
	return values, nil
}

// Format renders a name->value map as .env file content, the exact inverse of
// Parse: Parse(Format(m)) returns m. Keys are sorted so the output is
// deterministic, and a value is wrapped in double quotes only when writing it
// bare would not read back the same — when it is empty, holds whitespace or a
// '#', or starts and ends with a quote character.
//
// It lives here rather than beside its callers for the reason the package exists:
// one set of quoting rules, written down once, so a file this tree produces is a
// file this tree can read.
func Format(values map[string]string) []byte {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, key := range keys {
		buf.WriteString(key)
		buf.WriteByte('=')
		buf.WriteString(quote(values[key]))
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// quote wraps a value in double quotes when writing it bare would not survive a
// round-trip through Parse, escaping the backslash and quote characters that
// unquote would otherwise consume.
func quote(value string) string {
	if value != "" && !strings.ContainsAny(value, needsQuoting) && !strings.Contains(value, `\`) {
		return value
	}
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	return `"` + strings.ReplaceAll(escaped, `"`, `\"`) + `"`
}

// unquote strips a single pair of matching surrounding single or double quotes.
// Inside double quotes a backslash escapes the following character, so a value
// may contain the quote that wraps it; single quotes are literal throughout,
// which is the usual shell distinction and the reason to keep both forms.
func unquote(value string) string {
	if len(value) < 2 {
		return value
	}
	first := value[0]
	if first != '"' && first != '\'' {
		return value
	}
	if value[len(value)-1] != first {
		return value
	}
	inner := value[1 : len(value)-1]
	if first == '\'' {
		return inner
	}
	return unescape(inner)
}

// unescape resolves backslash escapes in a double-quoted value. A backslash
// before any character yields that character, so both \" and \\ round-trip what
// quote wrote; a trailing lone backslash is kept as itself.
func unescape(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	var buf strings.Builder
	buf.Grow(len(value))
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' && i+1 < len(value) {
			i++
			buf.WriteByte(value[i])
			continue
		}
		buf.WriteByte(value[i])
	}
	return buf.String()
}
