package admin

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
	"github.com/gin-gonic/gin"
)

func (h *Handler) ListSubscriptions(c *gin.Context) {
	limit, offset := parseListPagination(c, 50, 300)
	items, total, err := h.subscriptions.List(c.Request.Context(), SubscriptionListFilters{
		ExternalUserID:  parseOptionalInt64(firstNonEmpty(c.Query("externalUserId"), c.Query("user_id"), c.Query("userId"))),
		ExternalGroupID: parseOptionalInt64(firstNonEmpty(c.Query("externalGroupId"), c.Query("group_id"), c.Query("groupId"))),
		Status:          c.Query("status"),
		Platform:        c.Query("platform"),
		SortBy:          firstNonEmpty(c.Query("sortBy"), c.Query("sort_by")),
		SortOrder:       firstNonEmpty(c.Query("sortOrder"), c.Query("sort_order")),
		Limit:           limit,
		Offset:          offset,
	})
	if err != nil {
		writeSubscriptionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) AssignSubscription(c *gin.Context) {
	var req struct {
		ExternalUserID  int64  `json:"externalUserId"`
		ExternalGroupID int64  `json:"externalGroupId"`
		UserID          int64  `json:"user_id"`
		GroupID         int64  `json:"group_id"`
		ValidityDays    int    `json:"validityDays"`
		ValidityDaysRaw int    `json:"validity_days"`
		Notes           string `json:"notes"`
		IdempotencyKey  string `json:"idempotencyKey"`
		AuditReason     string `json:"auditReason"`
		Reason          string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}
	userID := req.ExternalUserID
	if userID == 0 {
		userID = req.UserID
	}
	groupID := req.ExternalGroupID
	if groupID == 0 {
		groupID = req.GroupID
	}
	validityDays := req.ValidityDays
	if validityDays == 0 {
		validityDays = req.ValidityDaysRaw
	}
	if userID <= 0 || groupID <= 0 || validityDays <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subscription_assignment_invalid"})
		return
	}

	item, err := h.subscriptions.Assign(c.Request.Context(), sub2api.AssignSubscriptionRequest{
		UserID:       userID,
		GroupID:      groupID,
		ValidityDays: validityDays,
		Notes:        strings.TrimSpace(req.Notes),
	}, firstNonEmpty(c.GetHeader("Idempotency-Key"), req.IdempotencyKey))
	if err != nil {
		writeSubscriptionError(c, err)
		return
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"subscription.assign",
		"subscription",
		strconv.FormatInt(item.ID, 10),
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{
			"external_user_id":  userID,
			"external_group_id": groupID,
			"validity_days":     validityDays,
			"notes":             strings.TrimSpace(req.Notes),
		}),
	)
	c.JSON(http.StatusOK, gin.H{"subscription": item})
}

func (h *Handler) ExtendSubscription(c *gin.Context) {
	subscriptionID, ok := parseSubscriptionParam(c)
	if !ok {
		return
	}
	var req struct {
		Days        int    `json:"days"`
		AuditReason string `json:"auditReason"`
		Reason      string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}
	if req.Days == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subscription_extend_days_required"})
		return
	}
	item, err := h.subscriptions.Extend(c.Request.Context(), subscriptionID, req.Days)
	if err != nil {
		writeSubscriptionError(c, err)
		return
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"subscription.extend",
		"subscription",
		strconv.FormatInt(subscriptionID, 10),
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{
			"days":              req.Days,
			"external_user_id":  item.UserID,
			"external_group_id": item.GroupID,
			"expires_at":        item.ExpiresAt,
		}),
	)
	c.JSON(http.StatusOK, gin.H{"subscription": item})
}

func (h *Handler) ResetSubscriptionQuota(c *gin.Context) {
	subscriptionID, ok := parseSubscriptionParam(c)
	if !ok {
		return
	}
	var req struct {
		Daily       bool   `json:"daily"`
		Weekly      bool   `json:"weekly"`
		Monthly     bool   `json:"monthly"`
		AuditReason string `json:"auditReason"`
		Reason      string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}
	if !req.Daily && !req.Weekly && !req.Monthly {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subscription_reset_quota_scope_required"})
		return
	}
	item, err := h.subscriptions.ResetQuota(c.Request.Context(), subscriptionID, req.Daily, req.Weekly, req.Monthly)
	if err != nil {
		writeSubscriptionError(c, err)
		return
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"subscription.reset_quota",
		"subscription",
		strconv.FormatInt(subscriptionID, 10),
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{
			"daily":             req.Daily,
			"weekly":            req.Weekly,
			"monthly":           req.Monthly,
			"external_user_id":  item.UserID,
			"external_group_id": item.GroupID,
		}),
	)
	c.JSON(http.StatusOK, gin.H{"subscription": item})
}

func (h *Handler) RevokeSubscription(c *gin.Context) {
	subscriptionID, ok := parseSubscriptionParam(c)
	if !ok {
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
	if err := h.subscriptions.Revoke(c.Request.Context(), subscriptionID); err != nil {
		writeSubscriptionError(c, err)
		return
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"subscription.revoke",
		"subscription",
		strconv.FormatInt(subscriptionID, 10),
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, nil),
	)
	c.JSON(http.StatusOK, gin.H{"status": "revoked"})
}

func parseSubscriptionParam(c *gin.Context) (int64, bool) {
	subscriptionID, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || subscriptionID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "subscription_id_invalid"})
		return 0, false
	}
	return subscriptionID, true
}

func writeSubscriptionError(c *gin.Context, err error) {
	code := gatewaySettingsErrorCode(err)
	status := http.StatusInternalServerError
	if strings.HasPrefix(code, "sub2api_") {
		status = http.StatusBadGateway
	}
	c.JSON(status, gin.H{"error": code, "detail": err.Error()})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
