package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

const (
	totpIssuer       = "Brevyn Cloud"
	totpSetupTTL     = 10 * time.Minute
	totpSetupKeyBase = "brevyn:admin:totp_setup"
)

type adminTOTPStatusResponse struct {
	Enabled bool `json:"enabled"`
}

type adminTOTPSetupResponse struct {
	Secret           string `json:"secret"`
	OtpauthURL       string `json:"otpauthUrl"`
	QRPNGDataURL     string `json:"qrPngDataUrl"`
	ExpiresInSeconds int    `json:"expiresInSeconds"`
}

func (h *Handler) GetTOTPStatus(c *gin.Context) {
	admin, ok := currentAdmin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	enabled, err := h.adminTOTPEnabled(c.Request.Context(), admin.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "totp_status_failed"})
		return
	}
	c.JSON(http.StatusOK, adminTOTPStatusResponse{Enabled: enabled})
}

func (h *Handler) SetupTOTP(c *gin.Context) {
	admin, ok := currentAdmin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if h.redis == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "redis_required"})
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      totpIssuer,
		AccountName: admin.Email,
		Period:      30,
		SecretSize:  20,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "totp_generate_failed"})
		return
	}
	encrypted, err := h.encryptSecret(key.Secret())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "totp_encrypt_failed"})
		return
	}
	if err := h.redis.Set(c.Request.Context(), adminTOTPSetupKey(admin.ID), encrypted, totpSetupTTL).Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "totp_setup_store_failed"})
		return
	}
	qrDataURL, err := totpQRCodeDataURL(key)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "totp_qr_failed"})
		return
	}
	c.JSON(http.StatusOK, adminTOTPSetupResponse{
		Secret:           key.Secret(),
		OtpauthURL:       key.URL(),
		QRPNGDataURL:     qrDataURL,
		ExpiresInSeconds: int(totpSetupTTL.Seconds()),
	})
}

func (h *Handler) EnableTOTP(c *gin.Context) {
	admin, ok := currentAdmin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	if h.redis == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "redis_required"})
		return
	}
	encrypted, err := h.redis.Get(c.Request.Context(), adminTOTPSetupKey(admin.ID)).Result()
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "totp_setup_expired"})
		return
	}
	ok, err = h.verifyAdminTOTP(encrypted, req.Code)
	if err != nil || !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_totp_code"})
		return
	}
	if _, err := h.postgres.Exec(c.Request.Context(), `
		UPDATE admin_users
		SET totp_enabled = true,
			totp_secret_encrypted = $2,
			updated_at = now()
		WHERE id = $1 AND status = 'active'
	`, admin.ID, encrypted); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "totp_enable_failed"})
		return
	}
	_ = h.redis.Del(c.Request.Context(), adminTOTPSetupKey(admin.ID)).Err()
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "admin.totp.enable", "admin_user", admin.PublicID, c.ClientIP(), c.Request.UserAgent(), "{}")
	c.JSON(http.StatusOK, adminTOTPStatusResponse{Enabled: true})
}

func (h *Handler) DisableTOTP(c *gin.Context) {
	admin, ok := currentAdmin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	var passwordHash string
	var enabled bool
	var encrypted string
	if err := h.postgres.QueryRow(c.Request.Context(), `
		SELECT password_hash, totp_enabled, coalesce(totp_secret_encrypted, '')
		FROM admin_users
		WHERE id = $1 AND status = 'active'
	`, admin.ID).Scan(&passwordHash, &enabled, &encrypted); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "totp_status_failed"})
		return
	}
	if !enabled {
		c.JSON(http.StatusOK, adminTOTPStatusResponse{Enabled: false})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}
	valid, err := h.verifyAdminTOTP(encrypted, req.Code)
	if err != nil || !valid {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_totp_code"})
		return
	}
	if _, err := h.postgres.Exec(c.Request.Context(), `
		UPDATE admin_users
		SET totp_enabled = false,
			totp_secret_encrypted = NULL,
			updated_at = now()
		WHERE id = $1
	`, admin.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "totp_disable_failed"})
		return
	}
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "admin.totp.disable", "admin_user", admin.PublicID, c.ClientIP(), c.Request.UserAgent(), "{}")
	c.JSON(http.StatusOK, adminTOTPStatusResponse{Enabled: false})
}

func (h *Handler) adminTOTPEnabled(ctx context.Context, adminID int64) (bool, error) {
	var enabled bool
	err := h.postgres.QueryRow(ctx, `SELECT totp_enabled FROM admin_users WHERE id = $1`, adminID).Scan(&enabled)
	return enabled, err
}

func (h *Handler) verifyAdminTOTP(encryptedSecret string, code string) (bool, error) {
	secret, err := h.decryptSecret(encryptedSecret)
	if err != nil {
		return false, err
	}
	return totp.ValidateCustom(sanitizeTOTPCode(code), secret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
}

func adminTOTPSetupKey(adminID int64) string {
	return fmt.Sprintf("%s:%d", totpSetupKeyBase, adminID)
}

func sanitizeTOTPCode(code string) string {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	code = strings.ReplaceAll(code, "-", "")
	return code
}

func totpQRCodeDataURL(key *otp.Key) (string, error) {
	img, err := key.Image(240, 240)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
