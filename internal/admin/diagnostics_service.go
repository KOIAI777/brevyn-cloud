package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const WorkerHeartbeatKey = "brevyn:worker:gateway_operations:heartbeat"

type DiagnosticsService struct {
	cfg      *config.Config
	postgres *pgxpool.Pool
	redis    *redis.Client
	gateway  *GatewaySettingsService
}

func NewDiagnosticsService(cfg *config.Config, postgres *pgxpool.Pool, redisClient *redis.Client, gateway *GatewaySettingsService) *DiagnosticsService {
	return &DiagnosticsService{cfg: cfg, postgres: postgres, redis: redisClient, gateway: gateway}
}

type DiagnosticsSnapshot struct {
	GeneratedAt         time.Time              `json:"generatedAt"`
	Services            ServiceDiagnostics     `json:"services"`
	Queue               QueueDiagnostics       `json:"queue"`
	Sub2API             Sub2APIDiagnosticState `json:"sub2api"`
	ProductionReadiness []ReadinessCheck       `json:"productionReadiness"`
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

type ReadinessCheck struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Detail  string `json:"detail"`
	Action  string `json:"action"`
	Section string `json:"section"`
}

type workerHeartbeatPayload struct {
	WorkerID string    `json:"workerId"`
	SeenAt   time.Time `json:"seenAt"`
}

func (s *DiagnosticsService) Snapshot(ctx context.Context) DiagnosticsSnapshot {
	now := time.Now().UTC()
	services := ServiceDiagnostics{
		API:      okCheck("api process is serving admin requests", 0, now),
		Postgres: s.checkPostgres(ctx),
		Redis:    s.checkRedis(ctx),
		Worker:   s.checkWorker(ctx),
	}
	queue := s.queueDiagnostics(ctx)
	sub2api := s.sub2APIDiagnostics(ctx)
	return DiagnosticsSnapshot{
		GeneratedAt:         now,
		Services:            services,
		Queue:               queue,
		Sub2API:             sub2api,
		ProductionReadiness: s.productionReadiness(ctx, services, queue, sub2api),
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

func (s *DiagnosticsService) productionReadiness(ctx context.Context, services ServiceDiagnostics, queue QueueDiagnostics, sub2api Sub2APIDiagnosticState) []ReadinessCheck {
	settings, _ := s.gateway.Load(ctx)
	defaultGroupID := s.gatewayDefaultGroupID(ctx)
	activeGroups, activeModels, schedulableAccounts, activeAdmins, totpAdmins := s.readinessCounts(ctx)
	officialProviderURLReady, officialProviderURLDetail := productionPublicURLStatus(s.cfg.OfficialProviderBaseURL)

	checks := []ReadinessCheck{
		readiness("services", "postgres", "Postgres 可用", services.Postgres.Status == "ok", services.Postgres.Detail, "检查 DATABASE_URL 和数据库健康状态"),
		readiness("services", "redis", "Redis 可用", services.Redis.Status == "ok", services.Redis.Detail, "检查 REDIS_URL；限速、锁和 worker heartbeat 都依赖 Redis"),
		readiness("services", "worker", "Worker heartbeat", services.Worker.Status == "ok", services.Worker.Detail, "确认 worker 容器运行，并能写入 Redis heartbeat"),
		readiness("gateway", "sub2api", "Sub2API 管理连接", sub2api.OK, sub2api.Status, "在设置页配置 Sub2API 地址和管理员凭据，然后重新检测"),
		readiness("gateway", "sub2api-auth", "Sub2API 鉴权", sub2api.AuthOK, fmt.Sprintf("auth_mode=%s", sub2api.AuthMode), "优先使用 Admin API Key；或确认管理员邮箱和密码正确"),
		readiness("gateway", "default-group", "默认余额分组", defaultGroupID > 0, fmt.Sprintf("default_group_id=%d", defaultGroupID), "在设置页选择 active standard 分组作为默认分组"),
		readiness("gateway", "active-groups", "已同步分组", activeGroups > 0, fmt.Sprintf("%d active groups", activeGroups), "在设置页同步 Sub2API 分组"),
		readiness("gateway", "active-models", "已同步模型", activeModels > 0, fmt.Sprintf("%d active models", activeModels), "确认 Sub2API 分组绑定账号/渠道后，在设置页同步模型"),
		readiness("gateway", "schedulable-accounts", "可调度账号", schedulableAccounts > 0, fmt.Sprintf("%d schedulable accounts", schedulableAccounts), "在 Sub2API 分组里绑定可调度账号，然后同步模型"),
		readiness("gateway", "official-provider-url", "客户端模型入口", officialProviderURLReady, officialProviderURLDetail, "配置 OFFICIAL_PROVIDER_BASE_URL 为公开 HTTPS Sub2API 入口，例如 https://api.brevyn.org"),
		readiness("queue", "queue-clear", "同步队列无阻塞", queue.DeadLetter == 0 && queue.StaleRunning == 0, fmt.Sprintf("%d dead / %d stale", queue.DeadLetter, queue.StaleRunning), "在队列页查看失败原因，修复后批量重试"),
		readiness("security", "admin-totp", "管理员 TOTP", activeAdmins > 0 && totpAdmins >= activeAdmins, fmt.Sprintf("%d/%d admins enabled", totpAdmins, activeAdmins), "上线前给所有 active 管理员开启 TOTP"),
		readiness("security", "production-secrets", "生产密钥", s.productionSecretsReady(settings), "secrets checked", "上线前替换开发默认密钥，并配置 HTTPS 域名"),
	}
	return checks
}

func readiness(section, key, label string, ok bool, detail, action string) ReadinessCheck {
	status := "ok"
	if !ok {
		status = "warn"
	}
	return ReadinessCheck{Section: section, Key: key, Label: label, Status: status, Detail: detail, Action: action}
}

func (s *DiagnosticsService) gatewayDefaultGroupID(ctx context.Context) int64 {
	var raw string
	if err := s.postgres.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, settingSub2APIDefaultGroup).Scan(&raw); err == nil {
		var parsed int64
		if _, scanErr := fmt.Sscan(strings.TrimSpace(raw), &parsed); scanErr == nil && parsed > 0 {
			return parsed
		}
	}
	return s.cfg.Sub2APIDefaultGroupID
}

func (s *DiagnosticsService) readinessCounts(ctx context.Context) (activeGroups int64, activeModels int64, schedulableAccounts int64, activeAdmins int64, totpAdmins int64) {
	_ = s.postgres.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM gateway_groups WHERE status = 'active'),
			(SELECT count(*) FROM gateway_group_models WHERE provider = 'sub2api' AND status = 'active'),
			(SELECT count(*) FROM gateway_upstream_accounts WHERE provider = 'sub2api' AND status = 'active' AND schedulable = true),
			(SELECT count(*) FROM admin_users WHERE status = 'active'),
			(SELECT count(*) FROM admin_users WHERE status = 'active' AND totp_enabled = true)
	`).Scan(&activeGroups, &activeModels, &schedulableAccounts, &activeAdmins, &totpAdmins)
	return
}

func (s *DiagnosticsService) productionSecretsReady(settings sub2APIRuntimeSettings) bool {
	if strings.TrimSpace(s.cfg.EncryptionKey) == "" ||
		strings.TrimSpace(s.cfg.SessionSecret) == "" ||
		strings.TrimSpace(s.cfg.JWTAccessSecret) == "" ||
		strings.TrimSpace(s.cfg.JWTRefreshSecret) == "" {
		return false
	}
	for _, value := range []string{s.cfg.AppBaseURL, s.cfg.AdminBaseURL, s.cfg.OfficialProviderBaseURL} {
		if ok, _ := productionPublicURLStatus(value); !ok {
			return false
		}
	}
	return strings.TrimSpace(settings.AdminAPIKey) != "" ||
		(strings.TrimSpace(settings.AdminEmail) != "" && strings.TrimSpace(settings.AdminPassword) != "")
}

func productionPublicURLStatus(value string) (bool, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return false, "missing"
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false, "invalid URL"
	}
	if parsed.Scheme != "https" {
		return false, "must use https"
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "0.0.0.0" || host == "::1" || strings.HasPrefix(host, "127.") {
		return false, "local URL is not production-ready"
	}
	return true, value
}
