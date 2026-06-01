package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type Device struct {
	ent.Schema
}

func (Device) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique().DefaultFunc(uuid.NewString),
		field.Int("user_id"),
		field.String("device_fingerprint_hash").MaxLen(128).NotEmpty(),
		field.String("name").MaxLen(120).Default(""),
		field.String("platform").MaxLen(64).Default(""),
		field.String("status").MaxLen(32).Default("active"),
		field.Time("last_seen_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
