package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

func (h *Handler) GetBackupConfig(c *gin.Context) {
	cfg, err := h.backups.RuntimeConfig(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "backup_config_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"config": cfg})
}

func (h *Handler) GetBackupS3Config(c *gin.Context) {
	cfg, err := h.backups.GetS3Config(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "backup_s3_config_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"config": cfg})
}

func (h *Handler) UpdateBackupS3Config(c *gin.Context) {
	admin, ok := currentAdmin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req struct {
		Endpoint        string `json:"endpoint"`
		Region          string `json:"region"`
		Bucket          string `json:"bucket"`
		AccessKeyID     string `json:"accessKeyId"`
		SecretAccessKey string `json:"secretAccessKey"`
		Prefix          string `json:"prefix"`
		ForcePathStyle  bool   `json:"forcePathStyle"`
		AuditReason     string `json:"auditReason"`
		Reason          string `json:"reason"`
	}
	if !bindOptionalJSON(c, &req) {
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}
	cfg, err := h.backups.UpdateS3Config(c.Request.Context(), BackupS3Config{
		Endpoint:        req.Endpoint,
		Region:          req.Region,
		Bucket:          req.Bucket,
		AccessKeyID:     req.AccessKeyID,
		SecretAccessKey: req.SecretAccessKey,
		Prefix:          req.Prefix,
		ForcePathStyle:  req.ForcePathStyle,
	}, admin.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "backup_s3_update_failed", "detail": err.Error()})
		return
	}
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "cloud.backup.s3.update", "app_settings", "cloud_backup_s3_config", c.ClientIP(), c.Request.UserAgent(), auditMetadataWithReason(auditReason, map[string]any{
		"bucket": cfg.Bucket,
		"prefix": cfg.Prefix,
	}))
	c.JSON(http.StatusOK, gin.H{"config": cfg})
}

func (h *Handler) TestBackupS3Config(c *gin.Context) {
	var req BackupS3Config
	if !bindOptionalJSON(c, &req) {
		return
	}
	if err := h.backups.TestS3Connection(c.Request.Context(), req); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "message": backupErrorCode(err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "connection_successful"})
}

func (h *Handler) GetBackupSchedule(c *gin.Context) {
	cfg, err := h.backups.GetSchedule(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "backup_schedule_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"schedule": cfg})
}

func (h *Handler) UpdateBackupSchedule(c *gin.Context) {
	admin, ok := currentAdmin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req struct {
		Enabled     bool   `json:"enabled"`
		CronExpr    string `json:"cronExpr"`
		RetainDays  int    `json:"retainDays"`
		RetainCount int    `json:"retainCount"`
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
	cfg, err := h.backups.UpdateSchedule(c.Request.Context(), BackupScheduleConfig{
		Enabled:     req.Enabled,
		CronExpr:    req.CronExpr,
		RetainDays:  req.RetainDays,
		RetainCount: req.RetainCount,
	}, admin.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "backup_schedule_update_failed", "detail": err.Error()})
		return
	}
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "cloud.backup.schedule.update", "app_settings", "cloud_backup_schedule", c.ClientIP(), c.Request.UserAgent(), auditMetadataWithReason(auditReason, map[string]any{
		"enabled": cfg.Enabled,
		"cron":    cfg.CronExpr,
	}))
	c.JSON(http.StatusOK, gin.H{"schedule": cfg})
}

func (h *Handler) CreateBackup(c *gin.Context) {
	admin, ok := currentAdmin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req struct {
		ExpireDays  *int   `json:"expireDays"`
		AuditReason string `json:"auditReason"`
		Reason      string `json:"reason"`
	}
	if !bindOptionalJSON(c, &req) {
		return
	}
	auditReason := normalizeAuditReason(req.AuditReason, req.Reason)
	expireDays := h.cfg.BackupRetentionDays
	if req.ExpireDays != nil {
		expireDays = *req.ExpireDays
	}
	record, err := h.backups.StartBackup(c.Request.Context(), "manual", expireDays)
	if err != nil {
		writeBackupError(c, err)
		return
	}
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "cloud.backup.create", "backup_record", record.ID, c.ClientIP(), c.Request.UserAgent(), auditMetadataWithReason(auditReason, map[string]any{
		"expireDays": expireDays,
	}))
	c.JSON(http.StatusAccepted, gin.H{"backup": record})
}

func (h *Handler) ListBackups(c *gin.Context) {
	limit := parseBoundedInt(c.Query("limit"), 100, 1, 200)
	items, total, err := h.backups.ListBackups(c.Request.Context(), limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "backup_list_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total, "limit": limit, "offset": 0})
}

func (h *Handler) GetBackup(c *gin.Context) {
	record, err := h.backups.GetBackup(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeBackupError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"backup": record})
}

func (h *Handler) DeleteBackup(c *gin.Context) {
	admin, ok := currentAdmin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
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
	id := strings.TrimSpace(c.Param("id"))
	if err := h.backups.DeleteBackup(c.Request.Context(), id); err != nil {
		writeBackupError(c, err)
		return
	}
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "cloud.backup.delete", "backup_record", id, c.ClientIP(), c.Request.UserAgent(), auditMetadataWithReason(auditReason, nil))
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (h *Handler) DownloadBackup(c *gin.Context) {
	record, path, err := h.backups.LocalFileForDownload(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeBackupError(c, err)
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", `attachment; filename="`+record.FileName+`"`)
	c.File(path)
}

func (h *Handler) GetBackupDownloadURL(c *gin.Context) {
	url, err := h.backups.DownloadURL(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeBackupError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": url})
}

func (h *Handler) RestoreBackup(c *gin.Context) {
	admin, ok := currentAdmin(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req struct {
		Password     string `json:"password"`
		Confirmation string `json:"confirmation"`
		AuditReason  string `json:"auditReason"`
		Reason       string `json:"reason"`
	}
	if !bindOptionalJSON(c, &req) {
		return
	}
	if strings.TrimSpace(req.Confirmation) != "RESTORE" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "restore_confirmation_required"})
		return
	}
	auditReason, ok := requireAuditReason(c, req.AuditReason, req.Reason)
	if !ok {
		return
	}
	if err := h.verifyAdminPassword(c.Request.Context(), admin.ID, req.Password); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	record, err := h.backups.StartRestore(c.Request.Context(), id)
	if err != nil {
		writeBackupError(c, err)
		return
	}
	h.writeAuditLog(c.Request.Context(), "admin", admin.ID, "cloud.backup.restore", "backup_record", id, c.ClientIP(), c.Request.UserAgent(), auditMetadataWithReason(auditReason, nil))
	c.JSON(http.StatusAccepted, gin.H{"backup": record})
}

func (h *Handler) verifyAdminPassword(ctx context.Context, adminID int64, password string) error {
	var passwordHash string
	if err := h.postgres.QueryRow(ctx, `
		SELECT password_hash FROM admin_users WHERE id = $1 AND status = 'active'
	`, adminID).Scan(&passwordHash); err != nil {
		return err
	}
	return bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password))
}

func writeBackupError(c *gin.Context, err error) {
	code := backupErrorCode(err)
	switch {
	case errors.Is(err, errBackupNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": code})
	case errors.Is(err, errBackupInProgress), errors.Is(err, errRestoreInProgress):
		c.Header("Retry-After", strconv.Itoa(10))
		c.JSON(http.StatusConflict, gin.H{"error": code})
	case errors.Is(err, errBackupNotCompleted), errors.Is(err, errBackupS3NotConfigured):
		c.JSON(http.StatusConflict, gin.H{"error": code})
	case errors.Is(err, errRestoreDisabled):
		c.JSON(http.StatusForbidden, gin.H{"error": code})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": code, "detail": err.Error()})
	}
}

func backupErrorCode(err error) string {
	switch {
	case errors.Is(err, errBackupNotFound):
		return "backup_not_found"
	case errors.Is(err, errBackupInProgress):
		return "backup_in_progress"
	case errors.Is(err, errRestoreInProgress):
		return "restore_in_progress"
	case errors.Is(err, errBackupNotCompleted):
		return "backup_not_completed"
	case errors.Is(err, errBackupS3NotConfigured):
		return "backup_s3_not_configured"
	case errors.Is(err, errRestoreDisabled):
		return "restore_disabled"
	default:
		if strings.TrimSpace(err.Error()) != "" {
			return err.Error()
		}
		return "backup_operation_failed"
	}
}
