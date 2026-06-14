// Package logger provides a connector that owns a configured slog logger. A log
// block binds to it by name and writes through its Logger(); the connector owns
// the output, opening a file on Start and closing it on Stop.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

// logFileMode is the permission applied to log files the connector creates.
// Owner read/write only, since logs can carry sensitive payload data.
const logFileMode = 0o600

func init() {
	core.MustRegisterConnector("logger", func() core.Connector {
		return &Connector{}
	})
}

// settings are the common slog knobs the logger exposes. Every field has a
// sensible default, so a logger connector can be declared with no settings.
type settings struct {
	// Output is "stdout" (default), "stderr", or a file path.
	Output string `json:"output"`
	// Format is "text" (default) or "json".
	Format string `json:"format"`
	// Level is the minimum level the logger emits (default info).
	Level string `json:"level"`
	// AddSource includes the source file:line on each record (default false).
	AddSource bool `json:"addSource"`
}

// Connector is a configured logger that flows' log blocks write through. When
// Output names a file it is opened on Start and closed on Stop.
type Connector struct {
	logger *slog.Logger
	file   *os.File
}

// Start parses the settings, opens the output, and builds the slog logger.
func (c *Connector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set settings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	level, err := core.ParseLevel(set.Level)
	if err != nil {
		return err
	}

	writer, file, err := openOutput(set.Output)
	if err != nil {
		return err
	}

	handler, err := newHandler(set.Format, writer, &slog.HandlerOptions{
		Level:     level,
		AddSource: set.AddSource,
	})
	if err != nil {
		if file != nil {
			_ = file.Close()
		}
		return err
	}

	c.logger = slog.New(handler)
	c.file = file
	return nil
}

// Stop closes the output file if the connector opened one.
func (c *Connector) Stop(context.Context) error {
	if c.file == nil {
		return nil
	}
	err := c.file.Close()
	c.file = nil
	if err != nil {
		return fmt.Errorf("close log output: %w", err)
	}
	return nil
}

// Logger returns the configured logger. It is the capability a log block binds
// to by referencing this connector by name.
func (c *Connector) Logger() (*slog.Logger, error) {
	if c.logger == nil {
		return nil, fmt.Errorf("logger connector not started")
	}
	return c.logger, nil
}

// openOutput resolves the output target to a writer. For a file path it opens
// (creating/appending) the file and returns it so Stop can close it; stdout and
// stderr need no cleanup.
func openOutput(output string) (io.Writer, *os.File, error) {
	switch output {
	case "", "stdout":
		return os.Stdout, nil, nil
	case "stderr":
		return os.Stderr, nil, nil
	default:
		//nolint:gosec // output is the operator-configured log destination from connector settings
		file, err := os.OpenFile(output, os.O_CREATE|os.O_APPEND|os.O_WRONLY, logFileMode)
		if err != nil {
			return nil, nil, fmt.Errorf("open log output %q: %w", output, err)
		}
		return file, file, nil
	}
}

// newHandler builds a text or json slog handler over w.
//
//nolint:ireturn // selecting between slog handler implementations
func newHandler(format string, w io.Writer, opts *slog.HandlerOptions) (slog.Handler, error) {
	switch format {
	case "", "text":
		return slog.NewTextHandler(w, opts), nil
	case "json":
		return slog.NewJSONHandler(w, opts), nil
	default:
		return nil, fmt.Errorf("log format %q is not one of text/json", format)
	}
}
