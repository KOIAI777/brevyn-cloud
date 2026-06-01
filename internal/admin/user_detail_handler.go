package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func (h *Handler) ListUserWalletTransactions(c *gin.Context) {
	userPublicID := strings.TrimSpace(c.Param("id"))
	if err := h.userDetails.EnsureUserExists(c.Request.Context(), userPublicID); errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user_query_failed"})
		return
	}

	items, err := h.userDetails.ListWalletTransactions(c.Request.Context(), userPublicID, parseBoundedInt(c.Query("limit"), 50, 1, 200))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "wallet_transactions_query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) ListUserDevices(c *gin.Context) {
	userPublicID := strings.TrimSpace(c.Param("id"))
	if err := h.userDetails.EnsureUserExists(c.Request.Context(), userPublicID); errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user_query_failed"})
		return
	}

	items, err := h.userDetails.ListDevices(c.Request.Context(), userPublicID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "devices_query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) ListUserGatewayAccounts(c *gin.Context) {
	userPublicID := strings.TrimSpace(c.Param("id"))
	if err := h.userDetails.EnsureUserExists(c.Request.Context(), userPublicID); errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user_query_failed"})
		return
	}

	items, err := h.userDetails.ListGatewayAccounts(c.Request.Context(), userPublicID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "gateway_accounts_query_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

func (h *Handler) ListUserSubscriptions(c *gin.Context) {
	userPublicID := strings.TrimSpace(c.Param("id"))
	if err := h.userDetails.EnsureUserExists(c.Request.Context(), userPublicID); errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "user_query_failed"})
		return
	}

	externalUserID, err := h.userDetails.ExternalSub2APIUserID(c.Request.Context(), userPublicID)
	if errors.Is(err, pgx.ErrNoRows) {
		c.JSON(http.StatusOK, gin.H{"items": []AdminSubscriptionItem{}, "total": 0})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "gateway_account_query_failed"})
		return
	}

	items, total, err := h.subscriptions.List(c.Request.Context(), SubscriptionListFilters{
		ExternalUserID: externalUserID,
		Limit:          100,
	})
	if err != nil {
		writeSubscriptionError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}
