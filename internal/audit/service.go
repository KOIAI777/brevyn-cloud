package audit

import (
	"context"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type Entry struct {
	ActorType  string
	ActorID    int64
	Action     string
	TargetType string
	TargetID   string
	IP         string
	UserAgent  string
	Metadata   string
}

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

type Service struct {
	db execer
}

func NewService(db execer) *Service {
	return &Service{db: db}
}

func (s *Service) Record(ctx context.Context, entry Entry) error {
	if entry.Metadata == "" {
		entry.Metadata = "{}"
	}
	_, err := s.db.Exec(ctx, `
		INSERT INTO audit_logs (public_id, actor_type, actor_id, action, target_type, target_id, ip, user_agent, metadata)
		VALUES ($1, $2, NULLIF($3, 0), $4, $5, $6, $7, $8, $9::jsonb)
	`, "aud_"+strconv.FormatInt(time.Now().UnixNano(), 36), entry.ActorType, entry.ActorID, entry.Action, entry.TargetType, entry.TargetID, entry.IP, entry.UserAgent, entry.Metadata)
	return err
}
