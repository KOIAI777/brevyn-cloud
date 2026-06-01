package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Product struct {
	ent.Schema
}

func (Product) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique(),
		field.String("sku").Unique(),
		field.String("name").NotEmpty(),
		field.String("description").Default(""),
		field.String("benefit_type").Default("balance"),
		field.Float("price_cny").Default(0),
		field.Float("original_price_cny").Optional().Nillable(),
		field.Float("value").Default(0),
		field.Int("validity_days").Default(0),
		field.Int64("gateway_group_id").Optional().Nillable(),
		field.Int64("external_group_id").Default(0),
		field.String("source").Default("manual"),
		field.String("features").Default(""),
		field.Bool("for_sale").Default(true),
		field.Int("sort_order").Default(0),
		field.String("status").Default("active"),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
