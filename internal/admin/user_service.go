package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/brevyn/brevyn-cloud/internal/gateway/sub2api"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type UserService struct {
	postgres *pgxpool.Pool
}

func NewUserService(postgres *pgxpool.Pool) *UserService {
	return &UserService{postgres: postgres}
}

type UserListFilter struct {
	Search        string
	Status        string
	SyncState     string
	Limit         int
	Offset        int
	GroupID       int64
	MinBalance    float64
	HasMinBalance bool
	MaxBalance    float64
	HasMaxBalance bool
}

type DeletedUser struct {
	DBID  int64
	Email string
}

type AdminUserItem struct {
	ID             string     `json:"id"`
	Email          string     `json:"email"`
	Status         string     `json:"status"`
	Balance        float64    `json:"balance"`
	DefaultGroupID int64      `json:"defaultGroupId"`
	GatewayEmail   string     `json:"gatewayEmail"`
	GatewayStatus  string     `json:"gatewayStatus"`
	DeviceCount    int64      `json:"deviceCount"`
	LastSeenAt     *time.Time `json:"lastSeenAt"`
	CreatedAt      time.Time  `json:"createdAt"`
}

type scanner interface {
	Scan(dest ...any) error
}

func (s *UserService) List(ctx context.Context, filter UserListFilter) ([]AdminUserItem, int, error) {
	filter.Search = strings.TrimSpace(filter.Search)
	filter.Status = strings.TrimSpace(strings.ToLower(filter.Status))
	filter.SyncState = strings.TrimSpace(filter.SyncState)

	where := []string{"1=1"}
	args := []any{}
	if filter.Search != "" {
		args = append(args, "%"+strings.ToLower(filter.Search)+"%")
		where = append(where, fmt.Sprintf(`(
			lower(u.public_id) LIKE $%d OR
			lower(u.email) LIKE $%d OR
			lower(coalesce(ga.external_email, '')) LIKE $%d OR
			coalesce(ga.external_user_id, 0)::text LIKE $%d OR
			coalesce(ga.default_group_id, 0)::text LIKE $%d
		)`, len(args), len(args), len(args), len(args), len(args)))
	}
	if filter.Status != "" && filter.Status != "all" {
		args = append(args, filter.Status)
		where = append(where, fmt.Sprintf("u.status = $%d", len(args)))
	}
	switch filter.SyncState {
	case "synced":
		where = append(where, "coalesce(ga.external_user_id, 0) > 0")
	case "local_only":
		where = append(where, "coalesce(ga.external_user_id, 0) = 0")
	case "gateway_disabled":
		where = append(where, "coalesce(ga.status, '') <> '' AND coalesce(ga.status, '') <> 'active'")
	}
	if filter.GroupID > 0 {
		args = append(args, filter.GroupID)
		where = append(where, fmt.Sprintf("coalesce(ga.default_group_id, 0) = $%d", len(args)))
	}
	if filter.HasMinBalance {
		args = append(args, filter.MinBalance)
		where = append(where, fmt.Sprintf("coalesce(wt.balance, 0) >= $%d", len(args)))
	}
	if filter.HasMaxBalance {
		args = append(args, filter.MaxBalance)
		where = append(where, fmt.Sprintf("coalesce(wt.balance, 0) <= $%d", len(args)))
	}

	userListJoins := `
		LEFT JOIN (
			SELECT DISTINCT ON (user_id) user_id, external_user_id, external_email, default_group_id, status
			FROM gateway_accounts
			ORDER BY user_id, id DESC
		) ga ON ga.user_id = u.id
		LEFT JOIN (
			SELECT user_id, sum(amount) AS balance
			FROM wallet_transactions
			GROUP BY user_id
		) wt ON wt.user_id = u.id`

	countSQL := `
		SELECT count(DISTINCT u.id)
		FROM users u
		` + userListJoins + `
		WHERE ` + strings.Join(where, " AND ")
	var total int
	if err := s.postgres.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.postgres.Query(ctx, `
		SELECT
			u.id,
			u.public_id,
			u.email,
			u.status,
			u.created_at,
			coalesce(ga.external_email, '') AS gateway_email,
			coalesce(ga.default_group_id, 0) AS default_group_id,
			coalesce(ga.status, '') AS gateway_status,
			coalesce(wt.balance, 0) AS balance,
			coalesce(ds.device_count, 0) AS device_count,
			ds.last_seen_at
		FROM users u
		`+userListJoins+`
		LEFT JOIN (
			SELECT user_id, count(*) FILTER (WHERE status = 'active') AS device_count, max(last_seen_at) AS last_seen_at
			FROM devices
			GROUP BY user_id
		) ds ON ds.user_id = u.id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY u.created_at DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items, err := scanUsers(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (s *UserService) Get(ctx context.Context, publicID string) (AdminUserItem, error) {
	row := s.postgres.QueryRow(ctx, `
		SELECT
			u.id,
			u.public_id,
			u.email,
			u.status,
			u.created_at,
			coalesce(ga.external_email, '') AS gateway_email,
			coalesce(ga.default_group_id, 0) AS default_group_id,
			coalesce(ga.status, '') AS gateway_status,
			coalesce(wt.balance, 0) AS balance,
			coalesce(ds.device_count, 0) AS device_count,
			ds.last_seen_at
		FROM users u
		LEFT JOIN (
			SELECT DISTINCT ON (user_id) user_id, external_email, default_group_id, status
			FROM gateway_accounts
			ORDER BY user_id, id DESC
		) ga ON ga.user_id = u.id
		LEFT JOIN (
			SELECT user_id, sum(amount) AS balance
			FROM wallet_transactions
			GROUP BY user_id
		) wt ON wt.user_id = u.id
		LEFT JOIN (
			SELECT user_id, count(*) FILTER (WHERE status = 'active') AS device_count, max(last_seen_at) AS last_seen_at
			FROM devices
			GROUP BY user_id
		) ds ON ds.user_id = u.id
		WHERE u.public_id = $1
	`, publicID)
	return scanUser(row)
}

func (s *UserService) GetByEmail(ctx context.Context, email string) (AdminUserItem, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var publicID string
	if err := s.postgres.QueryRow(ctx, `
		SELECT public_id FROM users WHERE email_hash = $1
	`, adminEmailHash(email)).Scan(&publicID); err != nil {
		return AdminUserItem{}, err
	}
	return s.Get(ctx, publicID)
}

func (s *UserService) Create(ctx context.Context, email, displayName, password string) (AdminUserItem, string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	displayName = strings.TrimSpace(displayName)
	if email == "" || !strings.Contains(email, "@") {
		return AdminUserItem{}, "", fmt.Errorf("invalid_email")
	}

	generatedPassword := ""
	if strings.TrimSpace(password) == "" {
		generatedPassword = "Bv-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20]
		password = generatedPassword
	}
	if len(password) < 8 {
		return AdminUserItem{}, "", fmt.Errorf("password_too_short")
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return AdminUserItem{}, "", err
	}

	publicID := "u_" + uuid.NewString()
	if _, err := s.postgres.Exec(ctx, `
		INSERT INTO users (public_id, email, email_hash, password_hash, display_name, status)
		VALUES ($1, $2, $3, $4, $5, 'active')
	`, publicID, email, adminEmailHash(email), string(passwordHash), displayName); err != nil {
		return AdminUserItem{}, "", err
	}
	user, err := s.Get(ctx, publicID)
	return user, generatedPassword, err
}

func (s *UserService) UpdateStatus(ctx context.Context, publicID, status string) (AdminUserItem, int64, error) {
	row := s.postgres.QueryRow(ctx, `
		WITH updated AS (
			UPDATE users
			SET status = $2, updated_at = now()
			WHERE public_id = $1
			RETURNING id, public_id, email, status, created_at
		)
		SELECT
			u.id,
			u.public_id,
			u.email,
			u.status,
			u.created_at,
			coalesce(ga.external_email, '') AS gateway_email,
			coalesce(ga.default_group_id, 0) AS default_group_id,
			coalesce(ga.status, '') AS gateway_status,
			coalesce(wt.balance, 0) AS balance,
			coalesce(ds.device_count, 0) AS device_count,
			ds.last_seen_at
		FROM updated u
		LEFT JOIN (
			SELECT DISTINCT ON (user_id) user_id, external_email, default_group_id, status
			FROM gateway_accounts
			ORDER BY user_id, id DESC
		) ga ON ga.user_id = u.id
		LEFT JOIN (
			SELECT user_id, sum(amount) AS balance
			FROM wallet_transactions
			GROUP BY user_id
		) wt ON wt.user_id = u.id
		LEFT JOIN (
			SELECT user_id, count(*) FILTER (WHERE status = 'active') AS device_count, max(last_seen_at) AS last_seen_at
			FROM devices
			GROUP BY user_id
		) ds ON ds.user_id = u.id
	`, publicID, status)

	var dbID int64
	var user AdminUserItem
	err := row.Scan(
		&dbID,
		&user.ID,
		&user.Email,
		&user.Status,
		&user.CreatedAt,
		&user.GatewayEmail,
		&user.DefaultGroupID,
		&user.GatewayStatus,
		&user.Balance,
		&user.DeviceCount,
		&user.LastSeenAt,
	)
	return user, dbID, err
}

func (s *UserService) Delete(ctx context.Context, publicID string) (DeletedUser, error) {
	var deleted DeletedUser
	if err := s.postgres.QueryRow(ctx, `
		SELECT id, email FROM users WHERE public_id = $1
	`, publicID).Scan(&deleted.DBID, &deleted.Email); err != nil {
		return deleted, err
	}
	_, err := s.postgres.Exec(ctx, `DELETE FROM users WHERE id = $1`, deleted.DBID)
	return deleted, err
}

func (s *UserService) SyncSub2APIUser(ctx context.Context, external sub2api.User) (bool, bool, error) {
	email := strings.ToLower(strings.TrimSpace(external.Email))
	if external.ID <= 0 || email == "" {
		return false, false, nil
	}

	tx, err := s.postgres.Begin(ctx)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var userDBID int64
	err = tx.QueryRow(ctx, `
		SELECT u.id
		FROM users u
		LEFT JOIN gateway_accounts ga ON ga.user_id = u.id AND ga.provider = 'sub2api'
		WHERE ga.external_user_id = $1 OR u.email_hash = $2
		ORDER BY CASE WHEN ga.external_user_id = $1 THEN 0 ELSE 1 END, u.id ASC
		LIMIT 1
	`, external.ID, adminEmailHash(email)).Scan(&userDBID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}

	defaultGroupID := int64(0)
	if len(external.AllowedGroups) > 0 {
		defaultGroupID = external.AllowedGroups[0]
	}
	var accountID int64
	err = tx.QueryRow(ctx, `
		SELECT id FROM gateway_accounts
		WHERE user_id = $1 AND provider = 'sub2api'
		ORDER BY id DESC
		LIMIT 1
	`, userDBID).Scan(&accountID)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `
			INSERT INTO gateway_accounts (user_id, provider, external_user_id, external_email, default_group_id, concurrency, status, last_synced_at)
			VALUES ($1, 'sub2api', $2, $3, $4, $5, $6, now())
		`, userDBID, external.ID, email, defaultGroupID, external.Concurrency, external.Status)
		if err != nil {
			return false, false, err
		}
	} else if err != nil {
		return false, false, err
	} else {
		_, err = tx.Exec(ctx, `
			UPDATE gateway_accounts
			SET external_user_id = $2,
				external_email = $3,
				default_group_id = $4,
				concurrency = $5,
				status = $6,
				last_synced_at = now(),
				updated_at = now()
			WHERE id = $1
		`, accountID, external.ID, email, defaultGroupID, external.Concurrency, external.Status)
		if err != nil {
			return false, false, err
		}
	}

	var currentBalance float64
	if err := tx.QueryRow(ctx, `
		SELECT coalesce(sum(amount), 0)
		FROM wallet_transactions
		WHERE user_id = $1
	`, userDBID).Scan(&currentBalance); err != nil {
		return false, false, err
	}
	delta := external.Balance - currentBalance
	balanceAdjusted := delta > 0.000001 || delta < -0.000001
	if balanceAdjusted {
		_, err = tx.Exec(ctx, `
			INSERT INTO wallet_transactions (public_id, user_id, kind, amount, balance_after, source, reference_id, notes)
			VALUES ($1, $2, 'gateway_sync', $3, $4, 'sub2api_sync', $5, 'Synced from Sub2API user balance')
		`, "wtx_"+uuid.NewString(), userDBID, delta, external.Balance, fmt.Sprintf("sub2api_user:%d", external.ID))
		if err != nil {
			return false, false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, false, err
	}
	return true, balanceAdjusted, nil
}

func scanUsers(rows pgx.Rows) ([]AdminUserItem, error) {
	users := []AdminUserItem{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return users, nil
}

func scanUser(row scanner) (AdminUserItem, error) {
	var dbID int64
	var user AdminUserItem
	err := row.Scan(
		&dbID,
		&user.ID,
		&user.Email,
		&user.Status,
		&user.CreatedAt,
		&user.GatewayEmail,
		&user.DefaultGroupID,
		&user.GatewayStatus,
		&user.Balance,
		&user.DeviceCount,
		&user.LastSeenAt,
	)
	return user, err
}

func adminEmailHash(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}

func isAdminUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
