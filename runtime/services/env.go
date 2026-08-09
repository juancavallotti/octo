package services

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// A hosted service configures itself from `octo run` flags, and every one of
// those flags takes its default from an environment variable of the same meaning
// (see HostedService.Flags). These are the readers for that: resolving the
// environment into the flag's default is what gives "the flag wins when passed"
// for free, and what makes `--help` print the value actually in effect.
//
// They read the process environment only. A runtime's .env file is loaded during
// config load, which happens after the flags are parsed.
//
// An unparseable value is logged and ignored rather than failing the run: a typo
// in a deployment's environment should not be the reason a runtime will not
// start, when carrying on with the documented default is both safe and obvious.

// EnvString returns the environment variable's value, or fallback when it is
// unset or empty.
func EnvString(name, fallback string) string {
	if v, ok := lookup(name); ok {
		return v
	}
	return fallback
}

// EnvBool parses a boolean environment variable, accepting what strconv does
// (1/t/T/true/TRUE, 0/f/F/false/FALSE) plus the conversational on/off/yes/no,
// case-insensitively and ignoring surrounding whitespace.
func EnvBool(name string, fallback bool) bool {
	raw, ok := lookup(name)
	if !ok {
		return fallback
	}
	trimmed := strings.TrimSpace(raw)
	switch strings.ToLower(trimmed) {
	case "on", "yes":
		return true
	case "off", "no":
		return false
	}
	v, err := strconv.ParseBool(trimmed)
	if err != nil {
		warnUnparseable(name, raw, fallback)
		return fallback
	}
	return v
}

// EnvInt parses an integer environment variable.
func EnvInt(name string, fallback int) int {
	raw, ok := lookup(name)
	if !ok {
		return fallback
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		warnUnparseable(name, raw, fallback)
		return fallback
	}
	return v
}

// lookup reads the variable, treating empty as unset: a variable set to nothing
// is how a deployment template says "I have no opinion", not "use the zero value".
func lookup(name string) (string, bool) {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// warnUnparseable reports a value that could not be read, naming the default
// used instead so the log line says what actually happened.
func warnUnparseable(name, raw string, fallback any) {
	slog.Warn("services: ignoring unparseable environment variable",
		"var", name, "value", raw, "default", fallback)
}
