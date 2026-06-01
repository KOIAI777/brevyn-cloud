package health

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Handler struct {
	postgres *pgxpool.Pool
	redis    *redis.Client
}

func NewHandler(postgres *pgxpool.Pool, redis *redis.Client) *Handler {
	return &Handler{postgres: postgres, redis: redis}
}

func (h *Handler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	if err := h.postgres.Ping(ctx); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "postgres": err.Error()})
		return
	}
	if err := h.redis.Ping(ctx).Err(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "redis": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}
