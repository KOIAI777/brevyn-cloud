package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type AuditLog struct {
	ent.Schema
}

func (AuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique().DefaultFunc(uuid.NewString),
		field.Int("user_id").Optional().Nillable(),
		field.Int("admin_user_id").Optional().Nillable(),
		field.String("action").MaxLen(128).NotEmpty(),
		field.String("resource_type").MaxLen(64).Default(""),
		field.String("resource_id").MaxLen(128).Default(""),
		field.String("ip").MaxLen(64).Default(""),
		field.String("user_agent").Default(""),
		field.String("metadata_json").Default("{}"),
		field.Time("created_at").Default(time.Now),
	}
}
