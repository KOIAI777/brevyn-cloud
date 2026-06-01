package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/audit"
	"github.com/gin-gonic/gin"
)

type OverviewSummary struct {
	ActiveUsers        int64   `json:"activeUsers"`
	UsersToday         int64   `json:"usersToday"`
	TotalUsers         int64   `json:"totalUsers"`
	WalletBalanceUSD   float64 `json:"walletBalanceUsd"`
	ActiveKeys         int64   `json:"activeKeys"`
	ReviewKeys         int64   `json:"reviewKeys"`
	RedemptionsToday   int64   `json:"redemptionsToday"`
	GatewayFailedToday int64   `json:"gatewayFailedToday"`
	RequestCountToday  int64   `json:"requestCountToday"`
	CostTodayUSD       float64 `json:"costTodayUsd"`
	UsageStatus        string  `json:"usageStatus"`
}

type UsageSummary struct {
	Usage  UsageMeter  `json:"usage"`
	Ledger UsageLedger `json:"ledger"`
}

type UsageMeter struct {
	Status            string  `json:"status"`
	Source            string  `json:"source"`
	RequestCountToday int64   `json:"requestCountToday"`
	InputTokensToday  int64   `json:"inputTokensToday"`
	OutputTokensToday int64   `json:"outputTokensToday"`
	CostTodayUSD      float64 `json:"costTodayUsd"`
	ActualCostUSD     float64 `json:"actualCostUsd"`
}

type UsageLedger struct {
	WalletBalanceUSD        float64 `json:"walletBalanceUsd"`
	WalletCreditsTodayUSD   float64 `json:"walletCreditsTodayUsd"`
	WalletCreditsTotalUSD   float64 `json:"walletCreditsTotalUsd"`
	BalanceRedeemedTodayUSD float64 `json:"balanceRedeemedTodayUsd"`
	BalanceRedeemedTotalUSD float64 `json:"balanceRedeemedTotalUsd"`
	RedemptionCountToday    int64   `json:"redemptionCountToday"`
	SubscriptionCountToday  int64   `json:"subscriptionCountToday"`
	GatewayFailedToday      int64   `json:"gatewayFailedToday"`
}

type ModelCatalogItem struct {
	ID                string    `json:"id"`
	DisplayName       string    `json:"displayName"`
	ProviderFamily    string    `json:"providerFamily"`
	Capabilities      []string  `json:"capabilities"`
	PublicVisible     bool      `json:"publicVisible"`
	SupportsStreaming bool      `json:"supportsStreaming"`
	Status            string    `json:"status"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

type AuditLogItem struct {
	ID          string    `json:"id"`
	ActorType   string    `json:"actorType"`
	ActorID     int64     `json:"actorId"`
	ActorLabel  string    `json:"actorLabel"`
	Action      string    `json:"action"`
	ActionLabel string    `json:"actionLabel"`
	TargetType  string    `json:"targetType"`
	TargetID    string    `json:"targetId"`
	IP          string    `json:"ip"`
	UserAgent   string    `json:"userAgent"`
	Metadata    string    `json:"metadata"`
	Summary     string    `json:"summary"`
	ResultTone  string    `json:"resultTone"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (h *Handler) Overview(c *gin.Context) {
	summary, err := h.dashboard.OverviewSummary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "overview_query_failed"})
		return
	}
	recent, _, err := h.queryRedemptions(c.Request.Context(), redemptionListFilters{Limit: 5})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "redemptions_query_failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"summary":           summary,
		"recentRedemptions": recent,
		"generatedAt":       time.Now().UTC(),
	})
}

func (h *Handler) UsageSummary(c *gin.Context) {
	summary, err := h.dashboard.UsageSummary(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "usage_summary_query_failed"})
		return
	}
	c.JSON(http.StatusOK, summary)
}

func (h *Handler) ListModels(c *gin.Context) {
	items, err := h.dashboard.ListModels(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "models_query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) ListAuditLogs(c *gin.Context) {
	limit, offset := parseListPagination(c, 100, 500)
	items, total, err := h.auditQueries.List(c.Request.Context(), AuditLogListFilters{
		Search:    c.Query("search"),
		Action:    c.Query("action"),
		ActorType: c.Query("actorType"),
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "audit_query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (h *Handler) writeAuditLog(ctx context.Context, actorType string, actorID int64, action string, targetType string, targetID string, ip string, userAgent string, metadata string) {
	if h.audit == nil {
		return
	}
	_ = h.audit.Record(ctx, audit.Entry{
		ActorType:  actorType,
		ActorID:    actorID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		IP:         ip,
		UserAgent:  userAgent,
		Metadata:   metadata,
	})
}
