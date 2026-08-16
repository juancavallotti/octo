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
// a quote character at the edges would be stripped as a wrapper, and a backslash
// would be read as an escape.
//
// It is not the whole test. Parse trims with strings.TrimSpace, which is Unicode
// whitespace, so this ASCII set alone would write a value ending in a no-break
// space bare and let Parse eat it. quote checks the trim as well.
const needsQuoting = " \t\n\r#\"'\\"

// invalidInKey are the characters a name may not contain, because each one would
// make Parse read the line as something other than the assignment that was
// written: '=' moves where the name ends, whitespace makes a name Parse trims
// differently (and lets a name end in "export "), '#' can comment the line out,
// and a quote or newline breaks the line's shape outright.
//
// As with needsQuoting this covers the ASCII cases; checkKey tests the Unicode
// trim separately, and a name has no quoted form to survive one in.
const invalidInKey = "= \t\n\r#\"'"

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
// '#', or contains a quote or backslash.
//
// A value may hold anything, including newlines: every character Parse treats as
// structure is escaped rather than written literally, so one entry is always one
// physical line and a value can never introduce another assignment. A name is
// more constrained, because nothing can escape it — Format returns an error
// naming the offending key rather than writing a file that reads back as
// something else.
//
// It lives here rather than beside its callers for the reason the package exists:
// one set of quoting rules, written down once, so a file this tree produces is a
// file this tree can read.
func Format(values map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	for _, key := range keys {
		if err := checkKey(key); err != nil {
			return nil, err
		}
		buf.WriteString(key)
		buf.WriteByte('=')
		buf.WriteString(quote(values[key]))
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// checkKey rejects a name Parse would not read back as itself. Unlike a value, a
// name has no quoted form to hide in.
func checkKey(key string) error {
	if key == "" {
		return fmt.Errorf("variable name is empty")
	}
	if i := strings.IndexAny(key, invalidInKey); i >= 0 {
		return fmt.Errorf("variable name %q contains %q", key, key[i])
	}
	// Parse trims the name with strings.TrimSpace, which is Unicode whitespace and
	// wider than invalidInKey's ASCII set. A name that trims to something else
	// would come back as that something else.
	if strings.TrimSpace(key) != key {
		return fmt.Errorf("variable name %q starts or ends with whitespace", key)
	}
	return nil
}

// quote wraps a value in double quotes when writing it bare would not survive a
// round-trip through Parse, escaping every character that carries meaning inside
// the quotes. The line breaks matter most: Parse reads one physical line per
// entry, so a literal newline in a value would turn the rest of it into further
// assignments.
func quote(value string) string {
	if value != "" && !strings.ContainsAny(value, needsQuoting) && strings.TrimSpace(value) == value {
		return value
	}
	return `"` + valueEscaper.Replace(value) + `"`
}

// valueEscaper writes the escapes unescape resolves. Backslash goes first so the
// escapes introduced after it are not escaped a second time — Replacer scans the
// input once and never rewrites its own output, which is why this is one Replacer
// rather than a chain of ReplaceAll calls.
var valueEscaper = strings.NewReplacer(
	`\`, `\\`,
	`"`, `\"`,
	"\n", `\n`,
	"\r", `\r`,
	"\t", `\t`,
)

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

// unescape resolves backslash escapes in a double-quoted value, the inverse of
// what quote writes. \n, \r and \t produce their control character, so a value
// may span lines while its entry stays on one; any other escaped character
// yields itself, which covers \" and \\ and leaves a stray backslash harmless.
func unescape(value string) string {
	if !strings.Contains(value, `\`) {
		return value
	}
	var buf strings.Builder
	buf.Grow(len(value))
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' || i+1 >= len(value) {
			buf.WriteByte(value[i])
			continue
		}
		i++
		switch value[i] {
		case 'n':
			buf.WriteByte('\n')
		case 'r':
			buf.WriteByte('\r')
		case 't':
			buf.WriteByte('\t')
		default:
			buf.WriteByte(value[i])
		}
	}
	return buf.String()
}
