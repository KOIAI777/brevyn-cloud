package admin

import (
	"errors"
	"net/http"
	"strings"

	redeemsvc "github.com/brevyn/brevyn-cloud/internal/redeem"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type createAdminUserRequest struct {
	Email           string `json:"email"`
	DisplayName     string `json:"displayName"`
	Password        string `json:"password"`
	SyncSub2API     *bool  `json:"syncSub2api"`
	ExternalGroupID int64  `json:"externalGroupId"`
}

type importSub2APIUserRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

func (h *Handler) CreateUser(c *gin.Context) {
	var req createAdminUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	user, generatedPassword, err := h.users.Create(c.Request.Context(), req.Email, req.DisplayName, req.Password)
	if isAdminUniqueViolation(err) {
		c.JSON(http.StatusConflict, gin.H{"error": "email_already_registered"})
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "invalid_email" || err.Error() == "password_too_short" {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	syncRemote := true
	if req.SyncSub2API != nil {
		syncRemote = *req.SyncSub2API
	}
	var account *adminGatewayAccountSummary
	var apiKey *adminGatewayAPIKeyResponse
	plainAPIKey := ""
	gatewayWarning := ""
	selectedGroupID := req.ExternalGroupID
	if syncRemote {
		if selectedGroupID <= 0 {
			selectedGroupID = h.defaultExternalGroupID(c.Request.Context())
		}
		if selectedGroupID <= 0 {
			gatewayWarning = "sub2api_default_group_not_configured"
		}
		gatewayUser, loadErr := h.keys.loadAdminGatewayUserByPublicID(c.Request.Context(), user.ID)
		if loadErr != nil {
			gatewayWarning = loadErr.Error()
		} else if gatewayWarning == "" {
			settings, loadErr := h.loadSub2APISettings(c.Request.Context())
			if loadErr != nil {
				gatewayWarning = loadErr.Error()
			} else {
				client := h.newSub2APIClient(settings)
				created, ensureErr := h.redeem.EnsureSub2APIAccount(c.Request.Context(), client, gatewayUser, selectedGroupID)
				if ensureErr != nil {
					gatewayWarning = ensureErr.Error()
				} else {
					account = &created
					keySummary, keyPlain, keyErr := h.redeem.EnsureGatewayAPIKeyForUser(c.Request.Context(), client, gatewayUser, created, selectedGroupID)
					if keyErr != nil {
						gatewayWarning = keyErr.Error()
					} else if keySummary != nil {
						plainAPIKey = keyPlain
						apiKey = &adminGatewayAPIKeyResponse{
							ID:              keySummary.ID,
							Provider:        keySummary.Provider,
							ExternalKeyID:   keySummary.ExternalKeyID,
							ExternalGroupID: keySummary.ExternalGroupID,
							MaskedAPIKey:    keySummary.MaskedAPIKey,
							Status:          keySummary.Status,
							UserID:          user.ID,
							UserEmail:       user.Email,
							RemoteSync:      "created",
							LastUsedAt:      keySummary.LastUsedAt,
							CreatedAt:       keySummary.CreatedAt,
						}
					}
				}
			}
		}
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"user.create",
		"user",
		user.ID,
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason("管理员创建用户", map[string]any{
			"email":             user.Email,
			"sync_sub2api":      syncRemote,
			"external_group_id": selectedGroupID,
			"concurrency":       redeemsvc.ManagedUserConcurrency,
			"api_key_created":   plainAPIKey != "",
			"sync_warning":      gatewayWarning,
		}),
	)

	status := http.StatusCreated
	if gatewayWarning != "" {
		status = http.StatusAccepted
	}
	c.JSON(status, gin.H{
		"user":              user,
		"generatedPassword": generatedPassword,
		"gatewayAccount":    account,
		"apiKey":            apiKey,
		"plainApiKey":       plainAPIKey,
		"gatewayWarning":    gatewayWarning,
		"managementMode":    "brevyn_managed",
	})
}

func (h *Handler) ImportSub2APIUser(c *gin.Context) {
	var req importSub2APIUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_email"})
		return
	}

	settings, err := h.loadSub2APISettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "sub2api_not_configured", "detail": err.Error()})
		return
	}
	externalUser, err := h.newSub2APIClient(settings).FindUserByEmail(c.Request.Context(), email)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "sub2api_user_not_found", "detail": err.Error()})
		return
	}

	user, err := h.users.GetByEmail(c.Request.Context(), email)
	created := false
	generatedPassword := ""
	if errors.Is(err, pgx.ErrNoRows) {
		user, generatedPassword, err = h.users.Create(c.Request.Context(), email, req.DisplayName, req.Password)
		created = true
	}
	if err != nil {
		status := http.StatusInternalServerError
		if err.Error() == "password_too_short" {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	applied, balanceAdjusted, err := h.users.SyncSub2APIUser(c.Request.Context(), *externalUser)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "sub2api_user_sync_failed", "detail": err.Error()})
		return
	}
	if !applied {
		c.JSON(http.StatusConflict, gin.H{"error": "sub2api_user_bind_failed"})
		return
	}
	user, err = h.users.GetByEmail(c.Request.Context(), email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user_query_failed"})
		return
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"user.import_sub2api",
		"user",
		user.ID,
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason("绑定已有 Sub2API 用户", map[string]any{
			"email":            user.Email,
			"external_user_id": externalUser.ID,
			"created":          created,
			"balance_adjusted": balanceAdjusted,
		}),
	)
	c.JSON(http.StatusOK, gin.H{
		"user":              user,
		"generatedPassword": generatedPassword,
		"created":           created,
		"balanceAdjusted":   balanceAdjusted,
		"managementMode":    "mapped_existing_account",
		"managementWarning": "已有 Sub2API 账号仅完成映射；若其密码不是 Brevyn 影子密码，自动轮换和禁用远端 Key 可能失败。",
	})
}
