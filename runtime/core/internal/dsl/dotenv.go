package dsl

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

// ParseDotEnv parses the contents of a .env file into a name->value map. Each
// non-blank line is a KEY=VALUE assignment; blank lines and lines beginning with
// '#' are ignored, an optional leading "export " is dropped, surrounding whitespace
// is trimmed, and a value wrapped in matching single or double quotes is unquoted.
// A non-empty, non-comment line without '=' is a parse error so typos surface early.
func ParseDotEnv(data []byte) (map[string]string, error) {
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

// unquote strips a single pair of matching surrounding single or double quotes.
func unquote(value string) string {
	if len(value) >= 2 {
		if first := value[0]; first == '"' || first == '\'' {
			if value[len(value)-1] == first {
				return value[1 : len(value)-1]
			}
		}
	}
	return value
}
