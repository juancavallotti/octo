package core

import (
	"fmt"
	"log/slog"
	"strings"
)

// ParseLevel maps a level name to an slog.Level. It accepts debug, info, warn
// (or warning), and error, case-insensitively, and defaults to info when the
// name is empty. The runtime standardizes structured logging on slog, so both
// the logger connector and the log block resolve levels through here.
func ParseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("log level %q is not one of debug/info/warn/error", name)
	}
}
