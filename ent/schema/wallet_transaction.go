package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"github.com/google/uuid"
)

type WalletTransaction struct {
	ent.Schema
}

func (WalletTransaction) Fields() []ent.Field {
	return []ent.Field{
		field.String("public_id").Unique().DefaultFunc(uuid.NewString),
		field.Int("user_id"),
		field.String("kind").MaxLen(32).NotEmpty(),
		field.Float("amount").Default(0),
		field.Float("balance_after").Default(0),
		field.String("source").MaxLen(64).Default("system"),
		field.String("reference_id").MaxLen(128).Default(""),
		field.String("notes").Default(""),
		field.Time("created_at").Default(time.Now),
	}
}
