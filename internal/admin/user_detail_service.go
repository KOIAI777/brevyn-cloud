package admin

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserDetailService struct {
	postgres *pgxpool.Pool
}

func NewUserDetailService(postgres *pgxpool.Pool) *UserDetailService {
	return &UserDetailService{postgres: postgres}
}

type AdminWalletTransactionItem struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind"`
	Amount       float64   `json:"amount"`
	BalanceAfter float64   `json:"balanceAfter"`
	Source       string    `json:"source"`
	ReferenceID  string    `json:"referenceId"`
	Notes        string    `json:"notes"`
	CreatedAt    time.Time `json:"createdAt"`
}

type AdminDeviceItem struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Platform   string     `json:"platform"`
	Status     string     `json:"status"`
	LastSeenAt *time.Time `json:"lastSeenAt"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type AdminGatewayAccountItem struct {
	ID             int64      `json:"id"`
	Provider       string     `json:"provider"`
	ExternalUserID int64      `json:"externalUserId"`
	ExternalEmail  string     `json:"externalEmail"`
	DefaultGroupID int64      `json:"defaultGroupId"`
	Concurrency    int        `json:"concurrency"`
	Status         string     `json:"status"`
	LastSyncedAt   *time.Time `json:"lastSyncedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

func (s *UserDetailService) ListWalletTransactions(ctx context.Context, userPublicID string, limit int) ([]AdminWalletTransactionItem, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.postgres.Query(ctx, `
		SELECT wt.public_id, wt.kind, wt.amount, wt.balance_after, wt.source,
			wt.reference_id, wt.notes, wt.created_at
		FROM wallet_transactions wt
		JOIN users u ON u.id = wt.user_id
		WHERE u.public_id = $1
		ORDER BY wt.created_at DESC, wt.id DESC
		LIMIT $2
	`, userPublicID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []AdminWalletTransactionItem{}
	for rows.Next() {
		var item AdminWalletTransactionItem
		if err := rows.Scan(&item.ID, &item.Kind, &item.Amount, &item.BalanceAfter,
			&item.Source, &item.ReferenceID, &item.Notes, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *UserDetailService) ListDevices(ctx context.Context, userPublicID string) ([]AdminDeviceItem, error) {
	rows, err := s.postgres.Query(ctx, `
		SELECT d.public_id, d.name, d.platform, d.status, d.last_seen_at, d.created_at, d.updated_at
		FROM devices d
		JOIN users u ON u.id = d.user_id
		WHERE u.public_id = $1
		ORDER BY d.last_seen_at DESC NULLS LAST, d.created_at DESC
	`, userPublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []AdminDeviceItem{}
	for rows.Next() {
		var item AdminDeviceItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Platform, &item.Status,
			&item.LastSeenAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *UserDetailService) ListGatewayAccounts(ctx context.Context, userPublicID string) ([]AdminGatewayAccountItem, error) {
	rows, err := s.postgres.Query(ctx, `
		SELECT ga.id, ga.provider, ga.external_user_id, ga.external_email,
			ga.default_group_id, ga.concurrency, ga.status, ga.last_synced_at, ga.created_at, ga.updated_at
		FROM gateway_accounts ga
		JOIN users u ON u.id = ga.user_id
		WHERE u.public_id = $1
		ORDER BY ga.created_at DESC, ga.id DESC
	`, userPublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []AdminGatewayAccountItem{}
	for rows.Next() {
		var item AdminGatewayAccountItem
		if err := rows.Scan(&item.ID, &item.Provider, &item.ExternalUserID, &item.ExternalEmail,
			&item.DefaultGroupID, &item.Concurrency, &item.Status, &item.LastSyncedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *UserDetailService) EnsureUserExists(ctx context.Context, userPublicID string) error {
	var id int64
	return s.postgres.QueryRow(ctx, `SELECT id FROM users WHERE public_id = $1`, userPublicID).Scan(&id)
}

func (s *UserDetailService) ExternalSub2APIUserID(ctx context.Context, userPublicID string) (int64, error) {
	var externalUserID int64
	err := s.postgres.QueryRow(ctx, `
		SELECT ga.external_user_id
		FROM gateway_accounts ga
		JOIN users u ON u.id = ga.user_id
		WHERE u.public_id = $1
			AND ga.provider = 'sub2api'
			AND ga.external_user_id > 0
		ORDER BY CASE WHEN ga.status = 'active' THEN 0 ELSE 1 END, ga.id DESC
		LIMIT 1
	`, userPublicID).Scan(&externalUserID)
	if err != nil {
		return 0, err
	}
	if externalUserID <= 0 {
		return 0, pgx.ErrNoRows
	}
	return externalUserID, nil
}
