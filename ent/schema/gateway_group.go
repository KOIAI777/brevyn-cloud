package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type GatewayGroup struct {
	ent.Schema
}

func (GatewayGroup) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique(),
		field.String("provider").Default("sub2api"),
		field.Int64("external_group_id").Default(0),
		field.String("name").NotEmpty(),
		field.String("description").Default(""),
		field.String("platform").Default("anthropic"),
		field.String("subscription_type").Default("standard"),
		field.Float("rate_multiplier").Default(1),
		field.Bool("is_exclusive").Default(false),
		field.Float("daily_limit_usd").Optional().Nillable(),
		field.Float("weekly_limit_usd").Optional().Nillable(),
		field.Float("monthly_limit_usd").Optional().Nillable(),
		field.Int("default_validity_days").Default(30),
		field.Int("rpm_limit").Default(0),
		field.Int("sort_order").Default(0),
		field.Bool("allow_image_generation").Default(false),
		field.Bool("image_rate_independent").Default(false),
		field.Float("image_rate_multiplier").Default(1),
		field.Float("image_price_1k").Optional().Nillable(),
		field.Float("image_price_2k").Optional().Nillable(),
		field.Float("image_price_4k").Optional().Nillable(),
		field.Bool("claude_code_only").Default(false),
		field.Int64("fallback_group_id").Optional().Nillable(),
		field.Int64("fallback_group_id_on_invalid_request").Optional().Nillable(),
		field.JSON("model_routing", map[string][]int64{}),
		field.Bool("model_routing_enabled").Default(false),
		field.Bool("mcp_xml_inject").Default(true),
		field.JSON("supported_model_scopes", []string{}),
		field.Bool("allow_messages_dispatch").Default(false),
		field.Bool("require_oauth_only").Default(false),
		field.Bool("require_privacy_set").Default(false),
		field.String("default_mapped_model").Default(""),
		field.JSON("messages_dispatch_model_config", map[string]any{}),
		field.String("status").Default("active"),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
