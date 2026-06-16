package httpapi

import (
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/admin"
	"github.com/brevyn/brevyn-cloud/internal/auth"
	"github.com/brevyn/brevyn-cloud/internal/config"
	"github.com/brevyn/brevyn-cloud/internal/health"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Dependencies struct {
	Health *health.Handler
	Admin  *admin.Handler
	Auth   *auth.Handler
}

func NewRouter(cfg *config.Config, logger *slog.Logger, deps Dependencies) http.Handler {
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		panic("invalid TRUSTED_PROXIES: " + err.Error())
	}
	router.Use(requestID())
	router.Use(gin.Recovery())
	router.Use(accessLog(logger))
	router.Use(securityHeaders())
	router.Use(cors(cfg.AllowedOrigins))
	router.Use(adminOriginGuard(cfg))

	router.GET("/healthz", deps.Health.Liveness)
	router.GET("/readyz", deps.Health.Readiness)

	v1 := router.Group("/api/v1")
	{
		authRoutes := v1.Group("/auth")
		{
			authRoutes.POST("/register", deps.Auth.Register)
			authRoutes.POST("/login", deps.Auth.Login)
			authRoutes.POST("/refresh", deps.Auth.Refresh)
			authRoutes.POST("/logout", deps.Auth.Logout)
		}

		userRoutes := v1.Group("")
		userRoutes.Use(deps.Auth.RequireUser())
		{
			userRoutes.GET("/me", deps.Auth.Me)
			userRoutes.GET("/me/groups", deps.Auth.Groups)
			userRoutes.GET("/me/gateway-entitlements", deps.Auth.GatewayEntitlements)
			userRoutes.GET("/me/wallet", deps.Auth.Wallet)
			userRoutes.GET("/me/api-keys", deps.Auth.APIKeys)
			userRoutes.GET("/provider/conversation", deps.Auth.ConversationProvider)
			userRoutes.GET("/provider/official", deps.Auth.OfficialProvider)
			userRoutes.POST("/redeem", deps.Auth.Redeem)
		}

		adminRoutes := v1.Group("/admin")
		{
			adminRoutes.GET("/health", deps.Admin.Health)
			adminRoutes.POST("/auth/login", deps.Admin.Login)
			adminRoutes.POST("/auth/logout", deps.Admin.Logout)

			protectedAdminRoutes := adminRoutes.Group("")
			protectedAdminRoutes.Use(deps.Admin.RequireAdmin())
			{
				protectedAdminRoutes.GET("/me", deps.Admin.Me)
				protectedAdminRoutes.GET("/security/totp", deps.Admin.GetTOTPStatus)
				protectedAdminRoutes.POST("/security/totp/setup", deps.Admin.SetupTOTP)
				protectedAdminRoutes.POST("/security/totp/enable", deps.Admin.EnableTOTP)
				protectedAdminRoutes.POST("/security/totp/disable", deps.Admin.DisableTOTP)
				protectedAdminRoutes.GET("/diagnostics", deps.Admin.Diagnostics)
				protectedAdminRoutes.GET("/overview", deps.Admin.Overview)
				protectedAdminRoutes.GET("/usage-summary", deps.Admin.UsageSummary)
				protectedAdminRoutes.GET("/users", deps.Admin.ListUsers)
				protectedAdminRoutes.POST("/users", deps.Admin.CreateUser)
				protectedAdminRoutes.POST("/users/sync-sub2api", deps.Admin.SyncSub2APIUsers)
				protectedAdminRoutes.POST("/users/import-sub2api", deps.Admin.ImportSub2APIUser)
				protectedAdminRoutes.GET("/users/:id", deps.Admin.GetUser)
				protectedAdminRoutes.PATCH("/users/:id", deps.Admin.UpdateUser)
				protectedAdminRoutes.DELETE("/users/:id", deps.Admin.DeleteUser)
				protectedAdminRoutes.GET("/users/:id/wallet-transactions", deps.Admin.ListUserWalletTransactions)
				protectedAdminRoutes.GET("/users/:id/devices", deps.Admin.ListUserDevices)
				protectedAdminRoutes.GET("/users/:id/gateway-accounts", deps.Admin.ListUserGatewayAccounts)
				protectedAdminRoutes.GET("/users/:id/subscriptions", deps.Admin.ListUserSubscriptions)
				protectedAdminRoutes.GET("/users/:id/api-keys", deps.Admin.ListUserAPIKeys)
				protectedAdminRoutes.POST("/users/:id/api-keys/rotate", deps.Admin.RotateUserAPIKey)
				protectedAdminRoutes.POST("/users/:id/gateway-group", deps.Admin.ChangeUserGatewayGroup)
				protectedAdminRoutes.POST("/users/:id/concurrency", deps.Admin.UpdateUserConcurrency)
				protectedAdminRoutes.POST("/users/:id/grant-balance", deps.Admin.GrantUserBalance)
				protectedAdminRoutes.GET("/sub2api/settings", deps.Admin.GetSub2APISettings)
				protectedAdminRoutes.PUT("/sub2api/settings", deps.Admin.UpdateSub2APISettings)
				protectedAdminRoutes.GET("/backups/config", deps.Admin.GetBackupConfig)
				protectedAdminRoutes.GET("/backups/s3-config", deps.Admin.GetBackupS3Config)
				protectedAdminRoutes.PUT("/backups/s3-config", deps.Admin.UpdateBackupS3Config)
				protectedAdminRoutes.POST("/backups/s3-config/test", deps.Admin.TestBackupS3Config)
				protectedAdminRoutes.GET("/backups/schedule", deps.Admin.GetBackupSchedule)
				protectedAdminRoutes.PUT("/backups/schedule", deps.Admin.UpdateBackupSchedule)
				protectedAdminRoutes.POST("/backups", deps.Admin.CreateBackup)
				protectedAdminRoutes.GET("/backups", deps.Admin.ListBackups)
				protectedAdminRoutes.GET("/backups/:id/download", deps.Admin.DownloadBackup)
				protectedAdminRoutes.GET("/backups/:id/download-url", deps.Admin.GetBackupDownloadURL)
				protectedAdminRoutes.POST("/backups/:id/restore", deps.Admin.RestoreBackup)
				protectedAdminRoutes.GET("/backups/:id", deps.Admin.GetBackup)
				protectedAdminRoutes.DELETE("/backups/:id", deps.Admin.DeleteBackup)
				protectedAdminRoutes.GET("/official-capabilities", deps.Admin.ListOfficialCapabilities)
				protectedAdminRoutes.PUT("/official-capabilities", deps.Admin.UpdateOfficialCapabilities)
				protectedAdminRoutes.POST("/sub2api/test", deps.Admin.TestSub2APIConnection)
				protectedAdminRoutes.POST("/sub2api/sync-groups", deps.Admin.SyncSub2APIGroups)
				protectedAdminRoutes.POST("/sub2api/sync-models", deps.Admin.SyncSub2APIModels)
				protectedAdminRoutes.GET("/gateway-groups", deps.Admin.ListGatewayGroups)
				protectedAdminRoutes.POST("/gateway-groups", deps.Admin.CreateGatewayGroup)
				protectedAdminRoutes.PUT("/gateway-groups/:externalGroupId/official-models", deps.Admin.UpdateGatewayGroupOfficialModels)
				protectedAdminRoutes.GET("/products", deps.Admin.ListProducts)
				protectedAdminRoutes.POST("/products", deps.Admin.CreateProduct)
				protectedAdminRoutes.PUT("/products/:id", deps.Admin.UpdateProduct)
				protectedAdminRoutes.DELETE("/products/:id", deps.Admin.DeleteProduct)
				protectedAdminRoutes.GET("/redeem-code-batches", deps.Admin.ListRedeemBatches)
				protectedAdminRoutes.POST("/redeem-code-batches/:id/disable", deps.Admin.DisableRedeemBatch)
				protectedAdminRoutes.GET("/redeem-codes", deps.Admin.ListRedeemCodes)
				protectedAdminRoutes.POST("/redeem-codes/generate", deps.Admin.GenerateRedeemCodes)
				protectedAdminRoutes.POST("/redeem-codes/:id/disable", deps.Admin.DisableRedeemCode)
				protectedAdminRoutes.GET("/redemptions", deps.Admin.ListRedemptions)
				protectedAdminRoutes.POST("/redemptions/:id/retry-sync", deps.Admin.RetryRedemptionSync)
				protectedAdminRoutes.GET("/subscriptions", deps.Admin.ListSubscriptions)
				protectedAdminRoutes.POST("/subscriptions/assign", deps.Admin.AssignSubscription)
				protectedAdminRoutes.POST("/subscriptions/:id/extend", deps.Admin.ExtendSubscription)
				protectedAdminRoutes.POST("/subscriptions/:id/reset-quota", deps.Admin.ResetSubscriptionQuota)
				protectedAdminRoutes.DELETE("/subscriptions/:id", deps.Admin.RevokeSubscription)
				protectedAdminRoutes.GET("/gateway-operations", deps.Admin.ListGatewayOperations)
				protectedAdminRoutes.POST("/gateway-operations/retry-failed", deps.Admin.RetryFailedGatewayOperations)
				protectedAdminRoutes.POST("/gateway-operations/:id/retry", deps.Admin.RetryGatewayOperation)
				protectedAdminRoutes.POST("/api-keys/:id/disable", deps.Admin.DisableAPIKey)
				protectedAdminRoutes.GET("/models", deps.Admin.ListModels)
				protectedAdminRoutes.GET("/audit-logs", deps.Admin.ListAuditLogs)
			}
		}
	}

	if _, err := os.Stat(filepath.Join(cfg.AdminWebDir, "index.html")); err == nil {
		router.GET("/admin", serveStaticWeb(cfg.AdminWebDir))
		router.GET("/admin/*filepath", serveStaticWeb(cfg.AdminWebDir))
		router.HEAD("/admin", serveStaticWeb(cfg.AdminWebDir))
		router.HEAD("/admin/*filepath", serveStaticWeb(cfg.AdminWebDir))
	}

	return router
}

func serveStaticWeb(dir string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		indexPath := filepath.Join(dir, "index.html")
		requested := c.Param("filepath")
		if requested == "" || requested == "/" {
			http.ServeFile(c.Writer, c.Request, indexPath)
			return
		}

		cleaned := strings.TrimPrefix(filepath.Clean(requested), string(filepath.Separator))
		assetPath := filepath.Join(dir, cleaned)
		if info, err := os.Stat(assetPath); err == nil && !info.IsDir() {
			http.ServeFile(c.Writer, c.Request, assetPath)
			return
		}

		http.ServeFile(c.Writer, c.Request, indexPath)
	}
}

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		c.Header("X-Request-Id", id)
		c.Set("request_id", id)
		c.Next()
	}
}

func accessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info("http request",
			"request_id", c.GetString("request_id"),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
			"ip", c.ClientIP(),
		)
	}
}

func securityHeaders() gin.HandlerFunc {
	const csp = "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'; " +
		"script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; " +
		"connect-src 'self'; form-action 'self'"
	return func(c *gin.Context) {
		c.Header("Content-Security-Policy", csp)
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Next()
	}
}

func cors(allowedOrigins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && slices.Contains(allowedOrigins, origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-Id, Idempotency-Key")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func adminOriginGuard(cfg *config.Config) gin.HandlerFunc {
	allowed := adminAllowedOrigins(cfg)
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, "/api/v1/admin") || !isUnsafeMethod(c.Request.Method) {
			c.Next()
			return
		}

		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin == "" {
			c.Next()
			return
		}
		if allowed["*"] || allowed[canonicalOrigin(origin)] || canonicalOrigin(origin) == requestOrigin(c.Request) {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin_origin_forbidden"})
	}
}

func adminAllowedOrigins(cfg *config.Config) map[string]bool {
	allowed := make(map[string]bool)
	for _, origin := range cfg.AdminAllowedOrigins {
		origin = canonicalOrigin(origin)
		if origin != "" {
			allowed[origin] = true
		}
	}
	for _, rawURL := range []string{cfg.AdminBaseURL} {
		origin := originFromURL(rawURL)
		if origin != "" {
			allowed[origin] = true
		}
	}
	return allowed
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func canonicalOrigin(origin string) string {
	origin = strings.TrimRight(strings.TrimSpace(origin), "/")
	if origin == "*" {
		return origin
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func originFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
}

func requestOrigin(req *http.Request) string {
	scheme := "http"
	if forwardedProto := firstHeaderValue(req.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
		scheme = strings.ToLower(forwardedProto)
	} else if req.TLS != nil {
		scheme = "https"
	}
	host := strings.ToLower(strings.TrimSpace(req.Host))
	return scheme + "://" + host
}

func firstHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if comma := strings.Index(value, ","); comma >= 0 {
		value = strings.TrimSpace(value[:comma])
	}
	return value
}
