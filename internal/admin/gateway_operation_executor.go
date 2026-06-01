package admin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brevyn/brevyn-cloud/internal/config"
	"github.com/brevyn/brevyn-cloud/internal/gateway/gatewayerror"
	"github.com/brevyn/brevyn-cloud/internal/gateway/operations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type GatewayOperationExecutor struct {
	handler *Handler
}

func NewGatewayOperationExecutor(cfg *config.Config, postgres *pgxpool.Pool, redisClients ...*redis.Client) *GatewayOperationExecutor {
	var redisClient *redis.Client
	if len(redisClients) > 0 {
		redisClient = redisClients[0]
	}
	return &GatewayOperationExecutor{
		handler: NewHandler(cfg, postgres, redisClient),
	}
}

func (e *GatewayOperationExecutor) ExecuteGatewayOperation(ctx context.Context, op operations.OperationRecord) (map[string]any, error) {
	switch op.Operation {
	case operations.OperationSyncRedemption:
		return e.syncRedemption(ctx, op)
	case operations.OperationSyncAdminBalanceGrant:
		return e.syncAdminBalanceGrant(ctx, op)
	case operations.OperationDisableAPIKeyRemote:
		return e.disableAPIKeyRemote(ctx, op)
	case operations.OperationSyncUserPolicyRemote:
		return e.syncUserPolicyRemote(ctx, op)
	case operations.OperationProvisionGatewayCredential:
		return e.provisionGatewayCredential(ctx, op)
	default:
		return nil, fmt.Errorf("unsupported gateway operation %s/%s", op.Operation, op.TargetType)
	}
}

func (e *GatewayOperationExecutor) syncRedemption(ctx context.Context, op operations.OperationRecord) (map[string]any, error) {
	if op.TargetType != "redeem_redemption" {
		return nil, fmt.Errorf("unsupported gateway operation %s/%s", op.Operation, op.TargetType)
	}
	target, err := e.handler.redeem.LoadSyncTarget(ctx, op.TargetID)
	if err != nil {
		return nil, gatewayerror.WithStage("load_redemption", err)
	}
	if target.User.Status != "active" {
		return nil, gatewayerror.WithStage("ensure_user", fmt.Errorf("user is not active: %s", target.User.Status))
	}

	settings, err := e.handler.redeem.LoadSub2APISettings(ctx)
	if err != nil {
		return nil, gatewayerror.WithStage("settings", err)
	}
	client := e.handler.redeem.NewSub2APIClient(settings)
	var account adminGatewayAccountSummary
	if target.Status == "synced" {
		account, err = e.handler.redeem.EnsureSub2APIAccount(ctx, client, target.User, target.ExternalGroupID)
		if err != nil {
			return nil, gatewayerror.WithStage("ensure_user", err)
		}
	} else {
		account, err = e.handler.redeem.SyncTargetToSub2API(ctx, target)
		if err != nil {
			info := gatewayerror.Classify(target.GatewayOperation, err)
			_ = e.handler.redeem.UpdateRedemptionStatus(ctx, target.DBID, "gateway_failed", info, account.ExternalUserID, target.ExternalGroupID, target.GatewayOperation)
			return nil, err
		}
		if err := e.handler.redeem.UpdateRedemptionStatus(ctx, target.DBID, "synced", gatewayerror.Info{}, account.ExternalUserID, target.ExternalGroupID, target.GatewayOperation); err != nil {
			return nil, gatewayerror.WithStage("mark_synced", err)
		}
	}
	if err := e.handler.redeem.EnsureGatewayAPIKeyForOperation(ctx, client, target.User, account, target.ExternalGroupID, op.PublicID); err != nil {
		info := gatewayerror.Classify("ensure_api_key", gatewayerror.WithStage("ensure_api_key", err))
		_ = e.handler.redeem.UpdateRedemptionError(ctx, target.DBID, info)
		return nil, gatewayerror.WithStage("ensure_api_key", err)
	}

	return map[string]any{
		"redemption_id":     target.PublicID,
		"external_user_id":  account.ExternalUserID,
		"external_group_id": target.ExternalGroupID,
		"operation":         target.GatewayOperation,
	}, nil
}

func (e *GatewayOperationExecutor) syncAdminBalanceGrant(ctx context.Context, op operations.OperationRecord) (map[string]any, error) {
	var payload struct {
		UserPublicID  string  `json:"user_public_id"`
		TransactionID string  `json:"transaction_id"`
		Amount        float64 `json:"amount"`
	}
	if err := json.Unmarshal([]byte(op.Payload), &payload); err != nil {
		return nil, gatewayerror.WithStage("decode_payload", err)
	}
	if payload.UserPublicID == "" || payload.TransactionID == "" || payload.Amount <= 0 {
		return nil, gatewayerror.WithStage("decode_payload", fmt.Errorf("invalid admin balance grant payload"))
	}
	if err := e.handler.keys.SyncAdminBalanceGrantRemote(ctx, payload.UserPublicID, payload.TransactionID, payload.Amount); err != nil {
		return nil, gatewayerror.WithStage("sync_admin_balance_grant", err)
	}
	return map[string]any{
		"transaction_id": payload.TransactionID,
		"user_public_id": payload.UserPublicID,
		"amount":         payload.Amount,
	}, nil
}

func (e *GatewayOperationExecutor) disableAPIKeyRemote(ctx context.Context, op operations.OperationRecord) (map[string]any, error) {
	var payload struct {
		APIKeyID string `json:"api_key_id"`
	}
	if err := json.Unmarshal([]byte(op.Payload), &payload); err != nil {
		return nil, gatewayerror.WithStage("decode_payload", err)
	}
	if payload.APIKeyID == "" {
		return nil, gatewayerror.WithStage("decode_payload", fmt.Errorf("invalid api key payload"))
	}
	if err := e.handler.keys.SyncDisabledAPIKeyRemote(ctx, payload.APIKeyID); err != nil {
		return nil, gatewayerror.WithStage("disable_api_key_remote", err)
	}
	return map[string]any{"api_key_id": payload.APIKeyID}, nil
}

func (e *GatewayOperationExecutor) syncUserPolicyRemote(ctx context.Context, op operations.OperationRecord) (map[string]any, error) {
	var payload UserPolicySyncInput
	if err := json.Unmarshal([]byte(op.Payload), &payload); err != nil {
		return nil, gatewayerror.WithStage("decode_payload", err)
	}
	if payload.UserPublicID == "" {
		return nil, gatewayerror.WithStage("decode_payload", fmt.Errorf("invalid user policy payload"))
	}
	if err := e.handler.keys.SyncUserPolicyRemote(ctx, payload); err != nil {
		return nil, gatewayerror.WithStage("sync_user_policy_remote", err)
	}
	return map[string]any{
		"user_public_id":    payload.UserPublicID,
		"status":            payload.Status,
		"external_group_id": payload.ExternalGroupID,
		"concurrency":       payload.Concurrency,
	}, nil
}

func (e *GatewayOperationExecutor) provisionGatewayCredential(ctx context.Context, op operations.OperationRecord) (map[string]any, error) {
	var payload struct {
		UserPublicID    string `json:"user_public_id"`
		ExternalGroupID int64  `json:"external_group_id"`
	}
	if err := json.Unmarshal([]byte(op.Payload), &payload); err != nil {
		return nil, gatewayerror.WithStage("decode_payload", err)
	}
	userPublicID := payload.UserPublicID
	if userPublicID == "" {
		userPublicID = op.TargetID
	}
	if userPublicID == "" {
		return nil, gatewayerror.WithStage("decode_payload", fmt.Errorf("invalid gateway provision payload"))
	}
	user, err := e.handler.keys.loadAdminGatewayUserByPublicID(ctx, userPublicID)
	if err != nil {
		return nil, gatewayerror.WithStage("load_user", err)
	}
	if user.Status != "active" {
		return nil, gatewayerror.WithStage("ensure_user", fmt.Errorf("user is not active: %s", user.Status))
	}
	groupID := payload.ExternalGroupID
	if groupID <= 0 {
		groupID = e.handler.redeem.DefaultExternalGroupID(ctx)
	}
	if groupID <= 0 {
		return nil, gatewayerror.WithStage("default_group", fmt.Errorf("default gateway group is not configured"))
	}
	settings, err := e.handler.redeem.LoadSub2APISettings(ctx)
	if err != nil {
		return nil, gatewayerror.WithStage("settings", err)
	}
	client := e.handler.redeem.NewSub2APIClient(settings)
	account, err := e.handler.redeem.EnsureSub2APIAccountForGroup(ctx, client, user, groupID)
	if err != nil {
		return nil, gatewayerror.WithStage("ensure_user", err)
	}
	if err := e.handler.redeem.EnsureGatewayAPIKeyForOperation(ctx, client, user, account, groupID, op.PublicID); err != nil {
		return nil, gatewayerror.WithStage("ensure_api_key", err)
	}
	return map[string]any{
		"user_public_id":    user.PublicID,
		"external_user_id":  account.ExternalUserID,
		"external_group_id": groupID,
	}, nil
}
