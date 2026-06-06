package admin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/gateway/gatewayerror"
	"github.com/brevyn/brevyn-cloud/internal/gateway/operations"
	redeemsvc "github.com/brevyn/brevyn-cloud/internal/redeem"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type adminRedemptionSyncTarget = redeemsvc.SyncTarget
type adminGatewayUser = redeemsvc.GatewayUser
type adminGatewayAccountSummary = redeemsvc.GatewayAccountSummary

func (h *Handler) RetryRedemptionSync(c *gin.Context) {
	redemptionID := strings.TrimSpace(c.Param("id"))
	if redemptionID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "redemption_required"})
		return
	}
	var req struct {
		AuditReason string `json:"auditReason"`
		Reason      string `json:"reason"`
	}
	if !bindOptionalJSON(c, &req) {
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}

	target, err := h.redeem.LoadSyncTarget(c.Request.Context(), redemptionID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "redemption_not_found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "redemption_query_failed"})
		return
	}
	if target.Status == "synced" {
		item, _ := h.queryRedemptionByPublicID(c.Request.Context(), target.PublicID)
		c.JSON(http.StatusOK, gin.H{"status": "already_synced", "redemption": item})
		return
	}
	if target.Status != "gateway_failed" && target.Status != "pending_gateway" {
		c.JSON(http.StatusConflict, gin.H{"error": "redemption_not_retryable", "status": target.Status})
		return
	}
	if target.User.Status != "active" {
		c.JSON(http.StatusConflict, gin.H{"error": "user_not_active", "status": target.User.Status})
		return
	}
	defer h.redeem.InvalidateGatewayEntitlementsCache(c.Request.Context(), target.User.DBID)
	operationID, _ := operations.EnsureRedemptionSync(c.Request.Context(), h.postgres, target.DBID, target.PublicID, target.User.DBID)
	_, _ = h.postgres.Exec(c.Request.Context(), `
		UPDATE gateway_operations
		SET attempts = attempts + 1,
			status = 'running',
			started_at = coalesce(started_at, now()),
			locked_at = now(),
			locked_by = 'admin-manual',
			updated_at = now()
		WHERE public_id = $1
	`, operationID)

	syncCtx, cancel := context.WithTimeout(c.Request.Context(), 45*time.Second)
	defer cancel()

	account, operation, syncErr := h.redeem.SyncTargetToSub2API(syncCtx, target)
	if strings.TrimSpace(operation) != "" {
		target.GatewayOperation = operation
	}
	if syncErr != nil {
		errInfo := gatewayerror.Classify(target.GatewayOperation, syncErr)
		_ = operations.MarkFailed(c.Request.Context(), h.postgres, operationID, errInfo, time.Now().UTC().Add(operations.Backoff(1)), !errInfo.Retryable)
		_ = h.redeem.UpdateRedemptionStatus(
			c.Request.Context(),
			target.DBID,
			"gateway_failed",
			errInfo,
			account.ExternalUserID,
			target.ExternalGroupID,
			target.GatewayOperation,
		)
		item, _ := h.queryRedemptionByPublicID(c.Request.Context(), target.PublicID)
		c.JSON(http.StatusBadGateway, gin.H{"error": "redemption_sync_failed", "detail": syncErr.Error(), "redemption": item})
		return
	}

	if err := h.redeem.UpdateRedemptionStatus(
		c.Request.Context(),
		target.DBID,
		"synced",
		gatewayerror.Info{},
		account.ExternalUserID,
		target.ExternalGroupID,
		target.GatewayOperation,
	); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "redemption_status_update_failed"})
		return
	}
	settings, settingsErr := h.redeem.LoadSub2APISettings(c.Request.Context())
	if settingsErr != nil {
		errInfo := gatewayerror.Classify("settings", gatewayerror.WithStage("settings", settingsErr))
		_ = h.redeem.UpdateRedemptionError(c.Request.Context(), target.DBID, errInfo)
		_ = operations.MarkFailed(c.Request.Context(), h.postgres, operationID, errInfo, time.Now().UTC().Add(operations.Backoff(1)), !errInfo.Retryable)
		item, _ := h.queryRedemptionByPublicID(c.Request.Context(), target.PublicID)
		c.JSON(http.StatusBadGateway, gin.H{"error": "gateway_key_sync_failed", "detail": settingsErr.Error(), "redemption": item})
		return
	}
	keyErr := h.redeem.EnsureGatewayAPIKeyForOperation(c.Request.Context(), h.redeem.NewSub2APIClient(settings), target.User, account, target.ExternalGroupID, operationID)
	if keyErr != nil {
		errInfo := gatewayerror.Classify("ensure_api_key", gatewayerror.WithStage("ensure_api_key", keyErr))
		_ = h.redeem.UpdateRedemptionError(c.Request.Context(), target.DBID, errInfo)
		_ = operations.MarkFailed(c.Request.Context(), h.postgres, operationID, errInfo, time.Now().UTC().Add(operations.Backoff(1)), !errInfo.Retryable)
		item, _ := h.queryRedemptionByPublicID(c.Request.Context(), target.PublicID)
		c.JSON(http.StatusBadGateway, gin.H{"error": "gateway_key_sync_failed", "detail": keyErr.Error(), "redemption": item})
		return
	}
	if operationID != "" {
		_ = operations.MarkSucceeded(c.Request.Context(), h.postgres, operationID, map[string]any{
			"redemption_id":     target.PublicID,
			"external_user_id":  account.ExternalUserID,
			"external_group_id": target.ExternalGroupID,
			"operation":         target.GatewayOperation,
			"source":            "admin_manual_retry",
		})
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"redemption.retry_sync",
		"redeem_redemption",
		target.PublicID,
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{
			"user_id":           target.User.PublicID,
			"external_user_id":  account.ExternalUserID,
			"kind":              target.Kind,
			"external_group_id": target.ExternalGroupID,
		}),
	)

	item, err := h.queryRedemptionByPublicID(c.Request.Context(), target.PublicID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"status": "synced"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "synced", "redemption": item})
}
