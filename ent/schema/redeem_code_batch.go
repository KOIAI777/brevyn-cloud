package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type RedeemCodeBatch struct {
	ent.Schema
}

func (RedeemCodeBatch) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique(),
		field.Int64("product_id").Optional().Nillable(),
		field.String("name").NotEmpty(),
		field.String("source").Default("ldxp"),
		field.Int("quantity").Default(0),
		field.String("status").Default("active"),
		field.String("notes").Default(""),
		field.Int64("created_by_admin_id").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
