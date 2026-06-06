package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type GatewayGroupService struct {
	postgres *pgxpool.Pool
}

func NewGatewayGroupService(postgres *pgxpool.Pool) *GatewayGroupService {
	return &GatewayGroupService{postgres: postgres}
}

func (s *GatewayGroupService) List(ctx context.Context) ([]GatewayGroupItem, error) {
	rows, err := s.postgres.Query(ctx, `
		SELECT public_id, provider, external_group_id, name, description, platform,
			subscription_type, rate_multiplier, is_exclusive, daily_limit_usd, weekly_limit_usd,
			monthly_limit_usd, default_validity_days, rpm_limit, sort_order,
			allow_image_generation, image_rate_independent, image_rate_multiplier,
			image_price_1k, image_price_2k, image_price_4k, claude_code_only,
			fallback_group_id, fallback_group_id_on_invalid_request, model_routing,
			model_routing_enabled, mcp_xml_inject, supported_model_scopes,
			allow_messages_dispatch, require_oauth_only, require_privacy_set,
			default_mapped_model, messages_dispatch_model_config, status, created_at, updated_at
		FROM gateway_groups
		ORDER BY sort_order ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []GatewayGroupItem{}
	for rows.Next() {
		item, err := scanGatewayGroup(rows)
		if err != nil {
			return nil, err
		}
		item.Models = []GatewayGroupModelItem{}
		item.Accounts = []GatewayUpstreamAccountItem{}
		item.Channels = []GatewayChannelItem{}
		item.OfficialModelConfig = emptyGatewayGroupOfficialConfig()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := s.attachModels(ctx, items); err != nil {
		return nil, err
	}
	if err := s.attachOfficialModelConfig(ctx, items); err != nil {
		return nil, err
	}
	if err := s.attachUpstreamAccounts(ctx, items); err != nil {
		return nil, err
	}
	if err := s.attachChannels(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func emptyGatewayGroupOfficialConfig() GatewayGroupOfficialConfig {
	return GatewayGroupOfficialConfig{
		Embedding: GatewayGroupOfficialPurposeConfig{ModelIDs: []string{}},
		Vision:    GatewayGroupOfficialPurposeConfig{ModelIDs: []string{}},
	}
}

func (s *GatewayGroupService) Create(ctx context.Context, req createGatewayGroupRequest) (GatewayGroupItem, error) {
	normalizeGatewayGroupRequest(&req)
	if req.Name == "" {
		return GatewayGroupItem{}, fmt.Errorf("name_required")
	}

	row := s.postgres.QueryRow(ctx, `
		INSERT INTO gateway_groups (
			public_id, provider, external_group_id, name, description, platform,
			subscription_type, rate_multiplier, daily_limit_usd, weekly_limit_usd,
			monthly_limit_usd, default_validity_days, status
		)
		VALUES ($1, 'sub2api', $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (provider, external_group_id) DO UPDATE
		SET name = excluded.name,
			description = excluded.description,
			platform = excluded.platform,
			subscription_type = excluded.subscription_type,
			rate_multiplier = excluded.rate_multiplier,
			daily_limit_usd = excluded.daily_limit_usd,
			weekly_limit_usd = excluded.weekly_limit_usd,
			monthly_limit_usd = excluded.monthly_limit_usd,
			default_validity_days = excluded.default_validity_days,
			status = excluded.status,
			updated_at = now()
		RETURNING public_id, provider, external_group_id, name, description, platform,
			subscription_type, rate_multiplier, is_exclusive, daily_limit_usd, weekly_limit_usd,
			monthly_limit_usd, default_validity_days, rpm_limit, sort_order,
			allow_image_generation, image_rate_independent, image_rate_multiplier,
			image_price_1k, image_price_2k, image_price_4k, claude_code_only,
			fallback_group_id, fallback_group_id_on_invalid_request, model_routing,
			model_routing_enabled, mcp_xml_inject, supported_model_scopes,
			allow_messages_dispatch, require_oauth_only, require_privacy_set,
			default_mapped_model, messages_dispatch_model_config, status, created_at, updated_at
	`, "gg_"+uuid.NewString(), req.ExternalGroupID, req.Name, req.Description, req.Platform,
		req.SubscriptionType, req.RateMultiplier, req.DailyLimitUSD, req.WeeklyLimitUSD,
		req.MonthlyLimitUSD, req.DefaultValidityDays, req.Status)
	return scanGatewayGroup(row)
}

func normalizeGatewayGroupRequest(req *createGatewayGroupRequest) {
	req.Name = strings.TrimSpace(req.Name)
	req.Description = strings.TrimSpace(req.Description)
	req.Platform = strings.TrimSpace(req.Platform)
	req.SubscriptionType = strings.TrimSpace(req.SubscriptionType)
	req.Status = strings.TrimSpace(req.Status)
	if req.Platform == "" {
		req.Platform = "anthropic"
	}
	if req.SubscriptionType == "" {
		req.SubscriptionType = "standard"
	}
	if req.RateMultiplier == 0 {
		req.RateMultiplier = 1
	}
	if req.DefaultValidityDays == 0 {
		req.DefaultValidityDays = 30
	}
	if req.Status == "" {
		req.Status = "active"
	}
}

func scanGatewayGroup(row scanner) (GatewayGroupItem, error) {
	var item GatewayGroupItem
	err := row.Scan(
		&item.ID,
		&item.Provider,
		&item.ExternalGroupID,
		&item.Name,
		&item.Description,
		&item.Platform,
		&item.SubscriptionType,
		&item.RateMultiplier,
		&item.IsExclusive,
		&item.DailyLimitUSD,
		&item.WeeklyLimitUSD,
		&item.MonthlyLimitUSD,
		&item.DefaultValidityDays,
		&item.RPMLimit,
		&item.SortOrder,
		&item.AllowImageGeneration,
		&item.ImageRateIndependent,
		&item.ImageRateMultiplier,
		&item.ImagePrice1K,
		&item.ImagePrice2K,
		&item.ImagePrice4K,
		&item.ClaudeCodeOnly,
		&item.FallbackGroupID,
		&item.FallbackGroupIDOnInvalidRequest,
		&item.ModelRouting,
		&item.ModelRoutingEnabled,
		&item.MCPXMLInject,
		&item.SupportedModelScopes,
		&item.AllowMessagesDispatch,
		&item.RequireOAuthOnly,
		&item.RequirePrivacySet,
		&item.DefaultMappedModel,
		&item.MessagesDispatchModelConfig,
		&item.Status,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	return item, err
}

func (s *GatewayGroupService) attachUpstreamAccounts(ctx context.Context, groups []GatewayGroupItem) error {
	if len(groups) == 0 {
		return nil
	}

	type groupKey struct {
		provider        string
		externalGroupID int64
	}
	groupIndexes := make(map[groupKey]int, len(groups))
	for index, group := range groups {
		groupIndexes[groupKey{provider: group.Provider, externalGroupID: group.ExternalGroupID}] = index
	}

	rows, err := s.postgres.Query(ctx, `
		SELECT public_id, provider, external_account_id, name, platform, account_type,
			status, schedulable, concurrency, current_concurrency, priority,
			rate_multiplier, error_message, group_ids, model_mapping, mapped_model_count,
			last_used_at, expires_at, rate_limited_at, rate_limit_reset_at,
			overload_until, temp_unschedulable_until, last_synced_at
		FROM gateway_upstream_accounts
		WHERE provider = 'sub2api' AND status <> 'inactive'
		ORDER BY priority ASC, external_account_id ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var provider string
		var groupIDsRaw []byte
		var modelMappingRaw []byte
		item := GatewayUpstreamAccountItem{}
		if err := rows.Scan(
			&item.ID,
			&provider,
			&item.ExternalAccountID,
			&item.Name,
			&item.Platform,
			&item.AccountType,
			&item.Status,
			&item.Schedulable,
			&item.Concurrency,
			&item.CurrentConcurrency,
			&item.Priority,
			&item.RateMultiplier,
			&item.ErrorMessage,
			&groupIDsRaw,
			&modelMappingRaw,
			&item.MappedModelCount,
			&item.LastUsedAt,
			&item.ExpiresAt,
			&item.RateLimitedAt,
			&item.RateLimitResetAt,
			&item.OverloadUntil,
			&item.TempUnschedulableUntil,
			&item.LastSyncedAt,
		); err != nil {
			return err
		}
		item.GroupIDs = parseInt64JSONList(groupIDsRaw)
		item.MappedModels = mappedModelsFromJSON(modelMappingRaw)
		for _, externalGroupID := range item.GroupIDs {
			index, ok := groupIndexes[groupKey{provider: provider, externalGroupID: externalGroupID}]
			if !ok {
				continue
			}
			groups[index].Accounts = append(groups[index].Accounts, item)
			groups[index].UpstreamAccountCount++
			if item.Status == "active" && item.Schedulable {
				groups[index].ActiveSchedulableAccountCount++
			}
		}
	}
	return rows.Err()
}

func parseInt64JSONList(raw []byte) []int64 {
	if len(raw) == 0 {
		return []int64{}
	}
	var items []int64
	if err := json.Unmarshal(raw, &items); err != nil {
		return []int64{}
	}
	return items
}

func mappedModelsFromJSON(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var mapping map[string]string
	if err := json.Unmarshal(raw, &mapping); err != nil {
		return []string{}
	}
	models := make([]string, 0, len(mapping))
	for model := range mapping {
		if strings.TrimSpace(model) != "" {
			models = append(models, model)
		}
	}
	sort.Strings(models)
	return models
}

func (s *GatewayGroupService) attachChannels(ctx context.Context, groups []GatewayGroupItem) error {
	if len(groups) == 0 {
		return nil
	}

	type groupKey struct {
		provider        string
		externalGroupID int64
	}
	groupIndexes := make(map[groupKey]int, len(groups))
	for index, group := range groups {
		groupIndexes[groupKey{provider: group.Provider, externalGroupID: group.ExternalGroupID}] = index
	}

	rows, err := s.postgres.Query(ctx, `
		SELECT public_id, provider, external_channel_id, name, description, status,
			billing_model_source, restrict_models, group_ids, model_mapping,
			model_pricing, pricing_count, last_synced_at
		FROM gateway_channels
		WHERE provider = 'sub2api' AND status <> 'inactive'
		ORDER BY external_channel_id ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var provider string
		var groupIDsRaw []byte
		item := GatewayChannelItem{}
		if err := rows.Scan(
			&item.ID,
			&provider,
			&item.ExternalChannelID,
			&item.Name,
			&item.Description,
			&item.Status,
			&item.BillingModelSource,
			&item.RestrictModels,
			&groupIDsRaw,
			&item.ModelMapping,
			&item.ModelPricing,
			&item.PricingCount,
			&item.LastSyncedAt,
		); err != nil {
			return err
		}
		item.GroupIDs = parseInt64JSONList(groupIDsRaw)
		for _, externalGroupID := range item.GroupIDs {
			index, ok := groupIndexes[groupKey{provider: provider, externalGroupID: externalGroupID}]
			if !ok {
				continue
			}
			groups[index].Channels = append(groups[index].Channels, item)
			groups[index].ChannelCount++
		}
	}
	return rows.Err()
}

func (s *GatewayGroupService) attachModels(ctx context.Context, groups []GatewayGroupItem) error {
	if len(groups) == 0 {
		return nil
	}

	type groupKey struct {
		provider        string
		externalGroupID int64
	}
	groupIndexes := make(map[groupKey]int, len(groups))
	for index, group := range groups {
		groupIndexes[groupKey{provider: group.Provider, externalGroupID: group.ExternalGroupID}] = index
	}

	rows, err := s.postgres.Query(ctx, `
		SELECT ggm.provider, ggm.external_group_id, ggm.public_id, ggm.external_channel_id, ggm.platform,
			ggm.model_id, ggm.display_name, ggm.provider_family, ggm.capabilities_json, ggm.pricing_json, ggm.billing_mode,
			ggm.status, ggm.last_synced_at, coalesce(gc.name, '')
		FROM gateway_group_models ggm
		LEFT JOIN gateway_channels gc
			ON gc.provider = ggm.provider AND gc.external_channel_id = ggm.external_channel_id
		WHERE ggm.status = 'active'
		ORDER BY ggm.external_group_id ASC, ggm.platform ASC, ggm.model_id ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var provider string
		var externalGroupID int64
		var item GatewayGroupModelItem
		var capabilitiesJSON string
		if err := rows.Scan(
			&provider,
			&externalGroupID,
			&item.ID,
			&item.ExternalChannelID,
			&item.Platform,
			&item.ModelID,
			&item.DisplayName,
			&item.ProviderFamily,
			&capabilitiesJSON,
			&item.Pricing,
			&item.BillingMode,
			&item.Status,
			&item.LastSyncedAt,
			&item.ChannelName,
		); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(capabilitiesJSON), &item.Capabilities); err != nil {
			item.Capabilities = []string{}
		}
		item.SourceType = gatewayModelSourceType(item)
		item.PricingStatus = gatewayModelPricingStatus(item)
		index, ok := groupIndexes[groupKey{provider: provider, externalGroupID: externalGroupID}]
		if !ok {
			continue
		}
		groups[index].Models = append(groups[index].Models, item)
		if item.PricingStatus == "configured" {
			groups[index].PricedModelCount++
		} else {
			groups[index].UnpricedModelCount++
		}
	}
	return rows.Err()
}

func (s *GatewayGroupService) attachOfficialModelConfig(ctx context.Context, groups []GatewayGroupItem) error {
	if len(groups) == 0 {
		return nil
	}
	type groupKey struct {
		provider        string
		externalGroupID int64
	}
	groupIndexes := make(map[groupKey]int, len(groups))
	for index, group := range groups {
		groups[index].OfficialModelConfig = emptyGatewayGroupOfficialConfig()
		groupIndexes[groupKey{provider: group.Provider, externalGroupID: group.ExternalGroupID}] = index
	}
	rows, err := s.postgres.Query(ctx, `
		SELECT provider, external_group_id, purpose, model_id, is_default
		FROM gateway_group_model_roles
		WHERE provider = 'sub2api' AND enabled = true
			AND purpose = ANY($1::text[])
		ORDER BY external_group_id ASC, purpose ASC, sort_order ASC, model_id ASC
	`, []string{"embedding", "vision"})
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var provider string
		var externalGroupID int64
		var purpose string
		var modelID string
		var isDefault bool
		if err := rows.Scan(&provider, &externalGroupID, &purpose, &modelID, &isDefault); err != nil {
			return err
		}
		index, ok := groupIndexes[groupKey{provider: provider, externalGroupID: externalGroupID}]
		if !ok {
			continue
		}
		switch purpose {
		case "embedding":
			groups[index].OfficialModelConfig.Embedding.ModelIDs = append(groups[index].OfficialModelConfig.Embedding.ModelIDs, modelID)
			if isDefault {
				groups[index].OfficialModelConfig.Embedding.DefaultModelID = modelID
			}
		case "vision":
			groups[index].OfficialModelConfig.Vision.ModelIDs = append(groups[index].OfficialModelConfig.Vision.ModelIDs, modelID)
			if isDefault {
				groups[index].OfficialModelConfig.Vision.DefaultModelID = modelID
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for index := range groups {
		ensureOfficialPurposeDefault(&groups[index].OfficialModelConfig.Embedding)
		ensureOfficialPurposeDefault(&groups[index].OfficialModelConfig.Vision)
	}
	return nil
}

func (s *GatewayGroupService) UpdateOfficialModelConfig(ctx context.Context, externalGroupID int64, req updateGatewayGroupOfficialModelsRequest) (GatewayGroupOfficialConfig, error) {
	if externalGroupID <= 0 {
		return emptyGatewayGroupOfficialConfig(), fmt.Errorf("invalid_external_group_id")
	}
	config := GatewayGroupOfficialConfig{
		Embedding: normalizeOfficialPurposeConfig(req.Embedding),
		Vision:    normalizeOfficialPurposeConfig(req.Vision),
	}
	if err := s.validateOfficialModelConfig(ctx, externalGroupID, config); err != nil {
		return emptyGatewayGroupOfficialConfig(), err
	}
	tx, err := s.postgres.Begin(ctx)
	if err != nil {
		return emptyGatewayGroupOfficialConfig(), err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		DELETE FROM gateway_group_model_roles
		WHERE provider = 'sub2api' AND external_group_id = $1 AND purpose = ANY($2::text[])
	`, externalGroupID, []string{"embedding", "vision"}); err != nil {
		return emptyGatewayGroupOfficialConfig(), err
	}
	insertPurpose := func(purpose string, purposeConfig GatewayGroupOfficialPurposeConfig) error {
		for index, modelID := range purposeConfig.ModelIDs {
			_, err := tx.Exec(ctx, `
				INSERT INTO gateway_group_model_roles (
					public_id, provider, external_group_id, purpose, model_id,
					enabled, is_default, sort_order
				)
				VALUES ($1, 'sub2api', $2, $3, $4, true, $5, $6)
			`, "ggmr_"+uuid.NewString(), externalGroupID, purpose, modelID, modelID == purposeConfig.DefaultModelID, index)
			if err != nil {
				return err
			}
		}
		return nil
	}
	if err := insertPurpose("embedding", config.Embedding); err != nil {
		return emptyGatewayGroupOfficialConfig(), err
	}
	if err := insertPurpose("vision", config.Vision); err != nil {
		return emptyGatewayGroupOfficialConfig(), err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE gateway_groups
		SET updated_at = now()
		WHERE provider = 'sub2api' AND external_group_id = $1
	`, externalGroupID); err != nil {
		return emptyGatewayGroupOfficialConfig(), err
	}
	if err := tx.Commit(ctx); err != nil {
		return emptyGatewayGroupOfficialConfig(), err
	}
	return config, nil
}

func normalizeOfficialPurposeConfig(config GatewayGroupOfficialPurposeConfig) GatewayGroupOfficialPurposeConfig {
	seen := map[string]struct{}{}
	modelIDs := []string{}
	for _, modelID := range config.ModelIDs {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		key := strings.ToLower(modelID)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		modelIDs = append(modelIDs, modelID)
	}
	out := GatewayGroupOfficialPurposeConfig{
		ModelIDs:       modelIDs,
		DefaultModelID: strings.TrimSpace(config.DefaultModelID),
	}
	ensureOfficialPurposeDefault(&out)
	return out
}

func ensureOfficialPurposeDefault(config *GatewayGroupOfficialPurposeConfig) {
	if len(config.ModelIDs) == 0 {
		config.DefaultModelID = ""
		return
	}
	for _, modelID := range config.ModelIDs {
		if strings.EqualFold(modelID, config.DefaultModelID) {
			config.DefaultModelID = modelID
			return
		}
	}
	config.DefaultModelID = config.ModelIDs[0]
}

func (s *GatewayGroupService) validateOfficialModelConfig(ctx context.Context, externalGroupID int64, config GatewayGroupOfficialConfig) error {
	var groupExists bool
	if err := s.postgres.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM gateway_groups
			WHERE provider = 'sub2api' AND external_group_id = $1
		)
	`, externalGroupID).Scan(&groupExists); err != nil {
		return err
	}
	if !groupExists {
		return fmt.Errorf("gateway_group_not_found")
	}
	rows, err := s.postgres.Query(ctx, `
		SELECT model_id
		FROM gateway_group_models
		WHERE provider = 'sub2api' AND external_group_id = $1 AND status = 'active'
	`, externalGroupID)
	if err != nil {
		return err
	}
	defer rows.Close()
	available := map[string]string{}
	for rows.Next() {
		var modelID string
		if err := rows.Scan(&modelID); err != nil {
			return err
		}
		available[strings.ToLower(modelID)] = modelID
	}
	if err := rows.Err(); err != nil {
		return err
	}
	validatePurpose := func(config GatewayGroupOfficialPurposeConfig) error {
		for _, modelID := range config.ModelIDs {
			if _, ok := available[strings.ToLower(modelID)]; !ok {
				return fmt.Errorf("official_model_not_in_group")
			}
		}
		if config.DefaultModelID != "" {
			found := false
			for _, modelID := range config.ModelIDs {
				if strings.EqualFold(modelID, config.DefaultModelID) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("official_default_model_not_enabled")
			}
		}
		return nil
	}
	if err := validatePurpose(config.Embedding); err != nil {
		return err
	}
	return validatePurpose(config.Vision)
}

func isOfficialModelConfigValidationError(err error) bool {
	switch err.Error() {
	case "invalid_external_group_id", "gateway_group_not_found", "official_model_not_in_group", "official_default_model_not_enabled":
		return true
	default:
		return false
	}
}

func gatewayModelSourceType(item GatewayGroupModelItem) string {
	if item.ExternalChannelID <= 0 {
		return "account_mapping"
	}
	if modelPricingConfigured(item.Pricing) {
		return "channel_pricing"
	}
	return "channel_mapping"
}

func gatewayModelPricingStatus(item GatewayGroupModelItem) string {
	if modelPricingConfigured(item.Pricing) {
		return "configured"
	}
	return "missing"
}

func modelPricingConfigured(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return false
	}
	var pricing map[string]any
	if err := json.Unmarshal(raw, &pricing); err != nil {
		return false
	}
	for _, key := range []string{
		"input_price",
		"output_price",
		"cache_write_price",
		"cache_read_price",
		"image_output_price",
		"per_request_price",
	} {
		if value, ok := pricing[key]; ok && value != nil {
			return true
		}
	}
	if intervals, ok := pricing["intervals"].([]any); ok && len(intervals) > 0 {
		return true
	}
	return false
}
