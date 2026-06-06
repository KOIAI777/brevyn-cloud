package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Sub2APIModelSyncResult struct {
	Status         string                `json:"status"`
	SyncedGroups   int                   `json:"syncedGroups"`
	SyncedAccounts int                   `json:"syncedAccounts"`
	SyncedChannels int                   `json:"syncedChannels"`
	SyncedModels   int                   `json:"syncedModels"`
	TotalGroups    int                   `json:"totalGroups"`
	TotalAccounts  int                   `json:"totalAccounts"`
	TotalChannels  int                   `json:"totalChannels"`
	Groups         []Sub2APIGroupPreview `json:"groups"`
}

type sub2APIGroupSyncOutcome struct {
	previews         []Sub2APIGroupPreview
	externalGroupIDs []int64
	groupByID        map[int64]sub2api.AdminGroup
}

type gatewayModelCandidate struct {
	Platform string
	ModelID  string
	Pricing  *sub2api.ChannelModelPricing
}

type gatewayPricingIndex struct {
	Exact     map[string]sub2api.ChannelModelPricing
	Wildcards []gatewayWildcardPricing
}

type gatewayWildcardPricing struct {
	Prefix  string
	Pricing sub2api.ChannelModelPricing
}

func (s *GatewaySettingsService) SyncModels(ctx context.Context) (Sub2APIModelSyncResult, error) {
	settings, err := s.Load(ctx)
	if err != nil {
		return Sub2APIModelSyncResult{}, fmt.Errorf("settings_load_failed: %w", err)
	}
	client := s.NewSub2APIClient(settings)
	groups, err := client.ListGroups(ctx)
	if err != nil {
		return Sub2APIModelSyncResult{}, fmt.Errorf("sub2api_group_fetch_failed: %w", err)
	}
	channels, err := client.ListChannels(ctx)
	if err != nil {
		return Sub2APIModelSyncResult{}, fmt.Errorf("sub2api_channel_fetch_failed: %w", err)
	}
	accounts, err := client.ListAccounts(ctx)
	if err != nil {
		return Sub2APIModelSyncResult{}, fmt.Errorf("sub2api_account_fetch_failed: %w", err)
	}

	tx, err := s.postgres.Begin(ctx)
	if err != nil {
		return Sub2APIModelSyncResult{}, fmt.Errorf("sync_begin_failed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	groupOutcome, err := syncSub2APIGroupsInTx(ctx, tx, groups)
	if err != nil {
		return Sub2APIModelSyncResult{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE gateway_group_models
		SET status = 'inactive', updated_at = now()
		WHERE provider = 'sub2api'
	`); err != nil {
		return Sub2APIModelSyncResult{}, fmt.Errorf("model_stale_mark_failed: %w", err)
	}

	externalChannelIDs := make([]int64, 0, len(channels))
	externalAccountIDs := make([]int64, 0, len(accounts))
	syncedChannels := 0
	syncedAccounts := 0
	syncedModels := 0
	syncedModelKeys := map[string]struct{}{}

	for _, account := range accounts {
		normalized := normalizeSub2APIAccount(account)
		if normalized.ID <= 0 {
			continue
		}
		externalAccountIDs = append(externalAccountIDs, normalized.ID)
		if err := upsertGatewayUpstreamAccount(ctx, tx, normalized); err != nil {
			return Sub2APIModelSyncResult{}, err
		}
		if normalized.Status != "active" || !sub2APIAccountSchedulable(normalized) {
			continue
		}
		candidates := supportedGatewayModelsFromAccount(normalized)
		if len(candidates) == 0 {
			continue
		}
		accountSynced := false
		for _, groupID := range normalized.GroupIDs {
			group, ok := groupOutcome.groupByID[groupID]
			if !ok || group.Status != "active" || group.Platform != normalized.Platform {
				continue
			}
			for _, candidate := range candidates {
				if err := upsertGatewayGroupModel(ctx, tx, group, 0, candidate); err != nil {
					return Sub2APIModelSyncResult{}, err
				}
				if markSyncedModel(syncedModelKeys, group.ID, candidate) {
					syncedModels++
				}
				accountSynced = true
			}
		}
		if accountSynced {
			syncedAccounts++
		}
	}

	if len(externalAccountIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE gateway_upstream_accounts
			SET status = 'inactive', updated_at = now()
			WHERE provider = 'sub2api' AND NOT (external_account_id = ANY($1::bigint[]))
		`, externalAccountIDs); err != nil {
			return Sub2APIModelSyncResult{}, fmt.Errorf("upstream_account_stale_mark_failed: %w", err)
		}
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE gateway_upstream_accounts
			SET status = 'inactive', updated_at = now()
			WHERE provider = 'sub2api'
		`); err != nil {
			return Sub2APIModelSyncResult{}, fmt.Errorf("upstream_account_stale_mark_failed: %w", err)
		}
	}

	for _, channel := range channels {
		normalized := normalizeSub2APIChannel(channel)
		if normalized.ID <= 0 {
			continue
		}
		externalChannelIDs = append(externalChannelIDs, normalized.ID)
		if err := upsertGatewayChannel(ctx, tx, normalized); err != nil {
			return Sub2APIModelSyncResult{}, err
		}
		syncedChannels++
		if normalized.Status != "active" {
			continue
		}

		candidates := supportedGatewayModels(normalized)
		for _, groupID := range normalized.GroupIDs {
			group, ok := groupOutcome.groupByID[groupID]
			if !ok || group.Status != "active" {
				continue
			}
			for _, candidate := range candidates {
				if candidate.Platform != group.Platform {
					continue
				}
				if err := upsertGatewayGroupModel(ctx, tx, group, normalized.ID, candidate); err != nil {
					return Sub2APIModelSyncResult{}, err
				}
				if markSyncedModel(syncedModelKeys, group.ID, candidate) {
					syncedModels++
				}
			}
			if err := backfillGatewayGroupModelPricing(ctx, tx, group, normalized); err != nil {
				return Sub2APIModelSyncResult{}, err
			}
		}
	}

	if len(externalChannelIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE gateway_channels
			SET status = 'inactive', updated_at = now()
			WHERE provider = 'sub2api' AND NOT (external_channel_id = ANY($1::bigint[]))
		`, externalChannelIDs); err != nil {
			return Sub2APIModelSyncResult{}, fmt.Errorf("channel_stale_mark_failed: %w", err)
		}
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE gateway_channels
			SET status = 'inactive', updated_at = now()
			WHERE provider = 'sub2api'
		`); err != nil {
			return Sub2APIModelSyncResult{}, fmt.Errorf("channel_stale_mark_failed: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Sub2APIModelSyncResult{}, fmt.Errorf("sync_commit_failed: %w", err)
	}

	return Sub2APIModelSyncResult{
		Status:         "ok",
		SyncedGroups:   len(groupOutcome.previews),
		SyncedAccounts: syncedAccounts,
		SyncedChannels: syncedChannels,
		SyncedModels:   syncedModels,
		TotalGroups:    len(groups),
		TotalAccounts:  len(accounts),
		TotalChannels:  len(channels),
		Groups:         groupOutcome.previews,
	}, nil
}

func syncSub2APIGroupsInTx(ctx context.Context, tx pgx.Tx, groups []sub2api.AdminGroup) (sub2APIGroupSyncOutcome, error) {
	outcome := sub2APIGroupSyncOutcome{
		previews:         make([]Sub2APIGroupPreview, 0, len(groups)),
		externalGroupIDs: make([]int64, 0, len(groups)),
		groupByID:        make(map[int64]sub2api.AdminGroup, len(groups)),
	}
	for _, group := range groups {
		normalized := normalizeSub2APIGroup(group)
		if normalized.ID <= 0 {
			continue
		}
		outcome.externalGroupIDs = append(outcome.externalGroupIDs, normalized.ID)
		outcome.groupByID[normalized.ID] = normalized
		if err := upsertGatewayGroup(ctx, tx, normalized); err != nil {
			return outcome, err
		}
		outcome.previews = append(outcome.previews, groupPreview(normalized))
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM gateway_groups
		WHERE provider = 'sub2api' AND external_group_id = 0 AND name = 'Brevyn Official'
	`); err != nil {
		return outcome, fmt.Errorf("placeholder_group_cleanup_failed: %w", err)
	}
	if len(outcome.externalGroupIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE gateway_groups
			SET status = 'inactive', updated_at = now()
			WHERE provider = 'sub2api' AND NOT (external_group_id = ANY($1::bigint[]))
		`, outcome.externalGroupIDs); err != nil {
			return outcome, fmt.Errorf("group_stale_mark_failed: %w", err)
		}
	}
	return outcome, nil
}

func upsertGatewayGroup(ctx context.Context, tx pgx.Tx, group sub2api.AdminGroup) error {
	modelRoutingJSON, err := json.Marshal(group.ModelRouting)
	if err != nil {
		return fmt.Errorf("group_model_routing_marshal_failed: %w", err)
	}
	supportedScopesJSON, err := json.Marshal(group.SupportedModelScopes)
	if err != nil {
		return fmt.Errorf("group_supported_scopes_marshal_failed: %w", err)
	}
	messagesConfigJSON, err := json.Marshal(group.MessagesDispatchModelConfig)
	if err != nil {
		return fmt.Errorf("group_messages_config_marshal_failed: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO gateway_groups (
			public_id, provider, external_group_id, name, description, platform,
			subscription_type, rate_multiplier, is_exclusive, daily_limit_usd,
			weekly_limit_usd, monthly_limit_usd, default_validity_days,
			rpm_limit, sort_order, allow_image_generation, image_rate_independent,
			image_rate_multiplier, image_price_1k, image_price_2k, image_price_4k,
			claude_code_only, fallback_group_id, fallback_group_id_on_invalid_request,
			model_routing, model_routing_enabled, mcp_xml_inject, supported_model_scopes,
			allow_messages_dispatch, require_oauth_only, require_privacy_set,
			default_mapped_model, messages_dispatch_model_config, status
		)
		VALUES (
			$1, 'sub2api', $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12,
			$13, $14, $15, $16,
			$17, $18, $19, $20,
			$21, $22, $23,
			$24, $25, $26, $27,
			$28, $29, $30,
			$31, $32, $33
		)
		ON CONFLICT (provider, external_group_id) DO UPDATE
		SET name = excluded.name,
			description = excluded.description,
			platform = excluded.platform,
			subscription_type = excluded.subscription_type,
			rate_multiplier = excluded.rate_multiplier,
			is_exclusive = excluded.is_exclusive,
			daily_limit_usd = excluded.daily_limit_usd,
			weekly_limit_usd = excluded.weekly_limit_usd,
			monthly_limit_usd = excluded.monthly_limit_usd,
			default_validity_days = excluded.default_validity_days,
			rpm_limit = excluded.rpm_limit,
			sort_order = excluded.sort_order,
			allow_image_generation = excluded.allow_image_generation,
			image_rate_independent = excluded.image_rate_independent,
			image_rate_multiplier = excluded.image_rate_multiplier,
			image_price_1k = excluded.image_price_1k,
			image_price_2k = excluded.image_price_2k,
			image_price_4k = excluded.image_price_4k,
			claude_code_only = excluded.claude_code_only,
			fallback_group_id = excluded.fallback_group_id,
			fallback_group_id_on_invalid_request = excluded.fallback_group_id_on_invalid_request,
			model_routing = excluded.model_routing,
			model_routing_enabled = excluded.model_routing_enabled,
			mcp_xml_inject = excluded.mcp_xml_inject,
			supported_model_scopes = excluded.supported_model_scopes,
			allow_messages_dispatch = excluded.allow_messages_dispatch,
			require_oauth_only = excluded.require_oauth_only,
			require_privacy_set = excluded.require_privacy_set,
			default_mapped_model = excluded.default_mapped_model,
			messages_dispatch_model_config = excluded.messages_dispatch_model_config,
			status = excluded.status,
			updated_at = now()
	`, "gg_"+uuid.NewString(), group.ID, group.Name, group.Description, group.Platform,
		group.SubscriptionType, group.RateMultiplier, group.IsExclusive, group.DailyLimitUSD,
		group.WeeklyLimitUSD, group.MonthlyLimitUSD, group.DefaultValidityDays,
		group.RPMLimit, group.SortOrder, group.AllowImageGeneration, group.ImageRateIndependent,
		group.ImageRateMultiplier, group.ImagePrice1K, group.ImagePrice2K, group.ImagePrice4K,
		group.ClaudeCodeOnly, group.FallbackGroupID, group.FallbackGroupIDOnInvalidRequest,
		modelRoutingJSON, group.ModelRoutingEnabled, group.MCPXMLInject, supportedScopesJSON,
		group.AllowMessagesDispatch, group.RequireOAuthOnly, group.RequirePrivacySet,
		group.DefaultMappedModel, messagesConfigJSON, group.Status); err != nil {
		return fmt.Errorf("group_upsert_failed: %w", err)
	}
	return nil
}

func normalizeSub2APIChannel(channel sub2api.AdminChannel) sub2api.AdminChannel {
	channel.Name = strings.TrimSpace(channel.Name)
	if channel.Name == "" {
		channel.Name = fmt.Sprintf("sub2api-channel-%d", channel.ID)
	}
	channel.Description = strings.TrimSpace(channel.Description)
	channel.Status = strings.TrimSpace(channel.Status)
	if channel.Status == "" {
		channel.Status = "active"
	}
	channel.BillingModelSource = strings.TrimSpace(channel.BillingModelSource)
	if channel.BillingModelSource == "" {
		channel.BillingModelSource = "channel_mapped"
	}
	if channel.ModelMapping == nil {
		channel.ModelMapping = map[string]map[string]string{}
	}
	for i := range channel.ModelPricing {
		channel.ModelPricing[i] = normalizeSub2APIModelPricing(channel.ModelPricing[i])
	}
	return channel
}

func normalizeSub2APIAccount(account sub2api.AdminAccount) sub2api.AdminAccount {
	account.Name = strings.TrimSpace(account.Name)
	if account.Name == "" {
		account.Name = fmt.Sprintf("sub2api-account-%d", account.ID)
	}
	account.Platform = strings.TrimSpace(account.Platform)
	if account.Platform == "" {
		account.Platform = "anthropic"
	}
	account.Type = strings.TrimSpace(account.Type)
	account.Status = strings.TrimSpace(account.Status)
	if account.Status == "" {
		account.Status = "active"
	}
	account.ErrorMessage = strings.TrimSpace(account.ErrorMessage)
	if account.Credentials == nil {
		account.Credentials = map[string]any{}
	}
	groupIDSet := map[int64]struct{}{}
	groupIDs := make([]int64, 0, len(account.GroupIDs)+len(account.AccountGroups))
	addGroupID := func(groupID int64) {
		if groupID <= 0 {
			return
		}
		if _, exists := groupIDSet[groupID]; exists {
			return
		}
		groupIDSet[groupID] = struct{}{}
		groupIDs = append(groupIDs, groupID)
	}
	for _, groupID := range account.GroupIDs {
		addGroupID(groupID)
	}
	for _, groupRef := range account.AccountGroups {
		addGroupID(groupRef.GroupID)
	}
	account.GroupIDs = groupIDs
	return account
}

func sub2APIAccountSchedulable(account sub2api.AdminAccount) bool {
	return account.Schedulable == nil || *account.Schedulable
}

func normalizeSub2APIModelPricing(pricing sub2api.ChannelModelPricing) sub2api.ChannelModelPricing {
	pricing.Platform = strings.TrimSpace(pricing.Platform)
	if pricing.Platform == "" {
		pricing.Platform = "anthropic"
	}
	pricing.BillingMode = strings.TrimSpace(pricing.BillingMode)
	if pricing.BillingMode == "" {
		pricing.BillingMode = "token"
	}
	models := make([]string, 0, len(pricing.Models))
	for _, model := range pricing.Models {
		model = strings.TrimSpace(model)
		if model != "" {
			models = append(models, model)
		}
	}
	pricing.Models = models
	return pricing
}

func upsertGatewayChannel(ctx context.Context, tx pgx.Tx, channel sub2api.AdminChannel) error {
	groupIDsJSON, err := json.Marshal(channel.GroupIDs)
	if err != nil {
		return fmt.Errorf("channel_groups_marshal_failed: %w", err)
	}
	mappingJSON, err := json.Marshal(channel.ModelMapping)
	if err != nil {
		return fmt.Errorf("channel_mapping_marshal_failed: %w", err)
	}
	pricingJSON, err := json.Marshal(channel.ModelPricing)
	if err != nil {
		return fmt.Errorf("channel_pricing_marshal_failed: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO gateway_channels (
			public_id, provider, external_channel_id, name, description, status,
			billing_model_source, restrict_models, group_ids, model_mapping,
			model_pricing, pricing_count, last_synced_at
		)
		VALUES ($1, 'sub2api', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
		ON CONFLICT (provider, external_channel_id) DO UPDATE
		SET name = excluded.name,
			description = excluded.description,
			status = excluded.status,
			billing_model_source = excluded.billing_model_source,
			restrict_models = excluded.restrict_models,
			group_ids = excluded.group_ids,
			model_mapping = excluded.model_mapping,
			model_pricing = excluded.model_pricing,
			pricing_count = excluded.pricing_count,
			last_synced_at = now(),
			updated_at = now()
	`, "gc_"+uuid.NewString(), channel.ID, channel.Name, channel.Description, channel.Status,
		channel.BillingModelSource, channel.RestrictModels, groupIDsJSON, mappingJSON,
		pricingJSON, len(channel.ModelPricing)); err != nil {
		return fmt.Errorf("channel_upsert_failed: %w", err)
	}
	return nil
}

func upsertGatewayUpstreamAccount(ctx context.Context, tx pgx.Tx, account sub2api.AdminAccount) error {
	modelMapping := accountModelMapping(account)
	if modelMapping == nil {
		modelMapping = map[string]string{}
	}
	groupIDsJSON, err := json.Marshal(account.GroupIDs)
	if err != nil {
		return fmt.Errorf("upstream_account_groups_marshal_failed: %w", err)
	}
	modelMappingJSON, err := json.Marshal(modelMapping)
	if err != nil {
		return fmt.Errorf("upstream_account_mapping_marshal_failed: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO gateway_upstream_accounts (
			public_id, provider, external_account_id, name, platform, account_type,
			status, schedulable, concurrency, current_concurrency, priority,
			rate_multiplier, error_message, group_ids, model_mapping, mapped_model_count,
			last_used_at, expires_at, rate_limited_at, rate_limit_reset_at,
			overload_until, temp_unschedulable_until, last_synced_at
		)
		VALUES (
			$1, 'sub2api', $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15,
			$16, $17, $18, $19,
			$20, $21, now()
		)
		ON CONFLICT (provider, external_account_id) DO UPDATE
		SET name = excluded.name,
			platform = excluded.platform,
			account_type = excluded.account_type,
			status = excluded.status,
			schedulable = excluded.schedulable,
			concurrency = excluded.concurrency,
			current_concurrency = excluded.current_concurrency,
			priority = excluded.priority,
			rate_multiplier = excluded.rate_multiplier,
			error_message = excluded.error_message,
			group_ids = excluded.group_ids,
			model_mapping = excluded.model_mapping,
			mapped_model_count = excluded.mapped_model_count,
			last_used_at = excluded.last_used_at,
			expires_at = excluded.expires_at,
			rate_limited_at = excluded.rate_limited_at,
			rate_limit_reset_at = excluded.rate_limit_reset_at,
			overload_until = excluded.overload_until,
			temp_unschedulable_until = excluded.temp_unschedulable_until,
			last_synced_at = now(),
			updated_at = now()
	`, "gua_"+uuid.NewString(), account.ID, account.Name, account.Platform, account.Type,
		account.Status, sub2APIAccountSchedulable(account), account.Concurrency, account.CurrentConcurrency,
		account.Priority, account.RateMultiplier, account.ErrorMessage, groupIDsJSON, modelMappingJSON,
		len(modelMapping), account.LastUsedAt, unixSecondsToTime(account.ExpiresAt), account.RateLimitedAt,
		account.RateLimitResetAt, account.OverloadUntil, account.TempUnschedulableUntil); err != nil {
		return fmt.Errorf("upstream_account_upsert_failed: %w", err)
	}
	return nil
}

func unixSecondsToTime(value *int64) *time.Time {
	if value == nil || *value <= 0 {
		return nil
	}
	t := time.Unix(*value, 0).UTC()
	return &t
}

func upsertGatewayGroupModel(ctx context.Context, tx pgx.Tx, group sub2api.AdminGroup, externalChannelID int64, candidate gatewayModelCandidate) error {
	pricingJSON := []byte(`{}`)
	billingMode := "token"
	if candidate.Pricing != nil {
		pricing := *candidate.Pricing
		pricing.ID = 0
		raw, err := json.Marshal(pricing)
		if err != nil {
			return fmt.Errorf("model_pricing_marshal_failed: %w", err)
		}
		pricingJSON = raw
		if strings.TrimSpace(pricing.BillingMode) != "" {
			billingMode = strings.TrimSpace(pricing.BillingMode)
		}
	}
	capabilitiesJSON := modelCapabilitiesJSON(candidate.ModelID)
	if _, err := tx.Exec(ctx, `
		INSERT INTO gateway_group_models (
			public_id, provider, external_group_id, external_channel_id, platform,
			model_id, display_name, provider_family, capabilities_json, pricing_json,
			billing_mode, status, last_synced_at
		)
		VALUES ($1, 'sub2api', $2, $3, $4, $5, $6, $7, $8, $9, $10, 'active', now())
		ON CONFLICT (provider, external_group_id, platform, model_id) DO UPDATE
		SET external_channel_id = excluded.external_channel_id,
			display_name = excluded.display_name,
			provider_family = excluded.provider_family,
			capabilities_json = excluded.capabilities_json,
			pricing_json = excluded.pricing_json,
			billing_mode = excluded.billing_mode,
			status = 'active',
			last_synced_at = now(),
			updated_at = now()
	`, "ggm_"+uuid.NewString(), group.ID, externalChannelID, candidate.Platform, candidate.ModelID,
		modelDisplayName(candidate.ModelID), providerFamilyForPlatform(candidate.Platform), capabilitiesJSON,
		pricingJSON, billingMode); err != nil {
		return fmt.Errorf("model_upsert_failed: %w", err)
	}
	return nil
}

func supportedGatewayModelsFromAccount(account sub2api.AdminAccount) []gatewayModelCandidate {
	mapping := accountModelMapping(account)
	items := make([]gatewayModelCandidate, 0, len(mapping))
	for model := range mapping {
		if !isConcreteModelName(model) {
			continue
		}
		items = append(items, gatewayModelCandidate{Platform: account.Platform, ModelID: strings.TrimSpace(model)})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return strings.ToLower(items[i].ModelID) < strings.ToLower(items[j].ModelID)
	})
	return items
}

func accountModelMapping(account sub2api.AdminAccount) map[string]string {
	raw := account.Credentials["model_mapping"]
	switch mapping := raw.(type) {
	case map[string]any:
		if len(mapping) == 0 {
			return nil
		}
		out := make(map[string]string, len(mapping))
		for key, value := range mapping {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if valueString, ok := value.(string); ok {
				out[key] = strings.TrimSpace(valueString)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case map[string]string:
		if len(mapping) == 0 {
			return nil
		}
		out := make(map[string]string, len(mapping))
		for key, value := range mapping {
			key = strings.TrimSpace(key)
			if key != "" {
				out[key] = strings.TrimSpace(value)
			}
		}
		return out
	default:
		return nil
	}
}

func supportedGatewayModels(channel sub2api.AdminChannel) []gatewayModelCandidate {
	pricingByPlatformModel := buildGatewayPricingIndex(channel.ModelPricing)

	seen := map[string]gatewayModelCandidate{}
	add := func(platform, model string, pricing *sub2api.ChannelModelPricing) {
		platform = strings.TrimSpace(platform)
		if platform == "" {
			platform = "anthropic"
		}
		model = strings.TrimSpace(model)
		if !isConcreteModelName(model) {
			return
		}
		key := platform + "\x00" + strings.ToLower(model)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = gatewayModelCandidate{Platform: platform, ModelID: model, Pricing: pricing}
	}

	for platform, mapping := range channel.ModelMapping {
		platform = strings.TrimSpace(platform)
		if platform == "" {
			platform = "anthropic"
		}
		for source, target := range mapping {
			source = strings.TrimSpace(source)
			if !isConcreteModelName(source) {
				continue
			}
			pricing := lookupModelPricing(pricingByPlatformModel, platform, target)
			if pricing == nil {
				pricing = lookupModelPricing(pricingByPlatformModel, platform, source)
			}
			add(platform, source, pricing)
		}
	}

	for _, pricing := range channel.ModelPricing {
		pricingCopy := pricing
		for _, model := range pricing.Models {
			add(pricing.Platform, model, &pricingCopy)
		}
	}

	items := make([]gatewayModelCandidate, 0, len(seen))
	for _, item := range seen {
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Platform != items[j].Platform {
			return items[i].Platform < items[j].Platform
		}
		return strings.ToLower(items[i].ModelID) < strings.ToLower(items[j].ModelID)
	})
	return items
}

func markSyncedModel(seen map[string]struct{}, groupID int64, candidate gatewayModelCandidate) bool {
	key := fmt.Sprintf("%d\x00%s\x00%s", groupID, candidate.Platform, strings.ToLower(candidate.ModelID))
	if _, exists := seen[key]; exists {
		return false
	}
	seen[key] = struct{}{}
	return true
}

func backfillGatewayGroupModelPricing(ctx context.Context, tx pgx.Tx, group sub2api.AdminGroup, channel sub2api.AdminChannel) error {
	pricingByPlatformModel := buildGatewayPricingIndex(channel.ModelPricing)
	if len(pricingByPlatformModel) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `
		SELECT model_id
		FROM gateway_group_models
		WHERE provider = 'sub2api'
			AND external_group_id = $1
			AND platform = $2
			AND status = 'active'
	`, group.ID, group.Platform)
	if err != nil {
		return fmt.Errorf("model_pricing_backfill_query_failed: %w", err)
	}
	defer rows.Close()

	candidates := []gatewayModelCandidate{}
	for rows.Next() {
		var modelID string
		if err := rows.Scan(&modelID); err != nil {
			return fmt.Errorf("model_pricing_backfill_scan_failed: %w", err)
		}
		pricing := lookupModelPricing(pricingByPlatformModel, group.Platform, modelID)
		if pricing == nil {
			continue
		}
		candidates = append(candidates, gatewayModelCandidate{
			Platform: group.Platform,
			ModelID:  modelID,
			Pricing:  pricing,
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("model_pricing_backfill_iter_failed: %w", err)
	}
	rows.Close()

	for _, candidate := range candidates {
		if err := upsertGatewayGroupModel(ctx, tx, group, channel.ID, candidate); err != nil {
			return err
		}
	}
	return nil
}

func buildGatewayPricingIndex(pricings []sub2api.ChannelModelPricing) map[string]*gatewayPricingIndex {
	index := map[string]*gatewayPricingIndex{}
	for _, pricing := range pricings {
		platform := strings.TrimSpace(pricing.Platform)
		if platform == "" {
			platform = "anthropic"
		}
		entry := index[platform]
		if entry == nil {
			entry = &gatewayPricingIndex{Exact: map[string]sub2api.ChannelModelPricing{}}
			index[platform] = entry
		}
		pricing.Platform = platform
		for _, model := range pricing.Models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if strings.HasSuffix(model, "*") {
				entry.Wildcards = append(entry.Wildcards, gatewayWildcardPricing{
					Prefix:  strings.ToLower(strings.TrimSuffix(model, "*")),
					Pricing: pricing,
				})
				continue
			}
			if !isConcreteModelName(model) {
				continue
			}
			key := strings.ToLower(model)
			if _, exists := entry.Exact[key]; !exists {
				entry.Exact[key] = pricing
			}
		}
	}
	return index
}

func lookupModelPricing(index map[string]*gatewayPricingIndex, platform, model string) *sub2api.ChannelModelPricing {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		platform = "anthropic"
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil
	}
	modelLower := strings.ToLower(model)
	if entry := index[platform]; entry != nil {
		if pricing, ok := entry.Exact[modelLower]; ok {
			return &pricing
		}
		for _, wildcard := range entry.Wildcards {
			if strings.HasPrefix(modelLower, wildcard.Prefix) {
				pricing := wildcard.Pricing
				return &pricing
			}
		}
	}
	return nil
}

func isConcreteModelName(model string) bool {
	model = strings.TrimSpace(model)
	return model != "" && !strings.Contains(model, "*")
}

func providerFamilyForPlatform(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "openai":
		return "openai-compatible"
	case "gemini":
		return "gemini-compatible"
	default:
		return "anthropic-compatible"
	}
}

func modelDisplayName(modelID string) string {
	parts := strings.FieldsFunc(modelID, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	for i := range parts {
		if parts[i] == "" {
			continue
		}
		lower := strings.ToLower(parts[i])
		switch lower {
		case "api", "gpt", "vl", "ui", "ocr":
			parts[i] = strings.ToUpper(lower)
		default:
			parts[i] = strings.ToUpper(lower[:1]) + lower[1:]
		}
	}
	display := strings.Join(parts, " ")
	if display == "" {
		return modelID
	}
	return display
}

func modelCapabilitiesJSON(modelID string) string {
	capabilities := []string{"chat"}
	lower := strings.ToLower(modelID)
	if strings.Contains(lower, "embedding") ||
		strings.Contains(lower, "embed") ||
		strings.Contains(lower, "bge") ||
		strings.Contains(lower, "gte") ||
		strings.Contains(lower, "e5") ||
		strings.Contains(lower, "jina") ||
		strings.Contains(lower, "voyage") {
		raw, _ := json.Marshal([]string{"embedding"})
		return string(raw)
	}
	if strings.Contains(lower, "claude") ||
		strings.Contains(lower, "vision") ||
		strings.Contains(lower, "vl") ||
		strings.Contains(lower, "gpt-4o") ||
		strings.Contains(lower, "gemini") {
		capabilities = append(capabilities, "vision_input")
	}
	raw, _ := json.Marshal(capabilities)
	return string(raw)
}
