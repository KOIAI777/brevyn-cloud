package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/config"
	"github.com/brevyn/brevyn-cloud/internal/gateway/gatewayerror"
	"github.com/brevyn/brevyn-cloud/internal/gateway/operations"
	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
	redeemsvc "github.com/brevyn/brevyn-cloud/internal/redeem"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sync/singleflight"
)

const (
	userContextKey = "user"
)

type Handler struct {
	cfg             *config.Config
	postgres        *pgxpool.Pool
	redis           *redis.Client
	gatewaySync     *redeemsvc.GatewaySyncService
	sub2            *sub2api.Client
	entitlementOnce singleflight.Group
	provisionSlots  chan struct{}
	registerLimiter *registerLimiter
	loginLimiter    *loginLimiter
	refreshLimiter  *refreshLimiter
	redeemLimiter   *redeemLimiter
	providerLimiter *providerLimiter
}

type Principal struct {
	ID          int64  `json:"-"`
	PublicID    string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Status      string `json:"status"`
}

type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	TokenType    string `json:"tokenType"`
	ExpiresIn    int64  `json:"expiresIn"`
}

type walletSummary struct {
	Balance float64 `json:"balance"`
}

type gatewayAccountSummary struct {
	Provider       string     `json:"provider"`
	ExternalUserID int64      `json:"externalUserId"`
	ExternalEmail  string     `json:"externalEmail"`
	DefaultGroupID int64      `json:"defaultGroupId"`
	Concurrency    int        `json:"concurrency"`
	Status         string     `json:"status"`
	LastSyncedAt   *time.Time `json:"lastSyncedAt"`
}

type apiKeySummary struct {
	ID              string     `json:"id"`
	Provider        string     `json:"provider"`
	ExternalKeyID   int64      `json:"externalKeyId"`
	ExternalGroupID int64      `json:"externalGroupId"`
	GroupName       string     `json:"groupName,omitempty"`
	GroupType       string     `json:"groupType,omitempty"`
	Platform        string     `json:"platform,omitempty"`
	MaskedAPIKey    string     `json:"maskedApiKey"`
	Status          string     `json:"status"`
	LastUsedAt      *time.Time `json:"lastUsedAt"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type gatewayGroupSummary struct {
	ExternalGroupID      int64                `json:"externalGroupId"`
	Name                 string               `json:"name"`
	Description          string               `json:"description"`
	Platform             string               `json:"platform"`
	SubscriptionType     string               `json:"subscriptionType"`
	RateMultiplier       float64              `json:"rateMultiplier"`
	DailyLimitUSD        *float64             `json:"dailyLimitUsd,omitempty"`
	WeeklyLimitUSD       *float64             `json:"weeklyLimitUsd,omitempty"`
	MonthlyLimitUSD      *float64             `json:"monthlyLimitUsd,omitempty"`
	DefaultValidityDays  int                  `json:"defaultValidityDays"`
	RPMLimit             int                  `json:"rpmLimit"`
	Status               string               `json:"status"`
	ModelCount           int                  `json:"modelCount"`
	Source               string               `json:"source,omitempty"`
	IsCurrent            bool                 `json:"isCurrent"`
	OfficialModelConfig  *officialModelConfig `json:"officialModelConfig,omitempty"`
	OfficialCapabilities []string             `json:"officialCapabilities,omitempty"`
}

type modelCatalogItem struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	DisplayName       string          `json:"displayName"`
	ProviderFamily    string          `json:"providerFamily"`
	Platform          string          `json:"platform,omitempty"`
	ExternalGroupID   int64           `json:"externalGroupId,omitempty"`
	GroupName         string          `json:"groupName,omitempty"`
	BillingMode       string          `json:"billingMode,omitempty"`
	Pricing           json.RawMessage `json:"pricing,omitempty"`
	Capabilities      []string        `json:"capabilities"`
	SupportsVision    bool            `json:"supportsVision"`
	SupportsStreaming bool            `json:"supportsStreaming"`
	Enabled           bool            `json:"enabled"`
}

type redemptionSummary struct {
	ID               string    `json:"id"`
	CodeID           string    `json:"codeId"`
	ProductName      string    `json:"productName"`
	Kind             string    `json:"kind"`
	Value            float64   `json:"value"`
	ValidityDays     int       `json:"validityDays"`
	ExternalUserID   int64     `json:"externalUserId"`
	ExternalGroupID  int64     `json:"externalGroupId"`
	GatewayOperation string    `json:"gatewayOperation"`
	Status           string    `json:"status"`
	ErrorMessage     string    `json:"errorMessage"`
	ErrorCode        string    `json:"errorCode"`
	ErrorClass       string    `json:"errorClass"`
	ErrorStage       string    `json:"errorStage"`
	ErrorRetryable   bool      `json:"errorRetryable"`
	ErrorDetail      string    `json:"errorDetail"`
	CreatedAt        time.Time `json:"createdAt"`
}

type redeemResult struct {
	Redemption  redemptionSummary     `json:"redemption"`
	Wallet      walletSummary         `json:"wallet"`
	Gateway     gatewayAccountSummary `json:"gateway"`
	APIKey      *apiKeySummary        `json:"apiKey,omitempty"`
	PlainAPIKey string                `json:"plainApiKey,omitempty"`
}

func NewHandler(cfg *config.Config, postgres *pgxpool.Pool, redisClient *redis.Client, sub2Clients ...*sub2api.Client) *Handler {
	var provisionSlots chan struct{}
	if cfg != nil && cfg.RegisterProvisionConcurrency > 0 {
		provisionSlots = make(chan struct{}, cfg.RegisterProvisionConcurrency)
	}
	var sub2 *sub2api.Client
	if len(sub2Clients) > 0 {
		sub2 = sub2Clients[0]
	}
	if sub2 == nil {
		var sub2Config sub2api.ClientConfig
		if cfg != nil {
			sub2Config = sub2api.ClientConfig{
				BaseURL:       cfg.Sub2APIBaseURL,
				AdminAPIKey:   cfg.Sub2APIAdminAPIKey,
				AdminEmail:    cfg.Sub2APIAdminEmail,
				AdminPassword: cfg.Sub2APIAdminPassword,
			}
		}
		sub2 = sub2api.NewClient(sub2Config)
	}
	return &Handler{
		cfg:             cfg,
		postgres:        postgres,
		redis:           redisClient,
		gatewaySync:     redeemsvc.NewGatewaySyncService(cfg, postgres, redisClient),
		sub2:            sub2,
		provisionSlots:  provisionSlots,
		registerLimiter: newRegisterLimiter(redisClient),
		loginLimiter:    newLoginLimiter(redisClient),
		refreshLimiter:  newRefreshLimiter(redisClient),
		redeemLimiter:   newRedeemLimiter(redisClient),
		providerLimiter: newProviderLimiter(redisClient),
	}
}

func (h *Handler) Register(c *gin.Context) {
	var req struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	email := normalizeEmail(req.Email)
	if email == "" || !strings.Contains(email, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_email"})
		return
	}
	if len(req.Password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password_too_short"})
		return
	}
	retryAfter, blocked, err := h.registerLimiter.allow(c.Request.Context(), c.ClientIP(), emailHash(email))
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rate_limit_unavailable"})
		return
	}
	if blocked {
		setRetryAfter(c, retryAfter)
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "register_rate_limited"})
		return
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "password_hash_failed"})
		return
	}

	var user Principal
	err = h.postgres.QueryRow(c.Request.Context(), `
		INSERT INTO users (public_id, email, email_hash, password_hash, display_name, status)
		VALUES ($1, $2, $3, $4, $5, 'active')
		RETURNING id, public_id, email, display_name, status
	`, "u_"+uuid.NewString(), email, emailHash(email), string(passwordHash), strings.TrimSpace(req.DisplayName)).Scan(
		&user.ID, &user.PublicID, &user.Email, &user.DisplayName, &user.Status,
	)
	if isUniqueViolation(err) {
		c.JSON(http.StatusConflict, gin.H{"error": "email_already_registered"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "register_failed"})
		return
	}

	tokens, err := h.issueTokenPair(c.Request.Context(), user, tokenMetadataFromRequest(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token_create_failed"})
		return
	}

	response := gin.H{
		"user":               user,
		"tokens":             tokens,
		"gateway":            nil,
		"apiKey":             nil,
		"gatewayStatus":      "locked",
		"gatewayOperation":   "",
		"gatewayWarning":     "",
		"gatewayWarningCode": "",
	}
	c.JSON(http.StatusCreated, response)
}

func (h *Handler) tryImmediateGatewayProvision(ctx context.Context, user Principal) (officialGatewayCredential, error) {
	if h.provisionSlots != nil {
		select {
		case h.provisionSlots <- struct{}{}:
			defer func() { <-h.provisionSlots }()
		default:
			return officialGatewayCredential{}, officialGatewayCredentialError{
				Status: http.StatusAccepted,
				Code:   "gateway_provision_busy",
				Err:    errors.New("gateway provisioning concurrency is full"),
			}
		}
	}
	provisionCtx, cancelProvision := context.WithTimeout(ctx, 3*time.Second)
	defer cancelProvision()
	return h.ensureOfficialGatewayCredential(provisionCtx, user)
}

func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	email := normalizeEmail(req.Email)
	if email == "" || strings.TrimSpace(req.Password) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}
	emailHashValue := emailHash(email)

	retryAfter, blocked, err := h.loginLimiter.allowRequest(c.Request.Context(), c.ClientIP(), emailHashValue)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rate_limit_unavailable"})
		return
	}
	if blocked {
		setRetryAfter(c, retryAfter)
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "login_rate_limited"})
		return
	}
	retryAfter, blocked, err = h.loginLimiter.failureBlocked(c.Request.Context(), c.ClientIP(), emailHashValue)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rate_limit_unavailable"})
		return
	}
	if blocked {
		setRetryAfter(c, retryAfter)
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "login_rate_limited"})
		return
	}

	user, passwordHash, err := h.findUserByEmail(c.Request.Context(), email)
	if err != nil || user.Status != "active" {
		if retryAfter, blocked, recordErr := h.loginLimiter.recordFailure(c.Request.Context(), c.ClientIP(), emailHashValue); recordErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rate_limit_unavailable"})
			return
		} else if blocked {
			setRetryAfter(c, retryAfter)
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "login_rate_limited"})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		if retryAfter, blocked, recordErr := h.loginLimiter.recordFailure(c.Request.Context(), c.ClientIP(), emailHashValue); recordErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rate_limit_unavailable"})
			return
		} else if blocked {
			setRetryAfter(c, retryAfter)
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "login_rate_limited"})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}
	_ = h.loginLimiter.clearPairFailure(c.Request.Context(), c.ClientIP(), emailHashValue)
	tokens, err := h.issueTokenPair(c.Request.Context(), user, tokenMetadataFromRequest(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "token_create_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user, "tokens": tokens})
}

func (h *Handler) Refresh(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	familyID := ""
	if claims, parseErr := h.parseToken(refreshToken, h.refreshSecret()); parseErr == nil && claims.Kind == "refresh" {
		familyID = claims.FamilyID
	}
	retryAfter, blocked, err := h.refreshLimiter.allow(c.Request.Context(), c.ClientIP(), familyID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rate_limit_unavailable"})
		return
	}
	if blocked {
		setRetryAfter(c, retryAfter)
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "refresh_rate_limited"})
		return
	}
	user, tokens, err := h.rotateRefreshToken(c.Request.Context(), refreshToken, tokenMetadataFromRequest(c))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_refresh_token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user, "tokens": tokens})
}

func (h *Handler) Logout(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refreshToken"`
	}
	_ = c.ShouldBindJSON(&req)
	if strings.TrimSpace(req.RefreshToken) != "" {
		_ = h.revokeRefreshToken(c.Request.Context(), req.RefreshToken, "logout")
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) RequireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
			abortAuthAPIError(c, http.StatusUnauthorized, "unauthorized", "请先登录")
			return
		}
		token := strings.TrimSpace(header[len("Bearer "):])
		claims, err := h.parseToken(token, h.accessSecret())
		if err != nil || claims.Kind != "access" {
			abortAuthAPIError(c, http.StatusUnauthorized, "unauthorized", "登录已失效，请重新登录")
			return
		}
		user, err := h.findUserByID(c.Request.Context(), claims.SubjectID)
		if err != nil || user.Status != "active" {
			abortAuthAPIError(c, http.StatusUnauthorized, "unauthorized", "登录已失效，请重新登录")
			return
		}
		c.Set(userContextKey, user)
		c.Next()
	}
}

func (h *Handler) Me(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	wallet, _ := h.wallet(c.Request.Context(), user.ID)
	gateway, _ := h.gatewaySync.GatewayAccount(c.Request.Context(), user.ID)
	var currentGroup *gatewayGroupSummary
	if gateway != nil && gateway.DefaultGroupID > 0 {
		currentGroup, _ = h.gatewayGroupByExternalID(c.Request.Context(), gateway.DefaultGroupID)
		if currentGroup != nil {
			currentGroup.IsCurrent = true
			if currentGroup.Source == "" {
				currentGroup.Source = "default"
			}
			groups := []gatewayGroupSummary{*currentGroup}
			if err := h.attachOfficialModelConfigs(c.Request.Context(), groups); err == nil {
				currentGroup = &groups[0]
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"user":         user,
		"wallet":       wallet,
		"gateway":      gatewayAccountPointerResponse(gateway),
		"currentGroup": currentGroup,
	})
}

func (h *Handler) Groups(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	gateway, _ := h.gatewaySync.GatewayAccount(c.Request.Context(), user.ID)
	currentGroupID := int64(0)
	if gateway != nil {
		currentGroupID = gateway.DefaultGroupID
	}
	groups, err := h.userGatewayGroups(c.Request.Context(), user.ID, currentGroupID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "groups_query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": groups, "total": len(groups)})
}

func (h *Handler) Wallet(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	wallet, err := h.wallet(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "wallet_query_failed"})
		return
	}
	rows, err := h.postgres.Query(c.Request.Context(), `
		SELECT public_id, kind, amount, balance_after, source, reference_id, notes, created_at
		FROM wallet_transactions
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT 50
	`, user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "wallet_query_failed"})
		return
	}
	defer rows.Close()
	items := []gin.H{}
	for rows.Next() {
		var id, kind, source, referenceID, notes string
		var amount, balanceAfter float64
		var createdAt time.Time
		if err := rows.Scan(&id, &kind, &amount, &balanceAfter, &source, &referenceID, &notes, &createdAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "wallet_scan_failed"})
			return
		}
		items = append(items, gin.H{
			"id": id, "kind": kind, "amount": amount, "balanceAfter": balanceAfter,
			"source": source, "referenceId": referenceID, "notes": notes, "createdAt": createdAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"wallet": wallet, "transactions": items})
}

func (h *Handler) APIKeys(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	items, err := h.userAPIKeys(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "api_keys_query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

type officialGatewayCredential struct {
	Account  redeemsvc.GatewayAccountSummary
	APIKey   *redeemsvc.GatewayAPIKeySummary
	PlainKey string
}

type gatewayGroupCredential = officialGatewayCredential

type officialGatewayCredentialError struct {
	Status     int
	Code       string
	Err        error
	RetryAfter time.Duration
}

func (e officialGatewayCredentialError) Error() string {
	return e.Err.Error()
}

func (e officialGatewayCredentialError) Unwrap() error {
	return e.Err
}

func (h *Handler) SystemAPIKey(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if retryAfter, blocked, err := h.providerLimiter.allowRead(c.Request.Context(), c.ClientIP(), user.ID); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rate_limit_unavailable"})
		return
	} else if blocked {
		setRetryAfter(c, retryAfter)
		writeAuthAPIError(c, http.StatusTooManyRequests, "provider_rate_limited", "官方配置请求过于频繁，请稍后再试")
		return
	}
	groupID, err := h.resolveOfficialProviderGroupID(c.Request.Context(), user, c.Query("externalGroupId"))
	if err != nil {
		writeOfficialGatewayCredentialError(c, err)
		return
	}
	credential, err := h.ensureOfficialGatewayCredentialForRequest(c.Request.Context(), user, groupID)
	if err != nil {
		writeOfficialGatewayCredentialError(c, err)
		return
	}
	c.Header("Deprecation", "true")
	c.Header("Link", `</api/v1/provider/official>; rel="successor-version"`)
	c.JSON(http.StatusOK, gin.H{
		"key":         credential.PlainKey,
		"baseUrl":     h.cfg.OfficialProviderBaseURL,
		"apiKey":      gatewayAPIKeyPointerResponse(credential.APIKey),
		"deprecated":  true,
		"replacement": "/api/v1/provider/official",
	})
}

func (h *Handler) OfficialProvider(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if retryAfter, blocked, err := h.providerLimiter.allowRead(c.Request.Context(), c.ClientIP(), user.ID); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rate_limit_unavailable"})
		return
	} else if blocked {
		setRetryAfter(c, retryAfter)
		writeAuthAPIError(c, http.StatusTooManyRequests, "provider_rate_limited", "官方配置请求过于频繁，请稍后再试")
		return
	}
	groupID, err := h.resolveOfficialProviderGroupID(c.Request.Context(), user, c.Query("externalGroupId"))
	if err != nil {
		writeOfficialGatewayCredentialError(c, err)
		return
	}
	credential, err := h.ensureOfficialGatewayCredentialForRequest(c.Request.Context(), user, groupID)
	if err != nil {
		writeOfficialGatewayCredentialError(c, err)
		return
	}
	credentialGroupID := groupID
	if credential.APIKey != nil && credential.APIKey.ExternalGroupID > 0 {
		credentialGroupID = credential.APIKey.ExternalGroupID
	}
	models, err := h.modelCatalogForGroups(c.Request.Context(), []int64{credentialGroupID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "models_query_failed"})
		return
	}
	legacyProvider := h.legacyOfficialAgentProvider(models, credential.PlainKey)
	providers, err := h.officialPurposeProviders(c.Request.Context(), credentialGroupID, models, credential.PlainKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "official_provider_config_query_failed"})
		return
	}
	if len(providers) == 0 {
		providers = []gin.H{legacyProvider}
	}
	c.JSON(http.StatusOK, gin.H{
		"provider":  legacyProvider,
		"providers": providers,
		"gateway":   gatewayAccountResponse(credential.Account),
		"apiKey":    gatewayAPIKeyPointerResponse(credential.APIKey),
	})
}

func (h *Handler) ConversationProvider(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if retryAfter, blocked, err := h.providerLimiter.allowRead(c.Request.Context(), c.ClientIP(), user.ID); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "rate_limit_unavailable"})
		return
	} else if blocked {
		setRetryAfter(c, retryAfter)
		writeAuthAPIError(c, http.StatusTooManyRequests, "provider_rate_limited", "套餐配置请求过于频繁，请稍后再试")
		return
	}

	groupID, err := h.resolveConversationProviderGroupID(c.Request.Context(), user, c.Query("externalGroupId"))
	if err != nil {
		writeOfficialGatewayCredentialError(c, err)
		return
	}
	credential, err := h.ensureGatewayCredentialForGroupRequest(c.Request.Context(), user, groupID)
	if err != nil {
		writeOfficialGatewayCredentialError(c, err)
		return
	}
	credentialGroupID := groupID
	if credential.APIKey != nil && credential.APIKey.ExternalGroupID > 0 {
		credentialGroupID = credential.APIKey.ExternalGroupID
	}
	models, err := h.modelCatalogForGroups(c.Request.Context(), []int64{credentialGroupID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "models_query_failed"})
		return
	}
	models = conversationProviderModels(models)
	if len(models) == 0 {
		writeAuthAPIError(c, http.StatusConflict, "conversation_models_not_configured", "该套餐暂未配置可用对话模型")
		return
	}
	group, err := h.gatewayGroupByExternalID(c.Request.Context(), credentialGroupID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "gateway_group_query_failed"})
		return
	}

	provider := h.conversationProvider(credentialGroupID, group, models, credential.PlainKey)
	c.JSON(http.StatusOK, gin.H{
		"provider":        provider,
		"providers":       []gin.H{provider},
		"gateway":         gatewayAccountResponse(credential.Account),
		"apiKey":          gatewayAPIKeyPointerResponse(credential.APIKey),
		"externalGroupId": credentialGroupID,
	})
}

func (h *Handler) legacyOfficialAgentProvider(models []modelCatalogItem, apiKey string) gin.H {
	return gin.H{
		"purpose":       "agent",
		"providerKind":  "custom-anthropic",
		"adapterKind":   "anthropic",
		"protocol":      "anthropic_messages",
		"name":          "Brevyn Official",
		"baseUrl":       h.cfg.OfficialProviderBaseURL,
		"authMode":      "api_key",
		"apiKey":        apiKey,
		"selectedModel": h.selectProviderModel(models),
		"enabled":       true,
		"models":        models,
	}
}

func (h *Handler) conversationProvider(externalGroupID int64, group *gatewayGroupSummary, models []modelCatalogItem, apiKey string) gin.H {
	groupName := ""
	if group != nil {
		groupName = strings.TrimSpace(group.Name)
	}
	if groupName == "" {
		groupName = fmt.Sprintf("套餐 %d", externalGroupID)
	}
	return gin.H{
		"purpose":         "agent",
		"providerKind":    "custom-anthropic",
		"adapterKind":     "anthropic",
		"protocol":        "anthropic_messages",
		"name":            "Brevyn Cloud · " + groupName,
		"baseUrl":         h.cfg.OfficialProviderBaseURL,
		"authMode":        "api_key",
		"apiKey":          apiKey,
		"selectedModel":   h.selectProviderModel(models),
		"enabled":         true,
		"models":          models,
		"externalGroupId": externalGroupID,
		"groupName":       groupName,
	}
}

type officialPurposeConfig struct {
	ModelIDs       []string `json:"modelIds"`
	DefaultModelID string   `json:"defaultModelId"`
}

type officialModelConfig map[string]officialPurposeConfig

type officialCapabilityDefinition struct {
	Key                   string   `json:"key"`
	Name                  string   `json:"name"`
	ProviderKind          string   `json:"providerKind"`
	AdapterKind           string   `json:"adapterKind"`
	Protocol              string   `json:"protocol"`
	ModelHintCapabilities []string `json:"modelHintCapabilities"`
	MinClientVersion      string   `json:"minClientVersion"`
}

func (h *Handler) officialPurposeProviders(ctx context.Context, externalGroupID int64, models []modelCatalogItem, apiKey string) ([]gin.H, error) {
	configs, err := h.officialPurposeConfigs(ctx, externalGroupID)
	if err != nil {
		return nil, err
	}
	if len(configs) == 0 {
		return nil, nil
	}
	definitions, err := h.officialCapabilityDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	providers := []gin.H{}
	for _, definition := range definitions {
		config, ok := configs[definition.Key]
		if !ok {
			continue
		}
		if provider := h.officialPurposeProvider(definition, config, models, apiKey); provider != nil {
			providers = append(providers, provider)
		}
	}
	return providers, nil
}

func (h *Handler) officialPurposeConfigs(ctx context.Context, externalGroupID int64) (map[string]officialPurposeConfig, error) {
	rows, err := h.postgres.Query(ctx, `
		SELECT purpose, model_id, is_default
		FROM gateway_group_model_roles
		WHERE provider = 'sub2api' AND external_group_id = $1
			AND enabled = true
			AND purpose IN (SELECT capability_key FROM official_capability_definitions WHERE enabled = true)
		ORDER BY purpose ASC, sort_order ASC, model_id ASC
	`, externalGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	configs := map[string]officialPurposeConfig{}
	for rows.Next() {
		var purpose string
		var modelID string
		var isDefault bool
		if err := rows.Scan(&purpose, &modelID, &isDefault); err != nil {
			return nil, err
		}
		config := configs[purpose]
		config.ModelIDs = append(config.ModelIDs, modelID)
		if isDefault {
			config.DefaultModelID = modelID
		}
		configs[purpose] = config
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for purpose, config := range configs {
		config.DefaultModelID = selectedConfiguredModel(config.DefaultModelID, config.ModelIDs)
		configs[purpose] = config
	}
	return configs, nil
}

func (h *Handler) officialPurposeProvider(definition officialCapabilityDefinition, config officialPurposeConfig, allModels []modelCatalogItem, apiKey string) gin.H {
	models := configuredPurposeModels(config.ModelIDs, allModels)
	if len(models) == 0 {
		return nil
	}
	selectedModel := selectedConfiguredModel(config.DefaultModelID, modelIDsFromCatalog(models))
	return gin.H{
		"purpose":               definition.Key,
		"providerKind":          definition.ProviderKind,
		"adapterKind":           definition.AdapterKind,
		"protocol":              definition.Protocol,
		"name":                  "Brevyn Official " + definition.Name,
		"baseUrl":               h.cfg.OfficialProviderBaseURL,
		"authMode":              "bearer",
		"apiKey":                apiKey,
		"selectedModel":         selectedModel,
		"enabled":               true,
		"models":                models,
		"minClientVersion":      definition.MinClientVersion,
		"modelHintCapabilities": definition.ModelHintCapabilities,
	}
}

func (h *Handler) officialCapabilityDefinitions(ctx context.Context) ([]officialCapabilityDefinition, error) {
	rows, err := h.postgres.Query(ctx, `
		SELECT capability_key, name, provider_kind, adapter_kind, protocol,
			model_hint_capabilities, min_client_version
		FROM official_capability_definitions
		WHERE enabled = true
		ORDER BY sort_order ASC, capability_key ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []officialCapabilityDefinition{}
	for rows.Next() {
		var item officialCapabilityDefinition
		var hintsRaw []byte
		if err := rows.Scan(
			&item.Key,
			&item.Name,
			&item.ProviderKind,
			&item.AdapterKind,
			&item.Protocol,
			&hintsRaw,
			&item.MinClientVersion,
		); err != nil {
			return nil, err
		}
		item.ModelHintCapabilities = decodeAuthStringList(hintsRaw)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		items = []officialCapabilityDefinition{
			{Key: "embedding", Name: "Embedding", ProviderKind: "custom-openai", AdapterKind: "openai_embedding", Protocol: "openai_compatible"},
			{Key: "vision", Name: "Vision", ProviderKind: "vision-custom-openai", AdapterKind: "openai_chat_completions", Protocol: "openai_compatible"},
			{Key: "ocr", Name: "Document OCR", ProviderKind: "ocr-openai-responses", AdapterKind: "openai_responses", Protocol: "openai_responses", ModelHintCapabilities: []string{"vision_input", "ocr", "document_parse", "table", "formula"}, MinClientVersion: "0.2.8"},
		}
	}
	return items, nil
}

func configuredPurposeModels(modelIDs []string, allModels []modelCatalogItem) []modelCatalogItem {
	byID := map[string]modelCatalogItem{}
	for _, model := range allModels {
		byID[strings.ToLower(model.ID)] = model
	}
	models := []modelCatalogItem{}
	for _, modelID := range modelIDs {
		if model, ok := byID[strings.ToLower(modelID)]; ok {
			models = append(models, model)
		}
	}
	return models
}

func conversationProviderModels(models []modelCatalogItem) []modelCatalogItem {
	filtered := make([]modelCatalogItem, 0, len(models))
	for _, model := range models {
		if model.Enabled == false {
			continue
		}
		if isConversationModel(model) {
			filtered = append(filtered, model)
		}
	}
	if len(filtered) > 0 {
		return filtered
	}
	for _, model := range models {
		if model.Enabled != false && !isEmbeddingModel(model) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func isConversationModel(model modelCatalogItem) bool {
	if hasCapability(model.Capabilities, "chat") {
		return true
	}
	if isEmbeddingModel(model) {
		return false
	}
	text := strings.ToLower(model.ID + " " + model.DisplayName + " " + model.Name + " " + model.ProviderFamily)
	if strings.Contains(text, "embedding") || strings.Contains(text, "embed") {
		return false
	}
	return true
}

func isEmbeddingModel(model modelCatalogItem) bool {
	if hasCapability(model.Capabilities, "embedding") {
		return true
	}
	text := strings.ToLower(model.ID + " " + model.DisplayName + " " + model.Name)
	return strings.Contains(text, "embedding") || strings.Contains(text, "embed")
}

func modelIDsFromCatalog(models []modelCatalogItem) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func decodeAuthStringList(raw []byte) []string {
	values := []string{}
	if len(raw) == 0 {
		return values
	}
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{}
	}
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func selectedConfiguredModel(selected string, modelIDs []string) string {
	selected = strings.TrimSpace(selected)
	for _, modelID := range modelIDs {
		if strings.EqualFold(modelID, selected) {
			return modelID
		}
	}
	if len(modelIDs) > 0 {
		return modelIDs[0]
	}
	return selected
}

func (h *Handler) ModelCatalog(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	credentialGroupID := int64(0)
	if raw := strings.TrimSpace(c.Query("externalGroupId")); raw != "" {
		requestedGroupID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || requestedGroupID <= 0 {
			writeAuthAPIError(c, http.StatusBadRequest, "invalid_external_group_id", "分组 ID 不正确")
			return
		}
		allowed, err := h.userOwnsExternalGroup(c.Request.Context(), user.ID, requestedGroupID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "group_permission_query_failed"})
			return
		}
		if officialGroup, err := h.groupHasOfficialCapabilities(c.Request.Context(), requestedGroupID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "official_capability_query_failed"})
			return
		} else if officialGroup {
			eligible, err := h.userHasOfficialCapabilityEntitlement(c.Request.Context(), user.ID)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "official_capability_query_failed"})
				return
			}
			if !eligible {
				writeOfficialGatewayCredentialError(c, officialGatewayCredentialError{
					Status: http.StatusAccepted,
					Code:   "official_capability_not_active",
					Err:    errors.New("official capability requires a balance package"),
				})
				return
			}
		}
		officialGroupID := h.defaultOfficialProviderGroupID(c.Request.Context(), user.ID)
		if !allowed && requestedGroupID != officialGroupID {
			writeAuthAPIError(c, http.StatusForbidden, "group_not_available", "当前账号无权查看该分组模型")
			return
		}
		credentialGroupID = requestedGroupID
	} else {
		credentialGroupID = h.defaultOfficialProviderGroupID(c.Request.Context(), user.ID)
		if credentialGroupID == 0 {
			writeOfficialGatewayCredentialError(c, officialGatewayCredentialError{
				Status: http.StatusAccepted,
				Code:   "official_capability_not_active",
				Err:    errors.New("official capability requires a balance package"),
			})
			return
		}
	}
	models, err := h.modelCatalogForGroups(c.Request.Context(), []int64{credentialGroupID})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "models_query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"items":           models,
		"total":           len(models),
		"externalGroupId": credentialGroupID,
	})
}

func (h *Handler) selectProviderModel(models []modelCatalogItem) string {
	defaultModel := strings.TrimSpace(h.cfg.OfficialProviderDefaultModel)
	for _, model := range models {
		if model.ID == defaultModel {
			return model.ID
		}
	}
	if len(models) > 0 {
		return models[0].ID
	}
	return defaultModel
}

func (h *Handler) userAPIKeys(ctx context.Context, userID int64) ([]apiKeySummary, error) {
	rows, err := h.postgres.Query(ctx, `
		SELECT
			gak.public_id,
			gak.provider,
			coalesce(gak.external_key_id, 0),
			gak.external_group_id,
			coalesce(gg.name, ''),
			coalesce(gg.subscription_type, ''),
			coalesce(gg.platform, ''),
			gak.masked_api_key,
			gak.status,
			gak.last_used_at,
			gak.created_at
		FROM gateway_api_keys gak
		LEFT JOIN gateway_groups gg
			ON gg.provider = gak.provider AND gg.external_group_id = gak.external_group_id
		WHERE gak.user_id = $1 AND gak.provider = 'sub2api'
		ORDER BY gak.status = 'active' DESC, gak.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []apiKeySummary{}
	for rows.Next() {
		var item apiKeySummary
		if err := rows.Scan(
			&item.ID,
			&item.Provider,
			&item.ExternalKeyID,
			&item.ExternalGroupID,
			&item.GroupName,
			&item.GroupType,
			&item.Platform,
			&item.MaskedAPIKey,
			&item.Status,
			&item.LastUsedAt,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (h *Handler) gatewayGroupByExternalID(ctx context.Context, externalGroupID int64) (*gatewayGroupSummary, error) {
	if externalGroupID <= 0 {
		return nil, nil
	}
	group, err := h.scanGatewayGroup(ctx, `
		SELECT
			gg.external_group_id,
			gg.name,
			gg.description,
			gg.platform,
			gg.subscription_type,
			gg.rate_multiplier,
			gg.daily_limit_usd,
			gg.weekly_limit_usd,
			gg.monthly_limit_usd,
			gg.default_validity_days,
			gg.rpm_limit,
			gg.status,
			(
				SELECT count(DISTINCT ggm.model_id)
				FROM gateway_group_models ggm
				WHERE ggm.provider = 'sub2api'
					AND ggm.status = 'active'
					AND ggm.external_group_id = gg.external_group_id
			) AS model_count,
			'' AS source
		FROM gateway_groups gg
		WHERE gg.provider = 'sub2api' AND gg.external_group_id = $1
	`, externalGroupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return &gatewayGroupSummary{
			ExternalGroupID: externalGroupID,
			Name:            fmt.Sprintf("group #%d", externalGroupID),
			Platform:        "anthropic",
			Status:          "unknown",
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return &group, nil
}

func (h *Handler) userGatewayGroups(ctx context.Context, userID int64, currentGroupID int64) ([]gatewayGroupSummary, error) {
	rows, err := h.postgres.Query(ctx, `
		WITH owned AS (
			SELECT default_group_id AS external_group_id, 'default' AS source
			FROM gateway_accounts
			WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND default_group_id > 0
			UNION ALL
			SELECT external_group_id, 'api_key' AS source
			FROM gateway_api_keys
			WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND external_group_id > 0
		),
		grouped AS (
			SELECT external_group_id, string_agg(DISTINCT source, ', ' ORDER BY source) AS source
			FROM owned
			GROUP BY external_group_id
		),
		model_counts AS (
			SELECT external_group_id, count(DISTINCT model_id) AS model_count
			FROM gateway_group_models
			WHERE provider = 'sub2api' AND status = 'active'
			GROUP BY external_group_id
		)
		SELECT
			grouped.external_group_id,
			coalesce(gg.name, 'group #' || grouped.external_group_id::text),
			coalesce(gg.description, ''),
			coalesce(gg.platform, 'anthropic'),
			coalesce(gg.subscription_type, ''),
			coalesce(gg.rate_multiplier, 1),
			gg.daily_limit_usd,
			gg.weekly_limit_usd,
			gg.monthly_limit_usd,
			coalesce(gg.default_validity_days, 0),
			coalesce(gg.rpm_limit, 0),
			coalesce(gg.status, 'unknown'),
			coalesce(model_counts.model_count, 0),
			grouped.source
		FROM grouped
		LEFT JOIN gateway_groups gg
			ON gg.provider = 'sub2api' AND gg.external_group_id = grouped.external_group_id
		LEFT JOIN model_counts
			ON model_counts.external_group_id = grouped.external_group_id
		ORDER BY (grouped.external_group_id = $2) DESC, grouped.external_group_id ASC
	`, userID, currentGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := []gatewayGroupSummary{}
	for rows.Next() {
		group, err := scanGatewayGroupRows(rows)
		if err != nil {
			return nil, err
		}
		group.IsCurrent = group.ExternalGroupID == currentGroupID
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	groups, err = h.appendDefaultOfficialGatewayGroup(ctx, userID, currentGroupID, groups)
	if err != nil {
		return nil, err
	}
	if err := h.attachOfficialModelConfigs(ctx, groups); err != nil {
		return nil, err
	}
	return groups, nil
}

func (h *Handler) appendDefaultOfficialGatewayGroup(ctx context.Context, userID int64, currentGroupID int64, groups []gatewayGroupSummary) ([]gatewayGroupSummary, error) {
	officialGroupID := h.officialCapabilityGroupID(ctx, userID)
	if officialGroupID <= 0 {
		return groups, nil
	}
	for i := range groups {
		if groups[i].ExternalGroupID != officialGroupID {
			continue
		}
		groups[i].Source = appendSource(groups[i].Source, "official_default")
		return groups, nil
	}
	group, err := h.gatewayGroupByExternalID(ctx, officialGroupID)
	if err != nil {
		return nil, err
	}
	if group == nil {
		group = &gatewayGroupSummary{
			ExternalGroupID: officialGroupID,
			Name:            fmt.Sprintf("group #%d", officialGroupID),
			Platform:        "openai",
			Status:          "unknown",
		}
	}
	group.Source = appendSource(group.Source, "official_default")
	group.IsCurrent = group.ExternalGroupID == currentGroupID
	return append(groups, *group), nil
}

func (h *Handler) attachOfficialModelConfigs(ctx context.Context, groups []gatewayGroupSummary) error {
	if len(groups) == 0 {
		return nil
	}
	groupIDs := []int64{}
	seen := map[int64]struct{}{}
	for _, group := range groups {
		if group.ExternalGroupID <= 0 {
			continue
		}
		if _, exists := seen[group.ExternalGroupID]; exists {
			continue
		}
		seen[group.ExternalGroupID] = struct{}{}
		groupIDs = append(groupIDs, group.ExternalGroupID)
	}
	if len(groupIDs) == 0 {
		return nil
	}
	rows, err := h.postgres.Query(ctx, `
		SELECT external_group_id, purpose, model_id, is_default
		FROM gateway_group_model_roles
		WHERE provider = 'sub2api'
			AND external_group_id = ANY($1::bigint[])
			AND enabled = true
			AND purpose IN (SELECT capability_key FROM official_capability_definitions WHERE enabled = true)
		ORDER BY external_group_id ASC, purpose ASC, sort_order ASC, model_id ASC
	`, groupIDs)
	if err != nil {
		return err
	}
	defer rows.Close()

	configs := map[int64]officialModelConfig{}
	for rows.Next() {
		var externalGroupID int64
		var purpose string
		var modelID string
		var isDefault bool
		if err := rows.Scan(&externalGroupID, &purpose, &modelID, &isDefault); err != nil {
			return err
		}
		config := configs[externalGroupID]
		if config == nil {
			config = officialModelConfig{}
		}
		purposeConfig := config[purpose]
		purposeConfig.ModelIDs = append(purposeConfig.ModelIDs, modelID)
		if isDefault {
			purposeConfig.DefaultModelID = modelID
		}
		config[purpose] = purposeConfig
		configs[externalGroupID] = config
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for index := range groups {
		config, ok := configs[groups[index].ExternalGroupID]
		if !ok {
			continue
		}
		capabilities := []string{}
		for purpose, purposeConfig := range config {
			purposeConfig.DefaultModelID = selectedConfiguredModel(purposeConfig.DefaultModelID, purposeConfig.ModelIDs)
			config[purpose] = purposeConfig
			if len(purposeConfig.ModelIDs) > 0 {
				capabilities = append(capabilities, purpose)
			}
		}
		if len(capabilities) == 0 {
			continue
		}
		groups[index].OfficialModelConfig = &config
		groups[index].OfficialCapabilities = capabilities
	}
	return nil
}

func (h *Handler) userOwnsExternalGroup(ctx context.Context, userID int64, externalGroupID int64) (bool, error) {
	var exists bool
	err := h.postgres.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM gateway_accounts
			WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND default_group_id = $2
			UNION
			SELECT 1
			FROM gateway_api_keys
			WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND external_group_id = $2
		)
	`, userID, externalGroupID).Scan(&exists)
	return exists, err
}

func (h *Handler) scanGatewayGroup(ctx context.Context, query string, args ...any) (gatewayGroupSummary, error) {
	row := h.postgres.QueryRow(ctx, query, args...)
	return scanGatewayGroupRow(row)
}

type gatewayGroupRow interface {
	Scan(dest ...any) error
}

func scanGatewayGroupRow(row gatewayGroupRow) (gatewayGroupSummary, error) {
	var group gatewayGroupSummary
	var daily, weekly, monthly sql.NullFloat64
	if err := row.Scan(
		&group.ExternalGroupID,
		&group.Name,
		&group.Description,
		&group.Platform,
		&group.SubscriptionType,
		&group.RateMultiplier,
		&daily,
		&weekly,
		&monthly,
		&group.DefaultValidityDays,
		&group.RPMLimit,
		&group.Status,
		&group.ModelCount,
		&group.Source,
	); err != nil {
		return group, err
	}
	group.DailyLimitUSD = nullFloatPtr(daily)
	group.WeeklyLimitUSD = nullFloatPtr(weekly)
	group.MonthlyLimitUSD = nullFloatPtr(monthly)
	return group, nil
}

func scanGatewayGroupRows(rows pgx.Rows) (gatewayGroupSummary, error) {
	return scanGatewayGroupRow(rows)
}

func nullFloatPtr(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func (h *Handler) userModelCatalog(ctx context.Context, userID int64) ([]modelCatalogItem, error) {
	groupIDs, err := h.userExternalGroupIDs(ctx, userID)
	if err != nil {
		return nil, err
	}
	models, err := h.modelCatalogForGroups(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	if len(models) > 0 {
		return models, nil
	}
	return h.publicModelCatalog(ctx)
}

func (h *Handler) userExternalGroupIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := h.postgres.Query(ctx, `
		SELECT DISTINCT external_group_id
		FROM (
			SELECT external_group_id
			FROM gateway_api_keys
			WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND external_group_id > 0
			UNION
			SELECT default_group_id AS external_group_id
			FROM gateway_accounts
			WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND default_group_id > 0
		) groups
		ORDER BY external_group_id ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groupIDs := []int64{}
	for rows.Next() {
		var groupID int64
		if err := rows.Scan(&groupID); err != nil {
			return nil, err
		}
		groupIDs = append(groupIDs, groupID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if officialGroupID := h.officialCapabilityGroupID(ctx, userID); officialGroupID > 0 {
		exists := false
		for _, groupID := range groupIDs {
			if groupID == officialGroupID {
				exists = true
				break
			}
		}
		if !exists {
			groupIDs = append(groupIDs, officialGroupID)
		}
	}
	return groupIDs, nil
}

func (h *Handler) modelCatalogForGroups(ctx context.Context, groupIDs []int64) ([]modelCatalogItem, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	rows, err := h.postgres.Query(ctx, `
		SELECT DISTINCT ON (ggm.model_id)
			ggm.model_id,
			ggm.display_name,
			ggm.provider_family,
			ggm.platform,
			ggm.external_group_id,
			coalesce(gg.name, ''),
			ggm.billing_mode,
			ggm.pricing_json::text,
			ggm.capabilities_json
		FROM gateway_group_models ggm
		LEFT JOIN gateway_groups gg
			ON gg.provider = ggm.provider AND gg.external_group_id = ggm.external_group_id
		WHERE ggm.provider = 'sub2api'
			AND ggm.status = 'active'
			AND ggm.external_group_id = ANY($1::bigint[])
		ORDER BY ggm.model_id ASC, ggm.external_group_id ASC
	`, groupIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	models := []modelCatalogItem{}
	for rows.Next() {
		var item modelCatalogItem
		var capabilitiesJSON string
		var pricingJSON string
		if err := rows.Scan(
			&item.ID,
			&item.DisplayName,
			&item.ProviderFamily,
			&item.Platform,
			&item.ExternalGroupID,
			&item.GroupName,
			&item.BillingMode,
			&pricingJSON,
			&capabilitiesJSON,
		); err != nil {
			return nil, err
		}
		item.Name = item.DisplayName
		item.SupportsStreaming = true
		item.Enabled = true
		if err := json.Unmarshal([]byte(capabilitiesJSON), &item.Capabilities); err != nil {
			item.Capabilities = []string{}
		}
		item.SupportsVision = hasCapability(item.Capabilities, "vision_input")
		if strings.TrimSpace(pricingJSON) != "" && strings.TrimSpace(pricingJSON) != "{}" {
			item.Pricing = json.RawMessage(pricingJSON)
		}
		models = append(models, item)
	}
	return models, rows.Err()
}

func (h *Handler) publicModelCatalog(ctx context.Context) ([]modelCatalogItem, error) {
	rows, err := h.postgres.Query(ctx, `
		SELECT model_id, display_name, provider_family, capabilities_json, supports_streaming
		FROM model_catalog
		WHERE status = 'active' AND public_visible = true
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	models := []modelCatalogItem{}
	for rows.Next() {
		var item modelCatalogItem
		var capabilitiesJSON string
		if err := rows.Scan(&item.ID, &item.DisplayName, &item.ProviderFamily, &capabilitiesJSON, &item.SupportsStreaming); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(capabilitiesJSON), &item.Capabilities); err != nil {
			item.Capabilities = []string{}
		}
		item.Name = item.DisplayName
		item.SupportsVision = hasCapability(item.Capabilities, "vision_input")
		item.Enabled = true
		models = append(models, item)
	}
	return models, rows.Err()
}

func hasCapability(capabilities []string, target string) bool {
	for _, capability := range capabilities {
		if capability == target {
			return true
		}
	}
	return false
}

func (h *Handler) resolveOfficialProviderGroupID(ctx context.Context, user Principal, raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		groupID := h.defaultOfficialProviderGroupID(ctx, user.ID)
		if groupID <= 0 {
			return 0, officialGatewayCredentialError{
				Status: http.StatusAccepted,
				Code:   "official_capability_not_active",
				Err:    errors.New("official capability requires a balance package"),
			}
		}
		return groupID, nil
	}
	eligible, err := h.userHasOfficialCapabilityEntitlement(ctx, user.ID)
	if err != nil {
		return 0, officialGatewayCredentialError{
			Status: http.StatusInternalServerError,
			Code:   "official_capability_query_failed",
			Err:    err,
		}
	}
	if !eligible {
		return 0, officialGatewayCredentialError{
			Status: http.StatusAccepted,
			Code:   "official_capability_not_active",
			Err:    errors.New("official capability requires a balance package"),
		}
	}
	requestedGroupID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || requestedGroupID <= 0 {
		return 0, officialGatewayCredentialError{
			Status: http.StatusBadRequest,
			Code:   "invalid_external_group_id",
			Err:    errors.New("externalGroupId is invalid"),
		}
	}
	officialGroup, err := h.groupHasOfficialCapabilities(ctx, requestedGroupID)
	if err != nil {
		return 0, officialGatewayCredentialError{
			Status: http.StatusInternalServerError,
			Code:   "official_capability_query_failed",
			Err:    err,
		}
	}
	if !officialGroup {
		return 0, officialGatewayCredentialError{
			Status: http.StatusForbidden,
			Code:   "group_not_official_capability",
			Err:    errors.New("requested group does not expose official capabilities"),
		}
	}
	allowed, err := h.userOwnsExternalGroup(ctx, user.ID, requestedGroupID)
	if err != nil {
		return 0, officialGatewayCredentialError{
			Status: http.StatusInternalServerError,
			Code:   "group_permission_query_failed",
			Err:    err,
		}
	}
	if !allowed && requestedGroupID != h.defaultOfficialProviderGroupID(ctx, user.ID) {
		return 0, officialGatewayCredentialError{
			Status: http.StatusForbidden,
			Code:   "group_not_available",
			Err:    errors.New("requested group is not available for current user"),
		}
	}
	return requestedGroupID, nil
}

func (h *Handler) resolveConversationProviderGroupID(ctx context.Context, user Principal, raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if account, err := h.gatewaySync.GatewayAccount(ctx, user.ID); err != nil {
			return 0, officialGatewayCredentialError{
				Status: http.StatusInternalServerError,
				Code:   "gateway_credential_query_failed",
				Err:    err,
			}
		} else if account != nil && account.DefaultGroupID > 0 {
			return account.DefaultGroupID, nil
		}
		return 0, officialGatewayCredentialError{
			Status: http.StatusAccepted,
			Code:   "conversation_provider_not_active",
			Err:    errors.New("conversation provider requires a redeemed package"),
		}
	}
	requestedGroupID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || requestedGroupID <= 0 {
		return 0, officialGatewayCredentialError{
			Status: http.StatusBadRequest,
			Code:   "invalid_external_group_id",
			Err:    errors.New("externalGroupId is invalid"),
		}
	}
	allowed, err := h.userOwnsExternalGroup(ctx, user.ID, requestedGroupID)
	if err != nil {
		return 0, officialGatewayCredentialError{
			Status: http.StatusInternalServerError,
			Code:   "group_permission_query_failed",
			Err:    err,
		}
	}
	if !allowed {
		return 0, officialGatewayCredentialError{
			Status: http.StatusForbidden,
			Code:   "group_not_available",
			Err:    errors.New("requested group is not available for current user"),
		}
	}
	return requestedGroupID, nil
}

func (h *Handler) defaultOfficialProviderGroupID(ctx context.Context, userID int64) int64 {
	return h.officialCapabilityGroupID(ctx, userID)
}

func (h *Handler) officialCapabilityGroupID(ctx context.Context, userID int64) int64 {
	eligible, err := h.userHasOfficialCapabilityEntitlement(ctx, userID)
	if err != nil || !eligible {
		return 0
	}
	if groupID, err := h.userOfficialCapabilityGroupID(ctx, userID); err == nil && groupID > 0 {
		return groupID
	}
	if groupID, err := h.firstOfficialCapabilityGroupID(ctx); err == nil && groupID > 0 {
		return groupID
	}
	return 0
}

func (h *Handler) userHasOfficialCapabilityEntitlement(ctx context.Context, userID int64) (bool, error) {
	var exists bool
	err := h.postgres.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM redeem_redemptions
			WHERE user_id = $1
				AND kind = 'balance'
				AND value > 0
				AND status = 'synced'
		)
	`, userID).Scan(&exists)
	return exists, err
}

func (h *Handler) userOfficialCapabilityGroupID(ctx context.Context, userID int64) (int64, error) {
	var groupID int64
	err := h.postgres.QueryRow(ctx, `
		WITH owned AS (
			SELECT default_group_id AS external_group_id, 0 AS priority
			FROM gateway_accounts
			WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND default_group_id > 0
			UNION ALL
			SELECT external_group_id, 1 AS priority
			FROM gateway_api_keys
			WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND external_group_id > 0
		)
		SELECT owned.external_group_id
		FROM owned
		JOIN gateway_group_model_roles ggmr
			ON ggmr.provider = 'sub2api' AND ggmr.external_group_id = owned.external_group_id
		JOIN official_capability_definitions ocd
			ON ocd.capability_key = ggmr.purpose AND ocd.enabled = true
		JOIN gateway_groups gg
			ON gg.provider = ggmr.provider AND gg.external_group_id = ggmr.external_group_id
		WHERE ggmr.enabled = true
			AND gg.status = 'active'
		ORDER BY owned.priority ASC, owned.external_group_id ASC
		LIMIT 1
	`, userID).Scan(&groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return groupID, err
}

func (h *Handler) groupHasOfficialCapabilities(ctx context.Context, externalGroupID int64) (bool, error) {
	if externalGroupID <= 0 {
		return false, nil
	}
	var exists bool
	err := h.postgres.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM gateway_group_model_roles ggmr
			JOIN official_capability_definitions ocd
				ON ocd.capability_key = ggmr.purpose AND ocd.enabled = true
			JOIN gateway_groups gg
				ON gg.provider = ggmr.provider AND gg.external_group_id = ggmr.external_group_id
			WHERE ggmr.provider = 'sub2api'
				AND ggmr.external_group_id = $1
				AND ggmr.enabled = true
				AND gg.status = 'active'
		)
	`, externalGroupID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (h *Handler) firstOfficialCapabilityGroupID(ctx context.Context) (int64, error) {
	var groupID int64
	err := h.postgres.QueryRow(ctx, `
		SELECT ggmr.external_group_id
		FROM gateway_group_model_roles ggmr
		JOIN official_capability_definitions ocd
			ON ocd.capability_key = ggmr.purpose AND ocd.enabled = true
		JOIN gateway_groups gg
			ON gg.provider = ggmr.provider AND gg.external_group_id = ggmr.external_group_id
		WHERE ggmr.provider = 'sub2api'
			AND ggmr.enabled = true
			AND gg.status = 'active'
		GROUP BY ggmr.external_group_id
		ORDER BY min(ggmr.sort_order) ASC, ggmr.external_group_id ASC
		LIMIT 1
	`).Scan(&groupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return groupID, nil
}

func (h *Handler) localOfficialGatewayCredential(ctx context.Context, user Principal, externalGroupID int64) (officialGatewayCredential, error) {
	account, apiKey, plainKey, ok, err := h.gatewaySync.ExistingOfficialGatewayCredential(ctx, user.ID, externalGroupID)
	if err != nil {
		return officialGatewayCredential{}, officialGatewayCredentialError{
			Status: http.StatusInternalServerError,
			Code:   "gateway_credential_query_failed",
			Err:    err,
		}
	}
	if !ok {
		return officialGatewayCredential{}, officialGatewayCredentialError{
			Status: http.StatusAccepted,
			Code:   "gateway_credential_missing",
			Err:    errors.New("gateway credential is not ready"),
		}
	}
	return officialGatewayCredential{Account: account, APIKey: apiKey, PlainKey: plainKey}, nil
}

func (h *Handler) ensureOfficialGatewayCredential(ctx context.Context, user Principal) (officialGatewayCredential, error) {
	return h.ensureOfficialGatewayCredentialForGroup(ctx, user, 0)
}

func (h *Handler) ensureOfficialGatewayCredentialForRequest(ctx context.Context, user Principal, externalGroupID int64) (officialGatewayCredential, error) {
	return h.ensureGatewayCredentialForGroupRequest(ctx, user, externalGroupID)
}

func (h *Handler) ensureGatewayCredentialForGroupRequest(ctx context.Context, user Principal, externalGroupID int64) (gatewayGroupCredential, error) {
	if externalGroupID <= 0 {
		return gatewayGroupCredential{}, officialGatewayCredentialError{
			Status: http.StatusBadRequest,
			Code:   "invalid_external_group_id",
			Err:    errors.New("externalGroupId is invalid"),
		}
	}
	credential, err := h.localOfficialGatewayCredential(ctx, user, externalGroupID)
	if err == nil {
		return credential, nil
	}
	var credentialErr officialGatewayCredentialError
	if !errors.As(err, &credentialErr) || credentialErr.Code != "gateway_credential_missing" {
		return gatewayGroupCredential{}, err
	}
	if retryAfter, blocked, limitErr := h.providerLimiter.allowProvision(ctx, user.ID, externalGroupID); limitErr != nil {
		return gatewayGroupCredential{}, officialGatewayCredentialError{
			Status: http.StatusServiceUnavailable,
			Code:   "rate_limit_unavailable",
			Err:    limitErr,
		}
	} else if blocked {
		return gatewayGroupCredential{}, officialGatewayCredentialError{
			Status:     http.StatusTooManyRequests,
			Code:       "provider_provision_rate_limited",
			Err:        errors.New("provider provisioning rate limited"),
			RetryAfter: retryAfter,
		}
	}
	release, locked, lockErr := h.acquireProviderProvisionLock(ctx, user.ID, externalGroupID)
	if lockErr != nil {
		return gatewayGroupCredential{}, officialGatewayCredentialError{
			Status: http.StatusServiceUnavailable,
			Code:   "provider_provision_lock_unavailable",
			Err:    lockErr,
		}
	}
	if !locked {
		if _, queueErr := h.enqueueGatewayProvisionForGroup(ctx, user, externalGroupID, errors.New("provider provisioning already in progress")); queueErr != nil {
			return gatewayGroupCredential{}, officialGatewayCredentialError{
				Status: http.StatusBadGateway,
				Code:   "gateway_provision_queue_failed",
				Err:    queueErr,
			}
		}
		return gatewayGroupCredential{}, officialGatewayCredentialError{
			Status:     http.StatusAccepted,
			Code:       "gateway_provision_in_progress",
			Err:        errors.New("gateway provisioning is already in progress"),
			RetryAfter: 30 * time.Second,
		}
	}
	defer func() {
		releaseCtx, cancelRelease := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelRelease()
		release(releaseCtx)
	}()

	if credential, err := h.localOfficialGatewayCredential(ctx, user, externalGroupID); err == nil {
		return credential, nil
	} else if !errors.As(err, &credentialErr) || credentialErr.Code != "gateway_credential_missing" {
		return gatewayGroupCredential{}, err
	}

	provisionCtx, cancelProvision := context.WithTimeout(ctx, 10*time.Second)
	defer cancelProvision()
	credential, err = h.ensureOfficialGatewayCredentialForGroup(provisionCtx, user, externalGroupID)
	if err != nil && shouldQueueGatewayProvision(err) {
		if _, queueErr := h.enqueueGatewayProvisionForGroup(ctx, user, externalGroupID, err); queueErr != nil {
			return gatewayGroupCredential{}, officialGatewayCredentialError{
				Status: http.StatusBadGateway,
				Code:   "gateway_provision_queue_failed",
				Err:    queueErr,
			}
		}
		return gatewayGroupCredential{}, officialGatewayCredentialError{
			Status:     http.StatusAccepted,
			Code:       "gateway_provision_queued",
			Err:        err,
			RetryAfter: 30 * time.Second,
		}
	}
	return credential, err
}

func (h *Handler) ensureOfficialGatewayCredentialForGroup(ctx context.Context, user Principal, externalGroupID int64) (officialGatewayCredential, error) {
	if externalGroupID <= 0 {
		externalGroupID = h.defaultOfficialProviderGroupID(ctx, user.ID)
	}
	if externalGroupID == 0 {
		if eligible, err := h.userHasOfficialCapabilityEntitlement(ctx, user.ID); err != nil {
			return officialGatewayCredential{}, officialGatewayCredentialError{
				Status: http.StatusInternalServerError,
				Code:   "official_capability_query_failed",
				Err:    err,
			}
		} else if !eligible {
			return officialGatewayCredential{}, officialGatewayCredentialError{
				Status: http.StatusAccepted,
				Code:   "official_capability_not_active",
				Err:    errors.New("official capability requires a balance package"),
			}
		}
		return officialGatewayCredential{}, officialGatewayCredentialError{
			Status: http.StatusConflict,
			Code:   "default_gateway_group_not_configured",
			Err:    errors.New("default gateway group is not configured"),
		}
	}
	if credential, err := h.localOfficialGatewayCredential(ctx, user, externalGroupID); err == nil {
		return credential, nil
	} else {
		var credentialErr officialGatewayCredentialError
		if !errors.As(err, &credentialErr) || credentialErr.Code != "gateway_credential_missing" {
			return officialGatewayCredential{}, err
		}
	}
	settings, err := h.gatewaySync.LoadSub2APISettings(ctx)
	if err != nil {
		return officialGatewayCredential{}, officialGatewayCredentialError{
			Status: http.StatusConflict,
			Code:   "sub2api_not_configured",
			Err:    err,
		}
	}
	client := h.gatewaySync.NewSub2APIClient(settings)
	gatewayUser := redeemsvc.GatewayUser{
		DBID:        user.ID,
		PublicID:    user.PublicID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Status:      user.Status,
	}
	account, err := h.gatewaySync.EnsureSub2APIAccountForGroup(ctx, client, gatewayUser, externalGroupID)
	if err != nil {
		return officialGatewayCredential{}, officialGatewayCredentialError{
			Status: http.StatusBadGateway,
			Code:   "gateway_account_sync_failed",
			Err:    err,
		}
	}
	apiKey, plainKey, err := h.gatewaySync.EnsureGatewayAPIKeyForUser(ctx, client, gatewayUser, account, externalGroupID)
	if err != nil {
		return officialGatewayCredential{}, officialGatewayCredentialError{
			Status: http.StatusBadGateway,
			Code:   "gateway_key_sync_failed",
			Err:    err,
		}
	}
	return officialGatewayCredential{Account: account, APIKey: apiKey, PlainKey: plainKey}, nil
}

func (h *Handler) enqueueGatewayProvision(ctx context.Context, user Principal, cause error) (string, error) {
	return h.enqueueGatewayProvisionForGroup(ctx, user, h.defaultOfficialProviderGroupID(ctx, user.ID), cause)
}

func (h *Handler) enqueueGatewayProvisionForGroup(ctx context.Context, user Principal, externalGroupID int64, cause error) (string, error) {
	reason := ""
	if cause != nil {
		reason = cause.Error()
	}
	if externalGroupID <= 0 {
		externalGroupID = h.defaultOfficialProviderGroupID(ctx, user.ID)
	}
	return operations.EnsureGatewayProvision(ctx, h.postgres, user.ID, user.PublicID, externalGroupID, reason)
}

func shouldQueueGatewayProvision(err error) bool {
	var credentialErr officialGatewayCredentialError
	if errors.As(err, &credentialErr) {
		return credentialErr.Code == "gateway_provision_busy" ||
			credentialErr.Code == "gateway_account_sync_failed" ||
			credentialErr.Code == "gateway_key_sync_failed"
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func writeOfficialGatewayCredentialError(c *gin.Context, err error) {
	var credentialErr officialGatewayCredentialError
	if errors.As(err, &credentialErr) {
		if credentialErr.RetryAfter > 0 {
			setRetryAfter(c, credentialErr.RetryAfter)
		}
		code := safeGatewayProvisionCode(err)
		payload := gin.H{"error": code, "detail": safeGatewayProvisionMessage(code)}
		if credentialErr.Status == http.StatusAccepted {
			if code == "official_capability_not_active" || code == "conversation_provider_not_active" {
				payload["status"] = "locked"
			} else {
				payload["status"] = "provisioning"
			}
			if credentialErr.RetryAfter > 0 {
				payload["retryAfterSeconds"] = int((credentialErr.RetryAfter + time.Second - 1) / time.Second)
			}
		}
		c.JSON(credentialErr.Status, payload)
		return
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "official_gateway_credential_failed"})
}

func safeGatewayProvisionCode(err error) string {
	var credentialErr officialGatewayCredentialError
	if errors.As(err, &credentialErr) && strings.TrimSpace(credentialErr.Code) != "" {
		return credentialErr.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "gateway_provision_timeout"
	}
	return "gateway_provision_failed"
}

func safeGatewayProvisionMessage(code string) string {
	switch strings.TrimSpace(code) {
	case "gateway_provision_busy", "gateway_credential_missing":
		return "官方配置正在准备中，请稍后刷新"
	case "gateway_provision_timeout":
		return "官方配置同步超时，请稍后刷新"
	case "gateway_account_sync_failed", "gateway_key_sync_failed":
		return "官方配置同步失败，请稍后重试或联系支持"
	case "gateway_provision_queue_failed":
		return "官方配置后台同步入队失败，请联系支持"
	case "default_gateway_group_not_configured":
		return "官方网关默认分组未配置，请联系支持"
	case "official_capability_not_active":
		return "兑换余额套餐后可启用官方模型配置"
	case "conversation_provider_not_active":
		return "兑换套餐后可启用对话模型配置"
	case "official_capability_query_failed":
		return "暂时无法校验官方能力资格，请稍后重试"
	case "group_not_official_capability":
		return "该分组未配置官方模型能力"
	case "invalid_external_group_id":
		return "分组 ID 不正确"
	case "group_not_available":
		return "当前账号无权使用该分组"
	case "group_permission_query_failed":
		return "暂时无法校验分组权限，请稍后重试"
	case "gateway_credential_query_failed":
		return "官方配置暂时不可用，请稍后重试"
	default:
		return "官方配置暂时不可用，请稍后重试"
	}
}

func (h *Handler) Redeem(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		writeAuthAPIError(c, http.StatusUnauthorized, "unauthorized", "请先登录")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeAuthAPIError(c, http.StatusBadRequest, "invalid_request", "请求格式不正确")
		return
	}
	code := normalizeCode(req.Code)
	if code == "" {
		writeAuthAPIError(c, http.StatusBadRequest, "code_required", "请输入兑换码")
		return
	}

	retryAfter, blocked, err := h.redeemLimiter.allow(c.Request.Context(), c.ClientIP(), user.ID, hashCode(code))
	if err != nil {
		writeAuthAPIError(c, http.StatusServiceUnavailable, "rate_limit_unavailable", "兑换风控暂时不可用，请稍后再试")
		return
	}
	if blocked {
		setRetryAfter(c, retryAfter)
		writeAuthAPIError(c, http.StatusTooManyRequests, "redeem_rate_limited", "兑换尝试过于频繁，请稍后再试")
		return
	}

	pending, err := h.markRedeemedLocally(c.Request.Context(), user, code)
	if errors.Is(err, errRedeemCodeNotFound) {
		writeAuthAPIError(c, http.StatusNotFound, "redeem_code_not_found", "兑换码不存在")
		return
	}
	if errors.Is(err, errRedeemCodeUsed) {
		writeAuthAPIError(c, http.StatusConflict, "redeem_code_used", "兑换码已被使用")
		return
	}
	if errors.Is(err, errRedeemCodeExpired) {
		writeAuthAPIError(c, http.StatusConflict, "redeem_code_expired", "兑换码已过期")
		return
	}
	if errors.Is(err, errRedeemCodeInvalidState) {
		writeAuthAPIError(c, http.StatusConflict, "redeem_code_invalid_state", "兑换码配置异常，请联系客服")
		return
	}
	if err != nil {
		writeAuthAPIError(c, http.StatusInternalServerError, "redeem_failed", "兑换失败，请稍后再试")
		return
	}
	defer h.invalidateGatewayEntitlementsCache(c.Request.Context(), user.ID)

	result, syncErr := h.syncRedemptionToGateway(c.Request.Context(), user, pending)
	if syncErr != nil {
		operation := strings.TrimSpace(result.Redemption.GatewayOperation)
		if operation == "" {
			operation = pending.GatewayOperation
		}
		errInfo := gatewayerror.Classify(operation, syncErr)
		_ = h.incrementGatewayOperationAttempt(c.Request.Context(), pending.OperationPublic)
		_ = operations.MarkFailed(c.Request.Context(), h.postgres, pending.OperationPublic, errInfo, time.Now().UTC().Add(operations.Backoff(1)), !errInfo.Retryable)
		_ = h.gatewaySync.UpdateRedemptionStatus(c.Request.Context(), pending.RedemptionDBID, "gateway_failed", errInfo, result.Redemption.ExternalUserID, pending.ExternalGroupID, operation)
		result.Redemption.Status = "gateway_failed"
		result.Redemption = applyRedemptionErrorInfo(result.Redemption, errInfo)
		c.JSON(http.StatusAccepted, gin.H{
			"status": "gateway_failed",
			"error":  authAPIError{Code: "redeem_gateway_sync_failed", Message: "兑换已记录，但网关同步失败，请稍后联系支持"},
			"result": result,
		})
		return
	}
	if result.Redemption.ErrorCode != "" {
		errInfo := gatewayInfoFromRedemption(result.Redemption)
		_ = h.incrementGatewayOperationAttempt(c.Request.Context(), pending.OperationPublic)
		_ = operations.MarkFailed(c.Request.Context(), h.postgres, pending.OperationPublic, errInfo, time.Now().UTC().Add(operations.Backoff(1)), false)
	} else {
		_ = operations.MarkSucceeded(c.Request.Context(), h.postgres, pending.OperationPublic, map[string]any{
			"redemption_id": result.Redemption.ID,
			"status":        result.Redemption.Status,
		})
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "result": result})
}

type authAPIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeAuthAPIError(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"error": authAPIError{Code: code, Message: message}})
}

func abortAuthAPIError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": authAPIError{Code: code, Message: message}})
}

func setRetryAfter(c *gin.Context, retryAfter time.Duration) {
	seconds := int((retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	c.Header("Retry-After", strconv.Itoa(seconds))
}

type tokenClaims struct {
	SubjectID int64  `json:"sub"`
	PublicID  string `json:"pid"`
	Kind      string `json:"kind"`
	TokenID   string `json:"jti"`
	FamilyID  string `json:"sid,omitempty"`
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
}

type tokenMetadata struct {
	IP        string
	UserAgent string
}

type refreshTokenStore interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func tokenMetadataFromRequest(c *gin.Context) tokenMetadata {
	return tokenMetadata{
		IP:        trimTokenMetadata(c.ClientIP(), 128),
		UserAgent: trimTokenMetadata(c.Request.UserAgent(), 500),
	}
}

func trimTokenMetadata(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func (h *Handler) issueTokenPair(ctx context.Context, user Principal, metadata tokenMetadata) (TokenPair, error) {
	tokens, _, err := h.issueTokenPairWithStore(ctx, h.postgres, user, metadata, "")
	return tokens, err
}

func (h *Handler) issueTokenPairWithStore(ctx context.Context, store refreshTokenStore, user Principal, metadata tokenMetadata, familyID string) (TokenPair, int64, error) {
	const (
		accessTTL  = 15 * time.Minute
		refreshTTL = 30 * 24 * time.Hour
	)
	now := time.Now().UTC()
	if familyID == "" {
		familyID = "rtf_" + uuid.NewString()
	}
	access, err := h.signToken(tokenClaims{
		SubjectID: user.ID,
		PublicID:  user.PublicID,
		Kind:      "access",
		TokenID:   "at_" + uuid.NewString(),
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(accessTTL).Unix(),
	}, h.accessSecret())
	if err != nil {
		return TokenPair{}, 0, err
	}
	refresh, err := h.signToken(tokenClaims{
		SubjectID: user.ID,
		PublicID:  user.PublicID,
		Kind:      "refresh",
		TokenID:   "rt_" + uuid.NewString(),
		FamilyID:  familyID,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(refreshTTL).Unix(),
	}, h.refreshSecret())
	if err != nil {
		return TokenPair{}, 0, err
	}
	var refreshTokenID int64
	if err := store.QueryRow(ctx, `
		INSERT INTO refresh_tokens (
			public_id, user_id, family_id, token_hash, status, expires_at,
			created_ip, user_agent
		)
		VALUES ($1, $2, $3, $4, 'active', $5, $6, $7)
		RETURNING id
	`, "rt_"+uuid.NewString(), user.ID, familyID, hashToken(refresh), now.Add(refreshTTL), metadata.IP, metadata.UserAgent).Scan(&refreshTokenID); err != nil {
		return TokenPair{}, 0, err
	}
	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(accessTTL.Seconds()),
	}, refreshTokenID, nil
}

func (h *Handler) rotateRefreshToken(ctx context.Context, refreshToken string, metadata tokenMetadata) (Principal, TokenPair, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	claims, err := h.parseToken(refreshToken, h.refreshSecret())
	if err != nil || claims.Kind != "refresh" || claims.TokenID == "" || claims.FamilyID == "" {
		return Principal{}, TokenPair{}, errors.New("invalid refresh token")
	}

	tx, err := h.postgres.Begin(ctx)
	if err != nil {
		return Principal{}, TokenPair{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var tokenID, userID int64
	var familyID, status string
	var expiresAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT id, user_id, family_id, status, expires_at
		FROM refresh_tokens
		WHERE token_hash = $1
		FOR UPDATE
	`, hashToken(refreshToken)).Scan(&tokenID, &userID, &familyID, &status, &expiresAt)
	if err != nil {
		return Principal{}, TokenPair{}, errors.New("invalid refresh token")
	}
	if userID != claims.SubjectID || familyID != claims.FamilyID {
		return Principal{}, TokenPair{}, errors.New("invalid refresh token")
	}
	if status != "active" {
		if status == "rotated" {
			_, _ = tx.Exec(ctx, `
				UPDATE refresh_tokens
				SET status = 'revoked', revoked_at = now(), revoked_reason = 'reuse_detected', updated_at = now()
				WHERE family_id = $1 AND status = 'active'
			`, familyID)
			_ = tx.Commit(ctx)
		}
		return Principal{}, TokenPair{}, errors.New("invalid refresh token")
	}
	if !expiresAt.After(time.Now().UTC()) {
		_, _ = tx.Exec(ctx, `
			UPDATE refresh_tokens
			SET status = 'expired', revoked_at = now(), revoked_reason = 'expired', updated_at = now()
			WHERE id = $1
		`, tokenID)
		_ = tx.Commit(ctx)
		return Principal{}, TokenPair{}, errors.New("refresh token expired")
	}

	user, err := findUserByIDWithStore(ctx, tx, claims.SubjectID)
	if err != nil || user.Status != "active" {
		return Principal{}, TokenPair{}, errors.New("invalid refresh token")
	}
	tokens, replacementID, err := h.issueTokenPairWithStore(ctx, tx, user, metadata, familyID)
	if err != nil {
		return Principal{}, TokenPair{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE refresh_tokens
		SET status = 'rotated',
			last_used_at = now(),
			revoked_at = now(),
			revoked_reason = 'rotation',
			replaced_by_token_id = $2,
			updated_at = now()
		WHERE id = $1 AND status = 'active'
	`, tokenID, replacementID); err != nil {
		return Principal{}, TokenPair{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Principal{}, TokenPair{}, err
	}
	return user, tokens, nil
}

func (h *Handler) revokeRefreshToken(ctx context.Context, refreshToken, reason string) error {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil
	}
	_, err := h.postgres.Exec(ctx, `
		UPDATE refresh_tokens
		SET status = 'revoked', revoked_at = now(), revoked_reason = $2, updated_at = now()
		WHERE token_hash = $1 AND status = 'active'
	`, hashToken(refreshToken), trimTokenMetadata(reason, 64))
	return err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func findUserByIDWithStore(ctx context.Context, store refreshTokenStore, id int64) (Principal, error) {
	var user Principal
	err := store.QueryRow(ctx, `
		SELECT id, public_id, email, display_name, status
		FROM users
		WHERE id = $1
	`, id).Scan(&user.ID, &user.PublicID, &user.Email, &user.DisplayName, &user.Status)
	return user, err
}

func (h *Handler) signToken(claims tokenClaims, secret []byte) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encodedHeader := base64.RawURLEncoding.EncodeToString(header)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := encodedHeader + "." + encodedPayload
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + signature, nil
}

func (h *Handler) parseToken(token string, secret []byte) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}, fmt.Errorf("invalid token")
	}
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	expected := mac.Sum(nil)
	actual, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(expected, actual) {
		return tokenClaims{}, fmt.Errorf("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, err
	}
	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return tokenClaims{}, err
	}
	if claims.SubjectID <= 0 || claims.ExpiresAt <= time.Now().Unix() {
		return tokenClaims{}, fmt.Errorf("token expired")
	}
	return claims, nil
}

func (h *Handler) accessSecret() []byte {
	return []byte(firstNonEmpty(h.cfg.JWTAccessSecret, h.cfg.SessionSecret, h.cfg.EncryptionKey, "brevyn-dev-access-secret"))
}

func (h *Handler) refreshSecret() []byte {
	return []byte(firstNonEmpty(h.cfg.JWTRefreshSecret, h.cfg.SessionSecret, h.cfg.EncryptionKey, "brevyn-dev-refresh-secret"))
}

func (h *Handler) findUserByEmail(ctx context.Context, email string) (Principal, string, error) {
	var user Principal
	var passwordHash string
	err := h.postgres.QueryRow(ctx, `
		SELECT id, public_id, email, display_name, status, password_hash
		FROM users
		WHERE email_hash = $1
	`, emailHash(email)).Scan(&user.ID, &user.PublicID, &user.Email, &user.DisplayName, &user.Status, &passwordHash)
	return user, passwordHash, err
}

func (h *Handler) findUserByID(ctx context.Context, id int64) (Principal, error) {
	var user Principal
	err := h.postgres.QueryRow(ctx, `
		SELECT id, public_id, email, display_name, status
		FROM users
		WHERE id = $1
	`, id).Scan(&user.ID, &user.PublicID, &user.Email, &user.DisplayName, &user.Status)
	return user, err
}

func currentUser(c *gin.Context) (Principal, bool) {
	value, ok := c.Get(userContextKey)
	if !ok {
		return Principal{}, false
	}
	user, ok := value.(Principal)
	return user, ok
}

func (h *Handler) wallet(ctx context.Context, userID int64) (walletSummary, error) {
	var balance float64
	err := h.postgres.QueryRow(ctx, `
		SELECT coalesce(sum(amount), 0)
		FROM wallet_transactions
		WHERE user_id = $1
	`, userID).Scan(&balance)
	return walletSummary{Balance: balance}, err
}

type pendingRedemption struct {
	RedemptionDBID   int64
	RedemptionPublic string
	OperationPublic  string
	CodeDBID         int64
	CodePublicID     string
	ProductDBID      sql.NullInt64
	ProductName      string
	BatchDBID        sql.NullInt64
	Kind             string
	Value            float64
	ValidityDays     int
	ExternalGroupID  int64
	GatewayOperation string
	BalanceAfter     float64
	CreatedAt        time.Time
}

type registerLimitRule struct {
	key    string
	limit  int64
	window time.Duration
}

type registerLimiter struct {
	redis *redis.Client
}

func newRegisterLimiter(redisClient *redis.Client) *registerLimiter {
	return &registerLimiter{redis: redisClient}
}

func (l *registerLimiter) allow(ctx context.Context, ip string, emailHash string) (time.Duration, bool, error) {
	if l == nil || l.redis == nil {
		return 0, false, nil
	}
	rules := []registerLimitRule{
		{key: "brevyn:rl:register:ip:" + hashRateLimitPart(ip), limit: 10, window: 10 * time.Minute},
		{key: "brevyn:rl:register:email:" + emailHash, limit: 5, window: 30 * time.Minute},
		{key: "brevyn:rl:register:ip_email:" + hashRateLimitPart(ip+":"+emailHash), limit: 5, window: 10 * time.Minute},
	}
	for _, rule := range rules {
		count, err := l.redis.Incr(ctx, rule.key).Result()
		if err != nil {
			return 0, false, err
		}
		if count == 1 {
			if err := l.redis.Expire(ctx, rule.key, rule.window).Err(); err != nil {
				return 0, false, err
			}
		}
		if count > rule.limit {
			ttl, err := l.redis.TTL(ctx, rule.key).Result()
			if err != nil || ttl <= 0 {
				ttl = rule.window
			}
			return ttl, true, nil
		}
	}
	return 0, false, nil
}

type loginLimitRule struct {
	key    string
	limit  int64
	window time.Duration
}

type loginLimiter struct {
	redis *redis.Client
}

func newLoginLimiter(redisClient *redis.Client) *loginLimiter {
	return &loginLimiter{redis: redisClient}
}

func (l *loginLimiter) allowRequest(ctx context.Context, ip string, emailHash string) (time.Duration, bool, error) {
	if l == nil || l.redis == nil {
		return 0, false, nil
	}
	rules := []loginLimitRule{
		{key: "brevyn:rl:login:req:ip:" + hashRateLimitPart(ip), limit: 120, window: 10 * time.Minute},
		{key: "brevyn:rl:login:req:ip_email:" + hashRateLimitPart(ip+":"+emailHash), limit: 20, window: 10 * time.Minute},
	}
	return l.incrementRules(ctx, rules)
}

func (l *loginLimiter) failureBlocked(ctx context.Context, ip string, emailHash string) (time.Duration, bool, error) {
	if l == nil || l.redis == nil {
		return 0, false, nil
	}
	rules := loginFailureRules(ip, emailHash)
	for _, rule := range rules {
		count, err := l.redis.Get(ctx, rule.key).Int64()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return 0, false, err
		}
		if count >= rule.limit {
			ttl, err := l.redis.TTL(ctx, rule.key).Result()
			if err != nil || ttl <= 0 {
				ttl = rule.window
			}
			return ttl, true, nil
		}
	}
	return 0, false, nil
}

func (l *loginLimiter) recordFailure(ctx context.Context, ip string, emailHash string) (time.Duration, bool, error) {
	if l == nil || l.redis == nil {
		return 0, false, nil
	}
	return l.incrementRules(ctx, loginFailureRules(ip, emailHash))
}

func (l *loginLimiter) clearPairFailure(ctx context.Context, ip string, emailHash string) error {
	if l == nil || l.redis == nil {
		return nil
	}
	return l.redis.Del(ctx, "brevyn:rl:login:fail:ip_email:"+hashRateLimitPart(ip+":"+emailHash)).Err()
}

func loginFailureRules(ip string, emailHash string) []loginLimitRule {
	return []loginLimitRule{
		{key: "brevyn:rl:login:fail:ip_email:" + hashRateLimitPart(ip+":"+emailHash), limit: 5, window: 15 * time.Minute},
		{key: "brevyn:rl:login:fail:email:" + emailHash, limit: 12, window: 15 * time.Minute},
		{key: "brevyn:rl:login:fail:ip:" + hashRateLimitPart(ip), limit: 60, window: 15 * time.Minute},
	}
}

func (l *loginLimiter) incrementRules(ctx context.Context, rules []loginLimitRule) (time.Duration, bool, error) {
	for _, rule := range rules {
		count, err := l.redis.Incr(ctx, rule.key).Result()
		if err != nil {
			return 0, false, err
		}
		if count == 1 {
			if err := l.redis.Expire(ctx, rule.key, rule.window).Err(); err != nil {
				return 0, false, err
			}
		}
		if count > rule.limit {
			ttl, err := l.redis.TTL(ctx, rule.key).Result()
			if err != nil || ttl <= 0 {
				ttl = rule.window
			}
			return ttl, true, nil
		}
	}
	return 0, false, nil
}

type refreshLimitRule struct {
	key    string
	limit  int64
	window time.Duration
}

type refreshLimiter struct {
	redis *redis.Client
}

func newRefreshLimiter(redisClient *redis.Client) *refreshLimiter {
	return &refreshLimiter{redis: redisClient}
}

func (l *refreshLimiter) allow(ctx context.Context, ip string, familyID string) (time.Duration, bool, error) {
	if l == nil || l.redis == nil {
		return 0, false, nil
	}
	rules := []refreshLimitRule{
		{key: "brevyn:rl:refresh:ip:" + hashRateLimitPart(ip), limit: 120, window: 10 * time.Minute},
	}
	if strings.TrimSpace(familyID) != "" {
		rules = append(rules, refreshLimitRule{
			key:    "brevyn:rl:refresh:family:" + hashRateLimitPart(familyID),
			limit:  30,
			window: 5 * time.Minute,
		})
	}
	for _, rule := range rules {
		count, err := l.redis.Incr(ctx, rule.key).Result()
		if err != nil {
			return 0, false, err
		}
		if count == 1 {
			if err := l.redis.Expire(ctx, rule.key, rule.window).Err(); err != nil {
				return 0, false, err
			}
		}
		if count > rule.limit {
			ttl, err := l.redis.TTL(ctx, rule.key).Result()
			if err != nil || ttl <= 0 {
				ttl = rule.window
			}
			return ttl, true, nil
		}
	}
	return 0, false, nil
}

var (
	errRedeemCodeNotFound     = errors.New("redeem code not found")
	errRedeemCodeUsed         = errors.New("redeem code used")
	errRedeemCodeExpired      = errors.New("redeem code expired")
	errRedeemCodeInvalidState = errors.New("redeem code invalid state")
)

type redeemLimitRule struct {
	key    string
	limit  int64
	window time.Duration
}

type redeemLimiter struct {
	redis *redis.Client
}

func newRedeemLimiter(redisClient *redis.Client) *redeemLimiter {
	return &redeemLimiter{redis: redisClient}
}

func (l *redeemLimiter) allow(ctx context.Context, ip string, userID int64, codeHash string) (time.Duration, bool, error) {
	if l == nil || l.redis == nil {
		return 0, false, nil
	}
	rules := []redeemLimitRule{
		{key: "brevyn:rl:redeem:ip:" + hashRateLimitPart(ip), limit: 60, window: 10 * time.Minute},
		{key: "brevyn:rl:redeem:user:" + strconv.FormatInt(userID, 10), limit: 20, window: 10 * time.Minute},
		{key: "brevyn:rl:redeem:code:" + codeHash, limit: 5, window: 10 * time.Minute},
	}
	for _, rule := range rules {
		count, err := l.redis.Incr(ctx, rule.key).Result()
		if err != nil {
			return 0, false, err
		}
		if count == 1 {
			if err := l.redis.Expire(ctx, rule.key, rule.window).Err(); err != nil {
				return 0, false, err
			}
		}
		if count > rule.limit {
			ttl, err := l.redis.TTL(ctx, rule.key).Result()
			if err != nil || ttl <= 0 {
				ttl = rule.window
			}
			return ttl, true, nil
		}
	}
	return 0, false, nil
}

type providerLimitRule struct {
	key    string
	limit  int64
	window time.Duration
}

type providerLimiter struct {
	redis *redis.Client
}

func newProviderLimiter(redisClient *redis.Client) *providerLimiter {
	return &providerLimiter{redis: redisClient}
}

func (l *providerLimiter) allowRead(ctx context.Context, ip string, userID int64) (time.Duration, bool, error) {
	if l == nil || l.redis == nil {
		return 0, false, nil
	}
	rules := []providerLimitRule{
		{key: "brevyn:rl:provider:read:user:" + strconv.FormatInt(userID, 10), limit: 120, window: 10 * time.Minute},
		{key: "brevyn:rl:provider:read:ip:" + hashRateLimitPart(ip), limit: 180, window: 10 * time.Minute},
	}
	return incrementProviderLimitRules(ctx, l.redis, rules)
}

func (l *providerLimiter) allowProvision(ctx context.Context, userID int64, externalGroupID int64) (time.Duration, bool, error) {
	if l == nil || l.redis == nil {
		return 0, false, nil
	}
	rules := []providerLimitRule{
		{key: "brevyn:rl:provider:provision:user_group:" + strconv.FormatInt(userID, 10) + ":" + strconv.FormatInt(externalGroupID, 10), limit: 10, window: 10 * time.Minute},
	}
	return incrementProviderLimitRules(ctx, l.redis, rules)
}

func incrementProviderLimitRules(ctx context.Context, redisClient *redis.Client, rules []providerLimitRule) (time.Duration, bool, error) {
	for _, rule := range rules {
		count, err := redisClient.Incr(ctx, rule.key).Result()
		if err != nil {
			return 0, false, err
		}
		if count == 1 {
			if err := redisClient.Expire(ctx, rule.key, rule.window).Err(); err != nil {
				return 0, false, err
			}
		}
		if count > rule.limit {
			ttl, err := redisClient.TTL(ctx, rule.key).Result()
			if err != nil || ttl <= 0 {
				ttl = rule.window
			}
			return ttl, true, nil
		}
	}
	return 0, false, nil
}

func (h *Handler) acquireProviderProvisionLock(ctx context.Context, userID int64, externalGroupID int64) (func(context.Context), bool, error) {
	if h.redis == nil {
		return func(context.Context) {}, true, nil
	}
	key := "brevyn:lock:provider:provision:user:" + strconv.FormatInt(userID, 10) + ":group:" + strconv.FormatInt(externalGroupID, 10)
	token := uuid.NewString()
	locked, err := h.redis.SetNX(ctx, key, token, 30*time.Second).Result()
	if err != nil {
		return nil, false, err
	}
	release := func(releaseCtx context.Context) {
		const releaseScript = `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) else return 0 end`
		_ = h.redis.Eval(releaseCtx, releaseScript, []string{key}, token).Err()
	}
	return release, locked, nil
}

func (h *Handler) markRedeemedLocally(ctx context.Context, user Principal, code string) (pendingRedemption, error) {
	tx, err := h.postgres.Begin(ctx)
	if err != nil {
		return pendingRedemption{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var pending pendingRedemption
	var status string
	var expiresAt sql.NullTime
	err = tx.QueryRow(ctx, `
		SELECT rc.id, rc.public_id, rc.kind, rc.value, rc.status, rc.expires_at,
			rc.product_id, coalesce(p.name, ''), rc.batch_id, rc.external_group_id, rc.validity_days
		FROM redeem_codes rc
		LEFT JOIN products p ON p.id = rc.product_id
		WHERE rc.code_hash = $1
		FOR UPDATE OF rc
	`, hashCode(code)).Scan(
		&pending.CodeDBID, &pending.CodePublicID, &pending.Kind, &pending.Value, &status, &expiresAt,
		&pending.ProductDBID, &pending.ProductName, &pending.BatchDBID, &pending.ExternalGroupID, &pending.ValidityDays,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return pendingRedemption{}, errRedeemCodeNotFound
	}
	if err != nil {
		return pendingRedemption{}, err
	}
	if status != "unused" {
		return pendingRedemption{}, errRedeemCodeUsed
	}
	if expiresAt.Valid && !expiresAt.Time.After(time.Now().UTC()) {
		return pendingRedemption{}, errRedeemCodeExpired
	}
	if pending.Kind != "balance" && pending.Kind != "subscription" {
		return pendingRedemption{}, fmt.Errorf("%w: unsupported kind %s", errRedeemCodeInvalidState, pending.Kind)
	}
	if pending.ExternalGroupID == 0 {
		pending.ExternalGroupID = h.gatewaySync.DefaultExternalGroupID(ctx)
	}
	if pending.Kind == "subscription" {
		if pending.ExternalGroupID == 0 || pending.ValidityDays == 0 {
			return pendingRedemption{}, fmt.Errorf("%w: subscription missing group or validity", errRedeemCodeInvalidState)
		}
		pending.GatewayOperation = "assign_subscription"
	} else {
		pending.GatewayOperation = "add_balance"
	}

	if pending.Kind == "balance" {
		if err := tx.QueryRow(ctx, `
			SELECT coalesce(sum(amount), 0) + $2
			FROM wallet_transactions
			WHERE user_id = $1
		`, user.ID, pending.Value).Scan(&pending.BalanceAfter); err != nil {
			return pendingRedemption{}, err
		}
	}

	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `
		UPDATE redeem_codes
		SET status = 'used', used_by_user_id = $2, used_at = $3, updated_at = now()
		WHERE id = $1 AND status = 'unused'
	`, pending.CodeDBID, user.ID, now)
	if err != nil {
		return pendingRedemption{}, err
	}
	if tag.RowsAffected() != 1 {
		return pendingRedemption{}, errRedeemCodeUsed
	}

	pending.RedemptionPublic = "rr_" + uuid.NewString()
	idempotencyKey := "redeem:" + pending.RedemptionPublic
	err = tx.QueryRow(ctx, `
		INSERT INTO redeem_redemptions (
			public_id, redeem_code_id, user_id, product_id, batch_id, kind, value,
			validity_days, gateway_provider, external_group_id, gateway_operation,
			status, idempotency_key
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'sub2api', $9, $10, 'pending_gateway', $11)
		RETURNING id, created_at
	`, pending.RedemptionPublic, pending.CodeDBID, user.ID, nullInt64Value(pending.ProductDBID),
		nullInt64Value(pending.BatchDBID), pending.Kind, pending.Value, pending.ValidityDays,
		pending.ExternalGroupID, pending.GatewayOperation, idempotencyKey).Scan(&pending.RedemptionDBID, &pending.CreatedAt)
	if isUniqueViolation(err) {
		return pendingRedemption{}, errRedeemCodeUsed
	}
	if err != nil {
		return pendingRedemption{}, err
	}

	if pending.Kind == "balance" {
		_, err = tx.Exec(ctx, `
			INSERT INTO wallet_transactions (public_id, user_id, kind, amount, balance_after, source, reference_id, notes)
			VALUES ($1, $2, 'redeem', $3, $4, 'redeem_code', $5, $6)
		`, "wtx_"+uuid.NewString(), user.ID, pending.Value, pending.BalanceAfter, pending.RedemptionPublic, "Brevyn redeem code")
		if err != nil {
			return pendingRedemption{}, err
		}
	}

	operationID, err := operations.CreateRedemptionSync(ctx, tx, pending.RedemptionDBID, pending.RedemptionPublic, user.ID)
	if err != nil {
		return pendingRedemption{}, err
	}
	pending.OperationPublic = operationID

	if err := tx.Commit(ctx); err != nil {
		return pendingRedemption{}, err
	}
	return pending, nil
}

func (h *Handler) syncRedemptionToGateway(ctx context.Context, user Principal, pending pendingRedemption) (redeemResult, error) {
	result := redeemResult{
		Redemption: redemptionSummary{
			ID:               pending.RedemptionPublic,
			CodeID:           pending.CodePublicID,
			ProductName:      pending.ProductName,
			Kind:             pending.Kind,
			Value:            pending.Value,
			ValidityDays:     pending.ValidityDays,
			ExternalGroupID:  pending.ExternalGroupID,
			GatewayOperation: pending.GatewayOperation,
			Status:           "pending_gateway",
			CreatedAt:        pending.CreatedAt,
		},
		Wallet: walletSummary{Balance: pending.BalanceAfter},
	}

	target := redeemsvc.SyncTarget{
		DBID:     pending.RedemptionDBID,
		PublicID: pending.RedemptionPublic,
		User: redeemsvc.GatewayUser{
			DBID:        user.ID,
			PublicID:    user.PublicID,
			Email:       user.Email,
			DisplayName: user.DisplayName,
			Status:      user.Status,
		},
		Kind:             pending.Kind,
		Value:            pending.Value,
		ValidityDays:     pending.ValidityDays,
		ExternalGroupID:  pending.ExternalGroupID,
		GatewayOperation: pending.GatewayOperation,
		Status:           "pending_gateway",
		ProductName:      pending.ProductName,
		CreatedAt:        pending.CreatedAt,
	}
	account, operation, err := h.gatewaySync.SyncTargetToSub2API(ctx, target)
	if strings.TrimSpace(operation) != "" {
		pending.GatewayOperation = operation
		result.Redemption.GatewayOperation = operation
	}
	if account.ExternalUserID > 0 {
		result.Gateway = gatewayAccountResponse(account)
		result.Redemption.ExternalUserID = account.ExternalUserID
	}
	if err != nil {
		return result, err
	}

	settings, err := h.gatewaySync.LoadSub2APISettings(ctx)
	if err != nil {
		return result, gatewayerror.WithStage("settings", err)
	}
	client := h.gatewaySync.NewSub2APIClient(settings)

	gatewayUser := redeemsvc.GatewayUser{
		DBID:        user.ID,
		PublicID:    user.PublicID,
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Status:      user.Status,
	}
	apiKey, plainKey, keyErr := h.gatewaySync.EnsureGatewayAPIKeyForUser(ctx, client, gatewayUser, account, pending.ExternalGroupID)
	if keyErr == nil {
		result.APIKey = gatewayAPIKeyPointerResponse(apiKey)
		result.PlainAPIKey = plainKey
	}

	wallet, _ := h.wallet(ctx, user.ID)
	result.Wallet = wallet
	result.Redemption.Status = "synced"
	if err := h.gatewaySync.UpdateRedemptionStatus(ctx, pending.RedemptionDBID, "synced", gatewayerror.Info{}, account.ExternalUserID, pending.ExternalGroupID, pending.GatewayOperation); err != nil {
		return result, err
	}
	if keyErr != nil {
		errInfo := gatewayerror.Classify("ensure_api_key", gatewayerror.WithStage("ensure_api_key", keyErr))
		errInfo.Code = "gateway_api_key_pending"
		errInfo.Class = "partial_success"
		errInfo.Message = "网关到账成功，但 API Key 暂未创建"
		errInfo.Retryable = true
		result.Redemption = applyRedemptionErrorInfo(result.Redemption, errInfo)
		_ = h.gatewaySync.UpdateRedemptionError(ctx, pending.RedemptionDBID, errInfo)
	}
	return result, nil
}

func applyRedemptionErrorInfo(redemption redemptionSummary, errInfo gatewayerror.Info) redemptionSummary {
	redemption.ErrorMessage = errInfo.Message
	redemption.ErrorCode = errInfo.Code
	redemption.ErrorClass = errInfo.Class
	redemption.ErrorStage = errInfo.Stage
	redemption.ErrorRetryable = errInfo.Retryable
	redemption.ErrorDetail = errInfo.Detail
	return redemption
}

func gatewayInfoFromRedemption(redemption redemptionSummary) gatewayerror.Info {
	return gatewayerror.Info{
		Code:      redemption.ErrorCode,
		Class:     redemption.ErrorClass,
		Stage:     redemption.ErrorStage,
		Message:   redemption.ErrorMessage,
		Detail:    redemption.ErrorDetail,
		Retryable: redemption.ErrorRetryable,
	}
}

func (h *Handler) incrementGatewayOperationAttempt(ctx context.Context, publicID string) error {
	if strings.TrimSpace(publicID) == "" {
		return nil
	}
	_, err := h.postgres.Exec(ctx, `
		UPDATE gateway_operations
		SET attempts = attempts + 1,
			started_at = coalesce(started_at, now()),
			updated_at = now()
		WHERE public_id = $1
	`, publicID)
	return err
}

func gatewayAccountResponse(account redeemsvc.GatewayAccountSummary) gatewayAccountSummary {
	return gatewayAccountSummary{
		Provider:       account.Provider,
		ExternalUserID: account.ExternalUserID,
		ExternalEmail:  account.ExternalEmail,
		DefaultGroupID: account.DefaultGroupID,
		Concurrency:    account.Concurrency,
		Status:         account.Status,
		LastSyncedAt:   account.LastSyncedAt,
	}
}

func gatewayAccountPointerResponse(account *redeemsvc.GatewayAccountSummary) *gatewayAccountSummary {
	if account == nil {
		return nil
	}
	item := gatewayAccountResponse(*account)
	return &item
}

func gatewayAPIKeyResponse(key redeemsvc.GatewayAPIKeySummary) apiKeySummary {
	return apiKeySummary{
		ID:              key.ID,
		Provider:        key.Provider,
		ExternalKeyID:   key.ExternalKeyID,
		ExternalGroupID: key.ExternalGroupID,
		MaskedAPIKey:    key.MaskedAPIKey,
		Status:          key.Status,
		LastUsedAt:      key.LastUsedAt,
		CreatedAt:       key.CreatedAt,
	}
}

func gatewayAPIKeyPointerResponse(key *redeemsvc.GatewayAPIKeySummary) *apiKeySummary {
	if key == nil {
		return nil
	}
	item := gatewayAPIKeyResponse(*key)
	return &item
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func emailHash(email string) string {
	sum := sha256.Sum256([]byte(normalizeEmail(email)))
	return hex.EncodeToString(sum[:])
}

func normalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeCode(code)))
	return hex.EncodeToString(sum[:])
}

func hashRateLimitPart(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func nullInt64Value(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
