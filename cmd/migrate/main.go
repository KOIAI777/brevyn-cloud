package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/brevyn/brevyn-cloud/internal/config"
	"github.com/brevyn/brevyn-cloud/internal/platform"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	postgres, err := platform.ConnectPostgres(ctx, cfg.DatabaseURL, cfg.PostgresMaxConns, cfg.PostgresMinConns)
	if err != nil {
		logger.Error("connect postgres", "error", err)
		os.Exit(1)
	}
	defer postgres.Close()

	if err := platform.EnsureSchema(ctx, postgres, cfg); err != nil {
		logger.Error("apply schema migrations", "error", err)
		os.Exit(1)
	}

	logger.Info("schema migrations applied", "version", platform.CurrentSchemaVersion())
}
