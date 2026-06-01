package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/audit"
	"github.com/brevyn/brevyn-cloud/internal/config"
	redeemsvc "github.com/brevyn/brevyn-cloud/internal/redeem"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

const adminSessionCookie = "brevyn_admin_session"

type Handler struct {
	cfg           *config.Config
	postgres      *pgxpool.Pool
	redis         *redis.Client
	audit         *audit.Service
	redeem        *redeemsvc.GatewaySyncService
	users         *UserService
	userDetails   *UserDetailService
	catalog       *CatalogService
	gatewayGroups *GatewayGroupService
	redeemQueries *RedeemQueryService
	dashboard     *DashboardQueryService
	auditQueries  *AuditQueryService
	operations    *GatewayOperationService
	diagnostics   *DiagnosticsService
	gateway       *GatewaySettingsService
	subscriptions *SubscriptionService
	keys          *GatewayKeyService
	sessions      *sessionManager
	limiter       *loginLimiter
}

type AdminPrincipal struct {
	ID       int64  `json:"-"`
	PublicID string `json:"id"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

func NewHandler(cfg *config.Config, postgres *pgxpool.Pool, redisClients ...*redis.Client) *Handler {
	secret := cfg.SessionSecret
	if strings.TrimSpace(secret) == "" {
		secret = cfg.JWTAccessSecret
	}
	var redisClient *redis.Client
	if len(redisClients) > 0 {
		redisClient = redisClients[0]
	}
	gateway := NewGatewaySettingsService(cfg, postgres)
	handler := &Handler{
		cfg:           cfg,
		postgres:      postgres,
		redis:         redisClient,
		audit:         audit.NewService(postgres),
		redeem:        redeemsvc.NewGatewaySyncService(cfg, postgres, redisClient),
		users:         NewUserService(postgres),
		userDetails:   NewUserDetailService(postgres),
		catalog:       NewCatalogService(postgres),
		gatewayGroups: NewGatewayGroupService(postgres),
		redeemQueries: NewRedeemQueryService(postgres),
		gateway:       gateway,
		dashboard:     NewDashboardQueryService(postgres, gateway),
		auditQueries:  NewAuditQueryService(postgres),
		operations:    NewGatewayOperationService(postgres),
		diagnostics:   NewDiagnosticsService(postgres, redisClient, gateway),
		subscriptions: NewSubscriptionService(gateway),
		limiter:       newLoginLimiter(5, 15*time.Minute, 15*time.Minute),
		sessions: &sessionManager{
			secret: []byte(secret),
			ttl:    12 * time.Hour,
			secure: cfg.Env == "production",
		},
	}
	handler.keys = NewGatewayKeyService(postgres, handler.gateway, handler.redeem)
	return handler
}

func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "surface": "admin"})
}

func (h *Handler) Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		TOTPCode string `json:"totpCode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	if retryAfter, blocked := h.adminLoginBlocked(c.Request.Context(), c.ClientIP(), req.Email); blocked {
		c.Header("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too_many_login_attempts"})
		return
	}

	loginRecord, err := h.findAdminLoginByEmail(c.Request.Context(), req.Email)
	if err != nil {
		h.recordAdminLoginFailure(c.Request.Context(), c.ClientIP(), req.Email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}
	admin := loginRecord.Admin
	if err := bcrypt.CompareHashAndPassword([]byte(loginRecord.PasswordHash), []byte(req.Password)); err != nil {
		h.recordAdminLoginFailure(c.Request.Context(), c.ClientIP(), req.Email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}

	if loginRecord.TOTPEnabled {
		if strings.TrimSpace(req.TOTPCode) == "" {
			c.JSON(http.StatusOK, gin.H{"totpRequired": true})
			return
		}
		ok, err := h.verifyAdminTOTP(loginRecord.TOTPSecretEncrypted, req.TOTPCode)
		if err != nil || !ok {
			h.recordAdminLoginFailure(c.Request.Context(), c.ClientIP(), req.Email)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_totp_code"})
			return
		}
	}

	if err := h.sessions.setCookie(c, admin); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session_create_failed"})
		return
	}

	h.recordAdminLoginSuccess(c.Request.Context(), c.ClientIP(), req.Email)
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "admin.login", "admin_user", admin.PublicID, c.ClientIP(), c.Request.UserAgent(), "{}")
	c.JSON(http.StatusOK, gin.H{"admin": admin, "totpRequired": false})
}

func (h *Handler) Logout(c *gin.Context) {
	h.sessions.clearCookie(c)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) Me(c *gin.Context) {
	admin, ok := currentAdmin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"admin": admin})
}

func (h *Handler) RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		admin, err := h.sessions.fromRequest(c)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		if err := h.ensureAdminStillActive(c.Request.Context(), admin); err != nil {
			h.sessions.clearCookie(c)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		c.Set("admin", admin)
		c.Next()
	}
}

func (h *Handler) ListUsers(c *gin.Context) {
	minBalance, hasMinBalance := parseOptionalFloat(c.Query("minBalance"))
	maxBalance, hasMaxBalance := parseOptionalFloat(c.Query("maxBalance"))
	items, total, err := h.users.List(c.Request.Context(), UserListFilter{
		Search:        c.Query("search"),
		Status:        c.Query("status"),
		SyncState:     c.Query("sync"),
		Limit:         parseBoundedInt(c.Query("limit"), 50, 1, 200),
		Offset:        parseBoundedInt(c.Query("offset"), 0, 0, 100000),
		GroupID:       parseOptionalInt64(c.Query("groupId")),
		MinBalance:    minBalance,
		HasMinBalance: hasMinBalance,
		MaxBalance:    maxBalance,
		HasMaxBalance: hasMaxBalance,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

func (h *Handler) GetUser(c *gin.Context) {
	publicID := strings.TrimSpace(c.Param("id"))
	user, err := h.users.Get(c.Request.Context(), publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (h *Handler) UpdateUser(c *gin.Context) {
	publicID := strings.TrimSpace(c.Param("id"))
	var req struct {
		Status             string `json:"status"`
		CascadeDisableKeys bool   `json:"cascadeDisableKeys"`
		AuditReason        string `json:"auditReason"`
		Reason             string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if req.Status != "active" && req.Status != "disabled" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_status"})
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}

	user, userDBID, err := h.users.UpdateStatus(c.Request.Context(), publicID, req.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user_update_failed"})
		return
	}

	admin, _ := currentAdmin(c)
	syncWarning := ""
	syncOperation := ""
	if req.Status == "disabled" && req.CascadeDisableKeys {
		disabledCount, warnings := h.keys.DisableUserActiveGatewayAPIKeys(c.Request.Context(), userDBID)
		if len(warnings) > 0 {
			syncWarning = fmt.Sprintf("disabled_%d_local_keys_remote_warnings: %s", disabledCount, strings.Join(warnings, "; "))
		} else {
			syncWarning = fmt.Sprintf("disabled_%d_gateway_keys", disabledCount)
		}
	}
	if err := h.keys.SyncUserStatusRemote(c.Request.Context(), publicID, req.Status); err != nil {
		policyInput := UserPolicySyncInput{UserPublicID: publicID, Status: req.Status}
		syncOperation, _ = h.keys.enqueueUserPolicyRetry(c.Request.Context(), adminGatewayUser{DBID: userDBID, PublicID: publicID, Email: user.Email, Status: req.Status}, policyInput, err)
		if syncWarning != "" {
			syncWarning += "; "
		}
		syncWarning += "remote_user_status_queued: " + err.Error()
	} else if syncWarning == "" {
		syncWarning = "remote_user_status_synced"
	}
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "user.local_update", "user", user.ID, c.ClientIP(), c.Request.UserAgent(), auditMetadataWithReason(auditReason, map[string]any{
		"status":               req.Status,
		"cascade_disable_keys": req.CascadeDisableKeys,
		"sync_warning":         syncWarning,
		"sync_operation":       syncOperation,
	}))
	c.JSON(http.StatusOK, gin.H{"user": user, "syncWarning": syncWarning, "syncOperation": syncOperation})
}

func (h *Handler) DeleteUser(c *gin.Context) {
	publicID := strings.TrimSpace(c.Param("id"))
	var req struct {
		AuditReason string `json:"auditReason"`
		Reason      string `json:"reason"`
	}
	if !bindOptionalJSON(c, &req) {
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}
	deleted, err := h.users.Delete(c.Request.Context(), publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user_delete_failed"})
		return
	}
	admin, _ := currentAdmin(c)
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "user.local_delete", "user", publicID, c.ClientIP(), c.Request.UserAgent(), auditMetadataWithReason(auditReason, map[string]any{
		"email": deleted.Email,
		"sync":  "local_only",
	}))
	c.JSON(http.StatusOK, gin.H{"status": "ok", "syncWarning": "local_user_delete_not_synced_to_sub2api"})
}

func (h *Handler) SyncSub2APIUsers(c *gin.Context) {
	var req struct {
		AuditReason string `json:"auditReason"`
		Reason      string `json:"reason"`
	}
	if !bindOptionalJSON(c, &req) {
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}
	settings, err := h.loadSub2APISettings(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "sub2api_not_configured", "detail": err.Error()})
		return
	}
	client := h.newSub2APIClient(settings)

	const pageSize = 100
	page := 1
	totalSeen := 0
	synced := 0
	skipped := 0
	balanceAdjusted := 0
	for {
		users, total, err := client.ListUsers(c.Request.Context(), page, pageSize, "")
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "sub2api_user_fetch_failed", "detail": err.Error()})
			return
		}
		if len(users) == 0 {
			break
		}
		for _, externalUser := range users {
			if externalUser.Role == "admin" {
				continue
			}
			totalSeen++
			applied, adjusted, err := h.users.SyncSub2APIUser(c.Request.Context(), externalUser)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "sub2api_user_sync_failed", "detail": err.Error()})
				return
			}
			if applied {
				synced++
			} else {
				skipped++
			}
			if adjusted {
				balanceAdjusted++
			}
		}
		if total > 0 && page*pageSize >= int(total) {
			break
		}
		if len(users) < pageSize {
			break
		}
		page++
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "sub2api.users.sync", "gateway_accounts", "sub2api", c.ClientIP(), c.Request.UserAgent(), auditMetadataWithReason(auditReason, map[string]any{
		"seen":             totalSeen,
		"synced":           synced,
		"skipped":          skipped,
		"balance_adjusted": balanceAdjusted,
	}))
	c.JSON(http.StatusOK, gin.H{
		"status":          "ok",
		"seen":            totalSeen,
		"synced":          synced,
		"skipped":         skipped,
		"balanceAdjusted": balanceAdjusted,
		"note":            "Only matched Brevyn users are synced; unmatched Sub2API users are skipped.",
	})
}

func (h *Handler) findAdminByEmail(ctx context.Context, email string) (AdminPrincipal, string, error) {
	var admin AdminPrincipal
	var passwordHash string
	err := h.postgres.QueryRow(ctx, `
		SELECT id, public_id, email, role, password_hash
		FROM admin_users
		WHERE email = $1 AND status = 'active'
	`, strings.ToLower(strings.TrimSpace(email))).Scan(&admin.ID, &admin.PublicID, &admin.Email, &admin.Role, &passwordHash)
	return admin, passwordHash, err
}

type adminLoginRecord struct {
	Admin               AdminPrincipal
	PasswordHash        string
	TOTPEnabled         bool
	TOTPSecretEncrypted string
}

func (h *Handler) findAdminLoginByEmail(ctx context.Context, email string) (adminLoginRecord, error) {
	var record adminLoginRecord
	err := h.postgres.QueryRow(ctx, `
		SELECT id, public_id, email, role, password_hash, totp_enabled, coalesce(totp_secret_encrypted, '')
		FROM admin_users
		WHERE email = $1 AND status = 'active'
	`, strings.ToLower(strings.TrimSpace(email))).Scan(
		&record.Admin.ID,
		&record.Admin.PublicID,
		&record.Admin.Email,
		&record.Admin.Role,
		&record.PasswordHash,
		&record.TOTPEnabled,
		&record.TOTPSecretEncrypted,
	)
	return record, err
}

func (h *Handler) ensureAdminStillActive(ctx context.Context, admin AdminPrincipal) error {
	var status string
	err := h.postgres.QueryRow(ctx, `
		SELECT status FROM admin_users WHERE id = $1 AND public_id = $2
	`, admin.ID, admin.PublicID).Scan(&status)
	if err != nil {
		return err
	}
	if status != "active" {
		return errors.New("admin inactive")
	}
	return nil
}

func currentAdmin(c *gin.Context) (AdminPrincipal, bool) {
	value, ok := c.Get("admin")
	if !ok {
		return AdminPrincipal{}, false
	}
	admin, ok := value.(AdminPrincipal)
	return admin, ok
}

func parseBoundedInt(raw string, fallback, min, max int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func parseOptionalInt64(raw string) int64 {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func parseOptionalFloat(raw string) (float64, bool) {
	if strings.TrimSpace(raw) == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

type sessionManager struct {
	secret []byte
	ttl    time.Duration
	secure bool
}

type sessionPayload struct {
	AdminID int64          `json:"aid"`
	Admin   AdminPrincipal `json:"admin"`
	Exp     int64          `json:"exp"`
}

func (s *sessionManager) setCookie(c *gin.Context, admin AdminPrincipal) error {
	token, err := s.sign(sessionPayload{
		AdminID: admin.ID,
		Admin:   admin,
		Exp:     time.Now().Add(s.ttl).Unix(),
	})
	if err != nil {
		return err
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(s.ttl.Seconds()),
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

func (s *sessionManager) clearCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *sessionManager) fromRequest(c *gin.Context) (AdminPrincipal, error) {
	cookie, err := c.Request.Cookie(adminSessionCookie)
	if err != nil {
		return AdminPrincipal{}, err
	}
	payload, err := s.verify(cookie.Value)
	if err != nil {
		return AdminPrincipal{}, err
	}
	return payload.Admin, nil
}

func (s *sessionManager) sign(payload sessionPayload) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	sig := s.signature(encoded)
	return encoded + "." + sig, nil
}

func (s *sessionManager) verify(token string) (sessionPayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return sessionPayload{}, errors.New("invalid token")
	}
	expected := s.signature(parts[0])
	if !hmac.Equal([]byte(parts[1]), []byte(expected)) {
		return sessionPayload{}, errors.New("invalid signature")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return sessionPayload{}, err
	}
	var payload sessionPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return sessionPayload{}, err
	}
	if time.Now().Unix() > payload.Exp {
		return sessionPayload{}, errors.New("session expired")
	}
	payload.Admin.ID = payload.AdminID
	return payload, nil
}

func (s *sessionManager) signature(encoded string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(encoded))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

type loginLimiter struct {
	mu            sync.Mutex
	maxFailures   int
	window        time.Duration
	blockDuration time.Duration
	attempts      map[string]loginAttempt
}

type loginAttempt struct {
	failures     int
	firstFailure time.Time
	blockedUntil time.Time
}

func newLoginLimiter(maxFailures int, window, blockDuration time.Duration) *loginLimiter {
	return &loginLimiter{
		maxFailures:   maxFailures,
		window:        window,
		blockDuration: blockDuration,
		attempts:      map[string]loginAttempt{},
	}
}

func (l *loginLimiter) blocked(ip, email string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	key := loginLimitKey(ip, email)
	attempt, ok := l.attempts[key]
	if !ok || attempt.blockedUntil.IsZero() {
		return 0, false
	}
	now := time.Now()
	if now.Before(attempt.blockedUntil) {
		return time.Until(attempt.blockedUntil), true
	}
	delete(l.attempts, key)
	return 0, false
}

func (l *loginLimiter) recordFailure(ip, email string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	key := loginLimitKey(ip, email)
	now := time.Now()
	l.pruneExpired(now)
	attempt := l.attempts[key]
	if attempt.firstFailure.IsZero() || now.Sub(attempt.firstFailure) > l.window {
		attempt = loginAttempt{firstFailure: now}
	}
	attempt.failures++
	if attempt.failures >= l.maxFailures {
		attempt.blockedUntil = now.Add(l.blockDuration)
	}
	l.attempts[key] = attempt
}

func (l *loginLimiter) recordSuccess(ip, email string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, loginLimitKey(ip, email))
}

func loginLimitKey(ip, email string) string {
	return strings.TrimSpace(ip) + "\x00" + strings.ToLower(strings.TrimSpace(email))
}

func (l *loginLimiter) pruneExpired(now time.Time) {
	for key, attempt := range l.attempts {
		if !attempt.blockedUntil.IsZero() && now.Before(attempt.blockedUntil) {
			continue
		}
		if now.Sub(attempt.firstFailure) > l.window {
			delete(l.attempts, key)
		}
	}
}
