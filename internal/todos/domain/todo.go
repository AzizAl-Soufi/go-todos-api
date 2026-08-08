package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Todo struct {
	ID        bson.ObjectID `json:"id" bson:"_id,omitempty"`
	UserID    bson.ObjectID `json:"userId" bson:"userId"`
	Title     string        `json:"title" bson:"title"`
	Completed bool          `json:"completed" bson:"completed"`
	CreatedAt time.Time     `json:"createdAt" bson:"createdAt,omitempty"`
}
