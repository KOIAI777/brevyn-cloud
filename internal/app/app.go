package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/admin"
	"github.com/brevyn/brevyn-cloud/internal/auth"
	"github.com/brevyn/brevyn-cloud/internal/config"
	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
	"github.com/brevyn/brevyn-cloud/internal/health"
	httpapi "github.com/brevyn/brevyn-cloud/internal/http"
	"github.com/brevyn/brevyn-cloud/internal/platform"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type App struct {
	cfg      *config.Config
	logger   *slog.Logger
	server   *http.Server
	postgres *pgxpool.Pool
	redis    *redis.Client
	admin    *admin.Handler
}

func New(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*App, error) {
	postgres, err := platform.ConnectPostgres(ctx, cfg.DatabaseURL, cfg.PostgresMaxConns, cfg.PostgresMinConns)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := platform.PrepareSchema(ctx, postgres, cfg); err != nil {
		postgres.Close()
		return nil, fmt.Errorf("prepare schema: %w", err)
	}

	redisClient, err := platform.ConnectRedis(ctx, cfg.RedisURL)
	if err != nil {
		postgres.Close()
		return nil, fmt.Errorf("connect redis: %w", err)
	}

	sub2 := sub2api.NewClient(sub2api.ClientConfig{
		BaseURL:       cfg.Sub2APIBaseURL,
		AdminAPIKey:   cfg.Sub2APIAdminAPIKey,
		AdminEmail:    cfg.Sub2APIAdminEmail,
		AdminPassword: cfg.Sub2APIAdminPassword,
	})

	adminHandler := admin.NewHandlerWithOptions(cfg, postgres, redisClient, admin.WithBackupService())
	router := httpapi.NewRouter(cfg, logger, httpapi.Dependencies{
		Health: health.NewHandler(postgres, redisClient),
		Admin:  adminHandler,
		Auth:   auth.NewHandler(cfg, postgres, redisClient, sub2),
	})
	adminHandler.StartBackgroundServices()

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadTimeout:       30 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	return &App{
		cfg:      cfg,
		logger:   logger,
		server:   server,
		postgres: postgres,
		redis:    redisClient,
		admin:    adminHandler,
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		a.logger.Info("api listening", "addr", a.server.Addr, "env", a.cfg.Env)
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
		defer cancel()
		if err := a.server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		a.close()
		return nil
	case err := <-errCh:
		a.close()
		return err
	}
}

func (a *App) close() {
	if a.admin != nil {
		a.admin.StopBackgroundServices()
	}
	if a.redis != nil {
		_ = a.redis.Close()
	}
	if a.postgres != nil {
		a.postgres.Close()
	}
}
