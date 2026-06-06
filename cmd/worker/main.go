package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/admin"
	"github.com/brevyn/brevyn-cloud/internal/config"
	"github.com/brevyn/brevyn-cloud/internal/gateway/operations"
	"github.com/brevyn/brevyn-cloud/internal/platform"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

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
	if err := platform.PrepareSchema(ctx, postgres, cfg); err != nil {
		logger.Error("prepare schema", "error", err)
		os.Exit(1)
	}

	redisClient, err := platform.ConnectRedis(ctx, cfg.RedisURL)
	if err != nil {
		logger.Error("connect redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	logger.Info("worker started", "env", cfg.Env)
	runner := operations.NewRunner(
		postgres,
		logger,
		admin.NewGatewayOperationExecutor(cfg, postgres, redisClient),
		operations.WithConcurrency(cfg.Sub2APIOperationConcurrency),
		operations.WithBatchSize(cfg.Sub2APIOperationBatchSize),
		operations.WithInterval(cfg.Sub2APIOperationInterval),
		operations.WithStaleTimeout(cfg.Sub2APIOperationStaleTimeout),
	)
	go writeWorkerHeartbeat(ctx, redisClient, runner.WorkerID(), logger)
	if err := runner.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("run gateway operations", "error", err)
		os.Exit(1)
	}
	logger.Info("worker stopped")
}

func writeWorkerHeartbeat(ctx context.Context, redisClient *redis.Client, workerID string, logger *slog.Logger) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		payload, _ := json.Marshal(map[string]any{
			"workerId": workerID,
			"seenAt":   time.Now().UTC(),
		})
		if err := redisClient.Set(ctx, admin.WorkerHeartbeatKey, string(payload), 30*time.Second).Err(); err != nil && ctx.Err() == nil {
			logger.Warn("write worker heartbeat failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
