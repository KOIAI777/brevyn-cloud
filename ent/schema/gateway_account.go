package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type GatewayAccount struct {
	ent.Schema
}

func (GatewayAccount) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.String("provider").MaxLen(32).Default("sub2api"),
		field.Int64("external_user_id"),
		field.String("external_email").MaxLen(255).NotEmpty(),
		field.Int64("default_group_id"),
		field.String("status").MaxLen(32).Default("active"),
		field.Time("last_synced_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
