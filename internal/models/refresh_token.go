package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// RefreshToken stores a hashed refresh token for a user.
// The plain token is sent to the client; only its SHA-256 hash is persisted.
type RefreshToken struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    bson.ObjectID `bson:"userId"        json:"userId"`
	TokenHash string        `bson:"tokenHash"     json:"-"` // never expose hash
	ExpiresAt time.Time     `bson:"expiresAt"     json:"expiresAt"`
	CreatedAt time.Time     `bson:"createdAt"     json:"createdAt"`
}
