package sub2api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type ClientConfig struct {
	BaseURL       string
	AdminAPIKey   string
	AdminEmail    string
	AdminPassword string
	HTTPClient    *http.Client
}

type Client struct {
	baseURL       string
	adminAPIKey   string
	adminEmail    string
	adminPassword string
	httpClient    *http.Client
}

type RequestError struct {
	Method     string
	Path       string
	StatusCode int
	Body       string
}

func (e RequestError) Error() string {
	return fmt.Sprintf("sub2api request failed: %s %s status=%d body=%s", e.Method, e.Path, e.StatusCode, strings.TrimSpace(e.Body))
}

func (e RequestError) HTTPStatusCode() int {
	return e.StatusCode
}

type cachedAdminToken struct {
	token     string
	expiresAt time.Time
}

var adminTokenCache = struct {
	sync.Mutex
	tokens map[string]cachedAdminToken
}{
	tokens: map[string]cachedAdminToken{},
}

func NewClient(cfg ClientConfig) *Client {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:       strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		adminAPIKey:   strings.TrimSpace(cfg.AdminAPIKey),
		adminEmail:    strings.TrimSpace(cfg.AdminEmail),
		adminPassword: cfg.AdminPassword,
		httpClient:    httpClient,
	}
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

type CreateUserRequest struct {
	Email         string  `json:"email"`
	Password      string  `json:"password"`
	Username      string  `json:"username"`
	Notes         string  `json:"notes"`
	Balance       float64 `json:"balance"`
	Concurrency   int     `json:"concurrency"`
	RPMLimit      int     `json:"rpm_limit"`
	AllowedGroups []int64 `json:"allowed_groups"`
}

type UpdateUserRequest struct {
	Email         string   `json:"email,omitempty"`
	Password      string   `json:"password,omitempty"`
	Username      *string  `json:"username,omitempty"`
	Notes         *string  `json:"notes,omitempty"`
	Balance       *float64 `json:"balance,omitempty"`
	Concurrency   *int     `json:"concurrency,omitempty"`
	RPMLimit      *int     `json:"rpm_limit,omitempty"`
	Status        string   `json:"status,omitempty"`
	AllowedGroups *[]int64 `json:"allowed_groups,omitempty"`
}

type CreateAPIKeyRequest struct {
	Name          string   `json:"name"`
	GroupID       int64    `json:"-"`
	Quota         float64  `json:"quota"`
	ExpiresInDays *int     `json:"expires_in_days,omitempty"`
	RateLimit5h   float64  `json:"rate_limit_5h"`
	RateLimit1d   float64  `json:"rate_limit_1d"`
	RateLimit7d   float64  `json:"rate_limit_7d"`
	IPWhitelist   []string `json:"ip_whitelist,omitempty"`
	IPBlacklist   []string `json:"ip_blacklist,omitempty"`
}

type UpdateAPIKeyRequest struct {
	Status *string `json:"status,omitempty"`
}

type BalanceRequest struct {
	Balance   float64 `json:"balance"`
	Operation string  `json:"operation"`
	Notes     string  `json:"notes"`
}

type AssignSubscriptionRequest struct {
	UserID       int64  `json:"user_id"`
	GroupID      int64  `json:"group_id"`
	ValidityDays int    `json:"validity_days"`
	Notes        string `json:"notes"`
}

type SubscriptionListFilter struct {
	Page      int
	PageSize  int
	UserID    int64
	GroupID   int64
	Status    string
	Platform  string
	SortBy    string
	SortOrder string
}

type ExtendSubscriptionRequest struct {
	Days int `json:"days"`
}

type ResetSubscriptionQuotaRequest struct {
	Daily   bool `json:"daily"`
	Weekly  bool `json:"weekly"`
	Monthly bool `json:"monthly"`
}

type HealthResponse struct {
	Status string `json:"status"`
}

type AdminGroup struct {
	ID                              int64              `json:"id"`
	Name                            string             `json:"name"`
	Description                     string             `json:"description"`
	Platform                        string             `json:"platform"`
	RateMultiplier                  float64            `json:"rate_multiplier"`
	IsExclusive                     bool               `json:"is_exclusive"`
	Status                          string             `json:"status"`
	SubscriptionType                string             `json:"subscription_type"`
	DailyLimitUSD                   *float64           `json:"daily_limit_usd"`
	WeeklyLimitUSD                  *float64           `json:"weekly_limit_usd"`
	MonthlyLimitUSD                 *float64           `json:"monthly_limit_usd"`
	DefaultValidityDays             int                `json:"default_validity_days"`
	RPMLimit                        int                `json:"rpm_limit"`
	SortOrder                       int                `json:"sort_order"`
	AllowImageGeneration            bool               `json:"allow_image_generation"`
	ImageRateIndependent            bool               `json:"image_rate_independent"`
	ImageRateMultiplier             float64            `json:"image_rate_multiplier"`
	ImagePrice1K                    *float64           `json:"image_price_1k"`
	ImagePrice2K                    *float64           `json:"image_price_2k"`
	ImagePrice4K                    *float64           `json:"image_price_4k"`
	ClaudeCodeOnly                  bool               `json:"claude_code_only"`
	FallbackGroupID                 *int64             `json:"fallback_group_id"`
	FallbackGroupIDOnInvalidRequest *int64             `json:"fallback_group_id_on_invalid_request"`
	ModelRouting                    map[string][]int64 `json:"model_routing"`
	ModelRoutingEnabled             bool               `json:"model_routing_enabled"`
	MCPXMLInject                    bool               `json:"mcp_xml_inject"`
	SupportedModelScopes            []string           `json:"supported_model_scopes"`
	AllowMessagesDispatch           bool               `json:"allow_messages_dispatch"`
	RequireOAuthOnly                bool               `json:"require_oauth_only"`
	RequirePrivacySet               bool               `json:"require_privacy_set"`
	DefaultMappedModel              string             `json:"default_mapped_model"`
	MessagesDispatchModelConfig     map[string]any     `json:"messages_dispatch_model_config"`
}

type AdminChannel struct {
	ID                         int64                        `json:"id"`
	Name                       string                       `json:"name"`
	Description                string                       `json:"description"`
	Status                     string                       `json:"status"`
	BillingModelSource         string                       `json:"billing_model_source"`
	RestrictModels             bool                         `json:"restrict_models"`
	GroupIDs                   []int64                      `json:"group_ids"`
	ModelPricing               []ChannelModelPricing        `json:"model_pricing"`
	ModelMapping               map[string]map[string]string `json:"model_mapping"`
	ApplyPricingToAccountStats bool                         `json:"apply_pricing_to_account_stats"`
}

type AdminAccount struct {
	ID                      int64             `json:"id"`
	Name                    string            `json:"name"`
	Platform                string            `json:"platform"`
	Type                    string            `json:"type"`
	Status                  string            `json:"status"`
	Schedulable             *bool             `json:"schedulable"`
	Credentials             map[string]any    `json:"credentials"`
	GroupIDs                []int64           `json:"group_ids"`
	AccountGroups           []AdminAccountRef `json:"account_groups"`
	Concurrency             int               `json:"concurrency"`
	CurrentConcurrency      int               `json:"current_concurrency"`
	Priority                int               `json:"priority"`
	RateMultiplier          float64           `json:"rate_multiplier"`
	ErrorMessage            string            `json:"error_message"`
	LastUsedAt              *time.Time        `json:"last_used_at"`
	ExpiresAt               *int64            `json:"expires_at"`
	RateLimitedAt           *time.Time        `json:"rate_limited_at"`
	RateLimitResetAt        *time.Time        `json:"rate_limit_reset_at"`
	OverloadUntil           *time.Time        `json:"overload_until"`
	TempUnschedulableUntil  *time.Time        `json:"temp_unschedulable_until"`
	TempUnschedulableReason string            `json:"temp_unschedulable_reason"`
}

type AdminAccountRef struct {
	GroupID  int64 `json:"group_id"`
	Priority int   `json:"priority"`
}

type ChannelModelPricing struct {
	ID               int64             `json:"id"`
	Platform         string            `json:"platform"`
	Models           []string          `json:"models"`
	BillingMode      string            `json:"billing_mode"`
	InputPrice       *float64          `json:"input_price"`
	OutputPrice      *float64          `json:"output_price"`
	CacheWritePrice  *float64          `json:"cache_write_price"`
	CacheReadPrice   *float64          `json:"cache_read_price"`
	ImageOutputPrice *float64          `json:"image_output_price"`
	PerRequestPrice  *float64          `json:"per_request_price"`
	Intervals        []PricingInterval `json:"intervals"`
}

type PricingInterval struct {
	ID              int64    `json:"id,omitempty"`
	MinTokens       int      `json:"min_tokens"`
	MaxTokens       *int     `json:"max_tokens"`
	TierLabel       string   `json:"tier_label,omitempty"`
	InputPrice      *float64 `json:"input_price"`
	OutputPrice     *float64 `json:"output_price"`
	CacheWritePrice *float64 `json:"cache_write_price"`
	CacheReadPrice  *float64 `json:"cache_read_price"`
	PerRequestPrice *float64 `json:"per_request_price"`
	SortOrder       int      `json:"sort_order,omitempty"`
}

type authResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	TokenType    string `json:"token_type"`
}

type User struct {
	ID            int64      `json:"id"`
	Email         string     `json:"email"`
	Username      string     `json:"username"`
	Role          string     `json:"role"`
	Balance       float64    `json:"balance"`
	Concurrency   int        `json:"concurrency"`
	Status        string     `json:"status"`
	AllowedGroups []int64    `json:"allowed_groups"`
	RPMLimit      int        `json:"rpm_limit"`
	LastActiveAt  *time.Time `json:"last_active_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type userListResponse struct {
	Items    []User `json:"items"`
	Total    int64  `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

type channelListResponse struct {
	Items    []AdminChannel `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

type accountListResponse struct {
	Items    []AdminAccount `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

type AdminSubscription struct {
	ID                 int64       `json:"id"`
	UserID             int64       `json:"user_id"`
	GroupID            int64       `json:"group_id"`
	StartsAt           time.Time   `json:"starts_at"`
	ExpiresAt          time.Time   `json:"expires_at"`
	Status             string      `json:"status"`
	DailyWindowStart   *time.Time  `json:"daily_window_start"`
	WeeklyWindowStart  *time.Time  `json:"weekly_window_start"`
	MonthlyWindowStart *time.Time  `json:"monthly_window_start"`
	DailyUsageUSD      float64     `json:"daily_usage_usd"`
	WeeklyUsageUSD     float64     `json:"weekly_usage_usd"`
	MonthlyUsageUSD    float64     `json:"monthly_usage_usd"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
	User               *User       `json:"user,omitempty"`
	Group              *AdminGroup `json:"group,omitempty"`
	AssignedBy         *int64      `json:"assigned_by"`
	AssignedAt         time.Time   `json:"assigned_at"`
	Notes              string      `json:"notes"`
	AssignedByUser     *User       `json:"assigned_by_user,omitempty"`
}

type subscriptionListResponse struct {
	Items    []AdminSubscription `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

type APIKey struct {
	ID      int64  `json:"id"`
	UserID  int64  `json:"user_id"`
	Key     string `json:"key"`
	Name    string `json:"name"`
	GroupID *int64 `json:"group_id"`
	Status  string `json:"status"`
}

type UsageStats struct {
	TotalRequests     int64   `json:"total_requests"`
	TotalInputTokens  int64   `json:"total_input_tokens"`
	TotalOutputTokens int64   `json:"total_output_tokens"`
	TotalCacheTokens  int64   `json:"total_cache_tokens"`
	TotalTokens       int64   `json:"total_tokens"`
	TotalCost         float64 `json:"total_cost"`
	TotalActualCost   float64 `json:"total_actual_cost"`
	AverageDurationMS float64 `json:"average_duration_ms"`
}

type envelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

func (c *Client) Health(ctx context.Context) (*HealthResponse, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("sub2api base url is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, RequestError{Method: http.MethodGet, Path: "/health", StatusCode: resp.StatusCode}
	}
	var out HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil && err != io.EOF {
		return nil, err
	}
	if out.Status == "" {
		out.Status = "ok"
	}
	return &out, nil
}

func (c *Client) Login(ctx context.Context) (string, error) {
	if c.adminEmail == "" || strings.TrimSpace(c.adminPassword) == "" {
		return "", fmt.Errorf("sub2api admin email/password is not configured")
	}
	return c.loginWithPassword(ctx, c.adminEmail, c.adminPassword)
}

func (c *Client) UserLogin(ctx context.Context, email, password string) (string, error) {
	if strings.TrimSpace(email) == "" || strings.TrimSpace(password) == "" {
		return "", fmt.Errorf("sub2api user email/password is not configured")
	}
	return c.loginWithPassword(ctx, email, password)
}

func (c *Client) loginWithPassword(ctx context.Context, email, password string) (string, error) {
	out, err := c.loginWithPasswordRaw(ctx, email, password)
	if err != nil {
		return "", err
	}
	return out.AccessToken, nil
}

func (c *Client) loginWithPasswordRaw(ctx context.Context, email, password string) (authResponse, error) {
	var out envelope[authResponse]
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, nil, &out); err != nil {
		return authResponse{}, err
	}
	if out.Code != 0 {
		return authResponse{}, fmt.Errorf("sub2api login failed: %s", out.Message)
	}
	if strings.TrimSpace(out.Data.AccessToken) == "" {
		return authResponse{}, fmt.Errorf("sub2api login did not return access token")
	}
	return out.Data, nil
}

func (c *Client) ListGroups(ctx context.Context) ([]AdminGroup, error) {
	headers, err := c.adminHeaders(ctx, "")
	if err != nil {
		return nil, err
	}

	var out envelope[[]AdminGroup]
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/admin/groups/all", nil, headers, &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("sub2api list groups failed: %s", out.Message)
	}
	return out.Data, nil
}

func (c *Client) ListChannels(ctx context.Context) ([]AdminChannel, error) {
	const pageSize = 500
	page := 1
	channels := []AdminChannel{}
	for {
		path := fmt.Sprintf("/api/v1/admin/channels?page=%d&page_size=%d&sort_by=id&sort_order=asc", page, pageSize)
		var out envelope[json.RawMessage]
		if err := c.doAdminJSON(ctx, http.MethodGet, path, nil, "", &out); err != nil {
			return nil, err
		}
		if out.Code != 0 {
			return nil, fmt.Errorf("sub2api list channels failed: %s", out.Message)
		}

		var paged channelListResponse
		if err := json.Unmarshal(out.Data, &paged); err == nil && paged.Items != nil {
			channels = append(channels, paged.Items...)
			if paged.Total <= int64(len(channels)) || len(paged.Items) == 0 || paged.PageSize <= 0 {
				return channels, nil
			}
			page++
			continue
		}

		var direct []AdminChannel
		if err := json.Unmarshal(out.Data, &direct); err != nil {
			return nil, err
		}
		return direct, nil
	}
}

func (c *Client) ListAccounts(ctx context.Context) ([]AdminAccount, error) {
	const pageSize = 500
	page := 1
	accounts := []AdminAccount{}
	for {
		path := fmt.Sprintf("/api/v1/admin/accounts?page=%d&page_size=%d&sort_by=id&sort_order=asc", page, pageSize)
		var out envelope[json.RawMessage]
		if err := c.doAdminJSON(ctx, http.MethodGet, path, nil, "", &out); err != nil {
			return nil, err
		}
		if out.Code != 0 {
			return nil, fmt.Errorf("sub2api list accounts failed: %s", out.Message)
		}

		var paged accountListResponse
		if err := json.Unmarshal(out.Data, &paged); err == nil && paged.Items != nil {
			accounts = append(accounts, paged.Items...)
			if paged.Total <= int64(len(accounts)) || len(paged.Items) == 0 || paged.PageSize <= 0 {
				return accounts, nil
			}
			page++
			continue
		}

		var direct []AdminAccount
		if err := json.Unmarshal(out.Data, &direct); err != nil {
			return nil, err
		}
		return direct, nil
	}
}

func (c *Client) CreateUser(ctx context.Context, input CreateUserRequest) (*User, error) {
	var out envelope[User]
	if err := c.doAdminJSON(ctx, http.MethodPost, "/api/v1/admin/users", input, "", &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("sub2api create user failed: %s", out.Message)
	}
	return &out.Data, nil
}

func (c *Client) UpdateUser(ctx context.Context, userID int64, input UpdateUserRequest) (*User, error) {
	var out envelope[User]
	if err := c.doAdminJSON(ctx, http.MethodPut, fmt.Sprintf("/api/v1/admin/users/%d", userID), input, "", &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("sub2api update user failed: %s", out.Message)
	}
	return &out.Data, nil
}

func (c *Client) ListUsers(ctx context.Context, page, pageSize int, search string) ([]User, int64, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	path := fmt.Sprintf("/api/v1/admin/users?page=%d&page_size=%d", page, pageSize)
	if strings.TrimSpace(search) != "" {
		path += "&search=" + urlQueryEscape(strings.TrimSpace(search))
	}

	var out envelope[json.RawMessage]
	if err := c.doAdminJSON(ctx, http.MethodGet, path, nil, "", &out); err != nil {
		return nil, 0, err
	}
	if out.Code != 0 {
		return nil, 0, fmt.Errorf("sub2api list users failed: %s", out.Message)
	}

	var paged userListResponse
	if err := json.Unmarshal(out.Data, &paged); err == nil && paged.Items != nil {
		return paged.Items, paged.Total, nil
	}

	var users []User
	if err := json.Unmarshal(out.Data, &users); err != nil {
		return nil, 0, err
	}
	return users, int64(len(users)), nil
}

func (c *Client) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, fmt.Errorf("email is required")
	}
	users, _, err := c.ListUsers(ctx, 1, 20, email)
	if err != nil {
		return nil, err
	}
	for i := range users {
		if strings.EqualFold(strings.TrimSpace(users[i].Email), email) {
			return &users[i], nil
		}
	}
	return nil, fmt.Errorf("sub2api user not found")
}

func (c *Client) GetUser(ctx context.Context, userID int64) (*User, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("user id is required")
	}

	var out envelope[User]
	if err := c.doAdminJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/users/%d", userID), nil, "", &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("sub2api get user failed: %s", out.Message)
	}
	return &out.Data, nil
}

func (c *Client) UpdateUserBalance(ctx context.Context, userID int64, input BalanceRequest, idempotencyKey string) error {
	var out envelope[User]
	if err := c.doAdminJSON(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/balance", userID), input, idempotencyKey, &out); err != nil {
		return err
	}
	if out.Code != 0 {
		return fmt.Errorf("sub2api update balance failed: %s", out.Message)
	}
	return nil
}

func (c *Client) AssignSubscription(ctx context.Context, input AssignSubscriptionRequest) error {
	return c.AssignSubscriptionWithIdempotency(ctx, input, "")
}

func (c *Client) AssignSubscriptionWithIdempotency(ctx context.Context, input AssignSubscriptionRequest, idempotencyKey string) error {
	_, err := c.AssignSubscriptionDetail(ctx, input, idempotencyKey)
	return err
}

func (c *Client) AssignSubscriptionDetail(ctx context.Context, input AssignSubscriptionRequest, idempotencyKey string) (*AdminSubscription, error) {
	var out envelope[AdminSubscription]
	if err := c.doAdminJSON(ctx, http.MethodPost, "/api/v1/admin/subscriptions/assign", input, idempotencyKey, &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("sub2api assign subscription failed: %s", out.Message)
	}
	return &out.Data, nil
}

func (c *Client) ListSubscriptions(ctx context.Context, filter SubscriptionListFilter) ([]AdminSubscription, int64, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 50
	}
	values := url.Values{}
	values.Set("page", fmt.Sprintf("%d", filter.Page))
	values.Set("page_size", fmt.Sprintf("%d", filter.PageSize))
	if filter.UserID > 0 {
		values.Set("user_id", fmt.Sprintf("%d", filter.UserID))
	}
	if filter.GroupID > 0 {
		values.Set("group_id", fmt.Sprintf("%d", filter.GroupID))
	}
	if strings.TrimSpace(filter.Status) != "" {
		values.Set("status", strings.TrimSpace(filter.Status))
	}
	if strings.TrimSpace(filter.Platform) != "" {
		values.Set("platform", strings.TrimSpace(filter.Platform))
	}
	if strings.TrimSpace(filter.SortBy) != "" {
		values.Set("sort_by", strings.TrimSpace(filter.SortBy))
	}
	if strings.TrimSpace(filter.SortOrder) != "" {
		values.Set("sort_order", strings.TrimSpace(filter.SortOrder))
	}

	var out envelope[json.RawMessage]
	if err := c.doAdminJSON(ctx, http.MethodGet, "/api/v1/admin/subscriptions?"+values.Encode(), nil, "", &out); err != nil {
		return nil, 0, err
	}
	if out.Code != 0 {
		return nil, 0, fmt.Errorf("sub2api list subscriptions failed: %s", out.Message)
	}

	var paged subscriptionListResponse
	if err := json.Unmarshal(out.Data, &paged); err == nil && paged.Items != nil {
		return paged.Items, paged.Total, nil
	}

	var items []AdminSubscription
	if err := json.Unmarshal(out.Data, &items); err != nil {
		return nil, 0, err
	}
	return items, int64(len(items)), nil
}

func (c *Client) ExtendSubscription(ctx context.Context, subscriptionID int64, input ExtendSubscriptionRequest, idempotencyKey string) (*AdminSubscription, error) {
	var out envelope[AdminSubscription]
	if err := c.doAdminJSON(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/subscriptions/%d/extend", subscriptionID), input, idempotencyKey, &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("sub2api extend subscription failed: %s", out.Message)
	}
	return &out.Data, nil
}

func (c *Client) ResetSubscriptionQuota(ctx context.Context, subscriptionID int64, input ResetSubscriptionQuotaRequest, idempotencyKey string) (*AdminSubscription, error) {
	var out envelope[AdminSubscription]
	if err := c.doAdminJSON(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/subscriptions/%d/reset-quota", subscriptionID), input, idempotencyKey, &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("sub2api reset subscription quota failed: %s", out.Message)
	}
	return &out.Data, nil
}

func (c *Client) RevokeSubscription(ctx context.Context, subscriptionID int64, idempotencyKey string) error {
	var out envelope[json.RawMessage]
	if err := c.doAdminJSON(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/admin/subscriptions/%d", subscriptionID), nil, idempotencyKey, &out); err != nil {
		return err
	}
	if out.Code != 0 {
		return fmt.Errorf("sub2api revoke subscription failed: %s", out.Message)
	}
	return nil
}

func (c *Client) CreateUserAPIKey(ctx context.Context, userToken string, input CreateAPIKeyRequest, idempotencyKey string) (*APIKey, error) {
	body := map[string]any{
		"name":            input.Name,
		"quota":           input.Quota,
		"rate_limit_5h":   input.RateLimit5h,
		"rate_limit_1d":   input.RateLimit1d,
		"rate_limit_7d":   input.RateLimit7d,
		"ip_whitelist":    input.IPWhitelist,
		"ip_blacklist":    input.IPBlacklist,
		"expires_in_days": input.ExpiresInDays,
	}
	if input.GroupID > 0 {
		body["group_id"] = input.GroupID
	}
	headers := map[string]string{"Authorization": "Bearer " + strings.TrimSpace(userToken)}
	if idempotencyKey != "" {
		headers["Idempotency-Key"] = idempotencyKey
	}
	var out envelope[APIKey]
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/keys", body, headers, &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("sub2api create api key failed: %s", out.Message)
	}
	if strings.TrimSpace(out.Data.Key) == "" {
		return nil, fmt.Errorf("sub2api create api key did not return key")
	}
	return &out.Data, nil
}

func (c *Client) UpdateUserAPIKey(ctx context.Context, userToken string, keyID int64, input UpdateAPIKeyRequest) (*APIKey, error) {
	headers := map[string]string{"Authorization": "Bearer " + strings.TrimSpace(userToken)}
	var out envelope[APIKey]
	if err := c.doJSON(ctx, http.MethodPut, fmt.Sprintf("/api/v1/keys/%d", keyID), input, headers, &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("sub2api update api key failed: %s", out.Message)
	}
	return &out.Data, nil
}

func (c *Client) AdminUsageStats(ctx context.Context, period, timezone string) (*UsageStats, error) {
	values := url.Values{}
	if strings.TrimSpace(period) != "" {
		values.Set("period", strings.TrimSpace(period))
	}
	if strings.TrimSpace(timezone) != "" {
		values.Set("timezone", strings.TrimSpace(timezone))
	}
	path := "/api/v1/admin/usage/stats"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var out envelope[UsageStats]
	if err := c.doAdminJSON(ctx, http.MethodGet, path, nil, "", &out); err != nil {
		return nil, err
	}
	if out.Code != 0 {
		return nil, fmt.Errorf("sub2api usage stats failed: %s", out.Message)
	}
	return &out.Data, nil
}

func (c *Client) DoAdmin(ctx context.Context, method, path string, body any, out any) error {
	return c.doAdminJSON(ctx, method, path, body, "", out)
}

func (c *Client) doAdminJSON(ctx context.Context, method, path string, body any, idempotencyKey string, out any) error {
	headers, err := c.adminHeaders(ctx, idempotencyKey)
	if err != nil {
		return err
	}
	return c.doJSON(ctx, method, path, body, headers, out)
}

func (c *Client) adminHeaders(ctx context.Context, idempotencyKey string) (map[string]string, error) {
	headers := map[string]string{}
	if c.adminAPIKey != "" {
		headers["x-api-key"] = c.adminAPIKey
	} else if c.adminEmail != "" && strings.TrimSpace(c.adminPassword) != "" {
		token, err := c.cachedAdminToken(ctx)
		if err != nil {
			return nil, err
		}
		headers["Authorization"] = "Bearer " + token
	} else {
		return nil, fmt.Errorf("sub2api admin auth is not configured")
	}
	if idempotencyKey != "" {
		headers["Idempotency-Key"] = idempotencyKey
	}
	return headers, nil
}

func (c *Client) cachedAdminToken(ctx context.Context) (string, error) {
	key := c.adminTokenCacheKey()
	now := time.Now()
	adminTokenCache.Lock()
	defer adminTokenCache.Unlock()
	if cached, ok := adminTokenCache.tokens[key]; ok && cached.token != "" && cached.expiresAt.After(now) {
		return cached.token, nil
	}

	out, err := c.loginWithPasswordRaw(ctx, c.adminEmail, c.adminPassword)
	if err != nil {
		return "", err
	}
	ttl := time.Duration(out.ExpiresIn) * time.Second
	if ttl <= time.Minute {
		ttl = 10 * time.Minute
	} else {
		ttl -= time.Minute
	}

	adminTokenCache.tokens[key] = cachedAdminToken{
		token:     out.AccessToken,
		expiresAt: now.Add(ttl),
	}
	return out.AccessToken, nil
}

func (c *Client) adminTokenCacheKey() string {
	sum := sha256.Sum256([]byte(c.baseURL + "\x00" + strings.ToLower(c.adminEmail) + "\x00" + c.adminPassword))
	return hex.EncodeToString(sum[:])
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, headers map[string]string, out any) error {
	if c.baseURL == "" {
		return fmt.Errorf("sub2api base url is not configured")
	}

	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return RequestError{Method: method, Path: path, StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(preview))}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func urlQueryEscape(value string) string {
	replacer := strings.NewReplacer(
		" ", "%20",
		"@", "%40",
		"+", "%2B",
		"&", "%26",
		"?", "%3F",
		"=", "%3D",
		"#", "%23",
	)
	return replacer.Replace(value)
}
