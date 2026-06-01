package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
)

type SubscriptionService struct {
	gateway *GatewaySettingsService
}

type SubscriptionListFilters struct {
	ExternalUserID  int64
	ExternalGroupID int64
	Status          string
	Platform        string
	SortBy          string
	SortOrder       string
	Limit           int
	Offset          int
}

type AdminSubscriptionItem struct {
	ID                 int64                       `json:"id"`
	UserID             int64                       `json:"userId"`
	GroupID            int64                       `json:"groupId"`
	StartsAt           time.Time                   `json:"startsAt"`
	ExpiresAt          time.Time                   `json:"expiresAt"`
	Status             string                      `json:"status"`
	DailyWindowStart   *time.Time                  `json:"dailyWindowStart"`
	WeeklyWindowStart  *time.Time                  `json:"weeklyWindowStart"`
	MonthlyWindowStart *time.Time                  `json:"monthlyWindowStart"`
	DailyUsageUSD      float64                     `json:"dailyUsageUsd"`
	WeeklyUsageUSD     float64                     `json:"weeklyUsageUsd"`
	MonthlyUsageUSD    float64                     `json:"monthlyUsageUsd"`
	CreatedAt          time.Time                   `json:"createdAt"`
	UpdatedAt          time.Time                   `json:"updatedAt"`
	User               *AdminSubscriptionUserItem  `json:"user,omitempty"`
	Group              *AdminSubscriptionGroupItem `json:"group,omitempty"`
	AssignedBy         *int64                      `json:"assignedBy"`
	AssignedAt         time.Time                   `json:"assignedAt"`
	Notes              string                      `json:"notes"`
	AssignedByUser     *AdminSubscriptionUserItem  `json:"assignedByUser,omitempty"`
}

type AdminSubscriptionUserItem struct {
	ID            int64      `json:"id"`
	Email         string     `json:"email"`
	Username      string     `json:"username"`
	Role          string     `json:"role"`
	Balance       float64    `json:"balance"`
	Concurrency   int        `json:"concurrency"`
	RPMLimit      int        `json:"rpmLimit"`
	Status        string     `json:"status"`
	AllowedGroups []int64    `json:"allowedGroups"`
	LastActiveAt  *time.Time `json:"lastActiveAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

type AdminSubscriptionGroupItem struct {
	ID                   int64    `json:"id"`
	Name                 string   `json:"name"`
	Description          string   `json:"description"`
	Platform             string   `json:"platform"`
	SubscriptionType     string   `json:"subscriptionType"`
	DailyLimitUSD        *float64 `json:"dailyLimitUsd"`
	WeeklyLimitUSD       *float64 `json:"weeklyLimitUsd"`
	MonthlyLimitUSD      *float64 `json:"monthlyLimitUsd"`
	RPMLimit             int      `json:"rpmLimit"`
	RateMultiplier       float64  `json:"rateMultiplier"`
	IsExclusive          bool     `json:"isExclusive"`
	Status               string   `json:"status"`
	AllowImageGeneration bool     `json:"allowImageGeneration"`
	ClaudeCodeOnly       bool     `json:"claudeCodeOnly"`
}

func NewSubscriptionService(gateway *GatewaySettingsService) *SubscriptionService {
	return &SubscriptionService{gateway: gateway}
}

func (s *SubscriptionService) List(ctx context.Context, filters SubscriptionListFilters) ([]AdminSubscriptionItem, int64, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, 0, err
	}
	if filters.Limit <= 0 {
		filters.Limit = 50
	}
	page := filters.Offset/filters.Limit + 1
	items, total, err := client.ListSubscriptions(ctx, sub2api.SubscriptionListFilter{
		Page:      page,
		PageSize:  filters.Limit,
		UserID:    filters.ExternalUserID,
		GroupID:   filters.ExternalGroupID,
		Status:    normalizedSubscriptionStatus(filters.Status),
		Platform:  strings.TrimSpace(filters.Platform),
		SortBy:    normalizedSubscriptionSortBy(filters.SortBy),
		SortOrder: normalizedSortOrder(filters.SortOrder),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("sub2api_subscription_fetch_failed: %w", err)
	}
	return mapAdminSubscriptions(items), total, nil
}

func (s *SubscriptionService) Assign(ctx context.Context, input sub2api.AssignSubscriptionRequest, idempotencyKey string) (*AdminSubscriptionItem, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	item, err := client.AssignSubscriptionDetail(ctx, input, subscriptionIdempotencyKey("assign", input.UserID, idempotencyKey))
	if err != nil {
		return nil, fmt.Errorf("sub2api_subscription_assign_failed: %w", err)
	}
	out := mapAdminSubscription(*item)
	return &out, nil
}

func (s *SubscriptionService) Extend(ctx context.Context, subscriptionID int64, days int) (*AdminSubscriptionItem, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	item, err := client.ExtendSubscription(ctx, subscriptionID, sub2api.ExtendSubscriptionRequest{Days: days}, subscriptionIdempotencyKey("extend", subscriptionID))
	if err != nil {
		return nil, fmt.Errorf("sub2api_subscription_extend_failed: %w", err)
	}
	out := mapAdminSubscription(*item)
	return &out, nil
}

func (s *SubscriptionService) ResetQuota(ctx context.Context, subscriptionID int64, daily, weekly, monthly bool) (*AdminSubscriptionItem, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	item, err := client.ResetSubscriptionQuota(ctx, subscriptionID, sub2api.ResetSubscriptionQuotaRequest{
		Daily:   daily,
		Weekly:  weekly,
		Monthly: monthly,
	}, subscriptionIdempotencyKey("reset-quota", subscriptionID))
	if err != nil {
		return nil, fmt.Errorf("sub2api_subscription_reset_quota_failed: %w", err)
	}
	out := mapAdminSubscription(*item)
	return &out, nil
}

func (s *SubscriptionService) Revoke(ctx context.Context, subscriptionID int64) error {
	client, err := s.client(ctx)
	if err != nil {
		return err
	}
	if err := client.RevokeSubscription(ctx, subscriptionID, subscriptionIdempotencyKey("revoke", subscriptionID)); err != nil {
		return fmt.Errorf("sub2api_subscription_revoke_failed: %w", err)
	}
	return nil
}

func (s *SubscriptionService) client(ctx context.Context) (*sub2api.Client, error) {
	settings, err := s.gateway.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("settings_load_failed: %w", err)
	}
	return s.gateway.NewSub2APIClient(settings), nil
}

func mapAdminSubscriptions(items []sub2api.AdminSubscription) []AdminSubscriptionItem {
	out := make([]AdminSubscriptionItem, 0, len(items))
	for _, item := range items {
		out = append(out, mapAdminSubscription(item))
	}
	return out
}

func mapAdminSubscription(item sub2api.AdminSubscription) AdminSubscriptionItem {
	return AdminSubscriptionItem{
		ID:                 item.ID,
		UserID:             item.UserID,
		GroupID:            item.GroupID,
		StartsAt:           item.StartsAt,
		ExpiresAt:          item.ExpiresAt,
		Status:             item.Status,
		DailyWindowStart:   item.DailyWindowStart,
		WeeklyWindowStart:  item.WeeklyWindowStart,
		MonthlyWindowStart: item.MonthlyWindowStart,
		DailyUsageUSD:      item.DailyUsageUSD,
		WeeklyUsageUSD:     item.WeeklyUsageUSD,
		MonthlyUsageUSD:    item.MonthlyUsageUSD,
		CreatedAt:          item.CreatedAt,
		UpdatedAt:          item.UpdatedAt,
		User:               mapSubscriptionUser(item.User),
		Group:              mapSubscriptionGroup(item.Group),
		AssignedBy:         item.AssignedBy,
		AssignedAt:         item.AssignedAt,
		Notes:              item.Notes,
		AssignedByUser:     mapSubscriptionUser(item.AssignedByUser),
	}
}

func mapSubscriptionUser(user *sub2api.User) *AdminSubscriptionUserItem {
	if user == nil {
		return nil
	}
	return &AdminSubscriptionUserItem{
		ID:            user.ID,
		Email:         user.Email,
		Username:      user.Username,
		Role:          user.Role,
		Balance:       user.Balance,
		Concurrency:   user.Concurrency,
		RPMLimit:      user.RPMLimit,
		Status:        user.Status,
		AllowedGroups: user.AllowedGroups,
		LastActiveAt:  user.LastActiveAt,
		CreatedAt:     user.CreatedAt,
		UpdatedAt:     user.UpdatedAt,
	}
}

func mapSubscriptionGroup(group *sub2api.AdminGroup) *AdminSubscriptionGroupItem {
	if group == nil {
		return nil
	}
	return &AdminSubscriptionGroupItem{
		ID:                   group.ID,
		Name:                 group.Name,
		Description:          group.Description,
		Platform:             group.Platform,
		SubscriptionType:     group.SubscriptionType,
		DailyLimitUSD:        group.DailyLimitUSD,
		WeeklyLimitUSD:       group.WeeklyLimitUSD,
		MonthlyLimitUSD:      group.MonthlyLimitUSD,
		RPMLimit:             group.RPMLimit,
		RateMultiplier:       group.RateMultiplier,
		IsExclusive:          group.IsExclusive,
		Status:               group.Status,
		AllowImageGeneration: group.AllowImageGeneration,
		ClaudeCodeOnly:       group.ClaudeCodeOnly,
	}
}

func normalizedSubscriptionStatus(value string) string {
	value = strings.TrimSpace(value)
	if value == "all" {
		return ""
	}
	return value
}

func normalizedSubscriptionSortBy(value string) string {
	switch strings.TrimSpace(value) {
	case "id", "user_id", "group_id", "starts_at", "expires_at", "status", "created_at", "updated_at", "assigned_at":
		return strings.TrimSpace(value)
	default:
		return "created_at"
	}
}

func normalizedSortOrder(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "asc") {
		return "asc"
	}
	return "desc"
}

func subscriptionIdempotencyKey(action string, id int64, idempotencyKey ...string) string {
	if len(idempotencyKey) > 0 {
		key := strings.TrimSpace(idempotencyKey[0])
		if key != "" {
			sum := sha256.Sum256([]byte(key))
			return fmt.Sprintf("brevyn-admin-subscription:%s:%d:%s", action, id, hex.EncodeToString(sum[:]))
		}
	}
	return fmt.Sprintf("brevyn-admin-subscription:%s:%d:%d", action, id, time.Now().UnixNano())
}
