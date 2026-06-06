package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type GatewayGroupItem struct {
	ID                              string                       `json:"id"`
	Provider                        string                       `json:"provider"`
	ExternalGroupID                 int64                        `json:"externalGroupId"`
	Name                            string                       `json:"name"`
	Description                     string                       `json:"description"`
	Platform                        string                       `json:"platform"`
	SubscriptionType                string                       `json:"subscriptionType"`
	RateMultiplier                  float64                      `json:"rateMultiplier"`
	IsExclusive                     bool                         `json:"isExclusive"`
	DailyLimitUSD                   *float64                     `json:"dailyLimitUsd"`
	WeeklyLimitUSD                  *float64                     `json:"weeklyLimitUsd"`
	MonthlyLimitUSD                 *float64                     `json:"monthlyLimitUsd"`
	DefaultValidityDays             int                          `json:"defaultValidityDays"`
	RPMLimit                        int                          `json:"rpmLimit"`
	SortOrder                       int                          `json:"sortOrder"`
	AllowImageGeneration            bool                         `json:"allowImageGeneration"`
	ImageRateIndependent            bool                         `json:"imageRateIndependent"`
	ImageRateMultiplier             float64                      `json:"imageRateMultiplier"`
	ImagePrice1K                    *float64                     `json:"imagePrice1k"`
	ImagePrice2K                    *float64                     `json:"imagePrice2k"`
	ImagePrice4K                    *float64                     `json:"imagePrice4k"`
	ClaudeCodeOnly                  bool                         `json:"claudeCodeOnly"`
	FallbackGroupID                 *int64                       `json:"fallbackGroupId"`
	FallbackGroupIDOnInvalidRequest *int64                       `json:"fallbackGroupIdOnInvalidRequest"`
	ModelRouting                    json.RawMessage              `json:"modelRouting"`
	ModelRoutingEnabled             bool                         `json:"modelRoutingEnabled"`
	MCPXMLInject                    bool                         `json:"mcpXmlInject"`
	SupportedModelScopes            json.RawMessage              `json:"supportedModelScopes"`
	AllowMessagesDispatch           bool                         `json:"allowMessagesDispatch"`
	RequireOAuthOnly                bool                         `json:"requireOauthOnly"`
	RequirePrivacySet               bool                         `json:"requirePrivacySet"`
	DefaultMappedModel              string                       `json:"defaultMappedModel"`
	MessagesDispatchModelConfig     json.RawMessage              `json:"messagesDispatchModelConfig"`
	Status                          string                       `json:"status"`
	CreatedAt                       time.Time                    `json:"createdAt"`
	UpdatedAt                       time.Time                    `json:"updatedAt"`
	Models                          []GatewayGroupModelItem      `json:"models"`
	Accounts                        []GatewayUpstreamAccountItem `json:"accounts"`
	Channels                        []GatewayChannelItem         `json:"channels"`
	OfficialModelConfig             GatewayGroupOfficialConfig    `json:"officialModelConfig"`
	UpstreamAccountCount            int                          `json:"upstreamAccountCount"`
	ActiveSchedulableAccountCount   int                          `json:"activeSchedulableAccountCount"`
	ChannelCount                    int                          `json:"channelCount"`
	PricedModelCount                int                          `json:"pricedModelCount"`
	UnpricedModelCount              int                          `json:"unpricedModelCount"`
}

type GatewayGroupModelItem struct {
	ID                string          `json:"id"`
	ExternalChannelID int64           `json:"externalChannelId"`
	Platform          string          `json:"platform"`
	ModelID           string          `json:"modelId"`
	DisplayName       string          `json:"displayName"`
	ProviderFamily    string          `json:"providerFamily"`
	Capabilities      []string        `json:"capabilities"`
	Pricing           json.RawMessage `json:"pricing"`
	BillingMode       string          `json:"billingMode"`
	Status            string          `json:"status"`
	LastSyncedAt      *time.Time      `json:"lastSyncedAt"`
	SourceType        string          `json:"sourceType"`
	PricingStatus     string          `json:"pricingStatus"`
	ChannelName       string          `json:"channelName"`
}

type GatewayGroupOfficialConfig map[string]GatewayGroupOfficialPurposeConfig

type GatewayGroupOfficialPurposeConfig struct {
	ModelIDs       []string `json:"modelIds"`
	DefaultModelID string   `json:"defaultModelId"`
}

type GatewayChannelItem struct {
	ID                 string          `json:"id"`
	ExternalChannelID  int64           `json:"externalChannelId"`
	Name               string          `json:"name"`
	Description        string          `json:"description"`
	Status             string          `json:"status"`
	BillingModelSource string          `json:"billingModelSource"`
	RestrictModels     bool            `json:"restrictModels"`
	GroupIDs           []int64         `json:"groupIds"`
	ModelMapping       json.RawMessage `json:"modelMapping"`
	ModelPricing       json.RawMessage `json:"modelPricing"`
	PricingCount       int             `json:"pricingCount"`
	LastSyncedAt       *time.Time      `json:"lastSyncedAt"`
}

type GatewayUpstreamAccountItem struct {
	ID                     string     `json:"id"`
	ExternalAccountID      int64      `json:"externalAccountId"`
	Name                   string     `json:"name"`
	Platform               string     `json:"platform"`
	AccountType            string     `json:"accountType"`
	Status                 string     `json:"status"`
	Schedulable            bool       `json:"schedulable"`
	Concurrency            int        `json:"concurrency"`
	CurrentConcurrency     int        `json:"currentConcurrency"`
	Priority               int        `json:"priority"`
	RateMultiplier         float64    `json:"rateMultiplier"`
	ErrorMessage           string     `json:"errorMessage"`
	GroupIDs               []int64    `json:"groupIds"`
	MappedModels           []string   `json:"mappedModels"`
	MappedModelCount       int        `json:"mappedModelCount"`
	LastUsedAt             *time.Time `json:"lastUsedAt"`
	ExpiresAt              *time.Time `json:"expiresAt"`
	RateLimitedAt          *time.Time `json:"rateLimitedAt"`
	RateLimitResetAt       *time.Time `json:"rateLimitResetAt"`
	OverloadUntil          *time.Time `json:"overloadUntil"`
	TempUnschedulableUntil *time.Time `json:"tempUnschedulableUntil"`
	LastSyncedAt           *time.Time `json:"lastSyncedAt"`
}

type ProductItem struct {
	ID               string    `json:"id"`
	SKU              string    `json:"sku"`
	Name             string    `json:"name"`
	Description      string    `json:"description"`
	BenefitType      string    `json:"benefitType"`
	PriceCNY         float64   `json:"priceCny"`
	OriginalPriceCNY *float64  `json:"originalPriceCny"`
	Value            float64   `json:"value"`
	ValidityDays     int       `json:"validityDays"`
	GatewayGroupID   string    `json:"gatewayGroupId"`
	GatewayGroupName string    `json:"gatewayGroupName"`
	ExternalGroupID  int64     `json:"externalGroupId"`
	Source           string    `json:"source"`
	Features         string    `json:"features"`
	ForSale          bool      `json:"forSale"`
	SortOrder        int       `json:"sortOrder"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type RedeemBatchItem struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Source      string    `json:"source"`
	OrderRef    string    `json:"orderRef"`
	Quantity    int       `json:"quantity"`
	Status      string    `json:"status"`
	Notes       string    `json:"notes"`
	ProductID   string    `json:"productId"`
	ProductName string    `json:"productName"`
	UnusedCount int       `json:"unusedCount"`
	UsedCount   int       `json:"usedCount"`
	CreatedAt   time.Time `json:"createdAt"`
}

type RedeemCodeItem struct {
	ID              string     `json:"id"`
	MaskedCode      string     `json:"maskedCode"`
	CodePrefix      string     `json:"codePrefix"`
	Kind            string     `json:"kind"`
	Value           float64    `json:"value"`
	ValidityDays    int        `json:"validityDays"`
	Status          string     `json:"status"`
	OrderRef        string     `json:"orderRef"`
	Notes           string     `json:"notes"`
	ProductID       string     `json:"productId"`
	ProductName     string     `json:"productName"`
	ProductSKU      string     `json:"productSku"`
	BatchID         string     `json:"batchId"`
	BatchName       string     `json:"batchName"`
	ExternalGroupID int64      `json:"externalGroupId"`
	Source          string     `json:"source"`
	UsedByUserID    string     `json:"usedByUserId"`
	UsedByEmail     string     `json:"usedByEmail"`
	UsedAt          *time.Time `json:"usedAt"`
	ExpiresAt       *time.Time `json:"expiresAt"`
	CreatedAt       time.Time  `json:"createdAt"`
}

type RedemptionItem struct {
	ID                   string     `json:"id"`
	RedeemCodeID         string     `json:"redeemCodeId"`
	UserID               string     `json:"userId"`
	UserEmail            string     `json:"userEmail"`
	ProductName          string     `json:"productName"`
	BatchName            string     `json:"batchName"`
	OrderRef             string     `json:"orderRef"`
	Kind                 string     `json:"kind"`
	Value                float64    `json:"value"`
	ValidityDays         int        `json:"validityDays"`
	ExternalUserID       int64      `json:"externalUserId"`
	ExternalGroupID      int64      `json:"externalGroupId"`
	GatewayOperation     string     `json:"gatewayOperation"`
	Status               string     `json:"status"`
	ErrorMessage         string     `json:"errorMessage"`
	ErrorCode            string     `json:"errorCode"`
	ErrorClass           string     `json:"errorClass"`
	ErrorStage           string     `json:"errorStage"`
	ErrorRetryable       bool       `json:"errorRetryable"`
	ErrorDetail          string     `json:"errorDetail"`
	OperationID          string     `json:"operationId"`
	OperationStatus      string     `json:"operationStatus"`
	OperationAttempts    int        `json:"operationAttempts"`
	OperationMaxAttempts int        `json:"operationMaxAttempts"`
	OperationNextRunAt   *time.Time `json:"operationNextRunAt"`
	CreatedAt            time.Time  `json:"createdAt"`
}

type createGatewayGroupRequest struct {
	ExternalGroupID     int64    `json:"externalGroupId"`
	Name                string   `json:"name"`
	Description         string   `json:"description"`
	Platform            string   `json:"platform"`
	SubscriptionType    string   `json:"subscriptionType"`
	RateMultiplier      float64  `json:"rateMultiplier"`
	DailyLimitUSD       *float64 `json:"dailyLimitUsd"`
	WeeklyLimitUSD      *float64 `json:"weeklyLimitUsd"`
	MonthlyLimitUSD     *float64 `json:"monthlyLimitUsd"`
	DefaultValidityDays int      `json:"defaultValidityDays"`
	Status              string   `json:"status"`
}

type updateGatewayGroupOfficialModelsRequest struct {
	Capabilities GatewayGroupOfficialConfig `json:"capabilities"`
	AuditReason  string                     `json:"auditReason"`
	Reason       string                     `json:"reason"`
}

type createProductRequest struct {
	SKU              string   `json:"sku"`
	Name             string   `json:"name"`
	Description      string   `json:"description"`
	BenefitType      string   `json:"benefitType"`
	PriceCNY         float64  `json:"priceCny"`
	OriginalPriceCNY *float64 `json:"originalPriceCny"`
	Value            float64  `json:"value"`
	ValidityDays     int      `json:"validityDays"`
	GatewayGroupID   string   `json:"gatewayGroupId"`
	ExternalGroupID  int64    `json:"externalGroupId"`
	Source           string   `json:"source"`
	Features         string   `json:"features"`
	ForSale          *bool    `json:"forSale"`
	SortOrder        int      `json:"sortOrder"`
	Status           string   `json:"status"`
}

type generateRedeemCodesRequest struct {
	ProductID     string     `json:"productId"`
	Count         int        `json:"count"`
	BatchName     string     `json:"batchName"`
	Source        string     `json:"source"`
	OrderRef      string     `json:"orderRef"`
	Notes         string     `json:"notes"`
	ExpiresAt     *time.Time `json:"expiresAt"`
	ExpiresInDays *int       `json:"expiresInDays"`
}

type generatedRedeemCode struct {
	Code       string `json:"code"`
	MaskedCode string `json:"maskedCode"`
	CodePrefix string `json:"codePrefix"`
}

func (h *Handler) ListGatewayGroups(c *gin.Context) {
	items, err := h.gatewayGroups.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) CreateGatewayGroup(c *gin.Context) {
	var req createGatewayGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	item, err := h.gatewayGroups.Create(c.Request.Context(), req)
	if err != nil {
		if err.Error() == "name_required" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create_failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"group": item})
}

func (h *Handler) UpdateGatewayGroupOfficialModels(c *gin.Context) {
	externalGroupID, err := strconv.ParseInt(strings.TrimSpace(c.Param("externalGroupId")), 10, 64)
	if err != nil || externalGroupID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_external_group_id"})
		return
	}
	var req updateGatewayGroupOfficialModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}
	config, err := h.gatewayGroups.UpdateOfficialModelConfig(c.Request.Context(), externalGroupID, req)
	if err != nil {
		status := http.StatusInternalServerError
		errorCode := "official_model_config_update_failed"
		if isOfficialModelConfigValidationError(err) {
			status = http.StatusBadRequest
			errorCode = err.Error()
		}
		c.JSON(status, gin.H{"error": errorCode})
		return
	}
	admin, _ := currentAdmin(c)
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "gateway_group.official_models.update", "gateway_group", strconv.FormatInt(externalGroupID, 10), c.ClientIP(), c.Request.UserAgent(), auditMetadataWithReason(auditReason, map[string]any{
		"external_group_id": externalGroupID,
		"capabilities":      config,
	}))
	c.JSON(http.StatusOK, gin.H{"officialModelConfig": config})
}

func (h *Handler) ListProducts(c *gin.Context) {
	items, err := h.catalog.ListProducts(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) CreateProduct(c *gin.Context) {
	var req createProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	item, err := h.catalog.CreateProduct(c.Request.Context(), req)
	if err != nil {
		status := http.StatusConflict
		errorCode := "product_create_failed"
		if isProductValidationError(err) {
			status = http.StatusBadRequest
			errorCode = err.Error()
		}
		c.JSON(status, gin.H{"error": errorCode})
		return
	}
	admin, _ := currentAdmin(c)
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "product.create", "product", item.ID, c.ClientIP(), c.Request.UserAgent(), "{}")
	c.JSON(http.StatusCreated, gin.H{"product": item})
}

func (h *Handler) UpdateProduct(c *gin.Context) {
	productID := strings.TrimSpace(c.Param("id"))
	var req createProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	item, err := h.catalog.UpdateProduct(c.Request.Context(), productID, req)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "product_not_found"})
		return
	}
	if err != nil {
		status := http.StatusConflict
		errorCode := "product_update_failed"
		if isProductValidationError(err) {
			status = http.StatusBadRequest
			errorCode = err.Error()
		}
		c.JSON(status, gin.H{"error": errorCode})
		return
	}
	admin, _ := currentAdmin(c)
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "product.update", "product", item.ID, c.ClientIP(), c.Request.UserAgent(), "{}")
	c.JSON(http.StatusOK, gin.H{"product": item})
}

func (h *Handler) DeleteProduct(c *gin.Context) {
	productID := strings.TrimSpace(c.Param("id"))
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
	item, err := h.catalog.ArchiveProduct(c.Request.Context(), productID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "product_not_found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "archive_failed"})
		return
	}
	admin, _ := currentAdmin(c)
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "product.archive", "product", item.ID, c.ClientIP(), c.Request.UserAgent(), auditMetadataWithReason(auditReason, nil))
	c.JSON(http.StatusOK, gin.H{"status": "ok", "mode": "archived", "product": item})
}

func (h *Handler) ListRedeemBatches(c *gin.Context) {
	limit, offset := parseListPagination(c, 50, 300)
	items, total, err := h.redeemQueries.ListRedeemBatches(c.Request.Context(), RedeemBatchListFilters{
		Search:    c.Query("search"),
		Status:    c.Query("status"),
		Source:    c.Query("source"),
		ProductID: c.Query("productId"),
		DateFrom:  c.Query("dateFrom"),
		DateTo:    c.Query("dateTo"),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		if err.Error() == "invalid_date" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) DisableRedeemBatch(c *gin.Context) {
	batchID := strings.TrimSpace(c.Param("id"))
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

	item, disabledCount, err := h.catalog.DisableRedeemBatch(c.Request.Context(), batchID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "redeem_batch_not_found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "redeem_batch_disable_failed", "detail": err.Error()})
		return
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"redeem_batch.disable",
		"redeem_code_batch",
		item.ID,
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{
			"batch_name":     item.Name,
			"product_name":   item.ProductName,
			"source":         item.Source,
			"order_ref":      item.OrderRef,
			"disabled_codes": disabledCount,
		}),
	)
	c.JSON(http.StatusOK, gin.H{"status": "disabled", "batch": item, "disabledCodes": disabledCount})
}

func (h *Handler) ListRedeemCodes(c *gin.Context) {
	limit, offset := parseListPagination(c, 100, 500)
	items, total, err := h.redeemQueries.ListRedeemCodes(c.Request.Context(), RedeemCodeListFilters{
		Search:    c.Query("search"),
		Status:    c.Query("status"),
		CodeType:  c.Query("type"),
		Source:    c.Query("source"),
		ProductID: c.Query("productId"),
		BatchID:   c.Query("batchId"),
		UsedBy:    c.Query("usedBy"),
		DateFrom:  c.Query("dateFrom"),
		DateTo:    c.Query("dateTo"),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		if err.Error() == "invalid_date" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) DisableRedeemCode(c *gin.Context) {
	codeID := strings.TrimSpace(c.Param("id"))
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

	item, err := h.catalog.DisableRedeemCode(c.Request.Context(), codeID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "redeem_code_not_found"})
		return
	}
	if err != nil {
		if err.Error() == "redeem_code_not_unused" {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "redeem_code_disable_failed", "detail": err.Error()})
		return
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"redeem_code.disable",
		"redeem_code",
		item.ID,
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{
			"code_prefix":  item.CodePrefix,
			"product_name": item.ProductName,
			"batch_name":   item.BatchName,
			"source":       item.Source,
			"order_ref":    item.OrderRef,
		}),
	)
	c.JSON(http.StatusOK, gin.H{"status": "disabled", "redeemCode": item})
}

func (h *Handler) GenerateRedeemCodes(c *gin.Context) {
	var req generateRedeemCodesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	req.ProductID = strings.TrimSpace(req.ProductID)
	req.BatchName = strings.TrimSpace(req.BatchName)
	req.Source = strings.TrimSpace(req.Source)
	req.OrderRef = strings.TrimSpace(req.OrderRef)
	req.Notes = strings.TrimSpace(req.Notes)
	if req.ProductID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "product_required"})
		return
	}
	if req.Count <= 0 || req.Count > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "count_must_be_1_to_500"})
		return
	}
	if req.BatchName == "" {
		req.BatchName = "manual-" + time.Now().Format("20060102-150405")
	}
	if req.Source == "" {
		req.Source = "ldxp"
	}
	expiresAt, err := resolveCodeExpiry(req.ExpiresAt, req.ExpiresInDays)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	admin, _ := currentAdmin(c)
	result, err := h.catalog.GenerateRedeemCodes(c.Request.Context(), req, admin.ID, expiresAt)
	if err != nil {
		var duplicateOrder *duplicateOrderRefError
		if errors.As(err, &duplicateOrder) {
			c.JSON(http.StatusConflict, gin.H{
				"error":  "order_ref_already_exists",
				"detail": fmt.Sprintf("订单号 %s 已生成批次 %s，未重复生成卡密", req.OrderRef, duplicateOrder.Batch.Name),
				"batch":  duplicateOrder.Batch,
			})
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "product_not_found"})
			return
		}
		status := http.StatusInternalServerError
		if isRedeemGenerationValidationError(err) {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": "generate_failed", "detail": err.Error()})
		return
	}
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "redeem_codes.generate", "product", req.ProductID, c.ClientIP(), c.Request.UserAgent(), fmt.Sprintf(`{"count":%d,"order_ref":%q}`, req.Count, req.OrderRef))
	c.JSON(http.StatusCreated, result)
}

func (h *Handler) ListRedemptions(c *gin.Context) {
	filters := redemptionListFilters{
		Search:     strings.TrimSpace(c.Query("search")),
		Status:     strings.TrimSpace(c.Query("status")),
		Kind:       strings.TrimSpace(c.Query("type")),
		Source:     strings.TrimSpace(c.Query("source")),
		ProductID:  strings.TrimSpace(c.Query("productId")),
		BatchID:    strings.TrimSpace(c.Query("batchId")),
		User:       strings.TrimSpace(c.Query("user")),
		ErrorClass: strings.TrimSpace(c.Query("errorClass")),
		Retryable:  strings.TrimSpace(c.Query("retryable")),
		DateFrom:   strings.TrimSpace(c.Query("dateFrom")),
		DateTo:     strings.TrimSpace(c.Query("dateTo")),
	}
	filters.Limit, filters.Offset = parseListPagination(c, 100, 500)
	items, total, err := h.queryRedemptions(c.Request.Context(), filters)
	if err != nil {
		if err.Error() == "invalid_date" {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "limit": filters.Limit, "offset": filters.Offset})
}

type generationProduct struct {
	dbID                  int64
	gatewayGroupDBID      *int64
	status                string
	forSale               bool
	groupSubscriptionType string
	groupStatus           string
	ID                    string  `json:"id"`
	SKU                   string  `json:"sku"`
	Name                  string  `json:"name"`
	BenefitType           string  `json:"benefitType"`
	Value                 float64 `json:"value"`
	ValidityDays          int     `json:"validityDays"`
	ExternalGroupID       int64   `json:"externalGroupId"`
}

func loadProductForGeneration(ctx context.Context, tx pgx.Tx, productID string) (generationProduct, error) {
	var product generationProduct
	err := tx.QueryRow(ctx, `
		SELECT p.id, p.public_id, p.sku, p.name, p.benefit_type, p.value, p.validity_days,
			p.gateway_group_id, p.external_group_id, p.status, p.for_sale,
			coalesce(gg.subscription_type, ''), coalesce(gg.status, '')
		FROM products p
		LEFT JOIN gateway_groups gg ON gg.id = p.gateway_group_id
		WHERE p.public_id = $1 OR p.sku = $1
	`, productID).Scan(&product.dbID, &product.ID, &product.SKU, &product.Name, &product.BenefitType,
		&product.Value, &product.ValidityDays, &product.gatewayGroupDBID, &product.ExternalGroupID,
		&product.status, &product.forSale, &product.groupSubscriptionType, &product.groupStatus)
	return product, err
}

func validateProductForGeneration(product generationProduct) error {
	if product.status != "active" {
		return fmt.Errorf("product_not_active")
	}
	if !product.forSale {
		return fmt.Errorf("product_not_for_sale")
	}
	if product.gatewayGroupDBID != nil && product.groupStatus != "active" {
		return fmt.Errorf("product_gateway_group_not_active")
	}
	switch product.BenefitType {
	case "balance":
		if product.Value <= 0 {
			return fmt.Errorf("balance_product_value_required")
		}
		if product.gatewayGroupDBID != nil && product.groupSubscriptionType == "subscription" {
			return fmt.Errorf("balance_product_requires_standard_group")
		}
	case "subscription":
		if product.gatewayGroupDBID == nil || product.ExternalGroupID == 0 {
			return fmt.Errorf("subscription_product_requires_gateway_group")
		}
		if product.groupSubscriptionType != "subscription" {
			return fmt.Errorf("subscription_product_requires_subscription_group")
		}
		if product.ValidityDays <= 0 {
			return fmt.Errorf("subscription_product_validity_days_required")
		}
	default:
		return fmt.Errorf("unsupported_benefit_type")
	}
	return nil
}

func isRedeemGenerationValidationError(err error) bool {
	if err == nil {
		return false
	}
	switch err.Error() {
	case "product_not_active",
		"product_not_for_sale",
		"product_gateway_group_not_active",
		"balance_product_value_required",
		"balance_product_requires_standard_group",
		"subscription_product_requires_gateway_group",
		"subscription_product_requires_subscription_group",
		"subscription_product_validity_days_required",
		"unsupported_benefit_type":
		return true
	default:
		return false
	}
}

func validateProductRequest(req *createProductRequest, allowAutoSKU bool) error {
	req.SKU = strings.ToLower(strings.TrimSpace(req.SKU))
	req.Name = strings.TrimSpace(req.Name)
	req.BenefitType = strings.TrimSpace(req.BenefitType)
	if req.BenefitType == "" {
		req.BenefitType = "balance"
	}
	if req.SKU == "" {
		if !allowAutoSKU {
			return fmt.Errorf("sku_required")
		}
		req.SKU = generateProductSKU(req)
	}
	if req.Name == "" {
		return fmt.Errorf("name_required")
	}
	switch req.BenefitType {
	case "balance":
		if req.Value <= 0 {
			return fmt.Errorf("balance_value_required")
		}
	case "subscription":
		if req.ValidityDays <= 0 {
			return fmt.Errorf("validity_days_required")
		}
	default:
		return fmt.Errorf("unsupported_benefit_type")
	}
	if req.PriceCNY < 0 {
		return fmt.Errorf("price_invalid")
	}
	return nil
}

func generateProductSKU(req *createProductRequest) string {
	suffix := strings.ToLower(strings.ReplaceAll(uuid.NewString()[:8], "-", ""))
	switch req.BenefitType {
	case "balance":
		return fmt.Sprintf("balance-usd-%s-%s", compactNumber(req.Value), suffix)
	case "subscription":
		return fmt.Sprintf("subscription-%dd-%s", req.ValidityDays, suffix)
	default:
		return "product-" + suffix
	}
}

func compactNumber(value float64) string {
	text := strconv.FormatFloat(value, 'f', 2, 64)
	text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	text = strings.ReplaceAll(text, ".", "-")
	if text == "" {
		return "0"
	}
	return text
}

func resolveCodeExpiry(expiresAt *time.Time, expiresInDays *int) (*time.Time, error) {
	if expiresAt != nil && expiresInDays != nil {
		return nil, fmt.Errorf("expiry_conflict")
	}
	if expiresInDays != nil {
		if *expiresInDays <= 0 {
			return nil, fmt.Errorf("expires_in_days_invalid")
		}
		value := time.Now().UTC().AddDate(0, 0, *expiresInDays)
		return &value, nil
	}
	if expiresAt == nil {
		return nil, nil
	}
	value := expiresAt.UTC()
	if !value.After(time.Now().UTC()) {
		return nil, fmt.Errorf("expires_at_must_be_future")
	}
	return &value, nil
}

func scanProduct(row scanner) (ProductItem, error) {
	var item ProductItem
	err := row.Scan(
		&item.ID,
		&item.SKU,
		&item.Name,
		&item.Description,
		&item.BenefitType,
		&item.PriceCNY,
		&item.OriginalPriceCNY,
		&item.Value,
		&item.ValidityDays,
		&item.GatewayGroupID,
		&item.GatewayGroupName,
		&item.ExternalGroupID,
		&item.Source,
		&item.Features,
		&item.ForSale,
		&item.SortOrder,
		&item.Status,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func scanRedeemCode(row scanner) (RedeemCodeItem, error) {
	var item RedeemCodeItem
	err := row.Scan(
		&item.ID,
		&item.CodePrefix,
		&item.Kind,
		&item.Value,
		&item.ValidityDays,
		&item.Status,
		&item.OrderRef,
		&item.Notes,
		&item.ProductID,
		&item.ProductName,
		&item.ProductSKU,
		&item.BatchID,
		&item.BatchName,
		&item.ExternalGroupID,
		&item.Source,
		&item.UsedByUserID,
		&item.UsedByEmail,
		&item.UsedAt,
		&item.ExpiresAt,
		&item.CreatedAt,
	)
	item.MaskedCode = maskCode(item.CodePrefix)
	return item, err
}

func nullableAdminID(adminID int64) any {
	if adminID <= 0 {
		return nil
	}
	return adminID
}

func generateCodeValue() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	raw := strings.ToUpper(hex.EncodeToString(bytes))
	parts := []string{raw[0:8], raw[8:16], raw[16:24], raw[24:32]}
	return "BVN-" + strings.Join(parts, "-"), nil
}

func hashCode(code string) string {
	sum := sha256.Sum256([]byte(normalizeCode(code)))
	return hex.EncodeToString(sum[:])
}

func normalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

func codePrefix(code string) string {
	code = normalizeCode(code)
	if len(code) <= 12 {
		return code
	}
	return code[:12]
}

func maskCode(prefix string) string {
	if prefix == "" {
		return "****"
	}
	return prefix + "-****"
}
