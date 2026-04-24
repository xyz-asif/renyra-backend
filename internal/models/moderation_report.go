package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Report type constants for content moderation
type ReportTargetType string

const (
	ReportTargetTypeUser ReportTargetType = "user"
	ReportTargetTypePost ReportTargetType = "post"
)

// Report reason constants
type ReportReason string

const (
	ReportReasonSpam            ReportReason = "spam_or_misleading"
	ReportReasonHarassment      ReportReason = "harassment_or_bullying"
	ReportReasonInappropriate   ReportReason = "inappropriate_content"
	ReportReasonImpersonation   ReportReason = "impersonation"
	ReportReasonOther           ReportReason = "other"
)

// ModerationReport represents a user-generated report for inappropriate content or behavior
type ModerationReport struct {
	ID              bson.ObjectID    `bson:"_id,omitempty" json:"id"`
	ReporterID      bson.ObjectID    `bson:"reporterId" json:"reporterId"`
	ReporterName    string           `bson:"reporterName" json:"reporterName"`
	ReporterEmail   string           `bson:"reporterEmail" json:"reporterEmail"`
	TargetType      ReportTargetType `bson:"targetType" json:"targetType"` // "user" or "post"
	TargetID        bson.ObjectID    `bson:"targetId" json:"targetId"`     // ID of reported user or post
	TargetName      string           `bson:"targetName,omitempty" json:"targetName,omitempty"` // Display name of target user or post title
	Reason          ReportReason     `bson:"reason" json:"reason"`
	Details         string           `bson:"details,omitempty" json:"details,omitempty"`
	Status          string           `bson:"status" json:"status"` // "pending", "under_review", "resolved", "dismissed"
	AdminNotes      string           `bson:"adminNotes,omitempty" json:"adminNotes,omitempty"`
	ResolvedBy      bson.ObjectID    `bson:"resolvedBy,omitempty" json:"resolvedBy,omitempty"`
	Resolution      string           `bson:"resolution,omitempty" json:"resolution,omitempty"`
	CreatedAt       time.Time        `bson:"createdAt" json:"createdAt"`
	UpdatedAt       time.Time        `bson:"updatedAt" json:"updatedAt"`
	ResolvedAt      *time.Time       `bson:"resolvedAt,omitempty" json:"resolvedAt,omitempty"`
}
