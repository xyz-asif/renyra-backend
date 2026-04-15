package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type AccountDeletion struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    bson.ObjectID `bson:"userId" json:"userId"`
	Email     string        `bson:"email" json:"email"`
	UserName  string        `bson:"userName" json:"userName"`
	Reason    string        `bson:"reason" json:"reason"`
	DeletedAt time.Time     `bson:"deletedAt" json:"deletedAt"`
}
