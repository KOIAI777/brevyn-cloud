package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func (h *Handler) ListGatewayOperations(c *gin.Context) {
	limit, offset := parseListPagination(c, 50, 300)
	items, total, err := h.operations.List(c.Request.Context(), GatewayOperationListFilters{
		Search:     c.Query("search"),
		Status:     c.Query("status"),
		Operation:  c.Query("operation"),
		Provider:   c.Query("provider"),
		ErrorClass: c.Query("errorClass"),
		Retryable:  c.Query("retryable"),
		User:       c.Query("user"),
		DateFrom:   c.Query("dateFrom"),
		DateTo:     c.Query("dateTo"),
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		if err.Error() == "invalid_date" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "gateway_operations_query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) RetryGatewayOperation(c *gin.Context) {
	publicID := strings.TrimSpace(c.Param("id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "operation_required"})
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
	item, err := h.operations.Retry(c.Request.Context(), publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "operation_not_found"})
		return
	}
	if err != nil {
		switch err.Error() {
		case "operation_running":
			c.JSON(http.StatusConflict, gin.H{"error": "operation_running"})
		case "operation_already_succeeded":
			c.JSON(http.StatusConflict, gin.H{"error": "operation_already_succeeded"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "operation_retry_failed", "detail": err.Error()})
		}
		return
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"gateway_operation.retry",
		"gateway_operation",
		item.ID,
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{
			"operation":    item.Operation,
			"status":       item.Status,
			"attempts":     item.Attempts,
			"max_attempts": item.MaxAttempts,
			"target_id":    item.TargetID,
		}),
	)
	c.JSON(http.StatusOK, gin.H{"status": "queued", "operation": item})
}
