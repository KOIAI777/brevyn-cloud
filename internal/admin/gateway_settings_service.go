package admin

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/config"
	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type GatewaySettingsService struct {
	cfg      *config.Config
	postgres *pgxpool.Pool
}

type Sub2APIGroupSyncResult struct {
	Status string                `json:"status"`
	Synced int                   `json:"synced"`
	Total  int                   `json:"total"`
	Groups []Sub2APIGroupPreview `json:"groups"`
}

func NewGatewaySettingsService(cfg *config.Config, postgres *pgxpool.Pool) *GatewaySettingsService {
	return &GatewaySettingsService{cfg: cfg, postgres: postgres}
}

func (s *GatewaySettingsService) Load(ctx context.Context) (sub2APIRuntimeSettings, error) {
	settings := sub2APIRuntimeSettings{
		BaseURL:       strings.TrimRight(strings.TrimSpace(s.cfg.Sub2APIBaseURL), "/"),
		AdminAPIKey:   strings.TrimSpace(s.cfg.Sub2APIAdminAPIKey),
		AdminEmail:    strings.TrimSpace(s.cfg.Sub2APIAdminEmail),
		AdminPassword: s.cfg.Sub2APIAdminPassword,
	}

	if value, ok, err := s.getAppSetting(ctx, settingSub2APIBaseURL); err != nil {
		return settings, err
	} else if ok && strings.TrimSpace(value) != "" {
		settings.BaseURL = strings.TrimRight(strings.TrimSpace(value), "/")
	}
	if value, ok, err := s.getAppSetting(ctx, settingSub2APIAdminEmail); err != nil {
		return settings, err
	} else if ok {
		settings.AdminEmail = strings.TrimSpace(value)
	}
	if value, ok, err := s.getAppSetting(ctx, settingSub2APIAdminAPIKey); err != nil {
		return settings, err
	} else if ok && strings.TrimSpace(value) != "" {
		decrypted, err := s.DecryptSecret(value)
		if err != nil {
			return settings, err
		}
		settings.AdminAPIKey = strings.TrimSpace(decrypted)
	}
	if value, ok, err := s.getAppSetting(ctx, settingSub2APIAdminPassword); err != nil {
		return settings, err
	} else if ok && strings.TrimSpace(value) != "" {
		decrypted, err := s.DecryptSecret(value)
		if err != nil {
			return settings, err
		}
		settings.AdminPassword = decrypted
	}

	return settings, nil
}

func (s *GatewaySettingsService) Update(ctx context.Context, req updateSub2APISettingsRequest, adminID int64) (sub2APIRuntimeSettings, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
	if baseURL == "" {
		return sub2APIRuntimeSettings{}, fmt.Errorf("base_url_required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return sub2APIRuntimeSettings{}, fmt.Errorf("invalid_base_url")
	}

	if err := s.upsertAppSetting(ctx, settingSub2APIBaseURL, baseURL, false, adminID); err != nil {
		return sub2APIRuntimeSettings{}, err
	}
	if err := s.upsertAppSetting(ctx, settingSub2APIAdminEmail, strings.TrimSpace(req.AdminEmail), false, adminID); err != nil {
		return sub2APIRuntimeSettings{}, err
	}
	if req.AdminAPIKey != nil {
		value := strings.TrimSpace(*req.AdminAPIKey)
		if value != "" {
			encrypted, err := s.EncryptSecret(value)
			if err != nil {
				return sub2APIRuntimeSettings{}, err
			}
			value = encrypted
		}
		if err := s.upsertAppSetting(ctx, settingSub2APIAdminAPIKey, value, true, adminID); err != nil {
			return sub2APIRuntimeSettings{}, err
		}
	}
	if req.AdminPassword != nil && strings.TrimSpace(*req.AdminPassword) != "" {
		encrypted, err := s.EncryptSecret(strings.TrimSpace(*req.AdminPassword))
		if err != nil {
			return sub2APIRuntimeSettings{}, err
		}
		if err := s.upsertAppSetting(ctx, settingSub2APIAdminPassword, encrypted, true, adminID); err != nil {
			return sub2APIRuntimeSettings{}, err
		}
	}
	if req.DefaultGroupID != nil {
		if *req.DefaultGroupID < 0 {
			return sub2APIRuntimeSettings{}, fmt.Errorf("default_group_invalid")
		}
		if *req.DefaultGroupID > 0 {
			if err := s.validateDefaultGroupID(ctx, *req.DefaultGroupID); err != nil {
				return sub2APIRuntimeSettings{}, err
			}
		}
		if err := s.upsertAppSetting(ctx, settingSub2APIDefaultGroup, strconv.FormatInt(*req.DefaultGroupID, 10), false, adminID); err != nil {
			return sub2APIRuntimeSettings{}, err
		}
	}
	return s.Load(ctx)
}

func (s *GatewaySettingsService) SettingsResponse(settings sub2APIRuntimeSettings, defaultGroupID int64) Sub2APISettingsResponse {
	return Sub2APISettingsResponse{
		BaseURL:               settings.BaseURL,
		AdminEmail:            settings.AdminEmail,
		HasAdminPassword:      strings.TrimSpace(settings.AdminPassword) != "",
		AdminAPIKeyConfigured: strings.TrimSpace(settings.AdminAPIKey) != "",
		AuthMode:              s.AuthMode(settings),
		DefaultGroupID:        defaultGroupID,
	}
}

func (s *GatewaySettingsService) TestConnection(ctx context.Context) Sub2APITestResult {
	start := time.Now()
	settings, err := s.Load(ctx)
	if err != nil {
		return Sub2APITestResult{
			OK:        false,
			Status:    "settings_error",
			LatencyMs: time.Since(start).Milliseconds(),
			Error:     err.Error(),
		}
	}
	client := s.NewSub2APIClient(settings)

	result := Sub2APITestResult{
		BaseURL:  settings.BaseURL,
		AuthMode: s.AuthMode(settings),
		Status:   "checking",
	}
	health, err := client.Health(ctx)
	if err != nil {
		result.OK = false
		result.Status = "health_failed"
		result.Error = err.Error()
		result.LatencyMs = time.Since(start).Milliseconds()
		return result
	}
	result.HealthOK = true
	result.Health = health

	groups, err := client.ListGroups(ctx)
	if err != nil {
		result.OK = false
		result.Status = "auth_failed"
		result.Error = err.Error()
		result.LatencyMs = time.Since(start).Milliseconds()
		return result
	}
	result.AuthOK = true
	result.OK = true
	result.Status = "ok"
	result.GroupCount = len(groups)
	result.GroupsPreview = sub2APIGroupPreviews(groups, 5)
	result.LatencyMs = time.Since(start).Milliseconds()
	return result
}

func (s *GatewaySettingsService) SyncGroups(ctx context.Context) (Sub2APIGroupSyncResult, error) {
	settings, err := s.Load(ctx)
	if err != nil {
		return Sub2APIGroupSyncResult{}, fmt.Errorf("settings_load_failed: %w", err)
	}
	groups, err := s.NewSub2APIClient(settings).ListGroups(ctx)
	if err != nil {
		return Sub2APIGroupSyncResult{}, fmt.Errorf("sub2api_group_fetch_failed: %w", err)
	}

	tx, err := s.postgres.Begin(ctx)
	if err != nil {
		return Sub2APIGroupSyncResult{}, fmt.Errorf("sync_begin_failed: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	synced := make([]Sub2APIGroupPreview, 0, len(groups))
	externalGroupIDs := make([]int64, 0, len(groups))
	for _, group := range groups {
		normalized := normalizeSub2APIGroup(group)
		externalGroupIDs = append(externalGroupIDs, normalized.ID)
		if err := upsertGatewayGroup(ctx, tx, normalized); err != nil {
			return Sub2APIGroupSyncResult{}, err
		}
		synced = append(synced, groupPreview(normalized))
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM gateway_groups
		WHERE provider = 'sub2api' AND external_group_id = 0 AND name = 'Brevyn Official'
	`); err != nil {
		return Sub2APIGroupSyncResult{}, fmt.Errorf("placeholder_group_cleanup_failed: %w", err)
	}
	if len(externalGroupIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE gateway_groups
			SET status = 'inactive', updated_at = now()
			WHERE provider = 'sub2api' AND NOT (external_group_id = ANY($1::bigint[]))
		`, externalGroupIDs); err != nil {
			return Sub2APIGroupSyncResult{}, fmt.Errorf("group_stale_mark_failed: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Sub2APIGroupSyncResult{}, fmt.Errorf("sync_commit_failed: %w", err)
	}
	return Sub2APIGroupSyncResult{Status: "ok", Synced: len(synced), Total: len(groups), Groups: synced}, nil
}

func (s *GatewaySettingsService) AuthMode(settings sub2APIRuntimeSettings) string {
	if strings.TrimSpace(settings.AdminAPIKey) != "" {
		return "admin_api_key"
	}
	if strings.TrimSpace(settings.AdminEmail) != "" && strings.TrimSpace(settings.AdminPassword) != "" {
		return "admin_credentials"
	}
	return "not_configured"
}

func (s *GatewaySettingsService) NewSub2APIClient(settings sub2APIRuntimeSettings) *sub2api.Client {
	return sub2api.NewClient(sub2api.ClientConfig{
		BaseURL:       settings.BaseURL,
		AdminAPIKey:   settings.AdminAPIKey,
		AdminEmail:    settings.AdminEmail,
		AdminPassword: settings.AdminPassword,
	})
}

func (s *GatewaySettingsService) EncryptSecret(plain string) (string, error) {
	block, err := aes.NewCipher(s.secretEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "v1:" + base64.StdEncoding.EncodeToString(sealed), nil
}

func (s *GatewaySettingsService) DecryptSecret(value string) (string, error) {
	if !strings.HasPrefix(value, "v1:") {
		return value, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "v1:"))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.secretEncryptionKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("encrypted setting is malformed")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *GatewaySettingsService) getAppSetting(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.postgres.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func (s *GatewaySettingsService) upsertAppSetting(ctx context.Context, key, value string, sensitive bool, adminID int64) error {
	_, err := s.postgres.Exec(ctx, `
		INSERT INTO app_settings (key, value, sensitive, updated_by_admin_id)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (key) DO UPDATE
		SET value = excluded.value,
			sensitive = excluded.sensitive,
			updated_by_admin_id = excluded.updated_by_admin_id,
			updated_at = now()
	`, key, value, sensitive, adminID)
	return err
}

func (s *GatewaySettingsService) validateDefaultGroupID(ctx context.Context, externalGroupID int64) error {
	var subscriptionType string
	if err := s.postgres.QueryRow(ctx, `
		SELECT subscription_type
		FROM gateway_groups
		WHERE provider = 'sub2api' AND external_group_id = $1 AND status = 'active'
	`, externalGroupID).Scan(&subscriptionType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("default_group_not_found")
		}
		return fmt.Errorf("default_group_lookup_failed: %w", err)
	}
	if subscriptionType != "standard" {
		return errors.New("default_group_must_be_standard")
	}
	return nil
}

func (s *GatewaySettingsService) secretEncryptionKey() []byte {
	seed := strings.TrimSpace(s.cfg.EncryptionKey)
	if seed == "" {
		seed = strings.TrimSpace(s.cfg.SessionSecret)
	}
	if seed == "" {
		seed = strings.TrimSpace(s.cfg.JWTAccessSecret)
	}
	if seed == "" {
		seed = strings.TrimSpace(s.cfg.AdminSeedPassword)
	}
	sum := sha256.Sum256([]byte(seed))
	return sum[:]
}

func isGatewaySettingsValidationError(err error) bool {
	if err == nil {
		return false
	}
	switch err.Error() {
	case "base_url_required",
		"invalid_base_url",
		"default_group_invalid",
		"default_group_not_found",
		"default_group_must_be_standard":
		return true
	default:
		return false
	}
}
