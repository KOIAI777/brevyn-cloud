package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type GatewayChannel struct {
	ent.Schema
}

func (GatewayChannel) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique(),
		field.String("provider").Default("sub2api"),
		field.Int64("external_channel_id").Default(0),
		field.String("name").NotEmpty(),
		field.String("description").Default(""),
		field.String("status").Default("active"),
		field.String("billing_model_source").Default("channel_mapped"),
		field.Bool("restrict_models").Default(false),
		field.JSON("group_ids", []int64{}),
		field.JSON("model_mapping", map[string]map[string]string{}),
		field.JSON("model_pricing", []map[string]any{}),
		field.Int("pricing_count").Default(0),
		field.Time("last_synced_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
