package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type GatewayAPIKey struct {
	ent.Schema
}

func (GatewayAPIKey) Fields() []ent.Field {
	return []ent.Field{
		field.Int("user_id"),
		field.Int("device_id"),
		field.Int("gateway_account_id"),
		field.String("provider").MaxLen(32).Default("sub2api"),
		field.Int64("external_key_id").Optional(),
		field.Int64("external_group_id"),
		field.String("encrypted_api_key").NotEmpty(),
		field.String("masked_api_key").MaxLen(64).NotEmpty(),
		field.String("status").MaxLen(32).Default("active"),
		field.Time("last_used_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
