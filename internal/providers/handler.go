package providers

import (
	"net/http"

	"github.com/brevyn/brevyn-cloud/internal/config"
	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
	"github.com/gin-gonic/gin"
)

type Handler struct {
	cfg  *config.Config
	sub2 *sub2api.Client
}

func NewHandler(cfg *config.Config, sub2 *sub2api.Client) *Handler {
	return &Handler{cfg: cfg, sub2: sub2}
}

func (h *Handler) Official(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"error":       "official_provider_not_implemented",
		"base_url":    h.cfg.OfficialProviderBaseURL,
		"model":       h.cfg.OfficialProviderDefaultModel,
		"gateway_url": h.sub2.BaseURL(),
	})
}
