package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type ModelCatalog struct {
	ent.Schema
}

func (ModelCatalog) Fields() []ent.Field {
	return []ent.Field{
		field.String("model_id").MaxLen(128).NotEmpty().Unique(),
		field.String("display_name").MaxLen(128).NotEmpty(),
		field.String("provider_family").MaxLen(64).NotEmpty(),
		field.String("capabilities_json").Default("[]"),
		field.Bool("public_visible").Default(true),
		field.Bool("supports_streaming").Default(true),
		field.String("status").MaxLen(32).Default("active"),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
