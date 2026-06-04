package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

const (
	gatewayEntitlementsCacheTTL      = 30 * time.Second
	gatewayEntitlementsStaleCacheTTL = 10 * time.Minute
)

type gatewayEntitlementsResponse struct {
	ExternalUserID     int64                          `json:"externalUserId"`
	Wallet             gatewayEntitlementWallet       `json:"wallet"`
	BalanceGroups      []balanceGroupEntitlement      `json:"balanceGroups"`
	SubscriptionGroups []subscriptionGroupEntitlement `json:"subscriptionGroups"`
	UpdatedAt          time.Time                      `json:"updatedAt"`
	Stale              bool                           `json:"stale"`
}

type gatewayEntitlementWallet struct {
	Source    string  `json:"source"`
	Scope     string  `json:"scope"`
	Remaining float64 `json:"remaining"`
	Unit      string  `json:"unit"`
	Status    string  `json:"status"`
}

type balanceGroupEntitlement struct {
	ExternalGroupID  int64   `json:"externalGroupId"`
	Name             string  `json:"name"`
	Description      string  `json:"description,omitempty"`
	Platform         string  `json:"platform"`
	BillingKind      string  `json:"billingKind"`
	SubscriptionType string  `json:"subscriptionType"`
	BalanceScope     string  `json:"balanceScope"`
	Remaining        float64 `json:"remaining"`
	Unit             string  `json:"unit"`
	RateMultiplier   float64 `json:"rateMultiplier"`
	Status           string  `json:"status"`
	GroupStatus      string  `json:"groupStatus,omitempty"`
	ModelCount       int     `json:"modelCount"`
	Source           string  `json:"source,omitempty"`
	IsCurrent        bool    `json:"isCurrent"`
}

type subscriptionGroupEntitlement struct {
	ExternalGroupID     int64                   `json:"externalGroupId"`
	Name                string                  `json:"name"`
	Description         string                  `json:"description,omitempty"`
	Platform            string                  `json:"platform"`
	BillingKind         string                  `json:"billingKind"`
	SubscriptionType    string                  `json:"subscriptionType"`
	RateMultiplier      float64                 `json:"rateMultiplier"`
	Status              string                  `json:"status"`
	GroupStatus         string                  `json:"groupStatus,omitempty"`
	ModelCount          int                     `json:"modelCount"`
	Source              string                  `json:"source,omitempty"`
	IsCurrent           bool                    `json:"isCurrent"`
	SubscriptionID      *int64                  `json:"subscriptionId,omitempty"`
	StartsAt            *time.Time              `json:"startsAt,omitempty"`
	ExpiresAt           *time.Time              `json:"expiresAt,omitempty"`
	Remaining           float64                 `json:"remaining"`
	Unit                string                  `json:"unit"`
	Unlimited           bool                    `json:"unlimited"`
	ConstrainingWindow  string                  `json:"constrainingWindow,omitempty"`
	DepletedWindow      string                  `json:"depletedWindow,omitempty"`
	Daily               *entitlementQuotaWindow `json:"daily,omitempty"`
	Weekly              *entitlementQuotaWindow `json:"weekly,omitempty"`
	Monthly             *entitlementQuotaWindow `json:"monthly,omitempty"`
	DefaultValidityDays int                     `json:"defaultValidityDays"`
}

type entitlementQuotaWindow struct {
	Limit       float64    `json:"limit"`
	Used        float64    `json:"used"`
	Remaining   float64    `json:"remaining"`
	Unit        string     `json:"unit"`
	WindowStart *time.Time `json:"windowStart,omitempty"`
}

// GatewayEntitlements returns Sub2API-backed wallet balance and subscription quota state.
func (h *Handler) GatewayEntitlements(c *gin.Context) {
	user, ok := currentUser(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	ctx := c.Request.Context()
	cacheKey := gatewayEntitlementsCacheKey(user.ID)
	staleKey := gatewayEntitlementsStaleCacheKey(user.ID)
	if !truthy(c.Query("refresh")) {
		if cached, ok := h.gatewayEntitlementsFromCache(ctx, cacheKey); ok {
			c.JSON(http.StatusOK, cached)
			return
		}
	}

	value, err, _ := h.entitlementOnce.Do(cacheKey, func() (any, error) {
		return h.loadGatewayEntitlements(ctx, user)
	})
	if err != nil {
		if cached, ok := h.gatewayEntitlementsFromCache(ctx, staleKey); ok {
			cached.Stale = true
			c.JSON(http.StatusOK, cached)
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "gateway_entitlements_unavailable"})
		return
	}
	resp := value.(*gatewayEntitlementsResponse)

	h.cacheGatewayEntitlements(ctx, cacheKey, resp, gatewayEntitlementsTTL(user.ID))
	h.cacheGatewayEntitlements(ctx, staleKey, resp, gatewayEntitlementsStaleCacheTTL)
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) loadGatewayEntitlements(ctx context.Context, user Principal) (*gatewayEntitlementsResponse, error) {
	now := time.Now().UTC()
	resp := &gatewayEntitlementsResponse{
		Wallet: gatewayEntitlementWallet{
			Source: "sub2api_user_balance",
			Scope:  "user",
			Unit:   "USD",
			Status: "gateway_unlinked",
		},
		BalanceGroups:      []balanceGroupEntitlement{},
		SubscriptionGroups: []subscriptionGroupEntitlement{},
		UpdatedAt:          now,
	}

	gateway, err := h.gatewaySync.GatewayAccount(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("gateway account: %w", err)
	}
	if gateway == nil || gateway.ExternalUserID <= 0 {
		return resp, nil
	}
	resp.ExternalUserID = gateway.ExternalUserID

	groups, err := h.userGatewayGroups(ctx, user.ID, gateway.DefaultGroupID)
	if err != nil {
		return nil, fmt.Errorf("gateway groups: %w", err)
	}

	settings, err := h.gatewaySync.LoadSub2APISettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("sub2api settings: %w", err)
	}
	sub2 := h.gatewaySync.NewSub2APIClient(settings)

	sub2User, err := sub2.GetUser(ctx, gateway.ExternalUserID)
	if err != nil {
		return nil, fmt.Errorf("sub2api user: %w", err)
	}
	resp.Wallet.Remaining = sub2User.Balance
	resp.Wallet.Status = walletStatus(sub2User)

	subscriptions, _, err := sub2.ListSubscriptions(ctx, sub2api.SubscriptionListFilter{
		Page:     1,
		PageSize: 100,
		UserID:   gateway.ExternalUserID,
		Status:   "active",
	})
	if err != nil {
		return nil, fmt.Errorf("sub2api subscriptions: %w", err)
	}

	groups = mergeSubscriptionGroups(groups, subscriptions, gateway.DefaultGroupID)
	subscriptionsByGroup := activeSubscriptionsByGroup(subscriptions)
	slices.SortFunc(groups, func(a, b gatewayGroupSummary) int {
		if a.ExternalGroupID == gateway.DefaultGroupID && b.ExternalGroupID != gateway.DefaultGroupID {
			return -1
		}
		if b.ExternalGroupID == gateway.DefaultGroupID && a.ExternalGroupID != gateway.DefaultGroupID {
			return 1
		}
		if a.ExternalGroupID < b.ExternalGroupID {
			return -1
		}
		if a.ExternalGroupID > b.ExternalGroupID {
			return 1
		}
		return 0
	})

	for _, group := range groups {
		sub := subscriptionsByGroup[group.ExternalGroupID]
		subscriptionType := entitlementSubscriptionType(group, sub)
		if subscriptionType == "subscription" {
			resp.SubscriptionGroups = append(resp.SubscriptionGroups, buildSubscriptionGroupEntitlement(group, sub, now))
			continue
		}
		resp.BalanceGroups = append(resp.BalanceGroups, buildBalanceGroupEntitlement(group, sub2User))
	}

	return resp, nil
}

func buildBalanceGroupEntitlement(group gatewayGroupSummary, user *sub2api.User) balanceGroupEntitlement {
	return balanceGroupEntitlement{
		ExternalGroupID:  group.ExternalGroupID,
		Name:             group.Name,
		Description:      group.Description,
		Platform:         group.Platform,
		BillingKind:      "balance",
		SubscriptionType: "standard",
		BalanceScope:     "user",
		Remaining:        user.Balance,
		Unit:             "USD",
		RateMultiplier:   group.RateMultiplier,
		Status:           balanceGroupStatus(group, user),
		GroupStatus:      group.Status,
		ModelCount:       group.ModelCount,
		Source:           group.Source,
		IsCurrent:        group.IsCurrent,
	}
}

func buildSubscriptionGroupEntitlement(group gatewayGroupSummary, sub *sub2api.AdminSubscription, now time.Time) subscriptionGroupEntitlement {
	item := subscriptionGroupEntitlement{
		ExternalGroupID:     group.ExternalGroupID,
		Name:                group.Name,
		Description:         group.Description,
		Platform:            group.Platform,
		BillingKind:         "subscription",
		SubscriptionType:    "subscription",
		RateMultiplier:      group.RateMultiplier,
		Status:              "not_subscribed",
		GroupStatus:         group.Status,
		ModelCount:          group.ModelCount,
		Source:              group.Source,
		IsCurrent:           group.IsCurrent,
		Unit:                "USD",
		DefaultValidityDays: group.DefaultValidityDays,
	}
	if sub == nil {
		return item
	}

	item.SubscriptionID = &sub.ID
	item.StartsAt = &sub.StartsAt
	item.ExpiresAt = &sub.ExpiresAt
	item.Source = appendSource(item.Source, "sub2api_subscription")
	item.Daily = buildQuotaWindow(subscriptionDailyLimit(group, sub), sub.DailyUsageUSD, sub.DailyWindowStart)
	item.Weekly = buildQuotaWindow(subscriptionWeeklyLimit(group, sub), sub.WeeklyUsageUSD, sub.WeeklyWindowStart)
	item.Monthly = buildQuotaWindow(subscriptionMonthlyLimit(group, sub), sub.MonthlyUsageUSD, sub.MonthlyWindowStart)
	item.Status, item.Remaining, item.Unlimited, item.ConstrainingWindow, item.DepletedWindow = subscriptionEntitlementStatus(group, sub, now, item.Daily, item.Weekly, item.Monthly)
	return item
}

func mergeSubscriptionGroups(groups []gatewayGroupSummary, subscriptions []sub2api.AdminSubscription, currentGroupID int64) []gatewayGroupSummary {
	seen := map[int64]bool{}
	for i := range groups {
		seen[groups[i].ExternalGroupID] = true
	}
	for i := range subscriptions {
		sub := &subscriptions[i]
		if sub.GroupID <= 0 || seen[sub.GroupID] || sub.Group == nil {
			continue
		}
		if normalizeSubscriptionType(sub.Group.SubscriptionType) != "subscription" {
			continue
		}
		groups = append(groups, gatewayGroupFromSub2(sub.Group, currentGroupID))
		seen[sub.GroupID] = true
	}
	return groups
}

func gatewayGroupFromSub2(group *sub2api.AdminGroup, currentGroupID int64) gatewayGroupSummary {
	return gatewayGroupSummary{
		ExternalGroupID:     group.ID,
		Name:                group.Name,
		Description:         group.Description,
		Platform:            group.Platform,
		SubscriptionType:    group.SubscriptionType,
		RateMultiplier:      defaultRateMultiplier(group.RateMultiplier),
		DailyLimitUSD:       group.DailyLimitUSD,
		WeeklyLimitUSD:      group.WeeklyLimitUSD,
		MonthlyLimitUSD:     group.MonthlyLimitUSD,
		DefaultValidityDays: group.DefaultValidityDays,
		RPMLimit:            group.RPMLimit,
		Status:              group.Status,
		Source:              "sub2api_subscription",
		IsCurrent:           group.ID == currentGroupID,
	}
}

func activeSubscriptionsByGroup(subscriptions []sub2api.AdminSubscription) map[int64]*sub2api.AdminSubscription {
	out := map[int64]*sub2api.AdminSubscription{}
	for i := range subscriptions {
		sub := &subscriptions[i]
		if sub.GroupID <= 0 {
			continue
		}
		existing := out[sub.GroupID]
		if existing == nil || sub.ExpiresAt.After(existing.ExpiresAt) {
			out[sub.GroupID] = sub
		}
	}
	return out
}

func entitlementSubscriptionType(group gatewayGroupSummary, sub *sub2api.AdminSubscription) string {
	if sub != nil && sub.Group != nil {
		return normalizeSubscriptionType(sub.Group.SubscriptionType)
	}
	return normalizeSubscriptionType(group.SubscriptionType)
}

func normalizeSubscriptionType(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "subscription") {
		return "subscription"
	}
	return "standard"
}

func walletStatus(user *sub2api.User) string {
	if !activeStatus(user.Status) {
		return "inactive_user"
	}
	if user.Balance <= 0 {
		return "insufficient_balance"
	}
	return "active"
}

func balanceGroupStatus(group gatewayGroupSummary, user *sub2api.User) string {
	if !activeStatus(group.Status) {
		return "inactive_group"
	}
	return walletStatus(user)
}

func subscriptionEntitlementStatus(group gatewayGroupSummary, sub *sub2api.AdminSubscription, now time.Time, windows ...*entitlementQuotaWindow) (string, float64, bool, string, string) {
	if !activeStatus(group.Status) {
		return "inactive_group", 0, false, "", ""
	}
	if !strings.EqualFold(strings.TrimSpace(sub.Status), "active") {
		return strings.ToLower(strings.TrimSpace(sub.Status)), 0, false, "", ""
	}
	if !sub.ExpiresAt.IsZero() && !sub.ExpiresAt.After(now) {
		return "expired", 0, false, "", ""
	}

	var remaining float64
	var constrainingWindow string
	var depletedWindow string
	limited := false
	names := []string{"daily", "weekly", "monthly"}
	for i, window := range windows {
		if window == nil {
			continue
		}
		if !limited || window.Remaining < remaining {
			remaining = window.Remaining
			constrainingWindow = names[i]
		}
		if window.Remaining <= 0 && depletedWindow == "" {
			depletedWindow = names[i]
		}
		limited = true
	}
	if !limited {
		return "active", 0, true, "", ""
	}
	if depletedWindow != "" {
		return "quota_exhausted", remaining, false, constrainingWindow, depletedWindow
	}
	return "active", remaining, false, constrainingWindow, ""
}

func buildQuotaWindow(limit *float64, used float64, windowStart *time.Time) *entitlementQuotaWindow {
	if limit == nil || *limit <= 0 {
		return nil
	}
	remaining := *limit - used
	if remaining < 0 {
		remaining = 0
	}
	return &entitlementQuotaWindow{
		Limit:       *limit,
		Used:        used,
		Remaining:   remaining,
		Unit:        "USD",
		WindowStart: windowStart,
	}
}

func subscriptionDailyLimit(group gatewayGroupSummary, sub *sub2api.AdminSubscription) *float64 {
	if sub != nil && sub.Group != nil && sub.Group.DailyLimitUSD != nil {
		return sub.Group.DailyLimitUSD
	}
	return group.DailyLimitUSD
}

func subscriptionWeeklyLimit(group gatewayGroupSummary, sub *sub2api.AdminSubscription) *float64 {
	if sub != nil && sub.Group != nil && sub.Group.WeeklyLimitUSD != nil {
		return sub.Group.WeeklyLimitUSD
	}
	return group.WeeklyLimitUSD
}

func subscriptionMonthlyLimit(group gatewayGroupSummary, sub *sub2api.AdminSubscription) *float64 {
	if sub != nil && sub.Group != nil && sub.Group.MonthlyLimitUSD != nil {
		return sub.Group.MonthlyLimitUSD
	}
	return group.MonthlyLimitUSD
}

func activeStatus(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" || value == "active" || value == "unknown"
}

func appendSource(current string, source string) string {
	current = strings.TrimSpace(current)
	if current == "" {
		return source
	}
	for _, part := range strings.Split(current, ",") {
		if strings.TrimSpace(part) == source {
			return current
		}
	}
	return current + ", " + source
}

func defaultRateMultiplier(value float64) float64 {
	if value <= 0 {
		return 1
	}
	return value
}

func gatewayEntitlementsCacheKey(userID int64) string {
	return fmt.Sprintf("brevyn:gateway-entitlements:%d", userID)
}

func gatewayEntitlementsStaleCacheKey(userID int64) string {
	return fmt.Sprintf("brevyn:gateway-entitlements:stale:%d", userID)
}

func gatewayEntitlementsTTL(userID int64) time.Duration {
	return gatewayEntitlementsCacheTTL + time.Duration(userID%15)*time.Second
}

func (h *Handler) gatewayEntitlementsFromCache(ctx context.Context, key string) (*gatewayEntitlementsResponse, bool) {
	if h.redis == nil {
		return nil, false
	}
	data, err := h.redis.Get(ctx, key).Bytes()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			return nil, false
		}
		return nil, false
	}
	var resp gatewayEntitlementsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, false
	}
	return &resp, true
}

func (h *Handler) cacheGatewayEntitlements(ctx context.Context, key string, resp *gatewayEntitlementsResponse, ttl time.Duration) {
	if h.redis == nil || resp == nil {
		return
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_ = h.redis.Set(ctx, key, data, ttl).Err()
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}
