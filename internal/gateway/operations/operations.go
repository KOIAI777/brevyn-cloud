package operations

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/gateway/gatewayerror"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	OperationSyncRedemption             = "sync_redemption"
	OperationSyncAdminBalanceGrant      = "sync_admin_balance_grant"
	OperationDisableAPIKeyRemote        = "disable_api_key_remote"
	OperationSyncUserPolicyRemote       = "sync_user_policy_remote"
	OperationProvisionGatewayCredential = "provision_gateway_credential"

	StatusPending    = "pending"
	StatusRunning    = "running"
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"
	StatusDeadLetter = "dead_letter"
)

type CreateFailedInput struct {
	Operation      string
	TargetType     string
	TargetID       string
	UserDBID       int64
	IdempotencyKey string
	Payload        any
	Error          gatewayerror.Info
}

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func CreateRedemptionSync(ctx context.Context, tx pgx.Tx, redemptionDBID int64, redemptionPublicID string, userDBID int64) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"redemption_id":        redemptionPublicID,
		"redemption_db_id":     redemptionDBID,
		"user_db_id":           userDBID,
		"operation_semantics":  "sync Brevyn redemption to Sub2API",
		"idempotency_contract": "sub2api calls use redemption public id",
	})
	var publicID string
	err := tx.QueryRow(ctx, `
		INSERT INTO gateway_operations (
			public_id, provider, operation, target_type, target_id,
			redemption_id, user_id, status, idempotency_key, payload, next_run_at
		)
		VALUES ($1, 'sub2api', $2, 'redeem_redemption', $3, $4, $5, 'pending', $6, $7::jsonb, now() + interval '30 seconds')
		ON CONFLICT (idempotency_key) WHERE idempotency_key <> '' DO UPDATE
		SET updated_at = gateway_operations.updated_at
		RETURNING public_id
	`, "gop_"+uuid.NewString(), OperationSyncRedemption, redemptionPublicID, redemptionDBID, userDBID, "gateway:"+OperationSyncRedemption+":"+redemptionPublicID, string(payload)).Scan(&publicID)
	return publicID, err
}

func CreateFailed(ctx context.Context, db interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, input CreateFailedInput) (string, error) {
	payload, err := json.Marshal(input.Payload)
	if err != nil {
		return "", err
	}
	status := StatusFailed
	if !input.Error.Retryable {
		status = StatusDeadLetter
	}

	var publicID string
	err = db.QueryRow(ctx, `
		INSERT INTO gateway_operations (
			public_id, provider, operation, target_type, target_id, user_id,
			status, idempotency_key, payload, next_run_at, completed_at,
			last_error_message, last_error_code, last_error_class, last_error_stage,
			last_error_retryable, last_error_detail
		)
		VALUES (
			$1, 'sub2api', $2, $3, $4, nullif($5, 0),
			$6, $7, $8::jsonb, now(), CASE WHEN $6 = 'dead_letter' THEN now() ELSE NULL END,
			$9, $10, $11, $12, $13, $14
		)
		ON CONFLICT (idempotency_key) WHERE idempotency_key <> '' DO UPDATE
		SET updated_at = gateway_operations.updated_at
		RETURNING public_id
	`, "gop_"+uuid.NewString(), input.Operation, input.TargetType, input.TargetID, input.UserDBID,
		status, input.IdempotencyKey, string(payload), input.Error.Message, input.Error.Code,
		input.Error.Class, input.Error.Stage, input.Error.Retryable, input.Error.Detail).Scan(&publicID)
	return publicID, err
}

func EnsureGatewayProvision(ctx context.Context, db interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, userDBID int64, userPublicID string, externalGroupID int64, reason string) (string, error) {
	payload, _ := json.Marshal(map[string]any{
		"user_public_id":       userPublicID,
		"external_group_id":    externalGroupID,
		"provision_reason":     reason,
		"operation_semantics":  "create or load Sub2API shadow user and official API key",
		"idempotency_contract": "one active key per user/group in Brevyn Cloud",
	})
	var publicID string
	err := db.QueryRow(ctx, `
		INSERT INTO gateway_operations (
			public_id, provider, operation, target_type, target_id,
			user_id, status, idempotency_key, payload, next_run_at
		)
		VALUES ($1, 'sub2api', $2, 'user', $3, $4, 'pending', $5, $6::jsonb, now())
		ON CONFLICT (idempotency_key) WHERE idempotency_key <> '' DO UPDATE
		SET updated_at = gateway_operations.updated_at
		RETURNING public_id
	`, "gop_"+uuid.NewString(), OperationProvisionGatewayCredential, userPublicID, userDBID,
		"gateway:"+OperationProvisionGatewayCredential+":"+userPublicID+":"+strconv.FormatInt(externalGroupID, 10), string(payload)).Scan(&publicID)
	return publicID, err
}

func MarkSucceeded(ctx context.Context, db execer, publicID string, result any) error {
	resultJSON := "{}"
	if result != nil {
		if payload, err := json.Marshal(result); err == nil {
			resultJSON = string(payload)
		}
	}
	_, err := db.Exec(ctx, `
		UPDATE gateway_operations
		SET status = 'succeeded',
			last_error_message = '',
			last_error_code = '',
			last_error_class = '',
			last_error_stage = '',
			last_error_retryable = false,
			last_error_detail = '',
			result = $2::jsonb,
			completed_at = now(),
			locked_at = NULL,
			locked_by = '',
			updated_at = now()
		WHERE public_id = $1
	`, publicID, resultJSON)
	return err
}

func MarkFailed(ctx context.Context, db execer, publicID string, info gatewayerror.Info, nextRunAt time.Time, deadLetter bool) error {
	status := StatusFailed
	if deadLetter {
		status = StatusDeadLetter
	}
	_, err := db.Exec(ctx, `
		UPDATE gateway_operations
		SET status = $2,
			last_error_message = $3,
			last_error_code = $4,
			last_error_class = $5,
			last_error_stage = $6,
			last_error_retryable = $7,
			last_error_detail = $8,
			next_run_at = $9,
			completed_at = CASE WHEN $10 THEN now() ELSE completed_at END,
			locked_at = NULL,
			locked_by = '',
			updated_at = now()
		WHERE public_id = $1
	`, publicID, status, info.Message, info.Code, info.Class, info.Stage, info.Retryable, info.Detail, nextRunAt, deadLetter)
	return err
}

func EnsureRedemptionSync(ctx context.Context, db interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, redemptionDBID int64, redemptionPublicID string, userDBID int64) (string, error) {
	var publicID string
	err := db.QueryRow(ctx, `
		INSERT INTO gateway_operations (
			public_id, provider, operation, target_type, target_id,
			redemption_id, user_id, status, idempotency_key, payload, next_run_at
		)
		VALUES ($1, 'sub2api', $2, 'redeem_redemption', $3, $4, $5, 'pending', $6, '{}'::jsonb, now())
		ON CONFLICT (idempotency_key) WHERE idempotency_key <> '' DO UPDATE
		SET updated_at = gateway_operations.updated_at
		RETURNING public_id
	`, "gop_"+uuid.NewString(), OperationSyncRedemption, redemptionPublicID, redemptionDBID, userDBID, "gateway:"+OperationSyncRedemption+":"+redemptionPublicID).Scan(&publicID)
	return publicID, err
}

func Backoff(attempts int) time.Duration {
	switch {
	case attempts <= 1:
		return time.Minute
	case attempts == 2:
		return 5 * time.Minute
	case attempts == 3:
		return 15 * time.Minute
	case attempts <= 5:
		return time.Hour
	default:
		return 6 * time.Hour
	}
}
