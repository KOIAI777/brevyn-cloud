package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type AdminUser struct {
	ent.Schema
}

func (AdminUser) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique().DefaultFunc(uuid.NewString),
		field.String("email").MaxLen(255).NotEmpty().Unique(),
		field.String("password_hash").NotEmpty(),
		field.String("role").MaxLen(32).Default("owner"),
		field.String("status").MaxLen(32).Default("active"),
		field.Bool("totp_enabled").Default(false),
		field.String("totp_secret_encrypted").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
