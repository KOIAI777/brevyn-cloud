package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env             string
	Port            string
	ShutdownTimeout time.Duration

	DatabaseURL      string
	RedisURL         string
	PostgresMaxConns int
	PostgresMinConns int

	AppBaseURL        string
	AdminBaseURL      string
	AllowedOrigins    []string
	TrustedProxies    []string
	AdminWebDir       string
	DeviceSoftLimit   int
	EncryptionKey     string
	SessionSecret     string
	JWTAccessSecret   string
	JWTRefreshSecret  string
	AdminSeedEmail    string
	AdminSeedPassword string

	Sub2APIBaseURL               string
	Sub2APIAdminAPIKey           string
	Sub2APIAdminEmail            string
	Sub2APIAdminPassword         string
	Sub2APIDefaultGroupID        int64
	RegisterProvisionConcurrency int
	Sub2APIOperationConcurrency  int
	Sub2APIOperationBatchSize    int
	Sub2APIOperationInterval     time.Duration
	Sub2APIOperationStaleTimeout time.Duration

	OfficialProviderBaseURL      string
	OfficialProviderDefaultModel string
}

func Load() (*Config, error) {
	cfg := &Config{
		Env:                          getenv("APP_ENV", "development"),
		Port:                         getenv("PORT", "4000"),
		ShutdownTimeout:              getDuration("SHUTDOWN_TIMEOUT", 10*time.Second),
		DatabaseURL:                  getenv("DATABASE_URL", "postgres://brevyn:brevyn@127.0.0.1:5432/brevyn_cloud?sslmode=disable"),
		RedisURL:                     getenv("REDIS_URL", "redis://127.0.0.1:6379/0"),
		PostgresMaxConns:             getInt("POSTGRES_MAX_CONNS", 30),
		PostgresMinConns:             getInt("POSTGRES_MIN_CONNS", 2),
		AppBaseURL:                   getenv("APP_BASE_URL", "http://127.0.0.1:4000"),
		AdminBaseURL:                 getenv("ADMIN_BASE_URL", "http://127.0.0.1:4000/admin"),
		AllowedOrigins:               getCSV("CORS_ALLOWED_ORIGINS", "http://127.0.0.1:5173,http://localhost:5173"),
		TrustedProxies:               getCSV("TRUSTED_PROXIES", ""),
		AdminWebDir:                  getenv("ADMIN_WEB_DIR", "./web/admin/dist"),
		DeviceSoftLimit:              getInt("DEVICE_SOFT_LIMIT", 3),
		Sub2APIBaseURL:               getenv("SUB2API_BASE_URL", "http://127.0.0.1:8080"),
		Sub2APIAdminAPIKey:           os.Getenv("SUB2API_ADMIN_API_KEY"),
		Sub2APIAdminEmail:            os.Getenv("SUB2API_ADMIN_EMAIL"),
		Sub2APIAdminPassword:         os.Getenv("SUB2API_ADMIN_PASSWORD"),
		RegisterProvisionConcurrency: getInt("REGISTER_PROVISION_CONCURRENCY", 10),
		Sub2APIOperationConcurrency:  getInt("SUB2API_OPERATION_CONCURRENCY", 5),
		Sub2APIOperationBatchSize:    getInt("SUB2API_OPERATION_BATCH_SIZE", 10),
		Sub2APIOperationInterval:     getDuration("SUB2API_OPERATION_INTERVAL", 10*time.Second),
		Sub2APIOperationStaleTimeout: getDuration("SUB2API_OPERATION_STALE_TIMEOUT", 5*time.Minute),
		EncryptionKey:                os.Getenv("ENCRYPTION_KEY"),
		SessionSecret:                os.Getenv("SESSION_SECRET"),
		JWTAccessSecret:              os.Getenv("JWT_ACCESS_SECRET"),
		JWTRefreshSecret:             os.Getenv("JWT_REFRESH_SECRET"),
		AdminSeedEmail:               getenv("ADMIN_SEED_EMAIL", "owner@brevyn.local"),
		AdminSeedPassword:            getenv("ADMIN_SEED_PASSWORD", "Brevyn@Admin2026"),
		OfficialProviderBaseURL: getenv(
			"OFFICIAL_PROVIDER_BASE_URL",
			"https://api.brevyn.org",
		),
		OfficialProviderDefaultModel: getenv("OFFICIAL_PROVIDER_DEFAULT_MODEL", ""),
	}

	groupID, err := strconv.ParseInt(getenv("SUB2API_DEFAULT_GROUP_ID", "0"), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse SUB2API_DEFAULT_GROUP_ID: %w", err)
	}
	cfg.Sub2APIDefaultGroupID = groupID

	if err := cfg.validateDeploymentSafety(); err != nil {
		return nil, err
	}

	if cfg.Env == "production" {
		if err := cfg.validateProduction(); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

func (c *Config) validateDeploymentSafety() error {
	if c.Env != "production" && (isPublicHTTPURL(c.AppBaseURL) || isPublicHTTPURL(c.AdminBaseURL)) {
		return fmt.Errorf("APP_ENV=production is required when APP_BASE_URL or ADMIN_BASE_URL is public")
	}
	return nil
}

func (c *Config) validateProduction() error {
	required := map[string]string{
		"ENCRYPTION_KEY":      c.EncryptionKey,
		"SESSION_SECRET":      c.SessionSecret,
		"JWT_ACCESS_SECRET":   c.JWTAccessSecret,
		"JWT_REFRESH_SECRET":  c.JWTRefreshSecret,
		"ADMIN_SEED_EMAIL":    c.AdminSeedEmail,
		"ADMIN_SEED_PASSWORD": c.AdminSeedPassword,
	}
	for name, value := range required {
		if strings.TrimSpace(value) == "" || value == "0" {
			return fmt.Errorf("%s is required in production", name)
		}
	}
	for name, value := range map[string]string{
		"ENCRYPTION_KEY":      c.EncryptionKey,
		"SESSION_SECRET":      c.SessionSecret,
		"JWT_ACCESS_SECRET":   c.JWTAccessSecret,
		"JWT_REFRESH_SECRET":  c.JWTRefreshSecret,
		"ADMIN_SEED_PASSWORD": c.AdminSeedPassword,
	} {
		if isKnownDevelopmentSecret(value) {
			return fmt.Errorf("%s must be changed from the development default in production", name)
		}
		if len(value) < 16 {
			return fmt.Errorf("%s must be at least 16 characters in production", name)
		}
	}
	if slicesContains(c.AllowedOrigins, "*") {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS must not contain * in production")
	}
	for _, origin := range c.AllowedOrigins {
		if err := validateProductionOrigin(origin); err != nil {
			return err
		}
	}
	if strings.TrimSpace(c.Sub2APIAdminAPIKey) == "" &&
		(strings.TrimSpace(c.Sub2APIAdminEmail) == "" || strings.TrimSpace(c.Sub2APIAdminPassword) == "") {
		return fmt.Errorf("SUB2API_ADMIN_API_KEY or SUB2API_ADMIN_EMAIL/SUB2API_ADMIN_PASSWORD is required in production")
	}
	for name, value := range map[string]string{
		"APP_BASE_URL":               c.AppBaseURL,
		"ADMIN_BASE_URL":             c.AdminBaseURL,
		"OFFICIAL_PROVIDER_BASE_URL": c.OfficialProviderBaseURL,
	} {
		if err := validateProductionPublicURL(name, value); err != nil {
			return err
		}
	}
	return nil
}

func validateProductionOrigin(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS contains invalid origin %q", value)
	}
	if parsed.Scheme != "https" && !isLocalHost(parsed.Hostname()) {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS origin %q must use https in production", value)
	}
	return nil
}

func validateProductionPublicURL(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required in production", name)
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be an absolute URL in production", name)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("%s must use https in production", name)
	}
	if isLocalHost(parsed.Hostname()) {
		return fmt.Errorf("%s must not point to a local address in production", name)
	}
	return nil
}

func isPublicHTTPURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	return !isLocalHost(parsed.Hostname())
}

func isLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "0.0.0.0" || host == "::1" || strings.HasPrefix(host, "127.")
}

func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}

func isKnownDevelopmentSecret(value string) bool {
	switch strings.TrimSpace(value) {
	case "change-me-access",
		"change-me-refresh",
		"change-me-session",
		"change-me-32-byte-secret",
		"Brevyn@Admin2026",
		"brevyn-dev-access-secret",
		"brevyn-dev-refresh-secret",
		"brevyn-dev-shadow-secret":
		return true
	default:
		return false
	}
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getCSV(key, fallback string) []string {
	raw := getenv(key, fallback)
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
