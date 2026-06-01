package admin

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *Handler) Diagnostics(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"diagnostics": h.diagnostics.Snapshot(c.Request.Context())})
}

func (h *Handler) RetryFailedGatewayOperations(c *gin.Context) {
	admin, ok := currentAdmin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
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
	count, err := h.operations.RetryFailed(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "gateway_operations_retry_failed", "detail": err.Error()})
		return
	}
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"gateway_operation.retry_failed",
		"gateway_operations",
		"retryable_failed",
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{"count": count}),
	)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "retried": count})
}
