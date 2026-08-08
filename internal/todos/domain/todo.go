package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Todo struct {
	ID        bson.ObjectID `json:"id" bson:"_id,omitempty"`
	Title     string        `json:"title" bson:"title"`
	Completed bool          `json:"completed" bson:"completed"`
	CreatedAt time.Time     `json:"created_at" bson:"created_at,omitempty"`
}
