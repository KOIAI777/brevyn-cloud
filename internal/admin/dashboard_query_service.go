package admin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DashboardQueryService struct {
	postgres *pgxpool.Pool
	gateway  *GatewaySettingsService
}

func NewDashboardQueryService(postgres *pgxpool.Pool, gateway *GatewaySettingsService) *DashboardQueryService {
	return &DashboardQueryService{postgres: postgres, gateway: gateway}
}

func (s *DashboardQueryService) OverviewSummary(ctx context.Context) (OverviewSummary, error) {
	var summary OverviewSummary
	err := s.postgres.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM users WHERE status = 'active'),
			(SELECT count(*) FROM users WHERE created_at >= date_trunc('day', now())),
			(SELECT count(*) FROM users),
			coalesce((SELECT sum(amount) FROM wallet_transactions), 0),
			(SELECT count(*) FROM gateway_api_keys WHERE status = 'active'),
			(SELECT count(*) FROM gateway_api_keys WHERE status <> 'active' OR encrypted_api_key = ''),
			(SELECT count(*) FROM redeem_redemptions WHERE created_at >= date_trunc('day', now())),
			(SELECT count(*) FROM redeem_redemptions WHERE status = 'gateway_failed' AND created_at >= date_trunc('day', now()))
	`).Scan(
		&summary.ActiveUsers,
		&summary.UsersToday,
		&summary.TotalUsers,
		&summary.WalletBalanceUSD,
		&summary.ActiveKeys,
		&summary.ReviewKeys,
		&summary.RedemptionsToday,
		&summary.GatewayFailedToday,
	)
	summary.UsageStatus = "not_synced"
	if stats, usageErr := s.loadSub2APIUsageToday(ctx); usageErr == nil {
		summary.UsageStatus = "synced"
		summary.RequestCountToday = stats.TotalRequests
		summary.CostTodayUSD = stats.TotalCost
	} else {
		summary.UsageStatus = "sync_error"
	}
	return summary, err
}

func (s *DashboardQueryService) UsageSummary(ctx context.Context) (UsageSummary, error) {
	var summary UsageSummary
	summary.Usage.Status = "not_synced"
	summary.Usage.Source = "sub2api_usage_sync_pending"

	err := s.postgres.QueryRow(ctx, `
		SELECT
			coalesce((SELECT sum(amount) FROM wallet_transactions), 0),
			coalesce((SELECT sum(amount) FROM wallet_transactions WHERE amount > 0 AND created_at >= date_trunc('day', now())), 0),
			coalesce((SELECT sum(amount) FROM wallet_transactions WHERE amount > 0), 0),
			coalesce((SELECT sum(value) FROM redeem_redemptions WHERE kind = 'balance' AND created_at >= date_trunc('day', now())), 0),
			coalesce((SELECT sum(value) FROM redeem_redemptions WHERE kind = 'balance'), 0),
			(SELECT count(*) FROM redeem_redemptions WHERE created_at >= date_trunc('day', now())),
			(SELECT count(*) FROM redeem_redemptions WHERE kind = 'subscription' AND created_at >= date_trunc('day', now())),
			(SELECT count(*) FROM redeem_redemptions WHERE status = 'gateway_failed' AND created_at >= date_trunc('day', now()))
	`).Scan(
		&summary.Ledger.WalletBalanceUSD,
		&summary.Ledger.WalletCreditsTodayUSD,
		&summary.Ledger.WalletCreditsTotalUSD,
		&summary.Ledger.BalanceRedeemedTodayUSD,
		&summary.Ledger.BalanceRedeemedTotalUSD,
		&summary.Ledger.RedemptionCountToday,
		&summary.Ledger.SubscriptionCountToday,
		&summary.Ledger.GatewayFailedToday,
	)
	if err != nil {
		return UsageSummary{}, err
	}

	if stats, err := s.loadSub2APIUsageToday(ctx); err == nil {
		applySub2APIUsage(&summary, stats)
	} else {
		summary.Usage.Status = "sync_error"
		summary.Usage.Source = "sub2api_admin_usage_stats_error"
	}
	return summary, nil
}

func (s *DashboardQueryService) ListModels(ctx context.Context) ([]ModelCatalogItem, error) {
	var syncedModelCount int
	if err := s.postgres.QueryRow(ctx, `
		SELECT count(*) FROM gateway_group_models WHERE provider = 'sub2api' AND status = 'active'
	`).Scan(&syncedModelCount); err != nil {
		return nil, err
	}

	query := `
		SELECT model_id, display_name, provider_family, capabilities_json,
			public_visible, supports_streaming, status, updated_at
		FROM model_catalog
		ORDER BY public_visible DESC, status ASC, id ASC
	`
	if syncedModelCount > 0 {
		query = `
			SELECT DISTINCT ON (model_id)
				model_id,
				display_name,
				provider_family,
				capabilities_json,
				true AS public_visible,
				true AS supports_streaming,
				status,
				updated_at
			FROM gateway_group_models
			WHERE provider = 'sub2api' AND status = 'active'
			ORDER BY model_id ASC, updated_at DESC
		`
	}

	rows, err := s.postgres.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ModelCatalogItem{}
	for rows.Next() {
		var item ModelCatalogItem
		var capabilitiesJSON string
		if err := rows.Scan(&item.ID, &item.DisplayName, &item.ProviderFamily, &capabilitiesJSON,
			&item.PublicVisible, &item.SupportsStreaming, &item.Status, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(capabilitiesJSON), &item.Capabilities); err != nil {
			item.Capabilities = []string{}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *DashboardQueryService) loadSub2APIUsageToday(ctx context.Context) (*sub2api.UsageStats, error) {
	settings, err := s.gateway.Load(ctx)
	if err != nil {
		return nil, err
	}
	usageCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.gateway.NewSub2APIClient(settings).AdminUsageStats(usageCtx, "today", "Asia/Shanghai")
}

func applySub2APIUsage(summary *UsageSummary, stats *sub2api.UsageStats) {
	summary.Usage.Status = "synced"
	summary.Usage.Source = "sub2api_admin_usage_stats"
	summary.Usage.RequestCountToday = stats.TotalRequests
	summary.Usage.InputTokensToday = stats.TotalInputTokens
	summary.Usage.OutputTokensToday = stats.TotalOutputTokens
	summary.Usage.CostTodayUSD = stats.TotalCost
	summary.Usage.ActualCostUSD = stats.TotalActualCost
}
