package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type grantBalanceRequest struct {
	Amount         float64 `json:"amount"`
	Notes          string  `json:"notes"`
	SyncSub2API    *bool   `json:"syncSub2api"`
	IdempotencyKey string  `json:"idempotencyKey"`
	AuditReason    string  `json:"auditReason"`
	Reason         string  `json:"reason"`
}

type rotateUserAPIKeyRequest struct {
	ExternalGroupID int64  `json:"externalGroupId"`
	AuditReason     string `json:"auditReason"`
	Reason          string `json:"reason"`
}

type changeUserGatewayGroupRequest struct {
	ExternalGroupID int64  `json:"externalGroupId"`
	AuditReason     string `json:"auditReason"`
	Reason          string `json:"reason"`
}

type updateUserConcurrencyRequest struct {
	Concurrency int    `json:"concurrency"`
	AuditReason string `json:"auditReason"`
	Reason      string `json:"reason"`
}

func (h *Handler) GrantUserBalance(c *gin.Context) {
	userPublicID := strings.TrimSpace(c.Param("id"))
	var req grantBalanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	req.Notes = strings.TrimSpace(req.Notes)
	if !isFiniteAmount(req.Amount) || req.Amount <= 0 || req.Amount > 100000 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount_must_be_0_to_100000"})
		return
	}
	if !hasMaxDecimalPlaces(req.Amount, 2) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "amount_precision_invalid"})
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}
	syncRemote := true
	if req.SyncSub2API != nil {
		syncRemote = *req.SyncSub2API
	}

	admin, _ := currentAdmin(c)
	idempotencyKey := firstNonEmpty(c.GetHeader("Idempotency-Key"), req.IdempotencyKey)
	result, err := h.keys.GrantUserBalance(c.Request.Context(), userPublicID, admin.ID, req.Amount, req.Notes, syncRemote, idempotencyKey)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
		return
	}
	if err != nil && err.Error() == "user_not_active" {
		c.JSON(http.StatusConflict, gin.H{"error": "user_not_active", "status": result.User.Status})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "grant_balance_failed"})
		return
	}

	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"user.balance_grant",
		"user",
		result.User.PublicID,
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{
			"amount":         req.Amount,
			"balance_after":  result.BalanceAfter,
			"transaction_id": result.TransactionID,
			"sync_warning":   result.SyncWarning,
			"sync_operation": result.SyncOperation,
			"notes":          req.Notes,
		}),
	)

	statusCode := http.StatusOK
	if result.SyncWarning != "" {
		statusCode = http.StatusAccepted
	}
	c.JSON(statusCode, gin.H{
		"status":        "ok",
		"transactionId": result.TransactionID,
		"balance":       result.BalanceAfter,
		"syncWarning":   result.SyncWarning,
		"syncOperation": result.SyncOperation,
	})
}

func (h *Handler) RotateUserAPIKey(c *gin.Context) {
	userPublicID := strings.TrimSpace(c.Param("id"))
	var req rotateUserAPIKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}

	result, err := h.keys.RotateUserAPIKey(c.Request.Context(), userPublicID, req.ExternalGroupID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
		return
	}
	if err != nil {
		errorCode := gatewaySettingsErrorCode(err)
		switch errorCode {
		case "user_not_active":
			c.JSON(http.StatusConflict, gin.H{"error": "user_not_active", "status": result.User.Status})
		case "gateway_group_not_configured":
			c.JSON(http.StatusConflict, gin.H{"error": errorCode})
		case "sub2api_not_configured":
			c.JSON(http.StatusConflict, gin.H{"error": errorCode, "detail": err.Error()})
		case "gateway_account_sync_failed", "gateway_user_login_failed", "gateway_key_create_failed":
			c.JSON(http.StatusBadGateway, gin.H{"error": errorCode, "detail": err.Error()})
		case "gateway_key_store_failed":
			c.JSON(http.StatusInternalServerError, gin.H{"error": errorCode, "detail": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "gateway_key_rotate_failed", "detail": err.Error()})
		}
		return
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"gateway_api_key.rotate",
		"user",
		result.User.PublicID,
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{
			"external_group_id": result.ExternalGroupID,
			"disabled_old_keys": result.DisabledCount,
			"warnings":          strings.Join(result.Warnings, "; "),
		}),
	)
	c.JSON(http.StatusCreated, gin.H{
		"apiKey":       adminGatewayAPIKeyToResponse(result.Record, "created", ""),
		"plainApiKey":  result.PlainAPIKey,
		"syncWarnings": result.Warnings,
	})
}

func (h *Handler) ChangeUserGatewayGroup(c *gin.Context) {
	userPublicID := strings.TrimSpace(c.Param("id"))
	var req changeUserGatewayGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}

	result, err := h.keys.ChangeUserGatewayGroup(c.Request.Context(), userPublicID, req.ExternalGroupID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
		return
	}
	if err != nil {
		errorCode := gatewaySettingsErrorCode(err)
		switch errorCode {
		case "user_not_active":
			c.JSON(http.StatusConflict, gin.H{"error": errorCode, "status": result.User.Status})
		case "gateway_group_required", "gateway_group_not_found", "gateway_group_not_active", "gateway_group_must_be_standard":
			c.JSON(http.StatusBadRequest, gin.H{"error": errorCode})
		case "sub2api_not_configured":
			c.JSON(http.StatusConflict, gin.H{"error": errorCode, "detail": err.Error()})
		case "gateway_account_sync_failed", "gateway_user_login_failed", "gateway_key_create_failed":
			c.JSON(http.StatusBadGateway, gin.H{"error": errorCode, "detail": err.Error()})
		case "gateway_key_store_failed":
			c.JSON(http.StatusInternalServerError, gin.H{"error": errorCode, "detail": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "gateway_group_change_failed", "detail": err.Error()})
		}
		return
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"user.gateway_group_change",
		"user",
		result.User.PublicID,
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{
			"external_group_id": result.ExternalGroupID,
			"disabled_old_keys": result.DisabledCount,
			"warnings":          strings.Join(result.Warnings, "; "),
		}),
	)
	c.JSON(http.StatusCreated, gin.H{
		"apiKey":       adminGatewayAPIKeyToResponse(result.Record, "created", ""),
		"plainApiKey":  result.PlainAPIKey,
		"syncWarnings": result.Warnings,
	})
}

func (h *Handler) UpdateUserConcurrency(c *gin.Context) {
	userPublicID := strings.TrimSpace(c.Param("id"))
	var req updateUserConcurrencyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}

	admin, _ := currentAdmin(c)
	result, err := h.keys.UpdateUserConcurrency(c.Request.Context(), userPublicID, req.Concurrency)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
		return
	}
	if err != nil {
		errorCode := gatewaySettingsErrorCode(err)
		switch errorCode {
		case "concurrency_must_be_1_to_500":
			c.JSON(http.StatusBadRequest, gin.H{"error": errorCode})
		case "user_not_active":
			c.JSON(http.StatusConflict, gin.H{"error": errorCode, "status": result.User.Status})
		case "sub2api_not_configured":
			c.JSON(http.StatusConflict, gin.H{"error": errorCode, "detail": err.Error()})
		default:
			h.writeAuditLog(
				c.Request.Context(),
				"admin",
				admin.ID,
				"user.concurrency_update",
				"user",
				result.User.PublicID,
				c.ClientIP(),
				c.Request.UserAgent(),
				auditMetadataWithReason(auditReason, map[string]any{
					"external_group_id": result.ExternalGroupID,
					"concurrency":       req.Concurrency,
					"sync_operation":    result.SyncOperation,
					"sync_warning":      err.Error(),
				}),
			)
			c.JSON(http.StatusAccepted, gin.H{
				"status":          "queued",
				"externalGroupId": result.ExternalGroupID,
				"concurrency":     req.Concurrency,
				"syncOperation":   result.SyncOperation,
			})
		}
		return
	}

	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"user.concurrency_update",
		"user",
		result.User.PublicID,
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{
			"external_group_id": result.ExternalGroupID,
			"concurrency":       result.Concurrency,
		}),
	)
	c.JSON(http.StatusOK, gin.H{
		"status":          "ok",
		"externalGroupId": result.ExternalGroupID,
		"concurrency":     result.Concurrency,
	})
}
