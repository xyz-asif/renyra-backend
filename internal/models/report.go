package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Report struct {
	ID          bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID      bson.ObjectID `bson:"userId" json:"userId"`
	UserName    string        `bson:"userName" json:"userName"`
	Email       string        `bson:"email" json:"email"`
	IsBug       bool          `bson:"isBug" json:"isBug"` // true = bug, false = feature request
	Title       string        `bson:"title" json:"title"`
	Description string        `bson:"description" json:"description"`
	ImageURL    string        `bson:"imageURL,omitempty" json:"imageURL,omitempty"` // optional screenshot/image URL
	Status      string        `bson:"status" json:"status"`                         // "open", "in_progress", "resolved", "closed"
	Priority    string        `bson:"priority" json:"priority"`                     // "low", "medium", "high", "critical"
	AdminReply  string        `bson:"adminReply,omitempty" json:"adminReply,omitempty"` // Message/reply from admins
	AppVersion  string        `bson:"appVersion,omitempty" json:"appVersion,omitempty"`
	DeviceInfo  string        `bson:"deviceInfo,omitempty" json:"deviceInfo,omitempty"`
	Platform    string        `bson:"platform,omitempty" json:"platform,omitempty"` // "ios", "android", "web"
	CreatedAt   time.Time     `bson:"createdAt" json:"createdAt"`
	UpdatedAt   time.Time     `bson:"updatedAt" json:"updatedAt"`
}
