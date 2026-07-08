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
	"reflect"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// logFileMode is the permission applied to log files the connector creates.
// Owner read/write only, since logs can carry sensitive payload data.
const logFileMode = 0o600

func init() {
	core.MustRegisterConnector("logger", func() core.Connector {
		return &Connector{}
	})

	// Package-level editor defaults: the logger connector and its log block share
	// the Data palette group and the ScrollText icon unless they set their own.
	core.RegisterExtension(core.ExtensionMeta{Group: "Data", Icon: "ScrollText"})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "logger",
		Label:    "Logger",
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

// settings are the common slog knobs the logger exposes. Every field has a
// sensible default, so a logger connector can be declared with no settings.
type connectorSettings struct {
	// stdout, stderr, or a file path.
	Output string `json:"output" octo:"label=Output,default=stdout"`
	// Log output format.
	Format string `json:"format" octo:"label=Format,type=enum,enum=text|json,default=text"`
	// Minimum log level.
	Level string `json:"level" octo:"label=Level,type=enum,enum=debug|info|warn|error,default=info"`
	// Include source file:line in log records.
	AddSource bool `json:"addSource" octo:"label=Add source,default=false"`
}

// Connector is a configured logger that flows' log blocks write through. When
// Output names a file it is opened on Start and closed on Stop.
type Connector struct {
	logger *slog.Logger
	file   *os.File
}

// Start parses the settings, opens the output, and builds the slog logger.
func (c *Connector) Start(ctx context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
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

	c.logger = slog.New(withLogSink(ctx, handler))
	c.file = file
	return nil
}

// withLogSink tees base through the runtime's central log sink when the active
// services module ships logs (the k8s module), so a log block's output reaches the
// aggregator in addition to this connector's own output. The standalone module
// ships nothing, so base is returned unchanged.
//
//nolint:ireturn // returns the slog.Handler interface intentionally
func withLogSink(ctx context.Context, base slog.Handler) slog.Handler {
	if shipper, ok := core.RuntimeServicesFromContext(ctx).(core.LogShipper); ok {
		if sink := shipper.LogSink(); sink != nil {
			return core.TeeHandler(base, sink)
		}
	}
	return base
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
