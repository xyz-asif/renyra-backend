package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Notification types — add new ones as you add features.
// The frontend switches on these to decide the icon and navigation.
const (
	NotifTypeConnectionRequest  = "connection_request"
	NotifTypeConnectionAccepted = "connection_accepted"
	NotifTypeNewMessage         = "new_message"
	NotifTypeReportStatusUpdated = "report_status_updated"
	NotifTypeReportAdminReply    = "report_admin_reply"
)

// Resource types — what entity the notification points to.
// The frontend switches on these to decide WHERE to navigate.
const (
	ResourceTypeConnection     = "connection"
	ResourceTypeChatRoom       = "chat_room"
	ResourceTypePoem           = "poem"
	ResourceTypeBugReport      = "bug_report"
	ResourceTypeFeatureRequest = "feature_request"
)

// Notification is the DB document stored in the "notifications" collection.
type Notification struct {
	ID           bson.ObjectID `bson:"_id,omitempty" json:"id"`
	RecipientID  bson.ObjectID `bson:"recipientId" json:"recipientId"`
	ActorID      bson.ObjectID `bson:"actorId" json:"actorId"`
	Type         string        `bson:"type" json:"type"`                 // e.g. "connection_request"
	ResourceType string        `bson:"resourceType" json:"resourceType"` // e.g. "connection"
	ResourceID   string        `bson:"resourceId" json:"resourceId"`     // hex ID of the resource
	Title        string        `bson:"title" json:"title"`
	Body         string        `bson:"body" json:"body"`
	ImageURL     string        `bson:"imageUrl,omitempty" json:"imageUrl,omitempty"` // actor's photo
	IsRead       bool          `bson:"isRead" json:"isRead"`
	GroupKey     string        `bson:"groupKey,omitempty" json:"groupKey,omitempty"` // for dedup/grouping
	CreatedAt    time.Time     `bson:"createdAt" json:"createdAt"`
}

// NotificationResponse is what the API returns to the frontend.
// Includes actor display info so the frontend doesn't need a separate user lookup.
type NotificationResponse struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	ResourceType  string    `json:"resourceType"`
	ResourceID    string    `json:"resourceId"`
	Title         string    `json:"title"`
	Body          string    `json:"body"`
	ActorID       string    `json:"actorId"`
	ActorName     string    `json:"actorName"`
	ActorPhotoURL string    `json:"actorPhotoUrl,omitempty"`
	IsRead        bool      `json:"isRead"`
	CreatedAt     time.Time `json:"createdAt"`
}

// SendNotificationRequest is the input other services use to create a notification.
// This is an internal struct, not an API request body.
type SendNotificationRequest struct {
	RecipientID  bson.ObjectID
	ActorID      bson.ObjectID
	Type         string // NotifType constant
	ResourceType string // ResourceType constant
	ResourceID   string // hex string of the resource
	Title        string
	Body         string
	GroupKey     string // optional: for dedup (e.g. "msg:<roomId>" to group messages per room)
}
