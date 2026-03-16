package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)



// UserStats holds denormalized statistics for a user.
type UserStats struct {
	FollowersCount        int `bson:"followersCount" json:"followersCount"`
	FollowingCount        int `bson:"followingCount" json:"followingCount"`
	TotalAnchors          int `bson:"totalAnchors" json:"totalAnchors"`
	ActiveAnchors         int `bson:"activeAnchors" json:"activeAnchors"`
	PublicAnchorsCount    int `bson:"publicAnchorsCount" json:"publicAnchorsCount"`
	TotalLikesReceived    int `bson:"totalLikesReceived" json:"totalLikesReceived"`
	TotalClonesReceived   int `bson:"totalClonesReceived" json:"totalClonesReceived"`
	TotalCommentsReceived int `bson:"totalCommentsReceived" json:"totalCommentsReceived"`
	TotalProfileViews     int `bson:"totalProfileViews" json:"totalProfileViews"`
}

// UserPreferences holds user-specific settings.
type UserPreferences struct {
	NotificationsEnabled    bool   `bson:"notificationsEnabled" json:"notificationsEnabled"`
	Theme                   string `bson:"theme" json:"theme"`
	Language                string `bson:"language" json:"language"`
	PrivateProfile          bool   `bson:"privateProfile" json:"privateProfile"`
	DefaultAnchorVisibility string `bson:"defaultAnchorVisibility" json:"defaultAnchorVisibility"`
}

// UserNotificationPreferences holds settings for specific notification types.
type UserNotificationPreferences struct {
	AnchorFollowed bool `bson:"anchorFollowed" json:"anchorFollowed"`
	NewContent     bool `bson:"newContent" json:"newContent"`
	NewFollower    bool `bson:"newFollower" json:"newFollower"`
	NewLike        bool `bson:"newLike" json:"newLike"`
	NewComment     bool `bson:"newComment" json:"newComment"`
	NewClone       bool `bson:"newClone" json:"newClone"`
	WeeklyDigest   bool `bson:"weeklyDigest" json:"weeklyDigest"`
}

// User represents a user in the system.
type User struct {
	ID                      bson.ObjectID               `bson:"_id,omitempty" json:"id"`
	FirebaseUID             string                      `bson:"firebaseUid" json:"firebaseUid"`
	Email                   string                      `bson:"email" json:"email"`
	DisplayName             string                      `bson:"displayName" json:"displayName"`
	PhotoURL                string                      `bson:"photoURL" json:"photoURL"`
	Bio                     string                      `bson:"bio" json:"bio"`
	Username                string                      `bson:"username,omitempty" json:"username,omitempty"`
	ExternalLink            string                      `bson:"externalLink,omitempty" json:"externalLink,omitempty"`
	CoverImageURL           string                      `bson:"coverImageURL,omitempty" json:"coverImageURL,omitempty"`
	IsProfileSetup          bool                        `bson:"isProfileSetup" json:"isProfileSetup"`
	IsEditor                bool                        `bson:"isEditor" json:"isEditor"`
	PostsCount              int                         `bson:"postsCount" json:"postsCount"`
	FollowersCount          int                         `bson:"followersCount" json:"followersCount"`
	FollowingCount          int                         `bson:"followingCount" json:"followingCount"`
	CreatedAt               time.Time                   `bson:"createdAt" json:"createdAt"`
	UpdatedAt               time.Time                   `bson:"updatedAt" json:"updatedAt"`
	LastLoginAt             time.Time                   `bson:"lastLoginAt" json:"lastLoginAt"`
	IsActive                bool                        `bson:"isActive" json:"isActive"`
	IsBanned                bool                        `bson:"isBanned" json:"isBanned"`
	BannedReason            *string                     `bson:"bannedReason" json:"bannedReason,omitempty"`
	BannedAt                *time.Time                  `bson:"bannedAt" json:"bannedAt,omitempty"`
	Stats                   UserStats                   `bson:"stats" json:"stats"`
	FCMTokens               []string                    `bson:"fcmTokens" json:"fcmTokens"`
	Preferences             UserPreferences             `bson:"preferences" json:"preferences"`
	NotificationPreferences UserNotificationPreferences `bson:"notificationPreferences" json:"notificationPreferences"`
}

// UserView represents a view event on a user profile.
type UserView struct {
	ID            bson.ObjectID  `bson:"_id,omitempty" json:"id"`
	UserID        bson.ObjectID  `bson:"userId" json:"userId"`
	ViewerID      *bson.ObjectID `bson:"viewerId" json:"viewerId,omitempty"` // null if anonymous
	SessionID     string         `bson:"sessionId" json:"sessionId"`
	DeviceType    string         `bson:"deviceType" json:"deviceType"`
	UserAgent     string         `bson:"userAgent" json:"userAgent"`
	Duration      int            `bson:"duration" json:"duration"`
	AnchorsViewed int            `bson:"anchorsViewed" json:"anchorsViewed"`
	Referrer      string         `bson:"referrer" json:"referrer"`
	ReferrerURL   string         `bson:"referrerUrl" json:"referrerUrl"`
	CreatedAt     time.Time      `bson:"createdAt" json:"createdAt"`
}
