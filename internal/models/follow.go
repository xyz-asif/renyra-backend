package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Follow struct {
	ID          bson.ObjectID `bson:"_id,omitempty" json:"id"`
	FollowerID  bson.ObjectID `bson:"followerId"    json:"followerId"`
	FollowingID bson.ObjectID `bson:"followingId"   json:"followingId"`
	CreatedAt   time.Time     `bson:"createdAt"     json:"createdAt"`
}
