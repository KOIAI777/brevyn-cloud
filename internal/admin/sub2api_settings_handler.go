package admin

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
	"github.com/gin-gonic/gin"
)

const (
	settingSub2APIBaseURL       = "sub2api.base_url"
	settingSub2APIAdminAPIKey   = "sub2api.admin_api_key"
	settingSub2APIAdminEmail    = "sub2api.admin_email"
	settingSub2APIAdminPassword = "sub2api.admin_password"
	settingSub2APIDefaultGroup  = "sub2api.default_group_id"
)

type Sub2APISettingsResponse struct {
	BaseURL               string `json:"baseUrl"`
	AdminEmail            string `json:"adminEmail"`
	HasAdminPassword      bool   `json:"hasAdminPassword"`
	AdminAPIKeyConfigured bool   `json:"adminApiKeyConfigured"`
	AuthMode              string `json:"authMode"`
	DefaultGroupID        int64  `json:"defaultGroupId"`
}

type updateSub2APISettingsRequest struct {
	BaseURL        string  `json:"baseUrl"`
	AdminEmail     string  `json:"adminEmail"`
	AdminPassword  *string `json:"adminPassword"`
	AdminAPIKey    *string `json:"adminApiKey"`
	DefaultGroupID *int64  `json:"defaultGroupId"`
}

type sub2APIRuntimeSettings struct {
	BaseURL       string
	AdminAPIKey   string
	AdminEmail    string
	AdminPassword string
}

type Sub2APITestResult struct {
	OK            bool                    `json:"ok"`
	Status        string                  `json:"status"`
	BaseURL       string                  `json:"baseUrl"`
	AuthMode      string                  `json:"authMode"`
	HealthOK      bool                    `json:"healthOk"`
	AuthOK        bool                    `json:"authOk"`
	GroupCount    int                     `json:"groupCount"`
	LatencyMs     int64                   `json:"latencyMs"`
	Error         string                  `json:"error"`
	GroupsPreview []Sub2APIGroupPreview   `json:"groupsPreview"`
	Health        *sub2api.HealthResponse `json:"health,omitempty"`
}

type Sub2APIGroupPreview struct {
	ExternalGroupID  int64   `json:"externalGroupId"`
	Name             string  `json:"name"`
	Platform         string  `json:"platform"`
	SubscriptionType string  `json:"subscriptionType"`
	RateMultiplier   float64 `json:"rateMultiplier"`
	RPMLimit         int     `json:"rpmLimit"`
	SortOrder        int     `json:"sortOrder"`
	Status           string  `json:"status"`
}

func (h *Handler) GetSub2APISettings(c *gin.Context) {
	settings, err := h.gateway.Load(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "settings_load_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": h.gateway.SettingsResponse(settings, h.defaultExternalGroupID(c.Request.Context()))})
}

func (h *Handler) UpdateSub2APISettings(c *gin.Context) {
	var req updateSub2APISettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	admin, _ := currentAdmin(c)
	ctx := c.Request.Context()
	settings, err := h.gateway.Update(ctx, req, admin.ID)
	if err != nil {
		status := http.StatusInternalServerError
		errorCode := "settings_save_failed"
		if isGatewaySettingsValidationError(err) {
			status = http.StatusBadRequest
			errorCode = err.Error()
		}
		c.JSON(status, gin.H{"error": errorCode})
		return
	}
	h.writeAuditLog(ctx, "admin", admin.ID, "sub2api.settings.update", "app_settings", "sub2api", c.ClientIP(), c.Request.UserAgent(), "{}")
	c.JSON(http.StatusOK, gin.H{"settings": h.gateway.SettingsResponse(settings, h.defaultExternalGroupID(ctx))})
}

func (h *Handler) TestSub2APIConnection(c *gin.Context) {
	c.JSON(http.StatusOK, h.gateway.TestConnection(c.Request.Context()))
}

func (h *Handler) SyncSub2APIGroups(c *gin.Context) {
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
	locked, unlock, err := h.tryAdminJobLock(c.Request.Context(), "brevyn:lock:admin:sync-groups", 60*time.Second)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "sync_lock_unavailable", "detail": err.Error()})
		return
	}
	if !locked {
		c.Header("Retry-After", strconv.Itoa(60))
		c.JSON(http.StatusConflict, gin.H{"error": "sync_already_running"})
		return
	}
	defer unlock()
	result, err := h.gateway.SyncGroups(c.Request.Context())
	if err != nil {
		errorCode := gatewaySettingsErrorCode(err)
		status := http.StatusInternalServerError
		if errorCode == "sub2api_group_fetch_failed" {
			status = http.StatusBadGateway
		}
		c.JSON(status, gin.H{"error": errorCode, "detail": err.Error()})
		return
	}
	admin, _ := currentAdmin(c)
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "sub2api.groups.sync", "gateway_groups", "sub2api", c.ClientIP(), c.Request.UserAgent(), auditMetadataWithReason(auditReason, map[string]any{
		"synced": result.Synced,
		"total":  result.Total,
	}))
	c.JSON(http.StatusOK, result)
}

func (h *Handler) SyncSub2APIModels(c *gin.Context) {
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
	locked, unlock, err := h.tryAdminJobLock(c.Request.Context(), "brevyn:lock:admin:sync-models", 180*time.Second)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "sync_lock_unavailable", "detail": err.Error()})
		return
	}
	if !locked {
		c.Header("Retry-After", strconv.Itoa(180))
		c.JSON(http.StatusConflict, gin.H{"error": "sync_already_running"})
		return
	}
	defer unlock()
	result, err := h.gateway.SyncModels(c.Request.Context())
	if err != nil {
		errorCode := gatewaySettingsErrorCode(err)
		status := http.StatusInternalServerError
		if errorCode == "sub2api_group_fetch_failed" || errorCode == "sub2api_channel_fetch_failed" {
			status = http.StatusBadGateway
		}
		c.JSON(status, gin.H{"error": errorCode, "detail": err.Error()})
		return
	}
	admin, _ := currentAdmin(c)
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "sub2api.models.sync", "gateway_group_models", "sub2api", c.ClientIP(), c.Request.UserAgent(), auditMetadataWithReason(auditReason, map[string]any{
		"synced_groups":   result.SyncedGroups,
		"synced_channels": result.SyncedChannels,
		"synced_models":   result.SyncedModels,
		"total_groups":    result.TotalGroups,
		"total_channels":  result.TotalChannels,
	}))
	c.JSON(http.StatusOK, result)
}

func (h *Handler) loadSub2APISettings(ctx context.Context) (sub2APIRuntimeSettings, error) {
	return h.gateway.Load(ctx)
}

func (h *Handler) newSub2APIClient(settings sub2APIRuntimeSettings) *sub2api.Client {
	return h.gateway.NewSub2APIClient(settings)
}

func (h *Handler) encryptSecret(plain string) (string, error) {
	return h.gateway.EncryptSecret(plain)
}

func (h *Handler) decryptSecret(value string) (string, error) {
	return h.gateway.DecryptSecret(value)
}

func gatewaySettingsErrorCode(err error) string {
	message := err.Error()
	if index := strings.Index(message, ":"); index > 0 {
		return message[:index]
	}
	if message == "" {
		return "gateway_settings_failed"
	}
	return message
}

func normalizeSub2APIGroup(group sub2api.AdminGroup) sub2api.AdminGroup {
	group.Name = strings.TrimSpace(group.Name)
	if group.Name == "" {
		group.Name = fmt.Sprintf("sub2api-group-%d", group.ID)
	}
	group.Description = strings.TrimSpace(group.Description)
	group.Platform = strings.TrimSpace(group.Platform)
	if group.Platform == "" {
		group.Platform = "anthropic"
	}
	group.SubscriptionType = strings.TrimSpace(group.SubscriptionType)
	if group.SubscriptionType == "" {
		group.SubscriptionType = "standard"
	}
	if group.RateMultiplier == 0 {
		group.RateMultiplier = 1
	}
	if group.DefaultValidityDays == 0 {
		group.DefaultValidityDays = 30
	}
	if group.ImageRateMultiplier == 0 {
		group.ImageRateMultiplier = 1
	}
	if group.ModelRouting == nil {
		group.ModelRouting = map[string][]int64{}
	}
	if group.SupportedModelScopes == nil {
		group.SupportedModelScopes = []string{}
	}
	if group.MessagesDispatchModelConfig == nil {
		group.MessagesDispatchModelConfig = map[string]any{}
	}
	group.Status = strings.TrimSpace(group.Status)
	if group.Status == "" {
		group.Status = "active"
	}
	return group
}

func sub2APIGroupPreviews(groups []sub2api.AdminGroup, limit int) []Sub2APIGroupPreview {
	if limit <= 0 || limit > len(groups) {
		limit = len(groups)
	}
	out := make([]Sub2APIGroupPreview, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, groupPreview(normalizeSub2APIGroup(groups[i])))
	}
	return out
}

func groupPreview(group sub2api.AdminGroup) Sub2APIGroupPreview {
	return Sub2APIGroupPreview{
		ExternalGroupID:  group.ID,
		Name:             group.Name,
		Platform:         group.Platform,
		SubscriptionType: group.SubscriptionType,
		RateMultiplier:   group.RateMultiplier,
		RPMLimit:         group.RPMLimit,
		SortOrder:        group.SortOrder,
		Status:           group.Status,
	}
}
