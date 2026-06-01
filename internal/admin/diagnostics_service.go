package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const WorkerHeartbeatKey = "brevyn:worker:gateway_operations:heartbeat"

type DiagnosticsService struct {
	postgres *pgxpool.Pool
	redis    *redis.Client
	gateway  *GatewaySettingsService
}

func NewDiagnosticsService(postgres *pgxpool.Pool, redisClient *redis.Client, gateway *GatewaySettingsService) *DiagnosticsService {
	return &DiagnosticsService{postgres: postgres, redis: redisClient, gateway: gateway}
}

type DiagnosticsSnapshot struct {
	GeneratedAt time.Time              `json:"generatedAt"`
	Services    ServiceDiagnostics     `json:"services"`
	Queue       QueueDiagnostics       `json:"queue"`
	Sub2API     Sub2APIDiagnosticState `json:"sub2api"`
}

type ServiceDiagnostics struct {
	API      DiagnosticCheck `json:"api"`
	Postgres DiagnosticCheck `json:"postgres"`
	Redis    DiagnosticCheck `json:"redis"`
	Worker   WorkerCheck     `json:"worker"`
}

type DiagnosticCheck struct {
	Status    string    `json:"status"`
	Detail    string    `json:"detail"`
	LatencyMs int64     `json:"latencyMs"`
	CheckedAt time.Time `json:"checkedAt"`
}

type WorkerCheck struct {
	Status     string     `json:"status"`
	Detail     string     `json:"detail"`
	WorkerID   string     `json:"workerId"`
	LastSeenAt *time.Time `json:"lastSeenAt"`
	AgeSeconds int64      `json:"ageSeconds"`
	CheckedAt  time.Time  `json:"checkedAt"`
}

type QueueDiagnostics struct {
	Total           int64      `json:"total"`
	Pending         int64      `json:"pending"`
	Running         int64      `json:"running"`
	Succeeded       int64      `json:"succeeded"`
	Failed          int64      `json:"failed"`
	DeadLetter      int64      `json:"deadLetter"`
	RetryableFailed int64      `json:"retryableFailed"`
	ReadyNow        int64      `json:"readyNow"`
	DueSoon         int64      `json:"dueSoon"`
	StaleRunning    int64      `json:"staleRunning"`
	LastSucceededAt *time.Time `json:"lastSucceededAt"`
	LastFailedAt    *time.Time `json:"lastFailedAt"`
	LastOperationAt *time.Time `json:"lastOperationAt"`
}

type Sub2APIDiagnosticState struct {
	Status     string    `json:"status"`
	OK         bool      `json:"ok"`
	BaseURL    string    `json:"baseUrl"`
	AuthMode   string    `json:"authMode"`
	HealthOK   bool      `json:"healthOk"`
	AuthOK     bool      `json:"authOk"`
	GroupCount int       `json:"groupCount"`
	LatencyMs  int64     `json:"latencyMs"`
	Error      string    `json:"error"`
	CheckedAt  time.Time `json:"checkedAt"`
}

type workerHeartbeatPayload struct {
	WorkerID string    `json:"workerId"`
	SeenAt   time.Time `json:"seenAt"`
}

func (s *DiagnosticsService) Snapshot(ctx context.Context) DiagnosticsSnapshot {
	now := time.Now().UTC()
	return DiagnosticsSnapshot{
		GeneratedAt: now,
		Services: ServiceDiagnostics{
			API:      okCheck("api process is serving admin requests", 0, now),
			Postgres: s.checkPostgres(ctx),
			Redis:    s.checkRedis(ctx),
			Worker:   s.checkWorker(ctx),
		},
		Queue:   s.queueDiagnostics(ctx),
		Sub2API: s.sub2APIDiagnostics(ctx),
	}
}

func (s *DiagnosticsService) checkPostgres(ctx context.Context) DiagnosticCheck {
	start := time.Now()
	checkedAt := start.UTC()
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.postgres.Ping(pingCtx); err != nil {
		return DiagnosticCheck{Status: "error", Detail: err.Error(), LatencyMs: time.Since(start).Milliseconds(), CheckedAt: checkedAt}
	}
	return okCheck("postgres ping ok", time.Since(start).Milliseconds(), checkedAt)
}

func (s *DiagnosticsService) checkRedis(ctx context.Context) DiagnosticCheck {
	start := time.Now()
	checkedAt := start.UTC()
	if s.redis == nil {
		return DiagnosticCheck{Status: "warn", Detail: "redis client is not attached to admin handler", CheckedAt: checkedAt}
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.redis.Ping(pingCtx).Err(); err != nil {
		return DiagnosticCheck{Status: "error", Detail: err.Error(), LatencyMs: time.Since(start).Milliseconds(), CheckedAt: checkedAt}
	}
	return okCheck("redis ping ok", time.Since(start).Milliseconds(), checkedAt)
}

func (s *DiagnosticsService) checkWorker(ctx context.Context) WorkerCheck {
	checkedAt := time.Now().UTC()
	if s.redis == nil {
		return WorkerCheck{Status: "warn", Detail: "worker heartbeat requires redis", CheckedAt: checkedAt}
	}
	raw, err := s.redis.Get(ctx, WorkerHeartbeatKey).Result()
	if errors.Is(err, redis.Nil) {
		return WorkerCheck{Status: "warn", Detail: "worker heartbeat not found", CheckedAt: checkedAt}
	}
	if err != nil {
		return WorkerCheck{Status: "error", Detail: err.Error(), CheckedAt: checkedAt}
	}
	var payload workerHeartbeatPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return WorkerCheck{Status: "warn", Detail: "worker heartbeat is malformed", CheckedAt: checkedAt}
	}
	age := int64(time.Since(payload.SeenAt).Seconds())
	status := "ok"
	detail := "worker heartbeat fresh"
	if age > 45 {
		status = "warn"
		detail = fmt.Sprintf("worker heartbeat is %ds old", age)
	}
	return WorkerCheck{Status: status, Detail: detail, WorkerID: payload.WorkerID, LastSeenAt: &payload.SeenAt, AgeSeconds: age, CheckedAt: checkedAt}
}

func (s *DiagnosticsService) queueDiagnostics(ctx context.Context) QueueDiagnostics {
	var out QueueDiagnostics
	_ = s.postgres.QueryRow(ctx, `
		SELECT
			count(*),
			count(*) FILTER (WHERE status = 'pending'),
			count(*) FILTER (WHERE status = 'running'),
			count(*) FILTER (WHERE status = 'succeeded'),
			count(*) FILTER (WHERE status = 'failed'),
			count(*) FILTER (WHERE status = 'dead_letter'),
			count(*) FILTER (WHERE status IN ('failed', 'dead_letter') AND last_error_retryable = true),
			count(*) FILTER (WHERE status IN ('pending', 'failed') AND next_run_at <= now() AND attempts < max_attempts AND (status = 'pending' OR last_error_retryable = true)),
			count(*) FILTER (WHERE status IN ('pending', 'failed') AND next_run_at <= now() + interval '15 minutes' AND attempts < max_attempts AND (status = 'pending' OR last_error_retryable = true)),
			count(*) FILTER (WHERE status = 'running' AND locked_at < now() - interval '5 minutes'),
			max(completed_at) FILTER (WHERE status = 'succeeded'),
			max(updated_at) FILTER (WHERE status IN ('failed', 'dead_letter')),
			max(updated_at)
		FROM gateway_operations
	`).Scan(
		&out.Total,
		&out.Pending,
		&out.Running,
		&out.Succeeded,
		&out.Failed,
		&out.DeadLetter,
		&out.RetryableFailed,
		&out.ReadyNow,
		&out.DueSoon,
		&out.StaleRunning,
		&out.LastSucceededAt,
		&out.LastFailedAt,
		&out.LastOperationAt,
	)
	return out
}

func (s *DiagnosticsService) sub2APIDiagnostics(ctx context.Context) Sub2APIDiagnosticState {
	checkedAt := time.Now().UTC()
	testCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	result := s.gateway.TestConnection(testCtx)
	return Sub2APIDiagnosticState{
		Status:     result.Status,
		OK:         result.OK,
		BaseURL:    result.BaseURL,
		AuthMode:   result.AuthMode,
		HealthOK:   result.HealthOK,
		AuthOK:     result.AuthOK,
		GroupCount: result.GroupCount,
		LatencyMs:  result.LatencyMs,
		Error:      result.Error,
		CheckedAt:  checkedAt,
	}
}

func okCheck(detail string, latencyMs int64, checkedAt time.Time) DiagnosticCheck {
	return DiagnosticCheck{Status: "ok", Detail: detail, LatencyMs: latencyMs, CheckedAt: checkedAt}
}
