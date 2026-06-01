package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type RefreshToken struct {
	ent.Schema
}

func (RefreshToken) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique().DefaultFunc(uuid.NewString),
		field.Int("user_id"),
		field.String("family_id").NotEmpty(),
		field.String("token_hash").NotEmpty().Unique(),
		field.String("status").MaxLen(32).Default("active"),
		field.Time("expires_at"),
		field.Time("last_used_at").Optional().Nillable(),
		field.Time("revoked_at").Optional().Nillable(),
		field.String("revoked_reason").Default(""),
		field.Int("replaced_by_token_id").Optional().Nillable(),
		field.String("created_ip").Default(""),
		field.String("user_agent").Default(""),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
