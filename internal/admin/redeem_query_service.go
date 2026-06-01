package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RedeemQueryService struct {
	postgres *pgxpool.Pool
}

func NewRedeemQueryService(postgres *pgxpool.Pool) *RedeemQueryService {
	return &RedeemQueryService{postgres: postgres}
}

type RedeemBatchListFilters struct {
	Search    string
	Status    string
	Source    string
	ProductID string
	DateFrom  string
	DateTo    string
	Limit     int
	Offset    int
}

type RedeemCodeListFilters struct {
	Search    string
	Status    string
	CodeType  string
	Source    string
	ProductID string
	BatchID   string
	UsedBy    string
	DateFrom  string
	DateTo    string
	Limit     int
	Offset    int
}

type redemptionListFilters struct {
	Search     string
	Status     string
	Kind       string
	Source     string
	ProductID  string
	BatchID    string
	User       string
	ErrorClass string
	Retryable  string
	DateFrom   string
	DateTo     string
	Limit      int
	Offset     int
}

func (s *RedeemQueryService) ListRedeemBatches(ctx context.Context, filters RedeemBatchListFilters) ([]RedeemBatchItem, int, error) {
	if filters.Limit <= 0 {
		filters.Limit = 50
	}

	where := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(filters.Search) != "" {
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(filters.Search))+"%")
		where = append(where, fmt.Sprintf(`(
			lower(b.public_id) LIKE $%d OR
			lower(b.name) LIKE $%d OR
			lower(b.source) LIKE $%d OR
			lower(coalesce(b.order_ref, '')) LIKE $%d OR
			lower(coalesce(b.notes, '')) LIKE $%d OR
			lower(coalesce(p.sku, '')) LIKE $%d OR
			lower(coalesce(p.name, '')) LIKE $%d
		)`, len(args), len(args), len(args), len(args), len(args), len(args), len(args)))
	}
	appendExactStringFilter(&where, &args, "b.status", filters.Status)
	appendExactStringFilter(&where, &args, "b.source", filters.Source)
	if strings.TrimSpace(filters.ProductID) != "" {
		args = append(args, strings.TrimSpace(filters.ProductID))
		where = append(where, fmt.Sprintf("(p.public_id = $%d OR lower(coalesce(p.sku, '')) = lower($%d))", len(args), len(args)))
	}
	if err := appendDateRangeValues("b.created_at", filters.DateFrom, filters.DateTo, &where, &args); err != nil {
		return nil, 0, err
	}

	var total int
	if err := s.postgres.QueryRow(ctx, `
		SELECT count(*)
		FROM redeem_code_batches b
		LEFT JOIN products p ON p.id = b.product_id
		WHERE `+strings.Join(where, " AND "), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, filters.Limit, filters.Offset)
	rows, err := s.postgres.Query(ctx, `
		SELECT b.public_id, b.name, b.source, b.order_ref, b.quantity, b.status, b.notes,
			coalesce(p.public_id, ''), coalesce(p.name, ''),
			count(rc.id) FILTER (WHERE rc.status = 'unused')::int AS unused_count,
			count(rc.id) FILTER (WHERE rc.status = 'used')::int AS used_count,
			b.created_at
		FROM redeem_code_batches b
		LEFT JOIN products p ON p.id = b.product_id
		LEFT JOIN redeem_codes rc ON rc.batch_id = b.id
		WHERE `+strings.Join(where, " AND ")+`
		GROUP BY b.id, p.public_id, p.name
		ORDER BY b.created_at DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []RedeemBatchItem{}
	for rows.Next() {
		var item RedeemBatchItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Source, &item.OrderRef, &item.Quantity, &item.Status,
			&item.Notes, &item.ProductID, &item.ProductName, &item.UnusedCount, &item.UsedCount,
			&item.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *RedeemQueryService) ListRedeemCodes(ctx context.Context, filters RedeemCodeListFilters) ([]RedeemCodeItem, int, error) {
	if filters.Limit <= 0 {
		filters.Limit = 100
	}

	where := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(filters.Search) != "" {
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(filters.Search))+"%")
		where = append(where, fmt.Sprintf(`(
			lower(rc.public_id) LIKE $%d OR
			lower(rc.code_prefix) LIKE $%d OR
			lower(coalesce(rc.order_ref, '')) LIKE $%d OR
			lower(coalesce(rc.notes, '')) LIKE $%d OR
			lower(coalesce(p.sku, '')) LIKE $%d OR
			lower(coalesce(p.name, '')) LIKE $%d OR
			lower(coalesce(b.name, '')) LIKE $%d OR
			lower(coalesce(b.order_ref, '')) LIKE $%d OR
			lower(coalesce(b.notes, '')) LIKE $%d OR
			lower(coalesce(u.email, '')) LIKE $%d OR
			lower(coalesce(u.public_id, '')) LIKE $%d
		)`, len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args)))
	}
	appendExactStringFilter(&where, &args, "rc.status", filters.Status)
	appendExactStringFilter(&where, &args, "rc.kind", filters.CodeType)
	appendExactStringFilter(&where, &args, "rc.source", filters.Source)
	if strings.TrimSpace(filters.ProductID) != "" {
		args = append(args, strings.TrimSpace(filters.ProductID))
		where = append(where, fmt.Sprintf("(p.public_id = $%d OR lower(coalesce(p.sku, '')) = lower($%d))", len(args), len(args)))
	}
	if strings.TrimSpace(filters.BatchID) != "" {
		args = append(args, strings.TrimSpace(filters.BatchID))
		where = append(where, fmt.Sprintf("(b.public_id = $%d OR lower(coalesce(b.name, '')) = lower($%d))", len(args), len(args)))
	}
	if strings.TrimSpace(filters.UsedBy) != "" {
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(filters.UsedBy))+"%")
		where = append(where, fmt.Sprintf("(lower(coalesce(u.public_id, '')) LIKE $%d OR lower(coalesce(u.email, '')) LIKE $%d)", len(args), len(args)))
	}
	if err := appendDateRangeValues("rc.created_at", filters.DateFrom, filters.DateTo, &where, &args); err != nil {
		return nil, 0, err
	}

	var total int
	if err := s.postgres.QueryRow(ctx, `
		SELECT count(*)
		FROM redeem_codes rc
		LEFT JOIN products p ON p.id = rc.product_id
		LEFT JOIN redeem_code_batches b ON b.id = rc.batch_id
		LEFT JOIN users u ON u.id = rc.used_by_user_id
		WHERE `+strings.Join(where, " AND "), args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filters.Limit, filters.Offset)

	rows, err := s.postgres.Query(ctx, `
		SELECT rc.public_id, rc.code_prefix, rc.kind, rc.value, rc.validity_days, rc.status, rc.order_ref, rc.notes,
			coalesce(p.public_id, ''), coalesce(p.name, ''), coalesce(p.sku, ''),
			coalesce(b.public_id, ''), coalesce(b.name, ''),
			rc.external_group_id, rc.source, coalesce(u.public_id, ''), coalesce(u.email, ''),
			rc.used_at, rc.expires_at, rc.created_at
		FROM redeem_codes rc
		LEFT JOIN products p ON p.id = rc.product_id
		LEFT JOIN redeem_code_batches b ON b.id = rc.batch_id
		LEFT JOIN users u ON u.id = rc.used_by_user_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY rc.created_at DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []RedeemCodeItem{}
	for rows.Next() {
		item, err := scanRedeemCode(rows)
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

func (s *RedeemQueryService) ListRedemptions(ctx context.Context, filters redemptionListFilters) ([]RedemptionItem, int, error) {
	if filters.Limit <= 0 {
		filters.Limit = 100
	}
	where := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(filters.Search) != "" {
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(filters.Search))+"%")
		where = append(where, fmt.Sprintf(`(
			lower(rr.public_id) LIKE $%d OR
			lower(rc.public_id) LIKE $%d OR
			lower(rc.code_prefix) LIKE $%d OR
			lower(u.public_id) LIKE $%d OR
			lower(u.email) LIKE $%d OR
			lower(coalesce(p.name, '')) LIKE $%d OR
			lower(coalesce(p.sku, '')) LIKE $%d OR
			lower(coalesce(b.name, '')) LIKE $%d OR
			lower(coalesce(rc.order_ref, '')) LIKE $%d OR
			lower(coalesce(rr.error_message, '')) LIKE $%d OR
			lower(coalesce(rr.gateway_error_code, '')) LIKE $%d OR
			lower(coalesce(rr.gateway_error_class, '')) LIKE $%d OR
			lower(coalesce(rr.gateway_error_stage, '')) LIKE $%d OR
			lower(coalesce(rr.gateway_error_detail, '')) LIKE $%d
		)`, len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args)))
	}
	appendExactStringFilter(&where, &args, "rr.status", filters.Status)
	appendExactStringFilter(&where, &args, "rr.kind", filters.Kind)
	appendExactStringFilter(&where, &args, "rc.source", filters.Source)
	appendExactStringFilter(&where, &args, "rr.gateway_error_class", filters.ErrorClass)
	if filters.Retryable == "true" || filters.Retryable == "false" {
		args = append(args, filters.Retryable == "true")
		where = append(where, fmt.Sprintf("rr.gateway_error_retryable = $%d", len(args)))
	}
	if strings.TrimSpace(filters.ProductID) != "" {
		args = append(args, strings.TrimSpace(filters.ProductID))
		where = append(where, fmt.Sprintf("(p.public_id = $%d OR lower(coalesce(p.sku, '')) = lower($%d))", len(args), len(args)))
	}
	if strings.TrimSpace(filters.BatchID) != "" {
		args = append(args, strings.TrimSpace(filters.BatchID))
		where = append(where, fmt.Sprintf("(b.public_id = $%d OR lower(coalesce(b.name, '')) = lower($%d))", len(args), len(args)))
	}
	if strings.TrimSpace(filters.User) != "" {
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(filters.User))+"%")
		where = append(where, fmt.Sprintf("(lower(u.public_id) LIKE $%d OR lower(u.email) LIKE $%d)", len(args), len(args)))
	}
	if err := appendDateRangeValues("rr.created_at", filters.DateFrom, filters.DateTo, &where, &args); err != nil {
		return nil, 0, err
	}

	var total int
	if err := s.postgres.QueryRow(ctx, `
		SELECT count(*)
		FROM redeem_redemptions rr
		JOIN redeem_codes rc ON rc.id = rr.redeem_code_id
		JOIN users u ON u.id = rr.user_id
		LEFT JOIN products p ON p.id = rr.product_id
		LEFT JOIN redeem_code_batches b ON b.id = rr.batch_id
		WHERE `+strings.Join(where, " AND "), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, filters.Limit, filters.Offset)
	rows, err := s.postgres.Query(ctx, `
		SELECT rr.public_id, rc.public_id, u.public_id, u.email, coalesce(p.name, ''),
			coalesce(b.name, ''), coalesce(rc.order_ref, ''), rr.kind, rr.value, rr.validity_days, rr.external_user_id,
			rr.external_group_id, rr.gateway_operation, rr.status, rr.error_message,
			rr.gateway_error_code, rr.gateway_error_class, rr.gateway_error_stage,
			rr.gateway_error_retryable, rr.gateway_error_detail,
			coalesce(go.public_id, ''), coalesce(go.status, ''), coalesce(go.attempts, 0),
			coalesce(go.max_attempts, 0), go.next_run_at, rr.created_at
		FROM redeem_redemptions rr
		JOIN redeem_codes rc ON rc.id = rr.redeem_code_id
		JOIN users u ON u.id = rr.user_id
		LEFT JOIN products p ON p.id = rr.product_id
		LEFT JOIN redeem_code_batches b ON b.id = rr.batch_id
		LEFT JOIN LATERAL (
			SELECT public_id, status, attempts, max_attempts, next_run_at
			FROM gateway_operations
			WHERE redemption_id = rr.id AND operation = 'sync_redemption'
			ORDER BY id DESC
			LIMIT 1
		) go ON true
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY rr.created_at DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []RedemptionItem{}
	for rows.Next() {
		var item RedemptionItem
		if err := rows.Scan(&item.ID, &item.RedeemCodeID, &item.UserID, &item.UserEmail,
			&item.ProductName, &item.BatchName, &item.OrderRef, &item.Kind, &item.Value, &item.ValidityDays,
			&item.ExternalUserID, &item.ExternalGroupID, &item.GatewayOperation, &item.Status,
			&item.ErrorMessage, &item.ErrorCode, &item.ErrorClass, &item.ErrorStage,
			&item.ErrorRetryable, &item.ErrorDetail, &item.OperationID, &item.OperationStatus,
			&item.OperationAttempts, &item.OperationMaxAttempts, &item.OperationNextRunAt,
			&item.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *RedeemQueryService) GetRedemptionByPublicID(ctx context.Context, publicID string) (RedemptionItem, error) {
	var item RedemptionItem
	err := s.postgres.QueryRow(ctx, `
		SELECT rr.public_id, rc.public_id, u.public_id, u.email, coalesce(p.name, ''),
			coalesce(b.name, ''), coalesce(rc.order_ref, ''), rr.kind, rr.value, rr.validity_days, rr.external_user_id,
			rr.external_group_id, rr.gateway_operation, rr.status, rr.error_message,
			rr.gateway_error_code, rr.gateway_error_class, rr.gateway_error_stage,
			rr.gateway_error_retryable, rr.gateway_error_detail,
			coalesce(go.public_id, ''), coalesce(go.status, ''), coalesce(go.attempts, 0),
			coalesce(go.max_attempts, 0), go.next_run_at, rr.created_at
		FROM redeem_redemptions rr
		JOIN redeem_codes rc ON rc.id = rr.redeem_code_id
		JOIN users u ON u.id = rr.user_id
		LEFT JOIN products p ON p.id = rr.product_id
		LEFT JOIN redeem_code_batches b ON b.id = rr.batch_id
		LEFT JOIN LATERAL (
			SELECT public_id, status, attempts, max_attempts, next_run_at
			FROM gateway_operations
			WHERE redemption_id = rr.id AND operation = 'sync_redemption'
			ORDER BY id DESC
			LIMIT 1
		) go ON true
		WHERE rr.public_id = $1
	`, publicID).Scan(
		&item.ID,
		&item.RedeemCodeID,
		&item.UserID,
		&item.UserEmail,
		&item.ProductName,
		&item.BatchName,
		&item.OrderRef,
		&item.Kind,
		&item.Value,
		&item.ValidityDays,
		&item.ExternalUserID,
		&item.ExternalGroupID,
		&item.GatewayOperation,
		&item.Status,
		&item.ErrorMessage,
		&item.ErrorCode,
		&item.ErrorClass,
		&item.ErrorStage,
		&item.ErrorRetryable,
		&item.ErrorDetail,
		&item.OperationID,
		&item.OperationStatus,
		&item.OperationAttempts,
		&item.OperationMaxAttempts,
		&item.OperationNextRunAt,
		&item.CreatedAt,
	)
	return item, err
}

func (h *Handler) queryRedemptions(ctx context.Context, filters redemptionListFilters) ([]RedemptionItem, int, error) {
	return h.redeemQueries.ListRedemptions(ctx, filters)
}

func (h *Handler) queryRedemptionByPublicID(ctx context.Context, publicID string) (RedemptionItem, error) {
	return h.redeemQueries.GetRedemptionByPublicID(ctx, publicID)
}
