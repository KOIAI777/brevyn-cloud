package admin

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type AuditQueryService struct {
	postgres *pgxpool.Pool
}

type AuditLogListFilters struct {
	Search    string
	Action    string
	ActorType string
	DateFrom  string
	DateTo    string
	Limit     int
	Offset    int
}

func NewAuditQueryService(postgres *pgxpool.Pool) *AuditQueryService {
	return &AuditQueryService{postgres: postgres}
}

func (s *AuditQueryService) List(ctx context.Context, filters AuditLogListFilters) ([]AuditLogItem, int, error) {
	if filters.Limit <= 0 {
		filters.Limit = 100
	}

	where := []string{"1=1"}
	args := []any{}
	if strings.TrimSpace(filters.Search) != "" {
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(filters.Search))+"%")
		where = append(where, fmt.Sprintf(`(
			lower(al.public_id) LIKE $%d OR
			lower(coalesce(au.email, u.email, '')) LIKE $%d OR
			lower(al.action) LIKE $%d OR
			lower(al.target_type) LIKE $%d OR
			lower(al.target_id) LIKE $%d OR
			lower(al.ip) LIKE $%d OR
			lower(al.user_agent) LIKE $%d OR
			lower(al.metadata::text) LIKE $%d
		)`, len(args), len(args), len(args), len(args), len(args), len(args), len(args), len(args)))
	}
	appendExactStringFilter(&where, &args, "al.action", filters.Action)
	appendExactStringFilter(&where, &args, "al.actor_type", filters.ActorType)
	if err := appendDateRangeValues("al.created_at", filters.DateFrom, filters.DateTo, &where, &args); err != nil {
		return nil, 0, err
	}

	var total int
	if err := s.postgres.QueryRow(ctx, `
		SELECT count(*)
		FROM audit_logs al
		LEFT JOIN admin_users au ON al.actor_type = 'admin' AND au.id = al.actor_id
		LEFT JOIN users u ON al.actor_type = 'user' AND u.id = al.actor_id
		WHERE `+strings.Join(where, " AND "), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, filters.Limit, filters.Offset)
	rows, err := s.postgres.Query(ctx, `
		SELECT al.public_id, al.actor_type, coalesce(al.actor_id, 0),
			coalesce(au.email, u.email, ''),
			al.action, al.target_type, al.target_id, al.ip, al.user_agent,
			al.metadata::text, al.created_at
		FROM audit_logs al
		LEFT JOIN admin_users au ON al.actor_type = 'admin' AND au.id = al.actor_id
		LEFT JOIN users u ON al.actor_type = 'user' AND u.id = al.actor_id
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY al.created_at DESC
		LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := []AuditLogItem{}
	for rows.Next() {
		var item AuditLogItem
		if err := rows.Scan(&item.ID, &item.ActorType, &item.ActorID, &item.ActorLabel,
			&item.Action, &item.TargetType, &item.TargetID, &item.IP, &item.UserAgent,
			&item.Metadata, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		if item.ActorLabel == "" {
			item.ActorLabel = item.ActorType
			if item.ActorID > 0 {
				item.ActorLabel += "#" + strconv.FormatInt(item.ActorID, 10)
			}
		}
		items = append(items, decorateAuditLog(item))
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
