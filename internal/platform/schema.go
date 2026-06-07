package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

const currentSchemaVersion = "20260607_redeem_backup_hardening"

type schemaExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func CurrentSchemaVersion() string {
	return currentSchemaVersion
}

func PrepareSchema(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) error {
	if cfg.MigrateOnStartup {
		return EnsureSchema(ctx, pool, cfg)
	}
	return VerifySchema(ctx, pool)
}

func VerifySchema(ctx context.Context, pool *pgxpool.Pool) error {
	var hasMigrationTable bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'schema_migrations'
		)
	`).Scan(&hasMigrationTable); err != nil {
		return fmt.Errorf("check schema migrations table: %w", err)
	}
	if !hasMigrationTable {
		return fmt.Errorf("database schema is not migrated; run brevyn-migrate before starting api and worker")
	}

	var hasCurrentVersion bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM schema_migrations WHERE version = $1
		)
	`, currentSchemaVersion).Scan(&hasCurrentVersion); err != nil {
		return fmt.Errorf("check schema version: %w", err)
	}
	if !hasCurrentVersion {
		return fmt.Errorf("database schema version %s is not applied; run brevyn-migrate", currentSchemaVersion)
	}
	return nil
}

func EnsureSchema(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) error {
	const schemaLockID int64 = 91940001

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire schema connection: %w", err)
	}
	defer conn.Release()

	lockCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, err := conn.Exec(lockCtx, `SELECT pg_advisory_lock($1)`, schemaLockID); err != nil {
		return fmt.Errorf("acquire schema lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `RESET lock_timeout`)
		_, _ = conn.Exec(context.Background(), `RESET statement_timeout`)
		var released bool
		_ = conn.QueryRow(context.Background(), `SELECT pg_advisory_unlock($1)`, schemaLockID).Scan(&released)
	}()

	if _, err := conn.Exec(ctx, `SET lock_timeout = '15s'`); err != nil {
		return fmt.Errorf("set schema lock timeout: %w", err)
	}
	if _, err := conn.Exec(ctx, `SET statement_timeout = '5min'`); err != nil {
		return fmt.Errorf("set schema statement timeout: %w", err)
	}

	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
				version TEXT PRIMARY KEY,
				applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
			)`,
		`CREATE TABLE IF NOT EXISTS admin_users (
				id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			email TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'owner',
			status TEXT NOT NULL DEFAULT 'active',
			totp_enabled BOOLEAN NOT NULL DEFAULT false,
			totp_secret_encrypted TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS totp_enabled BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE admin_users ADD COLUMN IF NOT EXISTS totp_secret_encrypted TEXT`,
		`CREATE TABLE IF NOT EXISTS app_settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT '',
			sensitive BOOLEAN NOT NULL DEFAULT false,
			updated_by_admin_id BIGINT REFERENCES admin_users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS backup_records (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'pending',
			progress TEXT NOT NULL DEFAULT '',
			backup_type TEXT NOT NULL DEFAULT 'postgres',
			storage_kind TEXT NOT NULL DEFAULT 'local',
			file_name TEXT NOT NULL DEFAULT '',
			file_path TEXT NOT NULL DEFAULT '',
			s3_key TEXT NOT NULL DEFAULT '',
			size_bytes BIGINT NOT NULL DEFAULT 0,
			sha256 TEXT NOT NULL DEFAULT '',
			triggered_by TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			finished_at TIMESTAMPTZ,
			expires_at TIMESTAMPTZ,
			restore_status TEXT NOT NULL DEFAULT '',
			restore_error TEXT NOT NULL DEFAULT '',
			restored_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			email TEXT NOT NULL UNIQUE,
			email_hash TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS refresh_tokens (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			family_id TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			status TEXT NOT NULL DEFAULT 'active',
			expires_at TIMESTAMPTZ NOT NULL,
			last_used_at TIMESTAMPTZ,
			revoked_at TIMESTAMPTZ,
			revoked_reason TEXT NOT NULL DEFAULT '',
			replaced_by_token_id BIGINT REFERENCES refresh_tokens(id) ON DELETE SET NULL,
			created_ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS devices (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			device_fingerprint_hash TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			platform TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			last_seen_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS gateway_accounts (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			provider TEXT NOT NULL DEFAULT 'sub2api',
			external_user_id BIGINT NOT NULL DEFAULT 0,
			external_email TEXT NOT NULL,
			default_group_id BIGINT NOT NULL DEFAULT 0,
			concurrency INT NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active',
			last_synced_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE gateway_accounts ADD COLUMN IF NOT EXISTS concurrency INT NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS gateway_api_keys (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			device_id BIGINT REFERENCES devices(id) ON DELETE SET NULL,
			gateway_account_id BIGINT REFERENCES gateway_accounts(id) ON DELETE SET NULL,
			provider TEXT NOT NULL DEFAULT 'sub2api',
			external_key_id BIGINT,
			external_group_id BIGINT NOT NULL DEFAULT 0,
			encrypted_api_key TEXT NOT NULL DEFAULT '',
			masked_api_key TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			last_used_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE gateway_api_keys ADD COLUMN IF NOT EXISTS public_id TEXT`,
		`UPDATE gateway_api_keys SET public_id = 'gak_' || md5(random()::text || clock_timestamp()::text) WHERE public_id IS NULL OR public_id = ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_gateway_api_keys_public_id ON gateway_api_keys(public_id)`,
		`CREATE TABLE IF NOT EXISTS wallet_transactions (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			kind TEXT NOT NULL,
			amount DOUBLE PRECISION NOT NULL DEFAULT 0,
			balance_after DOUBLE PRECISION NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT 'system',
			reference_id TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS gateway_groups (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			provider TEXT NOT NULL DEFAULT 'sub2api',
			external_group_id BIGINT NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			platform TEXT NOT NULL DEFAULT 'anthropic',
			subscription_type TEXT NOT NULL DEFAULT 'standard',
			rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
			is_exclusive BOOLEAN NOT NULL DEFAULT false,
			daily_limit_usd DOUBLE PRECISION,
			weekly_limit_usd DOUBLE PRECISION,
			monthly_limit_usd DOUBLE PRECISION,
			default_validity_days INT NOT NULL DEFAULT 30,
			rpm_limit INT NOT NULL DEFAULT 0,
			sort_order INT NOT NULL DEFAULT 0,
			allow_image_generation BOOLEAN NOT NULL DEFAULT false,
			image_rate_independent BOOLEAN NOT NULL DEFAULT false,
			image_rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
			image_price_1k DOUBLE PRECISION,
			image_price_2k DOUBLE PRECISION,
			image_price_4k DOUBLE PRECISION,
			claude_code_only BOOLEAN NOT NULL DEFAULT false,
			fallback_group_id BIGINT,
			fallback_group_id_on_invalid_request BIGINT,
			model_routing JSONB NOT NULL DEFAULT '{}'::jsonb,
			model_routing_enabled BOOLEAN NOT NULL DEFAULT false,
			mcp_xml_inject BOOLEAN NOT NULL DEFAULT true,
			supported_model_scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
			allow_messages_dispatch BOOLEAN NOT NULL DEFAULT false,
			require_oauth_only BOOLEAN NOT NULL DEFAULT false,
			require_privacy_set BOOLEAN NOT NULL DEFAULT false,
			default_mapped_model TEXT NOT NULL DEFAULT '',
			messages_dispatch_model_config JSONB NOT NULL DEFAULT '{}'::jsonb,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(provider, external_group_id)
		)`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS is_exclusive BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS rpm_limit INT NOT NULL DEFAULT 0`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS sort_order INT NOT NULL DEFAULT 0`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS allow_image_generation BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS image_rate_independent BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS image_rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS image_price_1k DOUBLE PRECISION`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS image_price_2k DOUBLE PRECISION`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS image_price_4k DOUBLE PRECISION`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS claude_code_only BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS fallback_group_id BIGINT`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS fallback_group_id_on_invalid_request BIGINT`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS model_routing JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS model_routing_enabled BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS mcp_xml_inject BOOLEAN NOT NULL DEFAULT true`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS supported_model_scopes JSONB NOT NULL DEFAULT '[]'::jsonb`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS allow_messages_dispatch BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS require_oauth_only BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS require_privacy_set BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS default_mapped_model TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE gateway_groups ADD COLUMN IF NOT EXISTS messages_dispatch_model_config JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`CREATE TABLE IF NOT EXISTS gateway_channels (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			provider TEXT NOT NULL DEFAULT 'sub2api',
			external_channel_id BIGINT NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			billing_model_source TEXT NOT NULL DEFAULT 'channel_mapped',
			restrict_models BOOLEAN NOT NULL DEFAULT false,
			group_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
			model_mapping JSONB NOT NULL DEFAULT '{}'::jsonb,
			model_pricing JSONB NOT NULL DEFAULT '[]'::jsonb,
			pricing_count INT NOT NULL DEFAULT 0,
			last_synced_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(provider, external_channel_id)
		)`,
		`CREATE TABLE IF NOT EXISTS official_capability_definitions (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			capability_key TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			provider_kind TEXT NOT NULL,
			adapter_kind TEXT NOT NULL,
			protocol TEXT NOT NULL,
			model_hint_capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
			min_client_version TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL DEFAULT true,
			sort_order INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`INSERT INTO official_capability_definitions (
			public_id, capability_key, name, description, provider_kind,
			adapter_kind, protocol, model_hint_capabilities,
			min_client_version, enabled, sort_order
		)
		VALUES
			('ocd_embedding', 'embedding', '向量检索', '课程资料语义检索、RAG 和知识库索引使用的文本向量能力。', 'custom-openai', 'openai_embedding', 'openai_compatible', '["embedding"]'::jsonb, '', true, 10),
			('ocd_vision', 'vision', '视觉识别', '聊天、图片理解和轻量文档识别使用的视觉输入能力。', 'vision-custom-openai', 'openai_chat_completions', 'openai_compatible', '["vision_input"]'::jsonb, '', true, 20),
			('ocd_ocr', 'ocr', '文档 OCR', '扫描 PDF、课件图片页和低文本覆盖页面进入索引前的 OCR 补充能力。', 'ocr-custom-openai', 'openai_chat_completions', 'openai_compatible', '["vision_input", "ocr"]'::jsonb, '0.2.8', true, 30)
		ON CONFLICT (capability_key) DO NOTHING`,
		`ALTER TABLE gateway_channels ADD COLUMN IF NOT EXISTS group_ids JSONB NOT NULL DEFAULT '[]'::jsonb`,
		`ALTER TABLE gateway_channels ADD COLUMN IF NOT EXISTS model_pricing JSONB NOT NULL DEFAULT '[]'::jsonb`,
		`ALTER TABLE gateway_channels ADD COLUMN IF NOT EXISTS pricing_count INT NOT NULL DEFAULT 0`,
		`CREATE TABLE IF NOT EXISTS gateway_upstream_accounts (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			provider TEXT NOT NULL DEFAULT 'sub2api',
			external_account_id BIGINT NOT NULL DEFAULT 0,
			name TEXT NOT NULL,
			platform TEXT NOT NULL DEFAULT 'anthropic',
			account_type TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'active',
			schedulable BOOLEAN NOT NULL DEFAULT true,
			concurrency INT NOT NULL DEFAULT 0,
			current_concurrency INT NOT NULL DEFAULT 0,
			priority INT NOT NULL DEFAULT 0,
			rate_multiplier DOUBLE PRECISION NOT NULL DEFAULT 1,
			error_message TEXT NOT NULL DEFAULT '',
			group_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
			model_mapping JSONB NOT NULL DEFAULT '{}'::jsonb,
			mapped_model_count INT NOT NULL DEFAULT 0,
			last_used_at TIMESTAMPTZ,
			expires_at TIMESTAMPTZ,
			rate_limited_at TIMESTAMPTZ,
			rate_limit_reset_at TIMESTAMPTZ,
			overload_until TIMESTAMPTZ,
			temp_unschedulable_until TIMESTAMPTZ,
			last_synced_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(provider, external_account_id)
		)`,
		`CREATE TABLE IF NOT EXISTS gateway_group_models (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			provider TEXT NOT NULL DEFAULT 'sub2api',
			external_group_id BIGINT NOT NULL DEFAULT 0,
			external_channel_id BIGINT NOT NULL DEFAULT 0,
			platform TEXT NOT NULL DEFAULT 'anthropic',
			model_id TEXT NOT NULL,
			display_name TEXT NOT NULL,
			provider_family TEXT NOT NULL DEFAULT 'anthropic-compatible',
			capabilities_json TEXT NOT NULL DEFAULT '[]',
			pricing_json JSONB NOT NULL DEFAULT '{}'::jsonb,
			billing_mode TEXT NOT NULL DEFAULT 'token',
			status TEXT NOT NULL DEFAULT 'active',
			last_synced_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(provider, external_group_id, platform, model_id)
		)`,
		`CREATE TABLE IF NOT EXISTS gateway_group_model_roles (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			provider TEXT NOT NULL DEFAULT 'sub2api',
			external_group_id BIGINT NOT NULL DEFAULT 0,
			purpose TEXT NOT NULL,
			model_id TEXT NOT NULL,
			enabled BOOLEAN NOT NULL DEFAULT true,
			is_default BOOLEAN NOT NULL DEFAULT false,
			sort_order INT NOT NULL DEFAULT 0,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(provider, external_group_id, purpose, model_id)
		)`,
		`CREATE TABLE IF NOT EXISTS products (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			sku TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			benefit_type TEXT NOT NULL DEFAULT 'balance',
			price_cny DOUBLE PRECISION NOT NULL DEFAULT 0,
			original_price_cny DOUBLE PRECISION,
			value DOUBLE PRECISION NOT NULL DEFAULT 0,
			validity_days INT NOT NULL DEFAULT 0,
			gateway_group_id BIGINT REFERENCES gateway_groups(id) ON DELETE SET NULL,
			external_group_id BIGINT NOT NULL DEFAULT 0,
			source TEXT NOT NULL DEFAULT 'manual',
			features TEXT NOT NULL DEFAULT '',
			for_sale BOOLEAN NOT NULL DEFAULT true,
			sort_order INT NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active',
			metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS redeem_code_batches (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			product_id BIGINT REFERENCES products(id) ON DELETE SET NULL,
			name TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'ldxp',
			order_ref TEXT NOT NULL DEFAULT '',
			quantity INT NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active',
			notes TEXT NOT NULL DEFAULT '',
			encrypted_codes TEXT NOT NULL DEFAULT '',
			created_by_admin_id BIGINT REFERENCES admin_users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS redeem_codes (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			code_hash TEXT NOT NULL UNIQUE,
			code_prefix TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'balance',
			value DOUBLE PRECISION NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'unused',
			used_by_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
			used_at TIMESTAMPTZ,
			expires_at TIMESTAMPTZ,
			order_ref TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS product_id BIGINT REFERENCES products(id) ON DELETE SET NULL`,
		`ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS batch_id BIGINT REFERENCES redeem_code_batches(id) ON DELETE SET NULL`,
		`ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS gateway_group_id BIGINT REFERENCES gateway_groups(id) ON DELETE SET NULL`,
		`ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS external_group_id BIGINT NOT NULL DEFAULT 0`,
		`ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS validity_days INT NOT NULL DEFAULT 0`,
		`ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual'`,
		`ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb`,
		`ALTER TABLE redeem_code_batches ADD COLUMN IF NOT EXISTS order_ref TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE redeem_code_batches ADD COLUMN IF NOT EXISTS encrypted_codes TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE redeem_codes ADD COLUMN IF NOT EXISTS order_ref TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS redeem_redemptions (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			redeem_code_id BIGINT NOT NULL REFERENCES redeem_codes(id) ON DELETE CASCADE,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			product_id BIGINT REFERENCES products(id) ON DELETE SET NULL,
			batch_id BIGINT REFERENCES redeem_code_batches(id) ON DELETE SET NULL,
			kind TEXT NOT NULL DEFAULT 'balance',
			value DOUBLE PRECISION NOT NULL DEFAULT 0,
			validity_days INT NOT NULL DEFAULT 0,
			gateway_provider TEXT NOT NULL DEFAULT 'sub2api',
			external_user_id BIGINT NOT NULL DEFAULT 0,
			external_group_id BIGINT NOT NULL DEFAULT 0,
			gateway_operation TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending_gateway',
			error_message TEXT NOT NULL DEFAULT '',
			gateway_error_code TEXT NOT NULL DEFAULT '',
			gateway_error_class TEXT NOT NULL DEFAULT '',
			gateway_error_stage TEXT NOT NULL DEFAULT '',
			gateway_error_retryable BOOLEAN NOT NULL DEFAULT false,
			gateway_error_detail TEXT NOT NULL DEFAULT '',
			idempotency_key TEXT NOT NULL DEFAULT '',
			metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(redeem_code_id)
		)`,
		`ALTER TABLE redeem_redemptions ADD COLUMN IF NOT EXISTS gateway_error_code TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE redeem_redemptions ADD COLUMN IF NOT EXISTS gateway_error_class TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE redeem_redemptions ADD COLUMN IF NOT EXISTS gateway_error_stage TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE redeem_redemptions ADD COLUMN IF NOT EXISTS gateway_error_retryable BOOLEAN NOT NULL DEFAULT false`,
		`ALTER TABLE redeem_redemptions ADD COLUMN IF NOT EXISTS gateway_error_detail TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE redeem_redemptions DROP CONSTRAINT IF EXISTS redeem_redemptions_idempotency_key_key`,
		`CREATE TABLE IF NOT EXISTS gateway_operations (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			provider TEXT NOT NULL DEFAULT 'sub2api',
			operation TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			target_type TEXT NOT NULL DEFAULT '',
			target_id TEXT NOT NULL DEFAULT '',
			redemption_id BIGINT REFERENCES redeem_redemptions(id) ON DELETE CASCADE,
			user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
			idempotency_key TEXT NOT NULL DEFAULT '',
			attempts INT NOT NULL DEFAULT 0,
			max_attempts INT NOT NULL DEFAULT 8,
			next_run_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			locked_at TIMESTAMPTZ,
			locked_by TEXT NOT NULL DEFAULT '',
			started_at TIMESTAMPTZ,
			completed_at TIMESTAMPTZ,
			last_error_message TEXT NOT NULL DEFAULT '',
			last_error_code TEXT NOT NULL DEFAULT '',
			last_error_class TEXT NOT NULL DEFAULT '',
			last_error_stage TEXT NOT NULL DEFAULT '',
			last_error_retryable BOOLEAN NOT NULL DEFAULT false,
			last_error_detail TEXT NOT NULL DEFAULT '',
			payload JSONB NOT NULL DEFAULT '{}'::jsonb,
			result JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id BIGSERIAL PRIMARY KEY,
			public_id TEXT NOT NULL UNIQUE,
			actor_type TEXT NOT NULL,
			actor_id BIGINT,
			action TEXT NOT NULL,
			target_type TEXT NOT NULL DEFAULT '',
			target_id TEXT NOT NULL DEFAULT '',
			ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE TABLE IF NOT EXISTS model_catalog (
			id BIGSERIAL PRIMARY KEY,
			model_id TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL,
			provider_family TEXT NOT NULL,
			capabilities_json TEXT NOT NULL DEFAULT '[]',
			public_visible BOOLEAN NOT NULL DEFAULT true,
			supports_streaming BOOLEAN NOT NULL DEFAULT true,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_status ON users(status)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_family_id ON refresh_tokens(family_id)`,
		`CREATE INDEX IF NOT EXISTS idx_refresh_tokens_active_expiry ON refresh_tokens(expires_at) WHERE status = 'active'`,
		`CREATE INDEX IF NOT EXISTS idx_backup_records_created_at ON backup_records(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_records_status ON backup_records(status)`,
		`CREATE INDEX IF NOT EXISTS idx_backup_records_expires_at ON backup_records(expires_at) WHERE expires_at IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_devices_user_id ON devices(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_accounts_user_id ON gateway_accounts(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_api_keys_user_id ON gateway_api_keys(user_id)`,
		`WITH ranked AS (
			SELECT id, row_number() OVER (PARTITION BY provider, user_id ORDER BY id DESC) AS rn
			FROM gateway_accounts
			WHERE status = 'active'
		)
		UPDATE gateway_accounts ga
		SET status = 'superseded', updated_at = now()
		FROM ranked
		WHERE ga.id = ranked.id AND ranked.rn > 1`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_gateway_accounts_provider_user_active
			ON gateway_accounts(provider, user_id)
			WHERE status = 'active'`,
		`WITH ranked AS (
			SELECT id, row_number() OVER (PARTITION BY provider, user_id, external_group_id ORDER BY id DESC) AS rn
			FROM gateway_api_keys
			WHERE status = 'active'
		)
		UPDATE gateway_api_keys gak
		SET status = 'superseded', updated_at = now()
		FROM ranked
		WHERE gak.id = ranked.id AND ranked.rn > 1`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_gateway_api_keys_provider_user_group_active
			ON gateway_api_keys(provider, user_id, external_group_id)
			WHERE status = 'active'`,
		`CREATE INDEX IF NOT EXISTS idx_wallet_transactions_user_id ON wallet_transactions(user_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_wallet_transactions_admin_idempotency
			ON wallet_transactions(reference_id)
			WHERE source = 'admin' AND reference_id LIKE 'idempotency:%'`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_groups_status ON gateway_groups(status)`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_groups_platform ON gateway_groups(platform)`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_channels_status ON gateway_channels(status)`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_upstream_accounts_status ON gateway_upstream_accounts(status)`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_upstream_accounts_platform ON gateway_upstream_accounts(platform)`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_group_models_group ON gateway_group_models(provider, external_group_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_group_models_model ON gateway_group_models(model_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_group_model_roles_group ON gateway_group_model_roles(provider, external_group_id, purpose, enabled)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_gateway_group_model_roles_default
			ON gateway_group_model_roles(provider, external_group_id, purpose)
			WHERE enabled = true AND is_default = true`,
		`CREATE INDEX IF NOT EXISTS idx_products_benefit_type ON products(benefit_type)`,
		`CREATE INDEX IF NOT EXISTS idx_products_for_sale ON products(for_sale)`,
		`CREATE INDEX IF NOT EXISTS idx_products_gateway_group_id ON products(gateway_group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_code_batches_product_id ON redeem_code_batches(product_id)`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_code_batches_order_ref ON redeem_code_batches(order_ref)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_redeem_code_batches_source_order_ref_unique
			ON redeem_code_batches(lower(source), lower(order_ref))
			WHERE order_ref <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_code_batches_created_at ON redeem_code_batches(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_codes_status ON redeem_codes(status)`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_codes_product_id ON redeem_codes(product_id)`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_codes_batch_id ON redeem_codes(batch_id)`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_codes_order_ref ON redeem_codes(order_ref)`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_codes_used_by_user_id ON redeem_codes(used_by_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_redemptions_user_id ON redeem_redemptions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_redemptions_status ON redeem_redemptions(status)`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_redemptions_gateway_error_class ON redeem_redemptions(gateway_error_class)`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_redemptions_gateway_error_retryable ON redeem_redemptions(gateway_error_retryable)`,
		`CREATE INDEX IF NOT EXISTS idx_redeem_redemptions_created_at ON redeem_redemptions(created_at DESC)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_redeem_redemptions_idempotency_key ON redeem_redemptions(idempotency_key) WHERE idempotency_key <> ''`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_gateway_operations_idempotency_key ON gateway_operations(idempotency_key) WHERE idempotency_key <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_operations_ready ON gateway_operations(status, next_run_at, id)`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_operations_redemption_id ON gateway_operations(redemption_id)`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_operations_last_error_class ON gateway_operations(last_error_class)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_model_catalog_status ON model_catalog(status)`,
	}

	for _, statement := range statements {
		if _, err := conn.Exec(ctx, statement); err != nil {
			return fmt.Errorf("apply schema statement: %w", err)
		}
	}

	if err := seedAdmin(ctx, conn, cfg); err != nil {
		return err
	}
	if err := seedCatalog(ctx, conn, cfg); err != nil {
		return err
	}
	if err := seedModelCatalog(ctx, conn); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, `
		INSERT INTO schema_migrations (version, applied_at)
		VALUES ($1, now())
		ON CONFLICT (version) DO NOTHING
	`, currentSchemaVersion); err != nil {
		return fmt.Errorf("record schema migration: %w", err)
	}
	return nil
}

func seedCatalog(ctx context.Context, pool schemaExecutor, cfg *config.Config) error {
	if cfg.Sub2APIDefaultGroupID > 0 {
		_, err := pool.Exec(ctx, `
			INSERT INTO gateway_groups (
				public_id, provider, external_group_id, name, description, platform,
				subscription_type, rate_multiplier, default_validity_days, status
			)
			VALUES ($1, 'sub2api', $2, 'Brevyn Official', 'Default Sub2API group for official Brevyn keys.', 'anthropic', 'standard', 1, 30, 'active')
			ON CONFLICT (provider, external_group_id) DO UPDATE
			SET name = excluded.name,
				description = excluded.description,
				platform = excluded.platform,
				updated_at = now()
		`, "gg_"+uuid.NewString(), cfg.Sub2APIDefaultGroupID)
		if err != nil {
			return fmt.Errorf("seed gateway group: %w", err)
		}
	} else if _, err := pool.Exec(ctx, `
		UPDATE gateway_groups
		SET status = 'inactive', updated_at = now()
		WHERE provider = 'sub2api' AND external_group_id = 0 AND name = 'Brevyn Official'
	`); err != nil {
		return fmt.Errorf("disable placeholder gateway group: %w", err)
	}

	return nil
}

func seedModelCatalog(ctx context.Context, pool schemaExecutor) error {
	models := []struct {
		id                string
		displayName       string
		providerFamily    string
		capabilitiesJSON  string
		publicVisible     bool
		supportsStreaming bool
	}{
		{
			id:                "claude-sonnet-4-5",
			displayName:       "Claude Sonnet 4.5",
			providerFamily:    "anthropic-compatible",
			capabilitiesJSON:  `["chat","vision_input"]`,
			publicVisible:     true,
			supportsStreaming: true,
		},
		{
			id:                "claude-haiku-4-5",
			displayName:       "Claude Haiku 4.5",
			providerFamily:    "anthropic-compatible",
			capabilitiesJSON:  `["chat","vision_input"]`,
			publicVisible:     true,
			supportsStreaming: true,
		},
		{
			id:                "deepseek-chat",
			displayName:       "DeepSeek Chat",
			providerFamily:    "anthropic-compatible",
			capabilitiesJSON:  `["chat"]`,
			publicVisible:     true,
			supportsStreaming: true,
		},
	}

	for _, model := range models {
		_, err := pool.Exec(ctx, `
			INSERT INTO model_catalog (
				model_id, display_name, provider_family, capabilities_json,
				public_visible, supports_streaming, status
			)
			VALUES ($1, $2, $3, $4, $5, $6, 'active')
			ON CONFLICT (model_id) DO UPDATE
			SET display_name = excluded.display_name,
				provider_family = excluded.provider_family,
				capabilities_json = excluded.capabilities_json,
				public_visible = excluded.public_visible,
				supports_streaming = excluded.supports_streaming,
				status = excluded.status,
				updated_at = now()
		`, model.id, model.displayName, model.providerFamily, model.capabilitiesJSON,
			model.publicVisible, model.supportsStreaming)
		if err != nil {
			return fmt.Errorf("seed model catalog %s: %w", model.id, err)
		}
	}

	return nil
}

func seedAdmin(ctx context.Context, pool schemaExecutor, cfg *config.Config) error {
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM admin_users`).Scan(&count); err != nil {
		return fmt.Errorf("count admin users: %w", err)
	}
	if count > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AdminSeedPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash admin password: %w", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO admin_users (public_id, email, password_hash, role, status)
		VALUES ($1, $2, $3, 'owner', 'active')
	`, "adm_"+uuid.NewString(), strings.ToLower(cfg.AdminSeedEmail), string(hash))
	if err != nil {
		return fmt.Errorf("seed admin user: %w", err)
	}
	return nil
}

func seedDemoUsers(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) error {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("demo-user-password"), bcrypt.MinCost)
	if err != nil {
		return fmt.Errorf("hash demo user password: %w", err)
	}

	demos := []struct {
		publicID string
		email    string
		status   string
		balance  float64
		devices  int
	}{
		{"u_9K2XQ", "student.one@example.com", "active", 18.42, 2},
		{"u_4M8RA", "student.two@example.com", "active", 6.10, 1},
		{"u_2P7LC", "student.three@example.com", "disabled", 0, 3},
	}

	for idx, demo := range demos {
		var userID int64
		err := pool.QueryRow(ctx, `
			INSERT INTO users (public_id, email, email_hash, password_hash, display_name, status)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (public_id) DO UPDATE
			SET status = excluded.status, updated_at = now()
			RETURNING id
		`, demo.publicID, demo.email, emailHash(demo.email), string(passwordHash), fmt.Sprintf("Demo User %d", idx+1), demo.status).Scan(&userID)
		if err != nil {
			return fmt.Errorf("seed demo user: %w", err)
		}

		_, err = pool.Exec(ctx, `
			INSERT INTO gateway_accounts (user_id, provider, external_user_id, external_email, default_group_id, status, last_synced_at)
			SELECT $1, 'sub2api', $2, $3, $4, 'active', now()
			WHERE NOT EXISTS (
				SELECT 1 FROM gateway_accounts WHERE user_id = $1 AND provider = 'sub2api'
			)
		`, userID, 1000+idx, fmt.Sprintf("%s@gateway.brevyn.internal", strings.ToLower(demo.publicID)), cfg.Sub2APIDefaultGroupID)
		if err != nil {
			return fmt.Errorf("seed gateway account: %w", err)
		}

		var existingDevices int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM devices WHERE user_id = $1`, userID).Scan(&existingDevices); err != nil {
			return fmt.Errorf("count demo devices: %w", err)
		}
		for deviceIdx := existingDevices; deviceIdx < demo.devices; deviceIdx++ {
			_, err = pool.Exec(ctx, `
				INSERT INTO devices (public_id, user_id, device_fingerprint_hash, name, platform, status, last_seen_at)
				VALUES ($1, $2, $3, $4, $5, 'active', now() - ($6 * interval '1 minute'))
			`, "dev_"+uuid.NewString(), userID, emailHash(fmt.Sprintf("%s-%d", demo.email, deviceIdx)), fmt.Sprintf("Device %d", deviceIdx+1), "macOS", 8+deviceIdx*17)
			if err != nil {
				return fmt.Errorf("seed device: %w", err)
			}
		}

		var existingSeedTransactions int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM wallet_transactions
			WHERE user_id = $1 AND source = 'seed' AND reference_id = 'demo'
		`, userID).Scan(&existingSeedTransactions); err != nil {
			return fmt.Errorf("count demo wallet transaction: %w", err)
		}
		if existingSeedTransactions > 0 {
			continue
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO wallet_transactions (public_id, user_id, kind, amount, balance_after, source, reference_id, notes)
			VALUES ($1, $2, 'grant', $3, $3, 'seed', 'demo', 'development demo balance')
		`, "wtx_"+uuid.NewString(), userID, demo.balance)
		if err != nil {
			return fmt.Errorf("seed wallet transaction: %w", err)
		}
	}

	return nil
}

func emailHash(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}
