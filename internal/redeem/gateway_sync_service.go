package redeem

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/config"
	"github.com/brevyn/brevyn-cloud/internal/gateway/gatewayerror"
	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	settingSub2APIBaseURL       = "sub2api.base_url"
	settingSub2APIAdminAPIKey   = "sub2api.admin_api_key"
	settingSub2APIAdminEmail    = "sub2api.admin_email"
	settingSub2APIAdminPassword = "sub2api.admin_password"
	settingSub2APIDefaultGroup  = "sub2api.default_group_id"
)

const ManagedUserConcurrency = 5

var gatewayUserLocks sync.Map

type GatewayUser struct {
	DBID        int64
	PublicID    string
	Email       string
	DisplayName string
	Status      string
}

type SyncTarget struct {
	DBID             int64
	PublicID         string
	User             GatewayUser
	Kind             string
	Value            float64
	ValidityDays     int
	ExternalGroupID  int64
	GatewayOperation string
	Status           string
	ProductName      string
	CreatedAt        time.Time
}

type GatewayAccountSummary struct {
	Provider       string
	ExternalUserID int64
	ExternalEmail  string
	DefaultGroupID int64
	Concurrency    int
	Status         string
	LastSyncedAt   *time.Time
}

type GatewayAPIKeySummary struct {
	ID              string
	Provider        string
	ExternalKeyID   int64
	ExternalGroupID int64
	MaskedAPIKey    string
	Status          string
	LastUsedAt      *time.Time
	CreatedAt       time.Time
}

type Sub2APISettings struct {
	BaseURL       string
	AdminAPIKey   string
	AdminEmail    string
	AdminPassword string
}

func (s *GatewaySyncService) GatewayAccount(ctx context.Context, userID int64) (*GatewayAccountSummary, error) {
	account, err := s.gatewayAccount(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &account, err
}

func (s *GatewaySyncService) ListGatewayAPIKeys(ctx context.Context, userID int64) ([]GatewayAPIKeySummary, error) {
	rows, err := s.postgres.Query(ctx, `
		SELECT public_id, provider, coalesce(external_key_id, 0), external_group_id, masked_api_key, status, last_used_at, created_at
		FROM gateway_api_keys
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []GatewayAPIKeySummary{}
	for rows.Next() {
		var item GatewayAPIKeySummary
		if err := rows.Scan(&item.ID, &item.Provider, &item.ExternalKeyID, &item.ExternalGroupID, &item.MaskedAPIKey, &item.Status, &item.LastUsedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *GatewaySyncService) ExistingOfficialGatewayCredential(ctx context.Context, userID int64, externalGroupID int64) (GatewayAccountSummary, *GatewayAPIKeySummary, string, bool, error) {
	account, err := s.gatewayAccount(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return GatewayAccountSummary{}, nil, "", false, nil
	}
	if err != nil {
		return GatewayAccountSummary{}, nil, "", false, err
	}
	if externalGroupID <= 0 {
		externalGroupID = account.DefaultGroupID
	}
	if externalGroupID <= 0 {
		return account, nil, "", false, nil
	}
	apiKey, encrypted, err := s.activeAPIKeyForGroup(ctx, userID, externalGroupID)
	if errors.Is(err, pgx.ErrNoRows) {
		return account, nil, "", false, nil
	}
	if err != nil {
		return account, nil, "", false, err
	}
	plain, err := s.decryptSecret(encrypted)
	if err != nil {
		return account, nil, "", false, err
	}
	return account, apiKey, plain, true, nil
}

type GatewaySyncService struct {
	cfg      *config.Config
	postgres *pgxpool.Pool
	redis    *redis.Client
}

func NewGatewaySyncService(cfg *config.Config, postgres *pgxpool.Pool, redisClients ...*redis.Client) *GatewaySyncService {
	var redisClient *redis.Client
	if len(redisClients) > 0 {
		redisClient = redisClients[0]
	}
	return &GatewaySyncService{cfg: cfg, postgres: postgres, redis: redisClient}
}

func (s *GatewaySyncService) InvalidateGatewayEntitlementsCache(ctx context.Context, userID int64) {
	if s == nil || s.redis == nil || userID <= 0 {
		return
	}
	userIDText := strconv.FormatInt(userID, 10)
	_ = s.redis.Del(ctx,
		"brevyn:gateway-entitlements:"+userIDText,
		"brevyn:gateway-entitlements:stale:"+userIDText,
		"brevyn:gateway-entitlements:force:"+userIDText,
	).Err()
}

func (s *GatewaySyncService) LoadSyncTarget(ctx context.Context, publicID string) (SyncTarget, error) {
	var target SyncTarget
	err := s.postgres.QueryRow(ctx, `
		SELECT
			rr.id,
			rr.public_id,
			rr.kind,
			rr.value,
			rr.validity_days,
			rr.external_group_id,
			rr.gateway_operation,
			rr.status,
			coalesce(p.name, ''),
			rr.created_at,
			u.id,
			u.public_id,
			u.email,
			u.display_name,
			u.status
		FROM redeem_redemptions rr
		JOIN users u ON u.id = rr.user_id
		LEFT JOIN products p ON p.id = rr.product_id
		WHERE rr.public_id = $1
	`, publicID).Scan(
		&target.DBID,
		&target.PublicID,
		&target.Kind,
		&target.Value,
		&target.ValidityDays,
		&target.ExternalGroupID,
		&target.GatewayOperation,
		&target.Status,
		&target.ProductName,
		&target.CreatedAt,
		&target.User.DBID,
		&target.User.PublicID,
		&target.User.Email,
		&target.User.DisplayName,
		&target.User.Status,
	)
	if err != nil {
		return target, err
	}
	if target.ExternalGroupID == 0 {
		target.ExternalGroupID = s.DefaultExternalGroupID(ctx)
	}
	if target.GatewayOperation == "" {
		switch target.Kind {
		case "balance":
			target.GatewayOperation = "add_balance"
		case "subscription":
			target.GatewayOperation = "assign_subscription"
		}
	}
	return target, nil
}

func (s *GatewaySyncService) SyncTargetToSub2API(ctx context.Context, target SyncTarget) (GatewayAccountSummary, string, error) {
	operation := normalizedGatewayOperation(target)
	if target.Kind != "balance" && target.Kind != "subscription" {
		return GatewayAccountSummary{}, operation, fmt.Errorf("unsupported redeem kind %s", target.Kind)
	}
	if target.Kind == "subscription" && (target.ExternalGroupID == 0 || target.ValidityDays <= 0) {
		return GatewayAccountSummary{}, operation, fmt.Errorf("subscription redemption is missing group or validity")
	}

	settings, err := s.LoadSub2APISettings(ctx)
	if err != nil {
		return GatewayAccountSummary{}, operation, gatewayerror.WithStage("settings", err)
	}
	client := s.NewSub2APIClient(settings)
	account, err := s.EnsureSub2APIAccountForGroup(ctx, client, target.User, target.ExternalGroupID)
	if err != nil {
		return GatewayAccountSummary{}, operation, gatewayerror.WithStage("ensure_user", err)
	}

	idempotencyKey := "brevyn-" + target.PublicID
	switch target.Kind {
	case "balance":
		operation = "add_balance"
		err = client.UpdateUserBalance(ctx, account.ExternalUserID, sub2api.BalanceRequest{
			Balance:   target.Value,
			Operation: "add",
			Notes:     "Brevyn retry " + target.PublicID,
		}, idempotencyKey)
	case "subscription":
		operation, err = syncSub2APISubscription(ctx, client, account.ExternalUserID, target, idempotencyKey)
	}
	if err != nil {
		return account, operation, gatewayerror.WithStage(operation, err)
	}
	return account, operation, nil
}

func normalizedGatewayOperation(target SyncTarget) string {
	operation := strings.TrimSpace(target.GatewayOperation)
	if operation != "" {
		return operation
	}
	switch target.Kind {
	case "balance":
		return "add_balance"
	case "subscription":
		return "assign_subscription"
	default:
		return "gateway"
	}
}

func syncSub2APISubscription(ctx context.Context, client *sub2api.Client, externalUserID int64, target SyncTarget, idempotencyKey string) (string, error) {
	subscription, found, err := findRenewableSub2APISubscription(ctx, client, externalUserID, target.ExternalGroupID)
	if err != nil {
		return "lookup_subscription", err
	}
	if found {
		_, err := client.ExtendSubscription(ctx, subscription.ID, sub2api.ExtendSubscriptionRequest{
			Days: target.ValidityDays,
		}, idempotencyKey)
		return "extend_subscription", err
	}
	err = client.AssignSubscriptionWithIdempotency(ctx, sub2api.AssignSubscriptionRequest{
		UserID:       externalUserID,
		GroupID:      target.ExternalGroupID,
		ValidityDays: target.ValidityDays,
		Notes:        "Brevyn retry " + target.PublicID,
	}, idempotencyKey)
	return "assign_subscription", err
}

func findRenewableSub2APISubscription(ctx context.Context, client *sub2api.Client, externalUserID, externalGroupID int64) (sub2api.AdminSubscription, bool, error) {
	subscriptions, _, err := client.ListSubscriptions(ctx, sub2api.SubscriptionListFilter{
		Page:      1,
		PageSize:  20,
		UserID:    externalUserID,
		GroupID:   externalGroupID,
		SortBy:    "expires_at",
		SortOrder: "desc",
	})
	if err != nil {
		return sub2api.AdminSubscription{}, false, err
	}
	if len(subscriptions) == 0 {
		return sub2api.AdminSubscription{}, false, nil
	}

	latest := subscriptions[0]
	for _, subscription := range subscriptions[1:] {
		if subscription.ExpiresAt.After(latest.ExpiresAt) {
			latest = subscription
			continue
		}
		if subscription.ExpiresAt.Equal(latest.ExpiresAt) && subscription.ID > latest.ID {
			latest = subscription
		}
	}
	return latest, true, nil
}

func (s *GatewaySyncService) EnsureSub2APIAccount(ctx context.Context, client *sub2api.Client, user GatewayUser, defaultGroupID int64) (GatewayAccountSummary, error) {
	unlock, err := s.lockGatewayUser(ctx, user.DBID)
	if err != nil {
		return GatewayAccountSummary{}, err
	}
	defer unlock()

	if defaultGroupID == 0 {
		defaultGroupID = s.DefaultExternalGroupID(ctx)
	}

	if account, err := s.gatewayAccount(ctx, user.DBID); err == nil && account.ExternalUserID > 0 {
		concurrency := account.Concurrency
		if concurrency <= 0 {
			concurrency = ManagedUserConcurrency
		}
		if defaultGroupID > 0 {
			if err := s.ensureSub2APIUserPolicy(ctx, client, account.ExternalUserID, user.DisplayName, defaultGroupID, concurrency); err != nil {
				return GatewayAccountSummary{}, err
			}
		}
		if defaultGroupID != 0 && account.DefaultGroupID != defaultGroupID {
			_, _ = s.postgres.Exec(ctx, `
				UPDATE gateway_accounts
				SET default_group_id = $3, concurrency = $4, last_synced_at = now(), updated_at = now()
				WHERE user_id = $1 AND provider = $2
			`, user.DBID, "sub2api", defaultGroupID, concurrency)
			account.DefaultGroupID = defaultGroupID
			account.Concurrency = concurrency
		} else {
			_, _ = s.postgres.Exec(ctx, `
				UPDATE gateway_accounts
				SET concurrency = $3, last_synced_at = now(), updated_at = now()
				WHERE user_id = $1 AND provider = $2 AND status = 'active'
			`, user.DBID, "sub2api", concurrency)
			account.Concurrency = concurrency
		}
		return account, nil
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return GatewayAccountSummary{}, err
	}

	allowedGroups := []int64{}
	if defaultGroupID > 0 {
		allowedGroups = append(allowedGroups, defaultGroupID)
	}
	externalUser, err := client.FindUserByEmail(ctx, user.Email)
	if err != nil {
		externalUser, err = client.CreateUser(ctx, sub2api.CreateUserRequest{
			Email:         user.Email,
			Password:      s.ShadowPassword(user.PublicID),
			Username:      user.DisplayName,
			Notes:         "Managed by Brevyn Cloud user " + user.PublicID,
			Balance:       0,
			Concurrency:   ManagedUserConcurrency,
			RPMLimit:      0,
			AllowedGroups: allowedGroups,
		})
		if err != nil {
			return GatewayAccountSummary{}, err
		}
	} else if defaultGroupID > 0 {
		if err := s.ensureSub2APIUserPolicy(ctx, client, externalUser.ID, user.DisplayName, defaultGroupID, ManagedUserConcurrency); err != nil {
			return GatewayAccountSummary{}, err
		}
	}

	var account GatewayAccountSummary
	err = s.postgres.QueryRow(ctx, `
		INSERT INTO gateway_accounts (user_id, provider, external_user_id, external_email, default_group_id, concurrency, status, last_synced_at)
		VALUES ($1, 'sub2api', $2, $3, $4, $5, 'active', now())
		RETURNING provider, external_user_id, external_email, default_group_id, concurrency, status, last_synced_at
	`, user.DBID, externalUser.ID, externalUser.Email, defaultGroupID, ManagedUserConcurrency).Scan(
		&account.Provider,
		&account.ExternalUserID,
		&account.ExternalEmail,
		&account.DefaultGroupID,
		&account.Concurrency,
		&account.Status,
		&account.LastSyncedAt,
	)
	if isUniqueViolation(err) {
		return s.gatewayAccount(ctx, user.DBID)
	}
	return account, err
}

func (s *GatewaySyncService) EnsureSub2APIAccountForGroup(ctx context.Context, client *sub2api.Client, user GatewayUser, externalGroupID int64) (GatewayAccountSummary, error) {
	unlock, err := s.lockGatewayUser(ctx, user.DBID)
	if err != nil {
		return GatewayAccountSummary{}, err
	}
	defer unlock()

	if externalGroupID == 0 {
		externalGroupID = s.DefaultExternalGroupID(ctx)
	}

	if account, err := s.gatewayAccount(ctx, user.DBID); err == nil && account.ExternalUserID > 0 {
		concurrency := account.Concurrency
		if concurrency <= 0 {
			concurrency = ManagedUserConcurrency
		}
		if externalGroupID > 0 {
			allowedGroups, groupErr := s.userAllowedExternalGroupIDs(ctx, user.DBID, externalGroupID)
			if groupErr != nil {
				allowedGroups = []int64{externalGroupID}
			}
			if err := s.ensureSub2APIUserPolicyWithGroups(ctx, client, account.ExternalUserID, user.DisplayName, allowedGroups, concurrency); err != nil {
				return GatewayAccountSummary{}, err
			}
		}
		if account.DefaultGroupID == 0 && externalGroupID > 0 {
			_, _ = s.postgres.Exec(ctx, `
				UPDATE gateway_accounts
				SET default_group_id = $3, concurrency = $4, last_synced_at = now(), updated_at = now()
				WHERE user_id = $1 AND provider = $2
			`, user.DBID, "sub2api", externalGroupID, concurrency)
			account.DefaultGroupID = externalGroupID
		} else {
			_, _ = s.postgres.Exec(ctx, `
				UPDATE gateway_accounts
				SET concurrency = $3, last_synced_at = now(), updated_at = now()
				WHERE user_id = $1 AND provider = $2 AND status = 'active'
			`, user.DBID, "sub2api", concurrency)
		}
		account.Concurrency = concurrency
		return account, nil
	} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return GatewayAccountSummary{}, err
	}

	allowedGroups := []int64{}
	if externalGroupID > 0 {
		allowedGroups = append(allowedGroups, externalGroupID)
	}
	externalUser, err := client.FindUserByEmail(ctx, user.Email)
	if err != nil {
		externalUser, err = client.CreateUser(ctx, sub2api.CreateUserRequest{
			Email:         user.Email,
			Password:      s.ShadowPassword(user.PublicID),
			Username:      user.DisplayName,
			Notes:         "Managed by Brevyn Cloud user " + user.PublicID,
			Balance:       0,
			Concurrency:   ManagedUserConcurrency,
			RPMLimit:      0,
			AllowedGroups: allowedGroups,
		})
		if err != nil {
			return GatewayAccountSummary{}, err
		}
	} else if externalGroupID > 0 {
		if err := s.ensureSub2APIUserPolicyWithGroups(ctx, client, externalUser.ID, user.DisplayName, allowedGroups, ManagedUserConcurrency); err != nil {
			return GatewayAccountSummary{}, err
		}
	}

	var account GatewayAccountSummary
	err = s.postgres.QueryRow(ctx, `
		INSERT INTO gateway_accounts (user_id, provider, external_user_id, external_email, default_group_id, concurrency, status, last_synced_at)
		VALUES ($1, 'sub2api', $2, $3, $4, $5, 'active', now())
		RETURNING provider, external_user_id, external_email, default_group_id, concurrency, status, last_synced_at
	`, user.DBID, externalUser.ID, externalUser.Email, externalGroupID, ManagedUserConcurrency).Scan(
		&account.Provider,
		&account.ExternalUserID,
		&account.ExternalEmail,
		&account.DefaultGroupID,
		&account.Concurrency,
		&account.Status,
		&account.LastSyncedAt,
	)
	if isUniqueViolation(err) {
		return s.gatewayAccount(ctx, user.DBID)
	}
	return account, err
}

func (s *GatewaySyncService) ensureSub2APIUserPolicy(ctx context.Context, client *sub2api.Client, externalUserID int64, displayName string, defaultGroupID int64, concurrency int) error {
	if externalUserID <= 0 || defaultGroupID <= 0 {
		return nil
	}
	if concurrency <= 0 {
		concurrency = ManagedUserConcurrency
	}
	allowedGroups := []int64{defaultGroupID}
	return s.ensureSub2APIUserPolicyWithGroups(ctx, client, externalUserID, displayName, allowedGroups, concurrency)
}

func (s *GatewaySyncService) ensureSub2APIUserPolicyWithGroups(ctx context.Context, client *sub2api.Client, externalUserID int64, displayName string, allowedGroups []int64, concurrency int) error {
	if externalUserID <= 0 {
		return nil
	}
	if concurrency <= 0 {
		concurrency = ManagedUserConcurrency
	}
	notes := "Managed by Brevyn Cloud"
	username := strings.TrimSpace(displayName)
	input := sub2api.UpdateUserRequest{
		Concurrency:   &concurrency,
		AllowedGroups: &allowedGroups,
		Notes:         &notes,
	}
	if username != "" {
		input.Username = &username
	}
	_, err := client.UpdateUser(ctx, externalUserID, input)
	return err
}

func (s *GatewaySyncService) EnsureGatewayAPIKeyForOperation(ctx context.Context, client *sub2api.Client, user GatewayUser, account GatewayAccountSummary, externalGroupID int64, operationPublicID string) error {
	_, _, err := s.EnsureGatewayAPIKeyForUser(ctx, client, user, account, externalGroupID)
	return err
}

func (s *GatewaySyncService) EnsureGatewayAPIKeyForUser(ctx context.Context, client *sub2api.Client, user GatewayUser, account GatewayAccountSummary, externalGroupID int64) (*GatewayAPIKeySummary, string, error) {
	unlock, err := s.lockGatewayUser(ctx, user.DBID)
	if err != nil {
		return nil, "", err
	}
	defer unlock()

	existing, encrypted, err := s.activeAPIKeyForGroup(ctx, user.DBID, externalGroupID)
	if err == nil && existing != nil {
		plain, decryptErr := s.decryptSecret(encrypted)
		if decryptErr != nil {
			return nil, "", decryptErr
		}
		return existing, plain, nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, "", err
	}

	userToken, err := client.UserLogin(ctx, account.ExternalEmail, s.ShadowPassword(user.PublicID))
	if err != nil {
		return nil, "", err
	}
	created, err := client.CreateUserAPIKey(ctx, userToken, sub2api.CreateAPIKeyRequest{
		Name:    "Brevyn App",
		GroupID: externalGroupID,
	}, "brevyn-key-"+strconv.FormatInt(user.DBID, 10)+"-"+strconv.FormatInt(externalGroupID, 10))
	if err != nil {
		return nil, "", err
	}

	encryptedKey, err := s.encryptSecret(created.Key)
	if err != nil {
		return nil, "", err
	}
	var item GatewayAPIKeySummary
	err = s.postgres.QueryRow(ctx, `
		INSERT INTO gateway_api_keys (
			public_id, user_id, gateway_account_id, provider, external_key_id,
			external_group_id, encrypted_api_key, masked_api_key, status
		)
		SELECT $1, $2, ga.id, 'sub2api', $3, $4, $5, $6, 'active'
		FROM gateway_accounts ga
		WHERE ga.user_id = $2 AND ga.provider = 'sub2api' AND ga.status = 'active'
		ORDER BY ga.id DESC
		LIMIT 1
		RETURNING public_id, provider, coalesce(external_key_id, 0), external_group_id, masked_api_key, status, last_used_at, created_at
	`, "gak_"+uuid.NewString(), user.DBID, created.ID, externalGroupID, encryptedKey, maskAPIKey(created.Key)).Scan(
		&item.ID,
		&item.Provider,
		&item.ExternalKeyID,
		&item.ExternalGroupID,
		&item.MaskedAPIKey,
		&item.Status,
		&item.LastUsedAt,
		&item.CreatedAt,
	)
	if isUniqueViolation(err) {
		existing, encrypted, existingErr := s.activeAPIKeyForGroup(ctx, user.DBID, externalGroupID)
		if existingErr == nil && existing != nil {
			plain, decryptErr := s.decryptSecret(encrypted)
			if decryptErr != nil {
				return nil, "", decryptErr
			}
			return existing, plain, nil
		}
		return nil, "", existingErr
	}
	if err != nil {
		return nil, "", err
	}
	return &item, created.Key, nil
}

func lockGatewayUser(userID int64) func() {
	value, _ := gatewayUserLocks.LoadOrStore(userID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *GatewaySyncService) lockGatewayUser(ctx context.Context, userID int64) (func(), error) {
	localUnlock := lockGatewayUser(userID)
	if s == nil || s.redis == nil {
		return localUnlock, nil
	}

	key := "brevyn:lock:gateway:user:" + strconv.FormatInt(userID, 10)
	token := uuid.NewString()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		locked, err := s.redis.SetNX(ctx, key, token, 2*time.Minute).Result()
		if err != nil {
			localUnlock()
			return nil, fmt.Errorf("gateway_user_lock_unavailable: %w", err)
		}
		if locked {
			return func() {
				releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				const releaseScript = `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) else return 0 end`
				_ = s.redis.Eval(releaseCtx, releaseScript, []string{key}, token).Err()
				localUnlock()
			}, nil
		}
		select {
		case <-ctx.Done():
			localUnlock()
			return nil, fmt.Errorf("gateway_user_lock_timeout: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (s *GatewaySyncService) UpdateRedemptionStatus(ctx context.Context, redemptionDBID int64, status string, errInfo gatewayerror.Info, externalUserID, externalGroupID int64, operation string) error {
	_, err := s.postgres.Exec(ctx, `
		UPDATE redeem_redemptions
		SET status = $2,
			error_message = $3,
			gateway_error_code = $4,
			gateway_error_class = $5,
			gateway_error_stage = $6,
			gateway_error_retryable = $7,
			gateway_error_detail = $8,
			external_user_id = $9,
			external_group_id = $10,
			gateway_operation = $11,
			updated_at = now()
		WHERE id = $1
	`, redemptionDBID, status, errInfo.Message, errInfo.Code, errInfo.Class, errInfo.Stage, errInfo.Retryable, errInfo.Detail, externalUserID, externalGroupID, operation)
	return err
}

func (s *GatewaySyncService) UpdateRedemptionError(ctx context.Context, redemptionDBID int64, errInfo gatewayerror.Info) error {
	_, err := s.postgres.Exec(ctx, `
		UPDATE redeem_redemptions
		SET error_message = $2,
			gateway_error_code = $3,
			gateway_error_class = $4,
			gateway_error_stage = $5,
			gateway_error_retryable = $6,
			gateway_error_detail = $7,
			updated_at = now()
		WHERE id = $1
	`, redemptionDBID, errInfo.Message, errInfo.Code, errInfo.Class, errInfo.Stage, errInfo.Retryable, errInfo.Detail)
	return err
}

func (s *GatewaySyncService) DefaultExternalGroupID(ctx context.Context) int64 {
	if groupID := s.configuredDefaultGroupID(ctx); groupID > 0 {
		return groupID
	}
	var groupID int64
	err := s.postgres.QueryRow(ctx, `
		SELECT external_group_id
		FROM gateway_groups
		WHERE provider = 'sub2api' AND status = 'active' AND subscription_type = 'standard' AND external_group_id > 0
		ORDER BY id ASC
		LIMIT 1
	`).Scan(&groupID)
	if err == nil {
		return groupID
	}
	return s.cfg.Sub2APIDefaultGroupID
}

func (s *GatewaySyncService) LoadSub2APISettings(ctx context.Context) (Sub2APISettings, error) {
	settings := Sub2APISettings{
		BaseURL:       strings.TrimRight(strings.TrimSpace(s.cfg.Sub2APIBaseURL), "/"),
		AdminAPIKey:   strings.TrimSpace(s.cfg.Sub2APIAdminAPIKey),
		AdminEmail:    strings.TrimSpace(s.cfg.Sub2APIAdminEmail),
		AdminPassword: s.cfg.Sub2APIAdminPassword,
	}
	rows, err := s.postgres.Query(ctx, `
		SELECT key, value, sensitive
		FROM app_settings
		WHERE key = ANY($1)
	`, []string{
		settingSub2APIBaseURL,
		settingSub2APIAdminAPIKey,
		settingSub2APIAdminEmail,
		settingSub2APIAdminPassword,
	})
	if err != nil {
		return settings, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		var sensitive bool
		if err := rows.Scan(&key, &value, &sensitive); err != nil {
			return settings, err
		}
		if sensitive && value != "" {
			decrypted, err := s.decryptSecret(value)
			if err != nil {
				return settings, err
			}
			value = decrypted
		}
		switch key {
		case settingSub2APIBaseURL:
			if strings.TrimSpace(value) != "" {
				settings.BaseURL = strings.TrimRight(strings.TrimSpace(value), "/")
			}
		case settingSub2APIAdminAPIKey:
			settings.AdminAPIKey = strings.TrimSpace(value)
		case settingSub2APIAdminEmail:
			settings.AdminEmail = strings.TrimSpace(value)
		case settingSub2APIAdminPassword:
			settings.AdminPassword = value
		}
	}
	if settings.BaseURL == "" {
		return settings, fmt.Errorf("sub2api base url is not configured")
	}
	if strings.TrimSpace(settings.AdminAPIKey) == "" && (strings.TrimSpace(settings.AdminEmail) == "" || strings.TrimSpace(settings.AdminPassword) == "") {
		return settings, fmt.Errorf("sub2api admin auth is not configured")
	}
	return settings, rows.Err()
}

func (s *GatewaySyncService) NewSub2APIClient(settings Sub2APISettings) *sub2api.Client {
	return sub2api.NewClient(sub2api.ClientConfig{
		BaseURL:       settings.BaseURL,
		AdminAPIKey:   settings.AdminAPIKey,
		AdminEmail:    settings.AdminEmail,
		AdminPassword: settings.AdminPassword,
	})
}

func (s *GatewaySyncService) ShadowPassword(userPublicID string) string {
	mac := hmac.New(sha256.New, []byte(firstNonEmpty(
		s.cfg.EncryptionKey,
		s.cfg.JWTAccessSecret,
		s.cfg.SessionSecret,
		"brevyn-dev-shadow-secret",
	)))
	mac.Write([]byte("sub2api-shadow-user:" + userPublicID))
	sum := mac.Sum(nil)
	return "Bv-" + base64.RawURLEncoding.EncodeToString(sum)[:30]
}

func (s *GatewaySyncService) gatewayAccount(ctx context.Context, userID int64) (GatewayAccountSummary, error) {
	var account GatewayAccountSummary
	err := s.postgres.QueryRow(ctx, `
		SELECT provider, external_user_id, external_email, default_group_id, concurrency, status, last_synced_at
		FROM gateway_accounts
		WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active'
		ORDER BY id DESC
		LIMIT 1
	`, userID).Scan(
		&account.Provider,
		&account.ExternalUserID,
		&account.ExternalEmail,
		&account.DefaultGroupID,
		&account.Concurrency,
		&account.Status,
		&account.LastSyncedAt,
	)
	return account, err
}

func (s *GatewaySyncService) activeGatewayAPIKeyExists(ctx context.Context, userDBID, externalGroupID int64) (bool, error) {
	var exists bool
	err := s.postgres.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM gateway_api_keys
			WHERE user_id = $1
				AND provider = 'sub2api'
				AND status = 'active'
				AND external_group_id = $2
		)
	`, userDBID, externalGroupID).Scan(&exists)
	return exists, err
}

func (s *GatewaySyncService) userAllowedExternalGroupIDs(ctx context.Context, userID int64, includeGroupID int64) ([]int64, error) {
	rows, err := s.postgres.Query(ctx, `
		SELECT DISTINCT external_group_id
		FROM (
			SELECT default_group_id AS external_group_id
			FROM gateway_accounts
			WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND default_group_id > 0
			UNION ALL
			SELECT external_group_id
			FROM gateway_api_keys
			WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND external_group_id > 0
			UNION ALL
			SELECT $2::bigint AS external_group_id
		) owned
		WHERE external_group_id > 0
		ORDER BY external_group_id ASC
	`, userID, includeGroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groupIDs := []int64{}
	for rows.Next() {
		var groupID int64
		if err := rows.Scan(&groupID); err != nil {
			return nil, err
		}
		groupIDs = append(groupIDs, groupID)
	}
	return groupIDs, rows.Err()
}

func (s *GatewaySyncService) activeAPIKeyForGroup(ctx context.Context, userID int64, externalGroupID int64) (*GatewayAPIKeySummary, string, error) {
	var item GatewayAPIKeySummary
	var encrypted string
	err := s.postgres.QueryRow(ctx, `
		SELECT public_id, provider, coalesce(external_key_id, 0), external_group_id,
			masked_api_key, status, last_used_at, created_at, encrypted_api_key
		FROM gateway_api_keys
		WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND external_group_id = $2
		ORDER BY id DESC
		LIMIT 1
	`, userID, externalGroupID).Scan(
		&item.ID,
		&item.Provider,
		&item.ExternalKeyID,
		&item.ExternalGroupID,
		&item.MaskedAPIKey,
		&item.Status,
		&item.LastUsedAt,
		&item.CreatedAt,
		&encrypted,
	)
	if err != nil {
		return nil, "", err
	}
	return &item, encrypted, nil
}

func (s *GatewaySyncService) configuredDefaultGroupID(ctx context.Context) int64 {
	var raw string
	err := s.postgres.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, settingSub2APIDefaultGroup).Scan(&raw)
	if err != nil {
		return 0
	}
	groupID, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || groupID <= 0 {
		return 0
	}
	var exists bool
	err = s.postgres.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM gateway_groups
			WHERE provider = 'sub2api'
				AND external_group_id = $1
				AND status = 'active'
				AND subscription_type = 'standard'
		)
	`, groupID).Scan(&exists)
	if err != nil || !exists {
		return 0
	}
	return groupID
}

func (s *GatewaySyncService) encryptSecret(plain string) (string, error) {
	block, err := aes.NewCipher(s.encryptionSecret())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "v1:" + base64.StdEncoding.EncodeToString(sealed), nil
}

func (s *GatewaySyncService) decryptSecret(value string) (string, error) {
	if !strings.HasPrefix(value, "v1:") {
		return value, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, "v1:"))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.encryptionSecret())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("encrypted setting is malformed")
	}
	nonce := raw[:gcm.NonceSize()]
	ciphertext := raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *GatewaySyncService) encryptionSecret() []byte {
	seed := firstNonEmpty(s.cfg.EncryptionKey, s.cfg.SessionSecret, s.cfg.JWTAccessSecret, s.cfg.AdminSeedPassword)
	sum := sha256.Sum256([]byte(seed))
	return sum[:]
}

func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "..." + key[len(key)-4:]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
