package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Poem visibility options
const (
	PoemVisibilityPublic  = "public"
	PoemVisibilityPrivate = "private" // draft state
)

// Poem moods — fixed set matching the static chips on the frontend
var ValidMoods = map[string]bool{
	"love": true, "grief": true, "nature": true, "nostalgia": true,
	"hope": true, "dark": true, "spiritual": true, "humour": true,
	"life": true, "longing": true,
}

type Poem struct {
	ID            bson.ObjectID `bson:"_id,omitempty"        json:"id"`
	AuthorID      bson.ObjectID `bson:"authorId"             json:"authorId"`
	Title         string        `bson:"title"                json:"title"`
	ContentJSON   string        `bson:"contentJson"          json:"contentJson"`   // Quill Delta JSON string
	PlainText     string        `bson:"plainText"            json:"plainText"`     // stripped plain text for search
	Hashtags      []string      `bson:"hashtags"             json:"hashtags"`      // lowercase, no #, max 10
	Mood          string        `bson:"mood,omitempty"       json:"mood,omitempty"` // single value from ValidMoods
	IsOriginal    bool          `bson:"isOriginal"           json:"isOriginal"`    // copyright checkbox
	Visibility    string        `bson:"visibility"           json:"visibility"`    // public | private
	AudioURL      string        `bson:"audioUrl,omitempty"   json:"audioUrl,omitempty"` // Cloudinary URL
	AudioDuration int           `bson:"audioDuration"        json:"audioDuration"` // seconds, 0 if no audio
	CoverColor    string        `bson:"coverColor,omitempty" json:"coverColor,omitempty"` // hex color from editor
	Description   string        `bson:"description,omitempty" json:"description,omitempty"`
	TextAlign     string        `bson:"textAlign,omitempty"   json:"textAlign,omitempty"`   // "left" | "center" | "right"
	Mentions      []bson.ObjectID `bson:"mentions,omitempty"    json:"mentions,omitempty"`    // user IDs parsed from description @mentions
	LikesCount    int           `bson:"likesCount"           json:"likesCount"`
	CommentsCount int           `bson:"commentsCount"        json:"commentsCount"`
	RepostsCount  int            `bson:"repostsCount"         json:"repostsCount"`
	IsRepost      bool           `bson:"isRepost"             json:"isRepost"`
	OriginalID    *bson.ObjectID `bson:"originalId,omitempty" json:"originalId,omitempty"`
	RepostNote    string         `bson:"repostNote,omitempty" json:"repostNote,omitempty"`
	IsDeleted     bool           `bson:"isDeleted"            json:"isDeleted"`
	CreatedAt     time.Time      `bson:"createdAt"            json:"createdAt"`
	UpdatedAt     time.Time      `bson:"updatedAt"            json:"updatedAt"`
	// PublishedAt is set when a poem first becomes public (either created directly
	// as public, or when a draft transitions private→public).  Used as the sort key
	// so that publishing an old draft surfaces it as a new entry at the top of lists.
	PublishedAt   *time.Time     `bson:"publishedAt,omitempty" json:"publishedAt,omitempty"`
}

// PoemResponse is what the API returns — includes author info inline
// so the frontend never needs a second user lookup
type PoemResponse struct {
	ID            string     `json:"id"`
	Author        PoemAuthor `json:"author"`
	Title         string     `json:"title"`
	ContentJSON   string     `json:"contentJson"`
	PlainText     string     `json:"plainText"`
	Hashtags      []string   `json:"hashtags"`
	Mood          string     `json:"mood,omitempty"`
	IsOriginal    bool       `json:"isOriginal"`
	Visibility    string     `json:"visibility"`
	AudioURL      string     `json:"audioUrl,omitempty"`
	AudioDuration int        `json:"audioDuration"`
	CoverColor    string     `json:"coverColor,omitempty"`
	Description   string     `json:"description,omitempty"`
	TextAlign     string     `json:"textAlign,omitempty"`
	Mentions      []MentionedUser `json:"mentions"`
	LikesCount     int           `json:"likesCount"`
	CommentsCount  int           `json:"commentsCount"`
	RepostsCount   int           `json:"repostsCount"`
	IsLikedByMe    bool          `json:"isLikedByMe"`
	IsRepostedByMe bool          `json:"isRepostedByMe"`
	IsRepost       bool          `json:"isRepost"`
	OriginalPoem   *PoemResponse `json:"originalPoem,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}

// PoemAuthor — embedded author info in every poem response
type PoemAuthor struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Username    string `json:"username"`
	PhotoURL    string `json:"photoURL"`
	IsEditor    bool   `json:"isEditor"`
}

// MentionedUser — embedded mention info in poem response
// so the frontend never needs a separate user lookup for mentions
type MentionedUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
	PhotoURL    string `json:"photoURL"`
}

// PoemsPage — paginated list response
type PoemsPage struct {
	Poems   []PoemResponse `json:"poems"`
	HasMore bool           `json:"hasMore"`
}
