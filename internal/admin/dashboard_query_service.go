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
	attribution, err := s.UsageAttribution(ctx)
	if err != nil {
		return UsageSummary{}, err
	}
	summary.Attribution = attribution
	return summary, nil
}

func (s *DashboardQueryService) UsageAttribution(ctx context.Context) (UsageAttribution, error) {
	products, err := s.productUsageAttribution(ctx)
	if err != nil {
		return UsageAttribution{}, err
	}
	groups, err := s.groupUsageAttribution(ctx)
	if err != nil {
		return UsageAttribution{}, err
	}
	users, err := s.userUsageAttribution(ctx)
	if err != nil {
		return UsageAttribution{}, err
	}
	return UsageAttribution{Products: products, Groups: groups, Users: users}, nil
}

func (s *DashboardQueryService) productUsageAttribution(ctx context.Context) ([]ProductUsageAttribution, error) {
	rows, err := s.postgres.Query(ctx, `
		SELECT
			coalesce(p.public_id, ''),
			coalesce(p.sku, ''),
			coalesce(p.name, '未绑定商品'),
			coalesce(p.benefit_type, rr.kind),
			count(rr.id),
			coalesce(sum(rr.value) FILTER (WHERE rr.kind = 'balance'), 0),
			count(rr.id) FILTER (WHERE rr.kind = 'subscription'),
			coalesce(sum(coalesce(p.price_cny, 0)), 0),
			max(rr.created_at)
		FROM redeem_redemptions rr
		LEFT JOIN products p ON p.id = rr.product_id
		WHERE rr.created_at >= date_trunc('day', now())
		GROUP BY p.public_id, p.sku, p.name, p.benefit_type, rr.kind
		ORDER BY count(rr.id) DESC, coalesce(sum(coalesce(p.price_cny, 0)), 0) DESC
		LIMIT 8
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []ProductUsageAttribution{}
	for rows.Next() {
		var item ProductUsageAttribution
		if err := rows.Scan(
			&item.ProductID,
			&item.SKU,
			&item.Name,
			&item.BenefitType,
			&item.RedeemedCount,
			&item.BalanceValueUSD,
			&item.SubscriptionCount,
			&item.RevenueCNY,
			&item.LastRedeemedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *DashboardQueryService) groupUsageAttribution(ctx context.Context) ([]GroupUsageAttribution, error) {
	rows, err := s.postgres.Query(ctx, `
		SELECT
			rr.external_group_id,
			coalesce(nullif(gg.name, ''), CASE WHEN rr.external_group_id > 0 THEN 'Sub2API group #' || rr.external_group_id::text ELSE '未绑定分组' END),
			coalesce(nullif(gg.subscription_type, ''), 'unknown'),
			count(rr.id),
			coalesce(sum(rr.value) FILTER (WHERE rr.kind = 'balance'), 0),
			count(rr.id) FILTER (WHERE rr.kind = 'subscription'),
			coalesce(keys.active_key_count, 0)
		FROM redeem_redemptions rr
		LEFT JOIN gateway_groups gg ON gg.provider = 'sub2api' AND gg.external_group_id = rr.external_group_id
		LEFT JOIN LATERAL (
			SELECT count(*) AS active_key_count
			FROM gateway_api_keys gak
			WHERE gak.provider = 'sub2api'
				AND gak.external_group_id = rr.external_group_id
				AND gak.status = 'active'
		) keys ON true
		WHERE rr.created_at >= date_trunc('day', now())
		GROUP BY rr.external_group_id, gg.name, gg.subscription_type, keys.active_key_count
		ORDER BY count(rr.id) DESC, rr.external_group_id ASC
		LIMIT 8
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []GroupUsageAttribution{}
	for rows.Next() {
		var item GroupUsageAttribution
		if err := rows.Scan(
			&item.ExternalGroupID,
			&item.Name,
			&item.SubscriptionType,
			&item.RedeemedCount,
			&item.BalanceValueUSD,
			&item.SubscriptionCount,
			&item.ActiveKeyCount,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *DashboardQueryService) userUsageAttribution(ctx context.Context) ([]UserUsageAttribution, error) {
	rows, err := s.postgres.Query(ctx, `
		SELECT
			u.public_id,
			u.email,
			coalesce(wallet.balance, 0),
			count(rr.id),
			coalesce(sum(rr.value) FILTER (WHERE rr.kind = 'balance'), 0),
			count(rr.id) FILTER (WHERE rr.kind = 'subscription'),
			count(rr.id) FILTER (WHERE rr.status = 'gateway_failed'),
			max(rr.created_at)
		FROM redeem_redemptions rr
		JOIN users u ON u.id = rr.user_id
		LEFT JOIN LATERAL (
			SELECT sum(amount) AS balance
			FROM wallet_transactions wt
			WHERE wt.user_id = u.id
		) wallet ON true
		WHERE rr.created_at >= date_trunc('day', now())
		GROUP BY u.id, wallet.balance
		ORDER BY count(rr.id) DESC, coalesce(sum(rr.value) FILTER (WHERE rr.kind = 'balance'), 0) DESC
		LIMIT 8
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []UserUsageAttribution{}
	for rows.Next() {
		var item UserUsageAttribution
		if err := rows.Scan(
			&item.UserID,
			&item.Email,
			&item.WalletBalanceUSD,
			&item.RedeemedCount,
			&item.BalanceValueUSD,
			&item.SubscriptionCount,
			&item.GatewayFailedCount,
			&item.LastRedeemedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
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
