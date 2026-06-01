package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type User struct {
	ent.Schema
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique().DefaultFunc(uuid.NewString),
		field.String("email").MaxLen(255).NotEmpty().Unique(),
		field.String("email_hash").MaxLen(128).NotEmpty().Unique(),
		field.String("password_hash").NotEmpty(),
		field.String("display_name").MaxLen(100).Default(""),
		field.String("status").MaxLen(32).Default("active"),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
