package admin

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	adminLoginPairFailureLimit = 5
	adminLoginIPFailureLimit   = 60
	adminLoginFailureWindow    = 15 * time.Minute
	adminLoginBlockDuration    = 15 * time.Minute
)

func (h *Handler) tryAdminJobLock(ctx context.Context, key string, ttl time.Duration) (bool, func(), error) {
	if h.redis == nil {
		return true, func() {}, nil
	}
	token := uuid.NewString()
	ok, err := h.redis.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return false, nil, err
	}
	if !ok {
		return false, func() {}, nil
	}
	unlock := func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.redis.Eval(releaseCtx, `
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
end
return 0
`, []string{key}, token).Err()
	}
	return true, unlock, nil
}

func (h *Handler) adminLoginBlocked(ctx context.Context, ip, email string) (time.Duration, bool) {
	if h.redis == nil {
		return h.limiter.blocked(ip, email)
	}
	pairTTL := h.adminLoginBlockTTL(ctx, adminLoginBlockKey("pair", ip, email))
	ipTTL := h.adminLoginBlockTTL(ctx, adminLoginBlockKey("ip", ip))
	if pairTTL <= 0 && ipTTL <= 0 {
		return 0, false
	}
	if ipTTL > pairTTL {
		return ipTTL, true
	}
	return pairTTL, true
}

func (h *Handler) recordAdminLoginFailure(ctx context.Context, ip, email string) {
	if h.redis == nil {
		h.limiter.recordFailure(ip, email)
		return
	}
	if err := h.incrementAdminLoginFailure(ctx, adminLoginFailKey("pair", ip, email), adminLoginBlockKey("pair", ip, email), adminLoginPairFailureLimit); err != nil {
		h.limiter.recordFailure(ip, email)
		return
	}
	_ = h.incrementAdminLoginFailure(ctx, adminLoginFailKey("ip", ip), adminLoginBlockKey("ip", ip), adminLoginIPFailureLimit)
}

func (h *Handler) recordAdminLoginSuccess(ctx context.Context, ip, email string) {
	if h.redis == nil {
		h.limiter.recordSuccess(ip, email)
		return
	}
	_ = h.redis.Del(ctx, adminLoginFailKey("pair", ip, email), adminLoginBlockKey("pair", ip, email)).Err()
}

func (h *Handler) adminLoginBlockTTL(ctx context.Context, key string) time.Duration {
	ttl, err := h.redis.TTL(ctx, key).Result()
	if err != nil || ttl <= 0 {
		return 0
	}
	return ttl
}

func (h *Handler) incrementAdminLoginFailure(ctx context.Context, failKey, blockKey string, maxFailures int64) error {
	count, err := h.redis.Incr(ctx, failKey).Result()
	if err != nil {
		return err
	}
	if count == 1 {
		if err := h.redis.Expire(ctx, failKey, adminLoginFailureWindow).Err(); err != nil {
			return err
		}
	}
	if count >= maxFailures {
		if err := h.redis.Set(ctx, blockKey, strconv.FormatInt(count, 10), adminLoginBlockDuration).Err(); err != nil {
			return err
		}
	}
	return nil
}

func adminLoginFailKey(scope string, values ...string) string {
	return "brevyn:rl:admin-login:fail:" + scope + ":" + adminRedisKeyDigest(values...)
}

func adminLoginBlockKey(scope string, values ...string) string {
	return "brevyn:rl:admin-login:block:" + scope + ":" + adminRedisKeyDigest(values...)
}

func adminRedisKeyDigest(values ...string) string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		normalized = append(normalized, strings.ToLower(strings.TrimSpace(value)))
	}
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x00")))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
