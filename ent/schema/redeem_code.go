package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type RedeemCode struct {
	ent.Schema
}

func (RedeemCode) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique().DefaultFunc(uuid.NewString),
		field.String("code_hash").MaxLen(128).NotEmpty().Unique(),
		field.String("code_prefix").MaxLen(16).NotEmpty(),
		field.String("kind").MaxLen(32).Default("balance"),
		field.Float("value").Default(0),
		field.String("status").MaxLen(32).Default("unused"),
		field.Int64("used_by_user_id").Optional().Nillable(),
		field.Time("used_at").Optional().Nillable(),
		field.Time("expires_at").Optional().Nillable(),
		field.Int64("product_id").Optional().Nillable(),
		field.Int64("batch_id").Optional().Nillable(),
		field.Int64("gateway_group_id").Optional().Nillable(),
		field.Int64("external_group_id").Default(0),
		field.Int("validity_days").Default(0),
		field.String("source").Default("manual"),
		field.String("notes").Default(""),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
