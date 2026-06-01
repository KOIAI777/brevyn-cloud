package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type GatewayOperationService struct {
	postgres *pgxpool.Pool
}

func NewGatewayOperationService(postgres *pgxpool.Pool) *GatewayOperationService {
	return &GatewayOperationService{postgres: postgres}
}

type GatewayOperationListFilters struct {
	Search     string
	Status     string
	Operation  string
	Provider   string
	ErrorClass string
	Retryable  string
	User       string
	DateFrom   string
	DateTo     string
	Limit      int
	Offset     int
}

type GatewayOperationItem struct {
	ID                 string     `json:"id"`
	Provider           string     `json:"provider"`
	Operation          string     `json:"operation"`
	Status             string     `json:"status"`
	TargetType         string     `json:"targetType"`
	TargetID           string     `json:"targetId"`
	RedemptionID       string     `json:"redemptionId"`
	UserID             string     `json:"userId"`
	UserEmail          string     `json:"userEmail"`
	IdempotencyKey     string     `json:"idempotencyKey"`
	Attempts           int        `json:"attempts"`
	MaxAttempts        int        `json:"maxAttempts"`
	NextRunAt          *time.Time `json:"nextRunAt"`
	LockedAt           *time.Time `json:"lockedAt"`
	LockedBy           string     `json:"lockedBy"`
	StartedAt          *time.Time `json:"startedAt"`
	CompletedAt        *time.Time `json:"completedAt"`
	LastErrorMessage   string     `json:"lastErrorMessage"`
	LastErrorCode      string     `json:"lastErrorCode"`
	LastErrorClass     string     `json:"lastErrorClass"`
	LastErrorStage     string     `json:"lastErrorStage"`
	LastErrorRetryable bool       `json:"lastErrorRetryable"`
	LastErrorDetail    string     `json:"lastErrorDetail"`
	Payload            string     `json:"payload"`
	Result             string     `json:"result"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
}

func (s *GatewayOperationService) List(ctx context.Context, filters GatewayOperationListFilters) ([]GatewayOperationItem, int, error) {
	if filters.Limit <= 0 {
		filters.Limit = 50
	}

	where := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(filters.Search) != "" {
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(filters.Search))+"%")
		where = append(where, fmt.Sprintf(`(
			lower(go.public_id) LIKE $%d OR
			lower(go.operation) LIKE $%d OR
			lower(go.target_type) LIKE $%d OR
			lower(go.target_id) LIKE $%d OR
			lower(go.idempotency_key) LIKE $%d OR
			lower(coalesce(rr.public_id, '')) LIKE $%d OR
			lower(coalesce(u.public_id, '')) LIKE $%d OR
			lower(coalesce(u.email, '')) LIKE $%d OR
			lower(coalesce(go.last_error_message, '')) LIKE $%d OR
			lower(coalesce(go.last_error_detail, '')) LIKE $%d
		)`, len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args)))
	}
	appendExactStringFilter(&where, &args, "go.status", filters.Status)
	appendExactStringFilter(&where, &args, "go.operation", filters.Operation)
	appendExactStringFilter(&where, &args, "go.provider", filters.Provider)
	appendExactStringFilter(&where, &args, "go.last_error_class", filters.ErrorClass)
	if filters.Retryable == "true" || filters.Retryable == "false" {
		args = append(args, filters.Retryable == "true")
		where = append(where, fmt.Sprintf("go.last_error_retryable = $%d", len(args)))
	}
	if strings.TrimSpace(filters.User) != "" {
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(filters.User))+"%")
		where = append(where, fmt.Sprintf("(lower(coalesce(u.public_id, '')) LIKE $%d OR lower(coalesce(u.email, '')) LIKE $%d)", len(args), len(args)))
	}
	if err := appendDateRangeValues("go.created_at", filters.DateFrom, filters.DateTo, &where, &args); err != nil {
		return nil, 0, err
	}

	var total int
	if err := s.postgres.QueryRow(ctx, `
		SELECT count(*)
		FROM gateway_operations go
		LEFT JOIN redeem_redemptions rr ON rr.id = go.redemption_id
		LEFT JOIN users u ON u.id = go.user_id
		WHERE `+strings.Join(where, " AND "), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, filters.Limit, filters.Offset)
	rows, err := s.postgres.Query(ctx, `
		SELECT go.public_id, go.provider, go.operation, go.status, go.target_type, go.target_id,
			coalesce(rr.public_id, ''), coalesce(u.public_id, ''), coalesce(u.email, ''),
			go.idempotency_key, go.attempts, go.max_attempts, go.next_run_at,
			go.locked_at, go.locked_by, go.started_at, go.completed_at,
			go.last_error_message, go.last_error_code, go.last_error_class, go.last_error_stage,
			go.last_error_retryable, go.last_error_detail, go.payload::text, go.result::text,
			go.created_at, go.updated_at
		FROM gateway_operations go
		LEFT JOIN redeem_redemptions rr ON rr.id = go.redemption_id
		LEFT JOIN users u ON u.id = go.user_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY
			CASE go.status
				WHEN 'running' THEN 0
				WHEN 'failed' THEN 1
				WHEN 'dead_letter' THEN 2
				WHEN 'pending' THEN 3
				ELSE 4
			END,
			go.next_run_at ASC,
			go.created_at DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []GatewayOperationItem{}
	for rows.Next() {
		item, err := scanGatewayOperation(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *GatewayOperationService) Retry(ctx context.Context, publicID string) (GatewayOperationItem, error) {
	tx, err := s.postgres.Begin(ctx)
	if err != nil {
		return GatewayOperationItem{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	if err := tx.QueryRow(ctx, `
		SELECT status FROM gateway_operations WHERE public_id = $1 FOR UPDATE
	`, publicID).Scan(&status); err != nil {
		return GatewayOperationItem{}, err
	}
	switch status {
	case "succeeded":
		return GatewayOperationItem{}, fmt.Errorf("operation_already_succeeded")
	case "running":
		return GatewayOperationItem{}, fmt.Errorf("operation_running")
	}

	if _, err := tx.Exec(ctx, `
		UPDATE gateway_operations
		SET status = 'pending',
			max_attempts = greatest(max_attempts, attempts + 3),
			next_run_at = now(),
			locked_at = NULL,
			locked_by = '',
			completed_at = NULL,
			updated_at = now()
		WHERE public_id = $1
	`, publicID); err != nil {
		return GatewayOperationItem{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return GatewayOperationItem{}, err
	}
	return s.Get(ctx, publicID)
}

func (s *GatewayOperationService) RetryFailed(ctx context.Context) (int64, error) {
	tag, err := s.postgres.Exec(ctx, `
		UPDATE gateway_operations
		SET status = 'pending',
			max_attempts = greatest(max_attempts, attempts + 3),
			next_run_at = now(),
			locked_at = NULL,
			locked_by = '',
			completed_at = NULL,
			updated_at = now()
		WHERE status IN ('failed', 'dead_letter')
			AND last_error_retryable = true
	`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *GatewayOperationService) Get(ctx context.Context, publicID string) (GatewayOperationItem, error) {
	row := s.postgres.QueryRow(ctx, `
		SELECT go.public_id, go.provider, go.operation, go.status, go.target_type, go.target_id,
			coalesce(rr.public_id, ''), coalesce(u.public_id, ''), coalesce(u.email, ''),
			go.idempotency_key, go.attempts, go.max_attempts, go.next_run_at,
			go.locked_at, go.locked_by, go.started_at, go.completed_at,
			go.last_error_message, go.last_error_code, go.last_error_class, go.last_error_stage,
			go.last_error_retryable, go.last_error_detail, go.payload::text, go.result::text,
			go.created_at, go.updated_at
		FROM gateway_operations go
		LEFT JOIN redeem_redemptions rr ON rr.id = go.redemption_id
		LEFT JOIN users u ON u.id = go.user_id
		WHERE go.public_id = $1
	`, publicID)
	return scanGatewayOperation(row)
}

func scanGatewayOperation(row scanner) (GatewayOperationItem, error) {
	var item GatewayOperationItem
	err := row.Scan(
		&item.ID,
		&item.Provider,
		&item.Operation,
		&item.Status,
		&item.TargetType,
		&item.TargetID,
		&item.RedemptionID,
		&item.UserID,
		&item.UserEmail,
		&item.IdempotencyKey,
		&item.Attempts,
		&item.MaxAttempts,
		&item.NextRunAt,
		&item.LockedAt,
		&item.LockedBy,
		&item.StartedAt,
		&item.CompletedAt,
		&item.LastErrorMessage,
		&item.LastErrorCode,
		&item.LastErrorClass,
		&item.LastErrorStage,
		&item.LastErrorRetryable,
		&item.LastErrorDetail,
		&item.Payload,
		&item.Result,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}
