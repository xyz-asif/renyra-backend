package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// ── Poem Like ──

type PoemLike struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    bson.ObjectID `bson:"userId"        json:"userId"`
	PoemID    bson.ObjectID `bson:"poemId"        json:"poemId"`
	CreatedAt time.Time     `bson:"createdAt"     json:"createdAt"`
}

// ── Comment ──

type Comment struct {
	ID         bson.ObjectID `bson:"_id,omitempty"      json:"id"`
	PoemID     bson.ObjectID `bson:"poemId"             json:"poemId"`
	AuthorID   bson.ObjectID `bson:"authorId"           json:"authorId"`
	Content    string        `bson:"content"            json:"content"`
	LikesCount int           `bson:"likesCount"         json:"likesCount"`
	IsDeleted  bool          `bson:"isDeleted"          json:"isDeleted"`
	CreatedAt  time.Time     `bson:"createdAt"          json:"createdAt"`
	UpdatedAt  time.Time     `bson:"updatedAt"          json:"updatedAt"`
}

// CommentResponse — enriched with author info and isLikedByMe
type CommentResponse struct {
	ID          string     `json:"id"`
	PoemID      string     `json:"poemId"`
	Author      PoemAuthor `json:"author"`
	Content     string     `json:"content"`
	LikesCount  int        `json:"likesCount"`
	IsLikedByMe bool       `json:"isLikedByMe"`
	IsDeleted   bool       `json:"isDeleted"`
	CreatedAt   time.Time  `json:"createdAt"`
}

type CommentsPage struct {
	Comments []CommentResponse `json:"comments"`
	HasMore  bool              `json:"hasMore"`
}

// ── Comment Like ──

type CommentLike struct {
	ID        bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID    bson.ObjectID `bson:"userId"        json:"userId"`
	CommentID bson.ObjectID `bson:"commentId"     json:"commentId"`
	CreatedAt time.Time     `bson:"createdAt"     json:"createdAt"`
}

// ── Notification types ──

const (
	NotifTypePoemLiked    = "poem_liked"
	NotifTypeCommented    = "commented"
	NotifTypeCommentLiked = "comment_liked"
	NotifTypeReposted     = "reposted"
	NotifTypeMentioned    = "mentioned"
	NotifTypeFollowed     = "followed"
)
