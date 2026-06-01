package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type GatewayUpstreamAccount struct {
	ent.Schema
}

func (GatewayUpstreamAccount) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique(),
		field.String("provider").Default("sub2api"),
		field.Int64("external_account_id").Default(0),
		field.String("name").NotEmpty(),
		field.String("platform").Default("anthropic"),
		field.String("account_type").Default(""),
		field.String("status").Default("active"),
		field.Bool("schedulable").Default(true),
		field.Int("concurrency").Default(0),
		field.Int("current_concurrency").Default(0),
		field.Int("priority").Default(0),
		field.Float("rate_multiplier").Default(1),
		field.String("error_message").Default(""),
		field.JSON("group_ids", []int64{}),
		field.JSON("model_mapping", map[string]string{}),
		field.Int("mapped_model_count").Default(0),
		field.Time("last_used_at").Optional().Nillable(),
		field.Time("expires_at").Optional().Nillable(),
		field.Time("rate_limited_at").Optional().Nillable(),
		field.Time("rate_limit_reset_at").Optional().Nillable(),
		field.Time("overload_until").Optional().Nillable(),
		field.Time("temp_unschedulable_until").Optional().Nillable(),
		field.Time("last_synced_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
