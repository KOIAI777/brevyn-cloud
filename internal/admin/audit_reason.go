package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const maxAuditReasonLength = 500

func bindOptionalJSON(c *gin.Context, dst any) bool {
	if c.Request.Body == nil {
		return true
	}
	if err := c.ShouldBindJSON(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return false
	}
	return true
}

func requireAuditReason(c *gin.Context, auditReason string, reason string) (string, bool) {
	resolved := normalizeAuditReason(auditReason, reason)
	if resolved == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "audit_reason_required"})
		return "", false
	}
	if len([]rune(resolved)) > maxAuditReasonLength {
		c.JSON(http.StatusBadRequest, gin.H{"error": "audit_reason_too_long", "max": maxAuditReasonLength})
		return "", false
	}
	return resolved, true
}

func normalizeAuditReason(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func auditMetadata(fields map[string]any) string {
	if fields == nil {
		fields = map[string]any{}
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return "{}"
	}
	return string(payload)
}

func auditMetadataWithReason(reason string, fields map[string]any) string {
	if fields == nil {
		fields = map[string]any{}
	}
	if strings.TrimSpace(reason) != "" {
		fields["reason"] = strings.TrimSpace(reason)
	}
	return auditMetadata(fields)
}
