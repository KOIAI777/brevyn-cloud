package config

import (
	"strings"
	"testing"
)

func TestLoadRejectsPublicDevelopmentURLs(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_BASE_URL", "https://cloud.example.com")
	t.Setenv("ADMIN_BASE_URL", "http://127.0.0.1:4000/admin")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "APP_ENV=production") {
		t.Fatalf("expected public development URL rejection, got %v", err)
	}
}

func TestLoadRejectsWildcardProductionCORS(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "*")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS") {
		t.Fatalf("expected wildcard CORS rejection, got %v", err)
	}
}

func TestLoadParsesTrustedProxies(t *testing.T) {
	t.Setenv("TRUSTED_PROXIES", "10.0.0.1, 172.16.0.0/12")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("expected 2 trusted proxies, got %d", len(cfg.TrustedProxies))
	}
	if cfg.TrustedProxies[0] != "10.0.0.1" || cfg.TrustedProxies[1] != "172.16.0.0/12" {
		t.Fatalf("unexpected trusted proxies: %#v", cfg.TrustedProxies)
	}
}

func TestProductionDefaultsDisableStartupMigrations(t *testing.T) {
	setProductionEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.MigrateOnStartup {
		t.Fatalf("expected production startup migrations to be disabled")
	}
}

func TestLoadRejectsProductionStartupMigrations(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("MIGRATE_ON_STARTUP", "true")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "MIGRATE_ON_STARTUP") {
		t.Fatalf("expected production startup migration rejection, got %v", err)
	}
}

func setProductionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_BASE_URL", "https://cloud.example.com")
	t.Setenv("ADMIN_BASE_URL", "https://cloud.example.com/admin")
	t.Setenv("OFFICIAL_PROVIDER_BASE_URL", "https://api.example.com")
	t.Setenv("ENCRYPTION_KEY", "test-encryption-secret-123")
	t.Setenv("SESSION_SECRET", "test-session-secret-123")
	t.Setenv("JWT_ACCESS_SECRET", "test-access-secret-123")
	t.Setenv("JWT_REFRESH_SECRET", "test-refresh-secret-123")
	t.Setenv("ADMIN_SEED_EMAIL", "owner@example.com")
	t.Setenv("ADMIN_SEED_PASSWORD", "test-admin-password-123")
	t.Setenv("SUB2API_ADMIN_API_KEY", "sub2api-admin-key-123")
}
