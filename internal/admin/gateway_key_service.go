package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/brevyn/brevyn-cloud/internal/gateway/gatewayerror"
	"github.com/brevyn/brevyn-cloud/internal/gateway/operations"
	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
	redeemsvc "github.com/brevyn/brevyn-cloud/internal/redeem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type GatewayKeyService struct {
	postgres *pgxpool.Pool
	gateway  *GatewaySettingsService
	redeem   *redeemsvc.GatewaySyncService
}

type BalanceGrantResult struct {
	User          adminGatewayUser
	TransactionID string
	BalanceAfter  float64
	SyncWarning   string
	SyncOperation string
}

type RotatedAPIKeyResult struct {
	User            adminGatewayUser
	Record          adminGatewayAPIKeyRecord
	PlainAPIKey     string
	ExternalGroupID int64
	DisabledCount   int
	Warnings        []string
}

type ChangedGatewayGroupResult struct {
	User            adminGatewayUser
	Record          adminGatewayAPIKeyRecord
	PlainAPIKey     string
	ExternalGroupID int64
	DisabledCount   int
	Warnings        []string
}

type UpdatedUserConcurrencyResult struct {
	User            adminGatewayUser
	ExternalGroupID int64
	Concurrency     int
	SyncOperation   string
}

type UserPolicySyncInput struct {
	UserPublicID    string `json:"user_public_id"`
	Status          string `json:"status,omitempty"`
	ExternalGroupID int64  `json:"external_group_id,omitempty"`
	Concurrency     int    `json:"concurrency,omitempty"`
}

func NewGatewayKeyService(postgres *pgxpool.Pool, gateway *GatewaySettingsService, redeem *redeemsvc.GatewaySyncService) *GatewayKeyService {
	return &GatewayKeyService{postgres: postgres, gateway: gateway, redeem: redeem}
}

func (s *GatewayKeyService) ListUserAPIKeys(ctx context.Context, userPublicID string) ([]adminGatewayAPIKeyRecord, error) {
	var userDBID int64
	if err := s.postgres.QueryRow(ctx, `
		SELECT id FROM users WHERE public_id = $1
	`, userPublicID).Scan(&userDBID); err != nil {
		return nil, err
	}

	rows, err := s.postgres.Query(ctx, `
		SELECT
			gak.id,
			gak.public_id,
			gak.provider,
			coalesce(gak.external_key_id, 0),
			gak.external_group_id,
			gak.masked_api_key,
			gak.status,
			u.id,
			u.public_id,
			u.email,
			coalesce(ga.external_email, ''),
			gak.last_used_at,
			gak.created_at
		FROM gateway_api_keys gak
		JOIN users u ON u.id = gak.user_id
		LEFT JOIN gateway_accounts ga ON ga.id = gak.gateway_account_id
		WHERE gak.user_id = $1
		ORDER BY gak.created_at DESC
	`, userDBID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []adminGatewayAPIKeyRecord{}
	for rows.Next() {
		record, err := scanAdminGatewayAPIKey(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *GatewayKeyService) DisableAPIKey(ctx context.Context, publicID string) (adminGatewayAPIKeyRecord, string, string, string, error) {
	record, err := s.findGatewayAPIKey(ctx, publicID)
	if err != nil {
		return adminGatewayAPIKeyRecord{}, "", "", "", err
	}

	if err := s.disableGatewayAPIKeyLocally(ctx, record.DBID); err != nil {
		return adminGatewayAPIKeyRecord{}, "", "", "", err
	}
	record.Status = "disabled"

	remoteSync := "skipped"
	syncWarning := ""
	syncOperation := ""
	if record.Provider == "sub2api" && record.ExternalKeyID > 0 {
		if err := s.disableSub2APIKey(ctx, record); err != nil {
			remoteSync = "failed"
			syncWarning = err.Error()
			syncOperation, _ = s.enqueueDisableAPIKeyRetry(ctx, record, err)
		} else {
			remoteSync = "disabled"
		}
	}
	return record, remoteSync, syncWarning, syncOperation, nil
}

func (s *GatewayKeyService) GrantUserBalance(ctx context.Context, userPublicID string, adminID int64, amount float64, notes string, syncRemote bool, idempotencyKey string) (BalanceGrantResult, error) {
	user, err := s.loadAdminGatewayUserByPublicID(ctx, userPublicID)
	if err != nil {
		return BalanceGrantResult{}, err
	}
	if user.Status != "active" {
		return BalanceGrantResult{User: user}, fmt.Errorf("user_not_active")
	}

	transactionID, balanceAfter, err := s.insertAdminBalanceGrant(ctx, user.DBID, adminID, amount, notes, idempotencyKey)
	if err != nil {
		return BalanceGrantResult{}, err
	}

	result := BalanceGrantResult{
		User:          user,
		TransactionID: transactionID,
		BalanceAfter:  balanceAfter,
	}
	if syncRemote {
		if err := s.SyncAdminBalanceGrantRemote(ctx, user.PublicID, transactionID, amount); err != nil {
			result.SyncWarning = err.Error()
			result.SyncOperation, _ = s.enqueueBalanceGrantRetry(ctx, user, transactionID, amount, err)
		}
	}
	return result, nil
}

func (s *GatewayKeyService) SyncAdminBalanceGrantRemote(ctx context.Context, userPublicID, transactionID string, amount float64) error {
	user, err := s.loadAdminGatewayUserByPublicID(ctx, userPublicID)
	if err != nil {
		return err
	}
	if user.Status != "active" {
		return fmt.Errorf("user_not_active")
	}
	defaultGroupID := s.preferredExternalGroupID(ctx, user.DBID)
	settings, err := s.gateway.Load(ctx)
	if err != nil {
		return err
	}
	client := s.gateway.NewSub2APIClient(settings)
	account, err := s.redeem.EnsureSub2APIAccount(ctx, client, user, defaultGroupID)
	if err != nil {
		return err
	}
	return client.UpdateUserBalance(ctx, account.ExternalUserID, sub2api.BalanceRequest{
		Balance:   amount,
		Operation: "add",
		Notes:     "Brevyn admin grant " + transactionID,
	}, "brevyn-admin-grant-"+transactionID)
}

func (s *GatewayKeyService) SyncDisabledAPIKeyRemote(ctx context.Context, publicID string) error {
	record, err := s.findGatewayAPIKey(ctx, publicID)
	if err != nil {
		return err
	}
	return s.disableSub2APIKey(ctx, record)
}

func (s *GatewayKeyService) RotateUserAPIKey(ctx context.Context, userPublicID string, externalGroupID int64) (RotatedAPIKeyResult, error) {
	user, err := s.loadAdminGatewayUserByPublicID(ctx, userPublicID)
	if err != nil {
		return RotatedAPIKeyResult{}, err
	}
	if user.Status != "active" {
		return RotatedAPIKeyResult{User: user}, fmt.Errorf("user_not_active")
	}

	if externalGroupID == 0 {
		externalGroupID = s.preferredExternalGroupID(ctx, user.DBID)
	}
	if externalGroupID == 0 {
		return RotatedAPIKeyResult{User: user}, fmt.Errorf("gateway_group_not_configured")
	}

	settings, err := s.gateway.Load(ctx)
	if err != nil {
		return RotatedAPIKeyResult{User: user, ExternalGroupID: externalGroupID}, fmt.Errorf("sub2api_not_configured: %w", err)
	}
	client := s.gateway.NewSub2APIClient(settings)
	account, err := s.redeem.EnsureSub2APIAccount(ctx, client, user, externalGroupID)
	if err != nil {
		return RotatedAPIKeyResult{User: user, ExternalGroupID: externalGroupID}, fmt.Errorf("gateway_account_sync_failed: %w", err)
	}

	disabledCount, warnings := s.DisableUserGatewayAPIKeysForGroup(ctx, user.DBID, externalGroupID)
	userToken, err := client.UserLogin(ctx, account.ExternalEmail, s.redeem.ShadowPassword(user.PublicID))
	if err != nil {
		return RotatedAPIKeyResult{User: user, ExternalGroupID: externalGroupID, DisabledCount: disabledCount, Warnings: warnings}, fmt.Errorf("gateway_user_login_failed: %w", err)
	}
	created, err := client.CreateUserAPIKey(ctx, userToken, sub2api.CreateAPIKeyRequest{
		Name:    "Brevyn App Rotated",
		GroupID: externalGroupID,
	}, "brevyn-key-rotate-"+uuid.NewString())
	if err != nil {
		return RotatedAPIKeyResult{User: user, ExternalGroupID: externalGroupID, DisabledCount: disabledCount, Warnings: warnings}, fmt.Errorf("gateway_key_create_failed: %w", err)
	}
	record, err := s.insertAdminGatewayAPIKey(ctx, user.DBID, created.ID, externalGroupID, created.Key)
	if err != nil {
		return RotatedAPIKeyResult{User: user, ExternalGroupID: externalGroupID, DisabledCount: disabledCount, Warnings: warnings}, fmt.Errorf("gateway_key_store_failed: %w", err)
	}

	return RotatedAPIKeyResult{
		User:            user,
		Record:          record,
		PlainAPIKey:     created.Key,
		ExternalGroupID: externalGroupID,
		DisabledCount:   disabledCount,
		Warnings:        warnings,
	}, nil
}

func (s *GatewayKeyService) ChangeUserGatewayGroup(ctx context.Context, userPublicID string, externalGroupID int64) (ChangedGatewayGroupResult, error) {
	user, err := s.loadAdminGatewayUserByPublicID(ctx, userPublicID)
	if err != nil {
		return ChangedGatewayGroupResult{}, err
	}
	if user.Status != "active" {
		return ChangedGatewayGroupResult{User: user}, fmt.Errorf("user_not_active")
	}
	if externalGroupID <= 0 {
		return ChangedGatewayGroupResult{User: user}, fmt.Errorf("gateway_group_required")
	}
	if err := s.validateStandardGatewayGroup(ctx, externalGroupID); err != nil {
		return ChangedGatewayGroupResult{User: user, ExternalGroupID: externalGroupID}, err
	}

	settings, err := s.gateway.Load(ctx)
	if err != nil {
		return ChangedGatewayGroupResult{User: user, ExternalGroupID: externalGroupID}, fmt.Errorf("sub2api_not_configured: %w", err)
	}
	client := s.gateway.NewSub2APIClient(settings)
	account, err := s.redeem.EnsureSub2APIAccount(ctx, client, user, externalGroupID)
	if err != nil {
		return ChangedGatewayGroupResult{User: user, ExternalGroupID: externalGroupID}, fmt.Errorf("gateway_account_sync_failed: %w", err)
	}

	disabledCount, warnings := s.DisableUserGatewayAPIKeysForGroup(ctx, user.DBID, 0)
	userToken, err := client.UserLogin(ctx, account.ExternalEmail, s.redeem.ShadowPassword(user.PublicID))
	if err != nil {
		return ChangedGatewayGroupResult{User: user, ExternalGroupID: externalGroupID, DisabledCount: disabledCount, Warnings: warnings}, fmt.Errorf("gateway_user_login_failed: %w", err)
	}
	created, err := client.CreateUserAPIKey(ctx, userToken, sub2api.CreateAPIKeyRequest{
		Name:    "Brevyn App Group",
		GroupID: externalGroupID,
	}, "brevyn-key-group-"+uuid.NewString())
	if err != nil {
		return ChangedGatewayGroupResult{User: user, ExternalGroupID: externalGroupID, DisabledCount: disabledCount, Warnings: warnings}, fmt.Errorf("gateway_key_create_failed: %w", err)
	}
	record, err := s.insertAdminGatewayAPIKey(ctx, user.DBID, created.ID, externalGroupID, created.Key)
	if err != nil {
		return ChangedGatewayGroupResult{User: user, ExternalGroupID: externalGroupID, DisabledCount: disabledCount, Warnings: warnings}, fmt.Errorf("gateway_key_store_failed: %w", err)
	}

	return ChangedGatewayGroupResult{
		User:            user,
		Record:          record,
		PlainAPIKey:     created.Key,
		ExternalGroupID: externalGroupID,
		DisabledCount:   disabledCount,
		Warnings:        warnings,
	}, nil
}

func (s *GatewayKeyService) UpdateUserConcurrency(ctx context.Context, userPublicID string, concurrency int) (UpdatedUserConcurrencyResult, error) {
	if concurrency < 1 || concurrency > 500 {
		return UpdatedUserConcurrencyResult{}, fmt.Errorf("concurrency_must_be_1_to_500")
	}
	user, err := s.loadAdminGatewayUserByPublicID(ctx, userPublicID)
	if err != nil {
		return UpdatedUserConcurrencyResult{}, err
	}
	if user.Status != "active" {
		return UpdatedUserConcurrencyResult{User: user}, fmt.Errorf("user_not_active")
	}
	externalGroupID := s.preferredExternalGroupID(ctx, user.DBID)
	input := UserPolicySyncInput{
		UserPublicID:    user.PublicID,
		ExternalGroupID: externalGroupID,
		Concurrency:     concurrency,
	}
	if err := s.SyncUserPolicyRemote(ctx, input); err != nil {
		operationID, _ := s.enqueueUserPolicyRetry(ctx, user, input, err)
		return UpdatedUserConcurrencyResult{User: user, ExternalGroupID: externalGroupID, Concurrency: concurrency, SyncOperation: operationID}, err
	}
	return UpdatedUserConcurrencyResult{User: user, ExternalGroupID: externalGroupID, Concurrency: concurrency}, nil
}

func (s *GatewayKeyService) SyncUserPolicyRemote(ctx context.Context, input UserPolicySyncInput) error {
	input.UserPublicID = strings.TrimSpace(input.UserPublicID)
	input.Status = strings.TrimSpace(strings.ToLower(input.Status))
	user, err := s.loadAdminGatewayUserByPublicID(ctx, input.UserPublicID)
	if err != nil {
		return err
	}
	settings, err := s.gateway.Load(ctx)
	if err != nil {
		return err
	}
	client := s.gateway.NewSub2APIClient(settings)

	externalGroupID := input.ExternalGroupID
	var account adminGatewayAccountSummary
	if externalGroupID > 0 {
		account, err = s.redeem.EnsureSub2APIAccount(ctx, client, user, externalGroupID)
		if err != nil {
			return err
		}
	} else {
		account, err = s.gatewayAccountByUser(ctx, user.DBID)
		if errors.Is(err, pgx.ErrNoRows) {
			if input.Status == "disabled" {
				return nil
			}
			externalGroupID = s.preferredExternalGroupID(ctx, user.DBID)
			account, err = s.redeem.EnsureSub2APIAccount(ctx, client, user, externalGroupID)
		}
		if err != nil {
			return err
		}
		if externalGroupID == 0 {
			externalGroupID = account.DefaultGroupID
		}
	}

	update := sub2api.UpdateUserRequest{}
	if input.Status != "" {
		update.Status = input.Status
	}
	if input.Concurrency > 0 {
		concurrency := input.Concurrency
		update.Concurrency = &concurrency
	}
	if externalGroupID > 0 && input.ExternalGroupID > 0 {
		allowedGroups := []int64{externalGroupID}
		update.AllowedGroups = &allowedGroups
	}
	if update.Status == "" && update.Concurrency == nil && update.AllowedGroups == nil {
		return nil
	}
	if _, err := client.UpdateUser(ctx, account.ExternalUserID, update); err != nil {
		return err
	}

	statusSQL := "status"
	args := []any{user.DBID, "sub2api", externalGroupID, input.Concurrency}
	if input.Status != "" {
		args = append(args, input.Status)
		statusSQL = fmt.Sprintf("$%d", len(args))
	}
	_, err = s.postgres.Exec(ctx, `
		UPDATE gateway_accounts
		SET default_group_id = CASE WHEN $3::bigint > 0 THEN $3 ELSE default_group_id END,
			concurrency = CASE WHEN $4::int > 0 THEN $4 ELSE concurrency END,
			status = `+statusSQL+`,
			last_synced_at = now(),
			updated_at = now()
		WHERE user_id = $1 AND provider = $2 AND external_user_id > 0
	`, args...)
	return err
}

func (s *GatewayKeyService) SyncUserStatusRemote(ctx context.Context, userPublicID, status string) error {
	return s.SyncUserPolicyRemote(ctx, UserPolicySyncInput{
		UserPublicID: userPublicID,
		Status:       status,
	})
}

func (s *GatewayKeyService) DisableUserActiveGatewayAPIKeys(ctx context.Context, userDBID int64) (int, []string) {
	return s.DisableUserGatewayAPIKeysForGroup(ctx, userDBID, 0)
}

func (s *GatewayKeyService) DisableUserGatewayAPIKeysForGroup(ctx context.Context, userDBID, externalGroupID int64) (int, []string) {
	records, err := s.activeGatewayAPIKeyRecords(ctx, userDBID, externalGroupID)
	if err != nil {
		return 0, []string{err.Error()}
	}
	warnings := []string{}
	disabled := 0
	for _, record := range records {
		if err := s.disableGatewayAPIKeyLocally(ctx, record.DBID); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s local: %s", record.ID, err.Error()))
			continue
		}
		disabled++
		if record.Provider == "sub2api" && record.ExternalKeyID > 0 {
			if err := s.disableSub2APIKey(ctx, record); err != nil {
				operationID, _ := s.enqueueDisableAPIKeyRetry(ctx, record, err)
				if operationID != "" {
					warnings = append(warnings, fmt.Sprintf("%s remote queued %s: %s", record.ID, operationID, err.Error()))
				} else {
					warnings = append(warnings, fmt.Sprintf("%s remote: %s", record.ID, err.Error()))
				}
			}
		}
	}
	return disabled, warnings
}

func (s *GatewayKeyService) enqueueBalanceGrantRetry(ctx context.Context, user adminGatewayUser, transactionID string, amount float64, syncErr error) (string, error) {
	return operations.CreateFailed(ctx, s.postgres, operations.CreateFailedInput{
		Operation:      operations.OperationSyncAdminBalanceGrant,
		TargetType:     "wallet_transaction",
		TargetID:       transactionID,
		UserDBID:       user.DBID,
		IdempotencyKey: "gateway:" + operations.OperationSyncAdminBalanceGrant + ":" + transactionID,
		Payload: map[string]any{
			"user_public_id": user.PublicID,
			"transaction_id": transactionID,
			"amount":         amount,
		},
		Error: gatewayerror.Classify(operations.OperationSyncAdminBalanceGrant, syncErr),
	})
}

func (s *GatewayKeyService) enqueueDisableAPIKeyRetry(ctx context.Context, record adminGatewayAPIKeyRecord, syncErr error) (string, error) {
	return operations.CreateFailed(ctx, s.postgres, operations.CreateFailedInput{
		Operation:      operations.OperationDisableAPIKeyRemote,
		TargetType:     "gateway_api_key",
		TargetID:       record.ID,
		UserDBID:       record.UserDBID,
		IdempotencyKey: "gateway:" + operations.OperationDisableAPIKeyRemote + ":" + record.ID,
		Payload: map[string]any{
			"api_key_id": record.ID,
		},
		Error: gatewayerror.Classify(operations.OperationDisableAPIKeyRemote, syncErr),
	})
}

func (s *GatewayKeyService) enqueueUserPolicyRetry(ctx context.Context, user adminGatewayUser, input UserPolicySyncInput, syncErr error) (string, error) {
	idempotencyKey := fmt.Sprintf("gateway:%s:%s:%s:%d:%d", operations.OperationSyncUserPolicyRemote, user.PublicID, input.Status, input.ExternalGroupID, input.Concurrency)
	return operations.CreateFailed(ctx, s.postgres, operations.CreateFailedInput{
		Operation:      operations.OperationSyncUserPolicyRemote,
		TargetType:     "user",
		TargetID:       user.PublicID,
		UserDBID:       user.DBID,
		IdempotencyKey: idempotencyKey,
		Payload:        input,
		Error:          gatewayerror.Classify(operations.OperationSyncUserPolicyRemote, syncErr),
	})
}

func (s *GatewayKeyService) findGatewayAPIKey(ctx context.Context, publicID string) (adminGatewayAPIKeyRecord, error) {
	var record adminGatewayAPIKeyRecord
	err := s.postgres.QueryRow(ctx, `
		SELECT
			gak.id,
			gak.public_id,
			gak.provider,
			coalesce(gak.external_key_id, 0),
			gak.external_group_id,
			gak.masked_api_key,
			gak.status,
			u.id,
			u.public_id,
			u.email,
			coalesce(ga.external_email, ''),
			gak.last_used_at,
			gak.created_at
		FROM gateway_api_keys gak
		JOIN users u ON u.id = gak.user_id
		LEFT JOIN LATERAL (
			SELECT external_email
			FROM gateway_accounts
			WHERE user_id = u.id AND provider = gak.provider
			ORDER BY id DESC
			LIMIT 1
		) ga ON true
		WHERE gak.public_id = $1
	`, publicID).Scan(
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

func (s *GatewayKeyService) disableGatewayAPIKeyLocally(ctx context.Context, dbID int64) error {
	_, err := s.postgres.Exec(ctx, `
		UPDATE gateway_api_keys
		SET status = 'disabled',
			encrypted_api_key = '',
			updated_at = now()
		WHERE id = $1
	`, dbID)
	return err
}

func (s *GatewayKeyService) disableSub2APIKey(ctx context.Context, record adminGatewayAPIKeyRecord) error {
	externalEmail := strings.TrimSpace(record.ExternalEmail)
	if externalEmail == "" {
		externalEmail = strings.TrimSpace(record.UserEmail)
	}
	if externalEmail == "" {
		return fmt.Errorf("sub2api external email is missing")
	}

	settings, err := s.gateway.Load(ctx)
	if err != nil {
		return err
	}
	client := s.gateway.NewSub2APIClient(settings)
	userToken, err := client.UserLogin(ctx, externalEmail, s.redeem.ShadowPassword(record.UserID))
	if err != nil {
		return err
	}
	disabled := "disabled"
	_, err = client.UpdateUserAPIKey(ctx, userToken, record.ExternalKeyID, sub2api.UpdateAPIKeyRequest{Status: &disabled})
	return err
}

func (s *GatewayKeyService) activeGatewayAPIKeyRecords(ctx context.Context, userDBID, externalGroupID int64) ([]adminGatewayAPIKeyRecord, error) {
	where := "gak.user_id = $1 AND gak.provider = 'sub2api' AND gak.status = 'active'"
	args := []any{userDBID}
	if externalGroupID > 0 {
		args = append(args, externalGroupID)
		where += fmt.Sprintf(" AND gak.external_group_id = $%d", len(args))
	}
	rows, err := s.postgres.Query(ctx, `
		SELECT
			gak.id,
			gak.public_id,
			gak.provider,
			coalesce(gak.external_key_id, 0),
			gak.external_group_id,
			gak.masked_api_key,
			gak.status,
			u.id,
			u.public_id,
			u.email,
			coalesce(ga.external_email, ''),
			gak.last_used_at,
			gak.created_at
		FROM gateway_api_keys gak
		JOIN users u ON u.id = gak.user_id
		LEFT JOIN gateway_accounts ga ON ga.id = gak.gateway_account_id
		WHERE `+where+`
		ORDER BY gak.id DESC
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []adminGatewayAPIKeyRecord{}
	for rows.Next() {
		record, err := scanAdminGatewayAPIKey(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (s *GatewayKeyService) loadAdminGatewayUserByPublicID(ctx context.Context, publicID string) (adminGatewayUser, error) {
	var user adminGatewayUser
	err := s.postgres.QueryRow(ctx, `
		SELECT id, public_id, email, display_name, status
		FROM users
		WHERE public_id = $1
	`, publicID).Scan(&user.DBID, &user.PublicID, &user.Email, &user.DisplayName, &user.Status)
	return user, err
}

func (s *GatewayKeyService) insertAdminBalanceGrant(ctx context.Context, userDBID, adminID int64, amount float64, notes string, idempotencyKey string) (string, float64, error) {
	tx, err := s.postgres.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userDBID); err != nil {
		return "", 0, err
	}
	referenceID := fmt.Sprintf("admin:%d", adminID)
	if idem := adminBalanceGrantReferenceID(adminID, userDBID, idempotencyKey); idem != "" {
		referenceID = idem
		transactionID, balanceAfter, err := loadAdminBalanceGrantByReference(ctx, tx, userDBID, referenceID)
		if err == nil {
			if err := tx.Commit(ctx); err != nil {
				return "", 0, err
			}
			return transactionID, balanceAfter, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", 0, err
		}
	}
	var balanceAfter float64
	if err := tx.QueryRow(ctx, `
		SELECT coalesce(sum(amount), 0) + $2
		FROM wallet_transactions
		WHERE user_id = $1
	`, userDBID, amount).Scan(&balanceAfter); err != nil {
		return "", 0, err
	}

	transactionID := "wtx_" + uuid.NewString()
	commandTag, err := tx.Exec(ctx, `
		INSERT INTO wallet_transactions (public_id, user_id, kind, amount, balance_after, source, reference_id, notes)
		VALUES ($1, $2, 'admin_grant', $3, $4, 'admin', $5, $6)
		ON CONFLICT DO NOTHING
	`, transactionID, userDBID, amount, balanceAfter, referenceID, notes)
	if err != nil {
		return "", 0, err
	}
	if commandTag.RowsAffected() == 0 {
		transactionID, balanceAfter, err = loadAdminBalanceGrantByReference(ctx, tx, userDBID, referenceID)
		if err != nil {
			return "", 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", 0, err
	}
	return transactionID, balanceAfter, nil
}

func loadAdminBalanceGrantByReference(ctx context.Context, tx pgx.Tx, userDBID int64, referenceID string) (string, float64, error) {
	var transactionID string
	var balanceAfter float64
	err := tx.QueryRow(ctx, `
		SELECT public_id, balance_after
		FROM wallet_transactions
		WHERE user_id = $1 AND source = 'admin' AND reference_id = $2
		LIMIT 1
	`, userDBID, referenceID).Scan(&transactionID, &balanceAfter)
	return transactionID, balanceAfter, err
}

func adminBalanceGrantReferenceID(adminID, userDBID int64, idempotencyKey string) string {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(idempotencyKey))
	return fmt.Sprintf("idempotency:balance-grant:admin:%d:user:%d:%s", adminID, userDBID, hex.EncodeToString(sum[:]))
}

func (s *GatewayKeyService) preferredExternalGroupID(ctx context.Context, userDBID int64) int64 {
	var groupID int64
	err := s.postgres.QueryRow(ctx, `
		SELECT external_group_id
		FROM gateway_api_keys
		WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND external_group_id > 0
		ORDER BY id DESC
		LIMIT 1
	`, userDBID).Scan(&groupID)
	if err == nil && groupID > 0 {
		return groupID
	}
	err = s.postgres.QueryRow(ctx, `
		SELECT default_group_id
		FROM gateway_accounts
		WHERE user_id = $1 AND provider = 'sub2api' AND status = 'active' AND default_group_id > 0
		ORDER BY id DESC
		LIMIT 1
	`, userDBID).Scan(&groupID)
	if err == nil && groupID > 0 {
		return groupID
	}
	return s.redeem.DefaultExternalGroupID(ctx)
}

func (s *GatewayKeyService) gatewayAccountByUser(ctx context.Context, userDBID int64) (adminGatewayAccountSummary, error) {
	var account adminGatewayAccountSummary
	err := s.postgres.QueryRow(ctx, `
		SELECT provider, external_user_id, external_email, default_group_id, concurrency, status, last_synced_at
		FROM gateway_accounts
		WHERE user_id = $1 AND provider = 'sub2api' AND external_user_id > 0
		ORDER BY CASE WHEN status = 'active' THEN 0 ELSE 1 END, id DESC
		LIMIT 1
	`, userDBID).Scan(
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

func (s *GatewayKeyService) validateStandardGatewayGroup(ctx context.Context, externalGroupID int64) error {
	var subscriptionType string
	var status string
	err := s.postgres.QueryRow(ctx, `
		SELECT subscription_type, status
		FROM gateway_groups
		WHERE provider = 'sub2api' AND external_group_id = $1
	`, externalGroupID).Scan(&subscriptionType, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("gateway_group_not_found")
	}
	if err != nil {
		return err
	}
	if status != "active" {
		return fmt.Errorf("gateway_group_not_active")
	}
	if subscriptionType != "standard" {
		return fmt.Errorf("gateway_group_must_be_standard")
	}
	return nil
}

func (s *GatewayKeyService) insertAdminGatewayAPIKey(ctx context.Context, userDBID int64, externalKeyID, externalGroupID int64, plainKey string) (adminGatewayAPIKeyRecord, error) {
	encryptedKey, err := s.gateway.EncryptSecret(plainKey)
	if err != nil {
		return adminGatewayAPIKeyRecord{}, err
	}
	publicID := "gak_" + uuid.NewString()
	if err := s.postgres.QueryRow(ctx, `
		INSERT INTO gateway_api_keys (
			public_id, user_id, gateway_account_id, provider, external_key_id,
			external_group_id, encrypted_api_key, masked_api_key, status
		)
		SELECT $1, $2, ga.id, 'sub2api', $3, $4, $5, $6, 'active'
		FROM gateway_accounts ga
		WHERE ga.user_id = $2 AND ga.provider = 'sub2api' AND ga.status = 'active'
		ORDER BY ga.id DESC
		LIMIT 1
		RETURNING public_id
	`, publicID, userDBID, externalKeyID, externalGroupID, encryptedKey, adminMaskAPIKey(plainKey)).Scan(&publicID); err != nil {
		return adminGatewayAPIKeyRecord{}, err
	}
	return s.findGatewayAPIKey(ctx, publicID)
}

func adminMaskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 12 {
		return key
	}
	return key[:8] + "..." + key[len(key)-4:]
}
