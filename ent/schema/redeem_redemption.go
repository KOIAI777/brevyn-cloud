package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type RedeemRedemption struct {
	ent.Schema
}

func (RedeemRedemption) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique(),
		field.Int64("redeem_code_id"),
		field.Int64("user_id"),
		field.Int64("product_id").Optional().Nillable(),
		field.Int64("batch_id").Optional().Nillable(),
		field.String("kind").Default("balance"),
		field.Float("value").Default(0),
		field.Int("validity_days").Default(0),
		field.String("gateway_provider").Default("sub2api"),
		field.Int64("external_user_id").Default(0),
		field.Int64("external_group_id").Default(0),
		field.String("gateway_operation").Default(""),
		field.String("status").Default("pending_gateway"),
		field.String("error_message").Default(""),
		field.String("idempotency_key").Default(""),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
