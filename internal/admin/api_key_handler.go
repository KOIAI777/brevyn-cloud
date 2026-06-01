package admin

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type adminGatewayAPIKeyRecord struct {
	DBID            int64
	ID              string
	Provider        string
	ExternalKeyID   int64
	ExternalGroupID int64
	MaskedAPIKey    string
	Status          string
	UserDBID        int64
	UserID          string
	UserEmail       string
	ExternalEmail   string
	LastUsedAt      *time.Time
	CreatedAt       time.Time
}

type adminGatewayAPIKeyResponse struct {
	ID              string     `json:"id"`
	Provider        string     `json:"provider"`
	ExternalKeyID   int64      `json:"externalKeyId"`
	ExternalGroupID int64      `json:"externalGroupId"`
	MaskedAPIKey    string     `json:"maskedApiKey"`
	Status          string     `json:"status"`
	UserID          string     `json:"userId"`
	UserEmail       string     `json:"userEmail"`
	RemoteSync      string     `json:"remoteSync"`
	SyncWarning     string     `json:"syncWarning,omitempty"`
	LastUsedAt      *time.Time `json:"lastUsedAt"`
	CreatedAt       time.Time  `json:"createdAt"`
}

func (h *Handler) ListUserAPIKeys(c *gin.Context) {
	userPublicID := strings.TrimSpace(c.Param("id"))
	if userPublicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_required"})
		return
	}

	records, err := h.keys.ListUserAPIKeys(c.Request.Context(), userPublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "api_keys_query_failed"})
		return
	}

	items := make([]adminGatewayAPIKeyResponse, 0, len(records))
	for _, record := range records {
		items = append(items, adminGatewayAPIKeyToResponse(record, "", ""))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) DisableAPIKey(c *gin.Context) {
	publicID := strings.TrimSpace(c.Param("id"))
	if publicID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api_key_required"})
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

	record, remoteSync, syncWarning, syncOperation, err := h.keys.DisableAPIKey(c.Request.Context(), publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "api_key_not_found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "api_key_disable_failed"})
		return
	}

	admin, _ := currentAdmin(c)
	h.writeAuditLog(
		c.Request.Context(),
		"admin",
		admin.ID,
		"gateway_api_key.disable",
		"gateway_api_key",
		record.ID,
		c.ClientIP(),
		c.Request.UserAgent(),
		auditMetadataWithReason(auditReason, map[string]any{
			"user_id":         record.UserID,
			"external_key_id": record.ExternalKeyID,
			"remote_sync":     remoteSync,
			"sync_warning":    syncWarning,
			"sync_operation":  syncOperation,
		}),
	)

	statusCode := http.StatusOK
	if syncWarning != "" {
		statusCode = http.StatusAccepted
	}
	c.JSON(statusCode, gin.H{
		"apiKey":        adminGatewayAPIKeyToResponse(record, remoteSync, syncWarning),
		"syncWarning":   syncWarning,
		"syncOperation": syncOperation,
	})
}

func scanAdminGatewayAPIKey(row scanner) (adminGatewayAPIKeyRecord, error) {
	var record adminGatewayAPIKeyRecord
	err := row.Scan(
		&record.DBID,
		&record.ID,
		&record.Provider,
		&record.ExternalKeyID,
		&record.ExternalGroupID,
		&record.MaskedAPIKey,
		&record.Status,
		&record.UserDBID,
		&record.UserID,
		&record.UserEmail,
		&record.ExternalEmail,
		&record.LastUsedAt,
		&record.CreatedAt,
	)
	return record, err
}

func adminGatewayAPIKeyToResponse(record adminGatewayAPIKeyRecord, remoteSync, syncWarning string) adminGatewayAPIKeyResponse {
	return adminGatewayAPIKeyResponse{
		ID:              record.ID,
		Provider:        record.Provider,
		ExternalKeyID:   record.ExternalKeyID,
		ExternalGroupID: record.ExternalGroupID,
		MaskedAPIKey:    record.MaskedAPIKey,
		Status:          record.Status,
		UserID:          record.UserID,
		UserEmail:       record.UserEmail,
		RemoteSync:      remoteSync,
		SyncWarning:     syncWarning,
		LastUsedAt:      record.LastUsedAt,
		CreatedAt:       record.CreatedAt,
	}
}
