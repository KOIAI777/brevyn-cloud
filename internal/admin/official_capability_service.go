package admin

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var officialCapabilityKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,47}$`)

type OfficialCapabilityDefinition struct {
	ID                    string   `json:"id"`
	Key                   string   `json:"key"`
	Name                  string   `json:"name"`
	Description           string   `json:"description"`
	ProviderKind          string   `json:"providerKind"`
	AdapterKind           string   `json:"adapterKind"`
	Protocol              string   `json:"protocol"`
	ModelHintCapabilities []string `json:"modelHintCapabilities"`
	MinClientVersion      string   `json:"minClientVersion"`
	Enabled               bool     `json:"enabled"`
	SortOrder             int      `json:"sortOrder"`
}

type OfficialCapabilityDefinitionInput struct {
	Key                   string   `json:"key"`
	Name                  string   `json:"name"`
	Description           string   `json:"description"`
	ProviderKind          string   `json:"providerKind"`
	AdapterKind           string   `json:"adapterKind"`
	Protocol              string   `json:"protocol"`
	ModelHintCapabilities []string `json:"modelHintCapabilities"`
	MinClientVersion      string   `json:"minClientVersion"`
	Enabled               *bool    `json:"enabled"`
	SortOrder             int      `json:"sortOrder"`
}

type OfficialCapabilityService struct {
	postgres *pgxpool.Pool
}

func NewOfficialCapabilityService(postgres *pgxpool.Pool) *OfficialCapabilityService {
	return &OfficialCapabilityService{postgres: postgres}
}

func (s *OfficialCapabilityService) List(ctx context.Context, includeDisabled bool) ([]OfficialCapabilityDefinition, error) {
	query := `
		SELECT public_id, capability_key, name, description, provider_kind,
			adapter_kind, protocol, model_hint_capabilities, min_client_version,
			enabled, sort_order
		FROM official_capability_definitions
	`
	if !includeDisabled {
		query += ` WHERE enabled = true`
	}
	query += ` ORDER BY sort_order ASC, capability_key ASC`
	rows, err := s.postgres.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []OfficialCapabilityDefinition{}
	for rows.Next() {
		var item OfficialCapabilityDefinition
		var hintsRaw []byte
		if err := rows.Scan(
			&item.ID,
			&item.Key,
			&item.Name,
			&item.Description,
			&item.ProviderKind,
			&item.AdapterKind,
			&item.Protocol,
			&hintsRaw,
			&item.MinClientVersion,
			&item.Enabled,
			&item.SortOrder,
		); err != nil {
			return nil, err
		}
		item.ModelHintCapabilities = decodeStringList(hintsRaw)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *OfficialCapabilityService) Replace(ctx context.Context, inputs []OfficialCapabilityDefinitionInput) ([]OfficialCapabilityDefinition, error) {
	normalized, err := normalizeOfficialCapabilityInputs(inputs)
	if err != nil {
		return nil, err
	}
	tx, err := s.postgres.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	seenKeys := make([]string, 0, len(normalized))
	for index, item := range normalized {
		if item.SortOrder == 0 {
			item.SortOrder = (index + 1) * 10
		}
		hintsRaw, err := json.Marshal(item.ModelHintCapabilities)
		if err != nil {
			return nil, err
		}
		enabled := true
		if item.Enabled != nil {
			enabled = *item.Enabled
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO official_capability_definitions (
				public_id, capability_key, name, description, provider_kind,
				adapter_kind, protocol, model_hint_capabilities,
				min_client_version, enabled, sort_order
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11)
			ON CONFLICT (capability_key) DO UPDATE SET
				name = EXCLUDED.name,
				description = EXCLUDED.description,
				provider_kind = EXCLUDED.provider_kind,
				adapter_kind = EXCLUDED.adapter_kind,
				protocol = EXCLUDED.protocol,
				model_hint_capabilities = EXCLUDED.model_hint_capabilities,
				min_client_version = EXCLUDED.min_client_version,
				enabled = EXCLUDED.enabled,
				sort_order = EXCLUDED.sort_order,
				updated_at = now()
		`, "ocd_"+uuid.NewString(), item.Key, item.Name, item.Description, item.ProviderKind,
			item.AdapterKind, item.Protocol, string(hintsRaw), item.MinClientVersion, enabled, item.SortOrder)
		if err != nil {
			return nil, err
		}
		seenKeys = append(seenKeys, item.Key)
	}
	if len(seenKeys) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE official_capability_definitions
			SET enabled = false, updated_at = now()
			WHERE NOT (capability_key = ANY($1::text[]))
		`, seenKeys); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.List(ctx, true)
}

func (s *OfficialCapabilityService) ActiveKeys(ctx context.Context) ([]string, error) {
	items, err := s.List(ctx, false)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	return keys, nil
}

func normalizeOfficialCapabilityInputs(inputs []OfficialCapabilityDefinitionInput) ([]OfficialCapabilityDefinitionInput, error) {
	out := make([]OfficialCapabilityDefinitionInput, 0, len(inputs))
	seen := map[string]struct{}{}
	for _, input := range inputs {
		item := OfficialCapabilityDefinitionInput{
			Key:                   strings.ToLower(strings.TrimSpace(input.Key)),
			Name:                  strings.TrimSpace(input.Name),
			Description:           strings.TrimSpace(input.Description),
			ProviderKind:          strings.TrimSpace(input.ProviderKind),
			AdapterKind:           strings.TrimSpace(input.AdapterKind),
			Protocol:              strings.TrimSpace(input.Protocol),
			ModelHintCapabilities: normalizeStringList(input.ModelHintCapabilities),
			MinClientVersion:      strings.TrimSpace(input.MinClientVersion),
			Enabled:               input.Enabled,
			SortOrder:             input.SortOrder,
		}
		if !officialCapabilityKeyPattern.MatchString(item.Key) {
			return nil, fmt.Errorf("invalid_capability_key")
		}
		if item.Name == "" {
			return nil, fmt.Errorf("capability_name_required")
		}
		if item.ProviderKind == "" || item.AdapterKind == "" || item.Protocol == "" {
			return nil, fmt.Errorf("capability_adapter_required")
		}
		if _, ok := seen[item.Key]; ok {
			return nil, fmt.Errorf("duplicate_capability_key")
		}
		seen[item.Key] = struct{}{}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].SortOrder == out[j].SortOrder {
			return out[i].Key < out[j].Key
		}
		return out[i].SortOrder < out[j].SortOrder
	})
	return out, nil
}

func normalizeStringList(values []string) []string {
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

func decodeStringList(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	values := []string{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{}
	}
	return normalizeStringList(values)
}
