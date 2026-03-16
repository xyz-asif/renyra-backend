package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Hashtag struct {
	ID         bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Tag        string        `bson:"tag"           json:"tag"`        // lowercase, no #
	UsageCount int           `bson:"usageCount"    json:"usageCount"`
	UpdatedAt  time.Time     `bson:"updatedAt"     json:"updatedAt"`
}
