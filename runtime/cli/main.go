package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/juancavallotti/eip-go/connectors/cron"
	_ "github.com/juancavallotti/eip-go/connectors/logger"
	_ "github.com/juancavallotti/eip-go/connectors/noop"
	"github.com/juancavallotti/eip-go/core"
	_ "github.com/juancavallotti/eip-go/processors/log"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("runtime stopped with error", "error", err)
		os.Exit(1)
	}

	slog.Info("runtime stopped")
}

func run() error {
	configPath := flag.String("config", "", "path to the runtime config")
	flag.Parse()

	if *configPath == "" {
		return errors.New("config path is required")
	}

	config, err := core.LoadConfig(*configPath)
	if err != nil {
		return err
	}

	slog.Info("starting runtime", "connectors", len(config.Connectors))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	service := core.NewService(config, core.DefaultRegistry())
	if err := service.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}

	return nil
}
