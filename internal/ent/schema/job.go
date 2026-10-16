package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Job struct {
	ent.Schema
}

func (Job) Fields() []ent.Field {
	return []ent.Field{
		field.String("source").NotEmpty(),
		// The column keeps its old name so regenerating does not drop and
		// recreate it, which would discard decision and delivered_at for every
		// existing row and re-send jobs already delivered.
		field.String("queue").StorageKey("lane").NotEmpty(),
		field.String("external_id").NotEmpty(),
		field.String("url").NotEmpty(),
		field.String("company").NotEmpty(),
		field.String("title").NotEmpty(),
		field.String("location").Default(""),
		field.Enum("decision").Values("approved", "rejected"),
		field.JSON("details", map[string]any{}).Optional(),
		field.Time("discovered_at").Default(time.Now).Immutable(),
		field.Time("delivered_at").Optional().Nillable(),
	}
}

func (Job) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source", "external_id").Unique(),
		index.Fields("decision", "delivered_at"),
		index.Fields("discovered_at"),
	}
}
