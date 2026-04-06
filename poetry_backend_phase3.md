# ChatBee Poetry App — Backend Phase 3
### Go + MongoDB + Fiber — Extends Existing Codebase

---

## Overview

This document covers:
1. Poem likes
2. Comments (flat, no nesting) with comment likes
3. Reposts (Twitter-style)
4. Mention detection in comments
5. Notifications wired to all social actions
6. Audio feed endpoint

Everything builds on the existing codebase. Do not touch any existing chat, WebSocket, profile, follow, or poem CRUD code unless explicitly stated.

---

## Part 1 — New Models

### File: `internal/models/social.go`

```go
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
    ID          string    `json:"id"`
    PoemID      string    `json:"poemId"`
    Author      PoemAuthor `json:"author"`
    Content     string    `json:"content"`
    LikesCount  int       `json:"likesCount"`
    IsLikedByMe bool      `json:"isLikedByMe"`
    IsDeleted   bool      `json:"isDeleted"`
    CreatedAt   time.Time `json:"createdAt"`
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
// Adding new notification type constants to the existing set

const (
    NotifTypePoemLiked    = "poem_liked"
    NotifTypeCommented    = "commented"
    NotifTypeCommentLiked = "comment_liked"
    NotifTypeReposted     = "reposted"
    NotifTypeMentioned    = "mentioned"
    NotifTypeFollowed     = "followed"    // already exists — confirm it's defined
)
```

### Update `internal/models/poem.go`

The `Poem` struct already has `IsRepost` and `OriginalID` from the Phase 1 spec. Confirm these fields exist:

```go
IsRepost   bool           `bson:"isRepost"             json:"isRepost"`
OriginalID *bson.ObjectID `bson:"originalId,omitempty" json:"originalId,omitempty"`
RepostNote string         `bson:"repostNote,omitempty" json:"repostNote,omitempty"` // always empty for now (Twitter style = no caption)
```

If `IsRepost` and `OriginalID` are not already in the struct, add them now.

### Update `internal/models/feed.go`

Add `IsLikedByMe` and `IsRepostedByMe` to `PoemResponse`:

```go
// Add to existing PoemResponse struct:
IsLikedByMe   bool              `json:"isLikedByMe"`
IsRepostedByMe bool             `json:"isRepostedByMe"`

// For reposts: embed the original poem inline
OriginalPoem  *PoemResponse     `json:"originalPoem,omitempty"`
```

---

## Part 2 — MongoDB Indexes

### File: `internal/database/indexes.go`

Add these index blocks after existing ones:

```go
// ── Poem Likes ──
poemLikesIndexes := []mongo.IndexModel{
    // Unique: one like per user per poem
    {
        Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "poemId", Value: 1}},
        Options: options.Index().SetUnique(true),
    },
    // All likes on a poem (for likers list)
    {Keys: bson.D{{Key: "poemId", Value: 1}, {Key: "_id", Value: -1}}},
    // All poems a user liked
    {Keys: bson.D{{Key: "userId", Value: 1}, {Key: "_id", Value: -1}}},
}
if _, err := db.Collection("poem_likes").Indexes().CreateMany(ctx, poemLikesIndexes); err != nil {
    log.Printf("Warning: Poem likes index issue: %v", err)
}

// ── Comments ──
commentsIndexes := []mongo.IndexModel{
    // Primary: all comments on a poem, oldest first
    {Keys: bson.D{{Key: "poemId", Value: 1}, {Key: "_id", Value: 1}}},
    // Cursor pagination
    {Keys: bson.D{{Key: "poemId", Value: 1}, {Key: "_id", Value: -1}}},
}
if _, err := db.Collection("comments").Indexes().CreateMany(ctx, commentsIndexes); err != nil {
    log.Printf("Warning: Comments index issue: %v", err)
}

// ── Comment Likes ──
commentLikesIndexes := []mongo.IndexModel{
    {
        Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "commentId", Value: 1}},
        Options: options.Index().SetUnique(true),
    },
    {Keys: bson.D{{Key: "commentId", Value: 1}}},
}
if _, err := db.Collection("comment_likes").Indexes().CreateMany(ctx, commentLikesIndexes); err != nil {
    log.Printf("Warning: Comment likes index issue: %v", err)
}
```

---

## Part 3 — Social Feature Package

### New package: `internal/features/social`

```
internal/features/social/
    repository.go
    service.go
    handler.go
```

### File: `internal/features/social/repository.go`

```go
package social

import (
    "context"
    "time"

    "github.com/xyz-asif/gotodo/internal/models"
    "go.mongodb.org/mongo-driver/v2/bson"
    "go.mongodb.org/mongo-driver/v2/mongo"
    "go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
    // Poem likes
    LikePoem(ctx context.Context, userID, poemID bson.ObjectID) error
    UnlikePoem(ctx context.Context, userID, poemID bson.ObjectID) error
    IsPoemLiked(ctx context.Context, userID, poemID bson.ObjectID) (bool, error)
    IsPoemLikedMany(ctx context.Context, userID bson.ObjectID, poemIDs []bson.ObjectID) (map[string]bool, error)
    GetPoemLikers(ctx context.Context, poemID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.PoemLike, error)

    // Comments
    CreateComment(ctx context.Context, comment *models.Comment) error
    GetCommentByID(ctx context.Context, commentID bson.ObjectID) (*models.Comment, error)
    GetCommentsByPoem(ctx context.Context, poemID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Comment, error)
    SoftDeleteComment(ctx context.Context, commentID, authorID bson.ObjectID) error
    IncrementCommentLikes(ctx context.Context, commentID bson.ObjectID) error
    DecrementCommentLikes(ctx context.Context, commentID bson.ObjectID) error

    // Comment likes
    LikeComment(ctx context.Context, userID, commentID bson.ObjectID) error
    UnlikeComment(ctx context.Context, userID, commentID bson.ObjectID) error
    IsCommentLiked(ctx context.Context, userID, commentID bson.ObjectID) (bool, error)
    IsCommentLikedMany(ctx context.Context, userID bson.ObjectID, commentIDs []bson.ObjectID) (map[string]bool, error)

    // Poem counters (used when toggling likes/comments/reposts)
    IncrementPoemLikes(ctx context.Context, poemID bson.ObjectID) error
    DecrementPoemLikes(ctx context.Context, poemID bson.ObjectID) error
    IncrementPoemComments(ctx context.Context, poemID bson.ObjectID) error
    DecrementPoemComments(ctx context.Context, poemID bson.ObjectID) error
    IncrementPoemReposts(ctx context.Context, poemID bson.ObjectID) error
    DecrementPoemReposts(ctx context.Context, poemID bson.ObjectID) error
}

type repository struct {
    poemLikes    *mongo.Collection
    comments     *mongo.Collection
    commentLikes *mongo.Collection
    poems        *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
    return &repository{
        poemLikes:    db.Collection("poem_likes"),
        comments:     db.Collection("comments"),
        commentLikes: db.Collection("comment_likes"),
        poems:        db.Collection("poems"),
    }
}

// ── Poem likes ──

func (r *repository) LikePoem(ctx context.Context, userID, poemID bson.ObjectID) error {
    _, err := r.poemLikes.InsertOne(ctx, models.PoemLike{
        UserID: userID, PoemID: poemID, CreatedAt: time.Now(),
    })
    return err
}

func (r *repository) UnlikePoem(ctx context.Context, userID, poemID bson.ObjectID) error {
    _, err := r.poemLikes.DeleteOne(ctx, bson.M{"userId": userID, "poemId": poemID})
    return err
}

func (r *repository) IsPoemLiked(ctx context.Context, userID, poemID bson.ObjectID) (bool, error) {
    count, err := r.poemLikes.CountDocuments(ctx, bson.M{"userId": userID, "poemId": poemID})
    return count > 0, err
}

func (r *repository) IsPoemLikedMany(ctx context.Context, userID bson.ObjectID, poemIDs []bson.ObjectID) (map[string]bool, error) {
    cursor, err := r.poemLikes.Find(ctx, bson.M{
        "userId": userID,
        "poemId": bson.M{"$in": poemIDs},
    })
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)

    var likes []models.PoemLike
    if err := cursor.All(ctx, &likes); err != nil {
        return nil, err
    }
    result := make(map[string]bool)
    for _, l := range likes {
        result[l.PoemID.Hex()] = true
    }
    return result, nil
}

func (r *repository) GetPoemLikers(ctx context.Context, poemID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.PoemLike, error) {
    filter := bson.M{"poemId": poemID}
    if beforeID != nil {
        filter["_id"] = bson.M{"$lt": *beforeID}
    }
    opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit))
    cursor, err := r.poemLikes.Find(ctx, filter, opts)
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)
    var result []models.PoemLike
    if err := cursor.All(ctx, &result); err != nil {
        return nil, err
    }
    return result, nil
}

// ── Comments ──

func (r *repository) CreateComment(ctx context.Context, comment *models.Comment) error {
    comment.CreatedAt = time.Now()
    comment.UpdatedAt = time.Now()
    comment.IsDeleted = false
    comment.LikesCount = 0
    res, err := r.comments.InsertOne(ctx, comment)
    if err != nil {
        return err
    }
    comment.ID = res.InsertedID.(bson.ObjectID)
    return nil
}

func (r *repository) GetCommentByID(ctx context.Context, commentID bson.ObjectID) (*models.Comment, error) {
    var c models.Comment
    err := r.comments.FindOne(ctx, bson.M{"_id": commentID, "isDeleted": false}).Decode(&c)
    if err != nil {
        return nil, err
    }
    return &c, nil
}

func (r *repository) GetCommentsByPoem(ctx context.Context, poemID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Comment, error) {
    filter := bson.M{"poemId": poemID, "isDeleted": false}
    if beforeID != nil {
        filter["_id"] = bson.M{"$lt": *beforeID}
    }
    opts := options.Find().
        SetSort(bson.D{{Key: "_id", Value: -1}}). // newest first
        SetLimit(int64(limit))
    cursor, err := r.comments.Find(ctx, filter, opts)
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)
    var result []models.Comment
    if err := cursor.All(ctx, &result); err != nil {
        return nil, err
    }
    return result, nil
}

func (r *repository) SoftDeleteComment(ctx context.Context, commentID, authorID bson.ObjectID) error {
    _, err := r.comments.UpdateOne(ctx,
        bson.M{"_id": commentID, "authorId": authorID},
        bson.M{"$set": bson.M{"isDeleted": true, "content": "This comment was deleted", "updatedAt": time.Now()}},
    )
    return err
}

func (r *repository) IncrementCommentLikes(ctx context.Context, commentID bson.ObjectID) error {
    _, err := r.comments.UpdateOne(ctx, bson.M{"_id": commentID}, bson.M{"$inc": bson.M{"likesCount": 1}})
    return err
}

func (r *repository) DecrementCommentLikes(ctx context.Context, commentID bson.ObjectID) error {
    _, err := r.comments.UpdateOne(ctx,
        bson.M{"_id": commentID, "likesCount": bson.M{"$gt": 0}},
        bson.M{"$inc": bson.M{"likesCount": -1}},
    )
    return err
}

// ── Comment likes ──

func (r *repository) LikeComment(ctx context.Context, userID, commentID bson.ObjectID) error {
    _, err := r.commentLikes.InsertOne(ctx, models.CommentLike{
        UserID: userID, CommentID: commentID, CreatedAt: time.Now(),
    })
    return err
}

func (r *repository) UnlikeComment(ctx context.Context, userID, commentID bson.ObjectID) error {
    _, err := r.commentLikes.DeleteOne(ctx, bson.M{"userId": userID, "commentId": commentID})
    return err
}

func (r *repository) IsCommentLiked(ctx context.Context, userID, commentID bson.ObjectID) (bool, error) {
    count, err := r.commentLikes.CountDocuments(ctx, bson.M{"userId": userID, "commentId": commentID})
    return count > 0, err
}

func (r *repository) IsCommentLikedMany(ctx context.Context, userID bson.ObjectID, commentIDs []bson.ObjectID) (map[string]bool, error) {
    cursor, err := r.commentLikes.Find(ctx, bson.M{
        "userId":    userID,
        "commentId": bson.M{"$in": commentIDs},
    })
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)
    var likes []models.CommentLike
    if err := cursor.All(ctx, &likes); err != nil {
        return nil, err
    }
    result := make(map[string]bool)
    for _, l := range likes {
        result[l.CommentID.Hex()] = true
    }
    return result, nil
}

// ── Poem counters ──

func (r *repository) IncrementPoemLikes(ctx context.Context, poemID bson.ObjectID) error {
    _, err := r.poems.UpdateOne(ctx, bson.M{"_id": poemID}, bson.M{"$inc": bson.M{"likesCount": 1}})
    return err
}

func (r *repository) DecrementPoemLikes(ctx context.Context, poemID bson.ObjectID) error {
    _, err := r.poems.UpdateOne(ctx, bson.M{"_id": poemID, "likesCount": bson.M{"$gt": 0}}, bson.M{"$inc": bson.M{"likesCount": -1}})
    return err
}

func (r *repository) IncrementPoemComments(ctx context.Context, poemID bson.ObjectID) error {
    _, err := r.poems.UpdateOne(ctx, bson.M{"_id": poemID}, bson.M{"$inc": bson.M{"commentsCount": 1}})
    return err
}

func (r *repository) DecrementPoemComments(ctx context.Context, poemID bson.ObjectID) error {
    _, err := r.poems.UpdateOne(ctx, bson.M{"_id": poemID, "commentsCount": bson.M{"$gt": 0}}, bson.M{"$inc": bson.M{"commentsCount": -1}})
    return err
}

func (r *repository) IncrementPoemReposts(ctx context.Context, poemID bson.ObjectID) error {
    _, err := r.poems.UpdateOne(ctx, bson.M{"_id": poemID}, bson.M{"$inc": bson.M{"repostsCount": 1}})
    return err
}

func (r *repository) DecrementPoemReposts(ctx context.Context, poemID bson.ObjectID) error {
    _, err := r.poems.UpdateOne(ctx, bson.M{"_id": poemID, "repostsCount": bson.M{"$gt": 0}}, bson.M{"$inc": bson.M{"repostsCount": -1}})
    return err
}
```

### File: `internal/features/social/service.go`

```go
package social

import (
    "context"
    "errors"
    "regexp"
    "strings"
    "time"

    "github.com/xyz-asif/gotodo/internal/features/notifications"
    "github.com/xyz-asif/gotodo/internal/features/users"
    "github.com/xyz-asif/gotodo/internal/models"
    "go.mongodb.org/mongo-driver/v2/bson"
    "go.mongodb.org/mongo-driver/v2/mongo"
)

// mentionRegex matches @username patterns in comment text
var mentionRegex = regexp.MustCompile(`@([a-z0-9_]{3,30})`)

type Service interface {
    // Poem likes
    TogglePoemLike(ctx context.Context, userIDStr, poemIDStr string) (bool, int, error)
    GetPoemLikers(ctx context.Context, poemIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error)

    // Comments
    AddComment(ctx context.Context, authorIDStr, poemIDStr, content string) (*models.CommentResponse, error)
    GetComments(ctx context.Context, poemIDStr, callerIDStr string, limit int, before string) (*models.CommentsPage, error)
    DeleteComment(ctx context.Context, authorIDStr, commentIDStr string) error
    ToggleCommentLike(ctx context.Context, userIDStr, commentIDStr string) (bool, int, error)

    // Reposts
    ToggleRepost(ctx context.Context, userIDStr, poemIDStr string) (bool, int, error)
    GetUserReposts(ctx context.Context, userIDStr string, limit int, before string) (*models.FeedPage, error)
}

type service struct {
    repo         Repository
    userRepo     users.Repository
    notifService notifications.Service
    poemsCol     *mongo.Collection // direct access for repost creation
}

func NewService(repo Repository, userRepo users.Repository, notifService notifications.Service, db interface{ Collection(string) *mongo.Collection }) Service {
    return &service{
        repo:         repo,
        userRepo:     userRepo,
        notifService: notifService,
        poemsCol:     db.Collection("poems"),
    }
}

// ── Poem likes ──

func (s *service) TogglePoemLike(ctx context.Context, userIDStr, poemIDStr string) (bool, int, error) {
    userID, err := bson.ObjectIDFromHex(userIDStr)
    if err != nil {
        return false, 0, errors.New("invalid user id")
    }
    poemID, err := bson.ObjectIDFromHex(poemIDStr)
    if err != nil {
        return false, 0, errors.New("invalid poem id")
    }

    // Fetch poem to verify it exists and get author for notification
    var poem models.Poem
    if err := s.poemsCol.FindOne(ctx, bson.M{"_id": poemID, "isDeleted": false}).Decode(&poem); err != nil {
        return false, 0, errors.New("poem not found")
    }

    alreadyLiked, err := s.repo.IsPoemLiked(ctx, userID, poemID)
    if err != nil {
        return false, 0, err
    }

    if alreadyLiked {
        if err := s.repo.UnlikePoem(ctx, userID, poemID); err != nil {
            return false, 0, err
        }
        go func() { _ = s.repo.DecrementPoemLikes(context.Background(), poemID) }()
        newCount := poem.LikesCount - 1
        if newCount < 0 { newCount = 0 }
        return false, newCount, nil
    }

    if err := s.repo.LikePoem(ctx, userID, poemID); err != nil {
        if mongo.IsDuplicateKeyError(err) {
            return true, poem.LikesCount, nil
        }
        return false, 0, err
    }

    go func() {
        _ = s.repo.IncrementPoemLikes(context.Background(), poemID)

        // Notify poem author — but not if liking own poem
        if poem.AuthorID != userID && s.notifService != nil {
            liker, _ := s.userRepo.GetUserByID(context.Background(), userID)
            name := "Someone"
            if liker != nil { name = liker.DisplayName }
            _ = s.notifService.Send(context.Background(), models.SendNotificationRequest{
                RecipientID:  poem.AuthorID,
                ActorID:      userID,
                Type:         models.NotifTypePoemLiked,
                ResourceType: "poem",
                ResourceID:   poemIDStr,
                Title:        name,
                Body:         "liked your poem \"" + poem.Title + "\"",
                GroupKey:     "like:" + poemIDStr,
            })
        }
    }()

    return true, poem.LikesCount + 1, nil
}

func (s *service) GetPoemLikers(ctx context.Context, poemIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error) {
    poemID, err := bson.ObjectIDFromHex(poemIDStr)
    if err != nil {
        return nil, false, errors.New("invalid poem id")
    }
    if limit <= 0 { limit = 20 }
    if limit > 50 { limit = 50 }

    var beforeID *bson.ObjectID
    if before != "" {
        id, err := bson.ObjectIDFromHex(before)
        if err != nil {
            return nil, false, errors.New("invalid before cursor")
        }
        beforeID = &id
    }

    likes, err := s.repo.GetPoemLikers(ctx, poemID, limit+1, beforeID)
    if err != nil {
        return nil, false, err
    }

    hasMore := len(likes) > limit
    if hasMore { likes = likes[:limit] }

    ids := make([]bson.ObjectID, 0, len(likes))
    for _, l := range likes { ids = append(ids, l.UserID) }

    userMap, err := s.userRepo.GetUsersByIDs(ctx, ids)
    if err != nil {
        return nil, false, err
    }

    results := make([]models.UserSearchResult, 0, len(ids))
    for _, id := range ids {
        user, ok := userMap[id]
        if !ok { continue }
        results = append(results, models.UserSearchResult{
            ID:          user.ID.Hex(),
            DisplayName: user.DisplayName,
            Username:    user.Username,
            PhotoURL:    user.PhotoURL,
            IsEditor:    user.IsEditor,
        })
    }
    return results, hasMore, nil
}

// ── Comments ──

func (s *service) AddComment(ctx context.Context, authorIDStr, poemIDStr, content string) (*models.CommentResponse, error) {
    authorID, err := bson.ObjectIDFromHex(authorIDStr)
    if err != nil {
        return nil, errors.New("invalid author id")
    }
    poemID, err := bson.ObjectIDFromHex(poemIDStr)
    if err != nil {
        return nil, errors.New("invalid poem id")
    }

    content = strings.TrimSpace(content)
    if content == "" {
        return nil, errors.New("comment cannot be empty")
    }
    if len([]rune(content)) > 500 {
        return nil, errors.New("comment must be 500 characters or less")
    }

    // Verify poem exists
    var poem models.Poem
    if err := s.poemsCol.FindOne(ctx, bson.M{"_id": poemID, "isDeleted": false}).Decode(&poem); err != nil {
        return nil, errors.New("poem not found")
    }

    comment := &models.Comment{
        PoemID:   poemID,
        AuthorID: authorID,
        Content:  content,
    }

    if err := s.repo.CreateComment(ctx, comment); err != nil {
        return nil, err
    }

    go func() {
        _ = s.repo.IncrementPoemComments(context.Background(), poemID)

        commenter, _ := s.userRepo.GetUserByID(context.Background(), authorID)
        name := "Someone"
        if commenter != nil { name = commenter.DisplayName }

        // Notify poem author (if not self-comment)
        if poem.AuthorID != authorID && s.notifService != nil {
            _ = s.notifService.Send(context.Background(), models.SendNotificationRequest{
                RecipientID:  poem.AuthorID,
                ActorID:      authorID,
                Type:         models.NotifTypeCommented,
                ResourceType: "poem",
                ResourceID:   poemIDStr,
                Title:        name,
                Body:         "commented on your post",
                GroupKey:     "comment:" + poemIDStr,
            })
        }

        // Detect @mentions and notify each mentioned user
        s.processMentions(context.Background(), content, authorID, poemIDStr, comment.ID.Hex())
    }()

    author, _ := s.userRepo.GetUserByID(ctx, authorID)
    resp := &models.CommentResponse{
        ID:     comment.ID.Hex(),
        PoemID: poemIDStr,
        Content: content,
        CreatedAt: comment.CreatedAt,
    }
    if author != nil {
        resp.Author = models.PoemAuthor{
            ID:          author.ID.Hex(),
            DisplayName: author.DisplayName,
            Username:    author.Username,
            PhotoURL:    author.PhotoURL,
            IsEditor:    author.IsEditor,
        }
    }
    return resp, nil
}

// processMentions scans comment content for @username patterns and sends notifications.
func (s *service) processMentions(ctx context.Context, content string, authorID bson.ObjectID, poemIDStr, commentIDStr string) {
    if s.notifService == nil {
        return
    }
    matches := mentionRegex.FindAllStringSubmatch(content, -1)
    notified := make(map[string]bool) // prevent duplicate notifications

    for _, match := range matches {
        if len(match) < 2 { continue }
        username := match[1]
        if notified[username] { continue }
        notified[username] = true

        // Look up user by username
        mentionedUser, err := s.userRepo.GetUserByUsername(ctx, username)
        if err != nil || mentionedUser == nil { continue }
        if mentionedUser.ID == authorID { continue } // don't notify self-mention

        commenter, _ := s.userRepo.GetUserByID(ctx, authorID)
        name := "Someone"
        if commenter != nil { name = commenter.DisplayName }

        _ = s.notifService.Send(ctx, models.SendNotificationRequest{
            RecipientID:  mentionedUser.ID,
            ActorID:      authorID,
            Type:         models.NotifTypeMentioned,
            ResourceType: "comment",
            ResourceID:   commentIDStr,
            Title:        name,
            Body:         "mentioned you in a comment",
        })
    }
}

func (s *service) GetComments(ctx context.Context, poemIDStr, callerIDStr string, limit int, before string) (*models.CommentsPage, error) {
    poemID, err := bson.ObjectIDFromHex(poemIDStr)
    if err != nil {
        return nil, errors.New("invalid poem id")
    }
    if limit <= 0 { limit = 20 }
    if limit > 50 { limit = 50 }

    var beforeID *bson.ObjectID
    if before != "" {
        id, err := bson.ObjectIDFromHex(before)
        if err != nil {
            return nil, errors.New("invalid before cursor")
        }
        beforeID = &id
    }

    comments, err := s.repo.GetCommentsByPoem(ctx, poemID, limit+1, beforeID)
    if err != nil {
        return nil, err
    }

    hasMore := len(comments) > limit
    if hasMore { comments = comments[:limit] }

    // Batch check which comments the caller has liked
    var likedMap map[string]bool
    if callerIDStr != "" {
        callerID, err := bson.ObjectIDFromHex(callerIDStr)
        if err == nil {
            ids := make([]bson.ObjectID, 0, len(comments))
            for _, c := range comments { ids = append(ids, c.ID) }
            likedMap, _ = s.repo.IsCommentLikedMany(ctx, callerID, ids)
        }
    }

    responses := make([]models.CommentResponse, 0, len(comments))
    for _, c := range comments {
        resp := models.CommentResponse{
            ID:          c.ID.Hex(),
            PoemID:      poemIDStr,
            Content:     c.Content,
            LikesCount:  c.LikesCount,
            IsLikedByMe: likedMap[c.ID.Hex()],
            IsDeleted:   c.IsDeleted,
            CreatedAt:   c.CreatedAt,
        }
        author, _ := s.userRepo.GetUserByID(ctx, c.AuthorID)
        if author != nil {
            resp.Author = models.PoemAuthor{
                ID:          author.ID.Hex(),
                DisplayName: author.DisplayName,
                Username:    author.Username,
                PhotoURL:    author.PhotoURL,
                IsEditor:    author.IsEditor,
            }
        }
        responses = append(responses, resp)
    }

    return &models.CommentsPage{Comments: responses, HasMore: hasMore}, nil
}

func (s *service) DeleteComment(ctx context.Context, authorIDStr, commentIDStr string) error {
    authorID, err := bson.ObjectIDFromHex(authorIDStr)
    if err != nil {
        return errors.New("invalid author id")
    }
    commentID, err := bson.ObjectIDFromHex(commentIDStr)
    if err != nil {
        return errors.New("invalid comment id")
    }

    comment, err := s.repo.GetCommentByID(ctx, commentID)
    if err != nil {
        return errors.New("comment not found")
    }
    if comment.AuthorID != authorID {
        return errors.New("unauthorized: only the comment author can delete this")
    }

    if err := s.repo.SoftDeleteComment(ctx, commentID, authorID); err != nil {
        return err
    }

    go func() { _ = s.repo.DecrementPoemComments(context.Background(), comment.PoemID) }()
    return nil
}

func (s *service) ToggleCommentLike(ctx context.Context, userIDStr, commentIDStr string) (bool, int, error) {
    userID, err := bson.ObjectIDFromHex(userIDStr)
    if err != nil {
        return false, 0, errors.New("invalid user id")
    }
    commentID, err := bson.ObjectIDFromHex(commentIDStr)
    if err != nil {
        return false, 0, errors.New("invalid comment id")
    }

    comment, err := s.repo.GetCommentByID(ctx, commentID)
    if err != nil {
        return false, 0, errors.New("comment not found")
    }

    alreadyLiked, err := s.repo.IsCommentLiked(ctx, userID, commentID)
    if err != nil {
        return false, 0, err
    }

    if alreadyLiked {
        if err := s.repo.UnlikeComment(ctx, userID, commentID); err != nil {
            return false, 0, err
        }
        go func() { _ = s.repo.DecrementCommentLikes(context.Background(), commentID) }()
        newCount := comment.LikesCount - 1
        if newCount < 0 { newCount = 0 }
        return false, newCount, nil
    }

    if err := s.repo.LikeComment(ctx, userID, commentID); err != nil {
        if mongo.IsDuplicateKeyError(err) {
            return true, comment.LikesCount, nil
        }
        return false, 0, err
    }

    go func() {
        _ = s.repo.IncrementCommentLikes(context.Background(), commentID)

        // Notify comment author
        if comment.AuthorID != userID && s.notifService != nil {
            liker, _ := s.userRepo.GetUserByID(context.Background(), userID)
            name := "Someone"
            if liker != nil { name = liker.DisplayName }
            _ = s.notifService.Send(context.Background(), models.SendNotificationRequest{
                RecipientID:  comment.AuthorID,
                ActorID:      userID,
                Type:         models.NotifTypeCommentLiked,
                ResourceType: "comment",
                ResourceID:   commentIDStr,
                Title:        name,
                Body:         "liked your comment",
                GroupKey:     "clike:" + commentIDStr,
            })
        }
    }()

    return true, comment.LikesCount + 1, nil
}

// ── Reposts ──

func (s *service) ToggleRepost(ctx context.Context, userIDStr, poemIDStr string) (bool, int, error) {
    userID, err := bson.ObjectIDFromHex(userIDStr)
    if err != nil {
        return false, 0, errors.New("invalid user id")
    }
    poemID, err := bson.ObjectIDFromHex(poemIDStr)
    if err != nil {
        return false, 0, errors.New("invalid poem id")
    }

    // Verify original poem exists
    var original models.Poem
    if err := s.poemsCol.FindOne(ctx, bson.M{"_id": poemID, "isDeleted": false}).Decode(&original); err != nil {
        return false, 0, errors.New("poem not found")
    }

    // Can't repost your own poem
    if original.AuthorID == userID {
        return false, 0, errors.New("cannot repost your own poem")
    }

    // Check if already reposted
    var existingRepost models.Poem
    err = s.poemsCol.FindOne(ctx, bson.M{
        "authorId":  userID,
        "isRepost":  true,
        "originalId": poemID,
        "isDeleted": false,
    }).Decode(&existingRepost)

    if err == nil {
        // Already reposted — un-repost
        _, err = s.poemsCol.UpdateOne(ctx,
            bson.M{"_id": existingRepost.ID},
            bson.M{"$set": bson.M{"isDeleted": true, "updatedAt": time.Now()}},
        )
        if err != nil {
            return false, 0, err
        }
        go func() { _ = s.repo.DecrementPoemReposts(context.Background(), poemID) }()
        newCount := original.RepostsCount - 1
        if newCount < 0 { newCount = 0 }
        return false, newCount, nil
    }

    // Create repost document
    repost := &models.Poem{
        AuthorID:    userID,
        IsRepost:    true,
        OriginalID:  &poemID,
        Title:       original.Title,
        ContentJSON: original.ContentJSON,
        PlainText:   original.PlainText,
        Hashtags:    original.Hashtags,
        Visibility:  models.PoemVisibilityPublic,
    }
    repost.CreatedAt = time.Now()
    repost.UpdatedAt = time.Now()

    res, err := s.poemsCol.InsertOne(ctx, repost)
    if err != nil {
        return false, 0, err
    }
    repost.ID = res.InsertedID.(bson.ObjectID)

    go func() {
        _ = s.repo.IncrementPoemReposts(context.Background(), poemID)
        _ = s.userRepo.IncrementPostsCount(context.Background(), userID)

        // Notify original author
        if s.notifService != nil {
            reposter, _ := s.userRepo.GetUserByID(context.Background(), userID)
            name := "Someone"
            if reposter != nil { name = reposter.DisplayName }
            _ = s.notifService.Send(context.Background(), models.SendNotificationRequest{
                RecipientID:  original.AuthorID,
                ActorID:      userID,
                Type:         models.NotifTypeReposted,
                ResourceType: "poem",
                ResourceID:   poemIDStr,
                Title:        name,
                Body:         "reposted your poem \"" + original.Title + "\"",
                GroupKey:     "repost:" + poemIDStr,
            })
        }
    }()

    return true, original.RepostsCount + 1, nil
}

func (s *service) GetUserReposts(ctx context.Context, userIDStr string, limit int, before string) (*models.FeedPage, error) {
    userID, err := bson.ObjectIDFromHex(userIDStr)
    if err != nil {
        return nil, errors.New("invalid user id")
    }
    if limit <= 0 { limit = 20 }
    if limit > 50 { limit = 50 }

    filter := bson.M{
        "authorId":  userID,
        "isRepost":  true,
        "isDeleted": false,
    }

    var beforeID *bson.ObjectID
    if before != "" {
        id, err := bson.ObjectIDFromHex(before)
        if err != nil {
            return nil, errors.New("invalid before cursor")
        }
        beforeID = &id
        filter["_id"] = bson.M{"$lt": *beforeID}
    }

    opts := options.Find().
        SetSort(bson.D{{Key: "_id", Value: -1}}).
        SetLimit(int64(limit + 1))

    cursor, err := s.poemsCol.Find(ctx, filter, opts)
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)

    var reposts []models.Poem
    if err := cursor.All(ctx, &reposts); err != nil {
        return nil, err
    }

    hasMore := len(reposts) > limit
    if hasMore { reposts = reposts[:limit] }

    // For each repost, fetch the original poem and embed it
    responses := make([]models.PoemResponse, 0, len(reposts))
    for _, rp := range reposts {
        resp := s.buildRepostResponse(ctx, &rp)
        responses = append(responses, resp)
    }

    return &models.FeedPage{Poems: responses, HasMore: hasMore}, nil
}

func (s *service) buildRepostResponse(ctx context.Context, rp *models.Poem) models.PoemResponse {
    resp := models.PoemResponse{
        ID:        rp.ID.Hex(),
        IsRepost:  true, // NOTE: add IsRepost to PoemResponse if not present
        CreatedAt: rp.CreatedAt,
        UpdatedAt: rp.UpdatedAt,
    }

    // Reposter info
    reposter, _ := s.userRepo.GetUserByID(ctx, rp.AuthorID)
    if reposter != nil {
        resp.Author = models.PoemAuthor{
            ID:          reposter.ID.Hex(),
            DisplayName: reposter.DisplayName,
            Username:    reposter.Username,
            PhotoURL:    reposter.PhotoURL,
            IsEditor:    reposter.IsEditor,
        }
    }

    // Original poem embedded
    if rp.OriginalID != nil {
        var original models.Poem
        if err := s.poemsCol.FindOne(ctx, bson.M{"_id": *rp.OriginalID}).Decode(&original); err == nil {
            originalResp := models.PoemResponse{
                ID:            original.ID.Hex(),
                Title:         original.Title,
                ContentJSON:   original.ContentJSON,
                PlainText:     original.PlainText,
                Hashtags:      original.Hashtags,
                Mood:          original.Mood,
                IsOriginal:    original.IsOriginal,
                Visibility:    original.Visibility,
                AudioURL:      original.AudioURL,
                AudioDuration: original.AudioDuration,
                LikesCount:    original.LikesCount,
                CommentsCount: original.CommentsCount,
                RepostsCount:  original.RepostsCount,
                CreatedAt:     original.CreatedAt,
            }
            originalAuthor, _ := s.userRepo.GetUserByID(ctx, original.AuthorID)
            if originalAuthor != nil {
                originalResp.Author = models.PoemAuthor{
                    ID:          originalAuthor.ID.Hex(),
                    DisplayName: originalAuthor.DisplayName,
                    Username:    originalAuthor.Username,
                    PhotoURL:    originalAuthor.PhotoURL,
                    IsEditor:    originalAuthor.IsEditor,
                }
            }
            if originalResp.Hashtags == nil {
                originalResp.Hashtags = []string{}
            }
            resp.OriginalPoem = &originalResp
        }
    }

    return resp
}
```

### File: `internal/features/social/handler.go`

```go
package social

import (
    "github.com/gofiber/fiber/v2"
    "github.com/xyz-asif/gotodo/internal/models"
    "github.com/xyz-asif/gotodo/pkg/response"
)

type Handler struct {
    service Service
}

func NewHandler(service Service) *Handler {
    return &Handler{service: service}
}

// POST /api/v1/poems/:id/like
func (h *Handler) TogglePoemLike(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }
    poemID := c.Params("id")
    liked, count, err := h.service.TogglePoemLike(c.Context(), user.ID.Hex(), poemID)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "OK", fiber.Map{"liked": liked, "likesCount": count})
}

// GET /api/v1/poems/:id/likes?limit=20&before=<id>
func (h *Handler) GetPoemLikers(c *fiber.Ctx) error {
    poemID := c.Params("id")
    limit := c.QueryInt("limit", 20)
    before := c.Query("before")
    users, hasMore, err := h.service.GetPoemLikers(c.Context(), poemID, limit, before)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "Likers retrieved", fiber.Map{"users": users, "hasMore": hasMore})
}

// POST /api/v1/poems/:id/comments
func (h *Handler) AddComment(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }
    poemID := c.Params("id")
    var req struct {
        Content string `json:"content"`
    }
    if err := c.BodyParser(&req); err != nil {
        return response.BadRequest(c, "Invalid request body")
    }
    comment, err := h.service.AddComment(c.Context(), user.ID.Hex(), poemID, req.Content)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.Created(c, "Comment added", comment)
}

// GET /api/v1/poems/:id/comments?limit=20&before=<id>
func (h *Handler) GetComments(c *fiber.Ctx) error {
    poemID := c.Params("id")
    limit := c.QueryInt("limit", 20)
    before := c.Query("before")
    callerID := ""
    if user, ok := c.Locals("user").(*models.User); ok {
        callerID = user.ID.Hex()
    }
    page, err := h.service.GetComments(c.Context(), poemID, callerID, limit, before)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "Comments retrieved", page)
}

// DELETE /api/v1/comments/:id
func (h *Handler) DeleteComment(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }
    commentID := c.Params("id")
    if err := h.service.DeleteComment(c.Context(), user.ID.Hex(), commentID); err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "Comment deleted", nil)
}

// POST /api/v1/comments/:id/like
func (h *Handler) ToggleCommentLike(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }
    commentID := c.Params("id")
    liked, count, err := h.service.ToggleCommentLike(c.Context(), user.ID.Hex(), commentID)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "OK", fiber.Map{"liked": liked, "likesCount": count})
}

// POST /api/v1/poems/:id/repost
func (h *Handler) ToggleRepost(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }
    poemID := c.Params("id")
    reposted, count, err := h.service.ToggleRepost(c.Context(), user.ID.Hex(), poemID)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "OK", fiber.Map{"reposted": reposted, "repostsCount": count})
}

// GET /api/v1/users/:id/reposts?limit=20&before=<id>
func (h *Handler) GetUserReposts(c *fiber.Ctx) error {
    userID := c.Params("id")
    limit := c.QueryInt("limit", 20)
    before := c.Query("before")
    page, err := h.service.GetUserReposts(c.Context(), userID, limit, before)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "Reposts retrieved", page)
}
```

---

## Part 4 — Audio Feed Endpoint

### File: `internal/features/feed/repository.go`

Add this method to the existing feed `Repository` interface and implement it:

```go
// Add to interface:
GetAudioFeed(ctx context.Context, limit int, beforeID *bson.ObjectID) ([]models.Poem, error)

// Implementation:
func (r *repository) GetAudioFeed(ctx context.Context, limit int, beforeID *bson.ObjectID) ([]models.Poem, error) {
    matchFilter := bson.M{
        "visibility": models.PoemVisibilityPublic,
        "isDeleted":  false,
        "audioUrl":   bson.M{"$exists": true, "$ne": ""},
    }
    if beforeID != nil {
        matchFilter["_id"] = bson.M{"$lt": *beforeID}
    }

    // Same engagement scoring as explore feed
    pipeline := mongo.Pipeline{
        {{Key: "$match", Value: matchFilter}},
        {{Key: "$addFields", Value: bson.M{
            "engagementScore": bson.M{
                "$subtract": []interface{}{
                    bson.M{"$add": []interface{}{
                        bson.M{"$multiply": []interface{}{"$likesCount", 3}},
                        bson.M{"$multiply": []interface{}{"$commentsCount", 2}},
                        bson.M{"$multiply": []interface{}{"$repostsCount", 1.5}},
                    }},
                    bson.M{"$multiply": []interface{}{
                        bson.M{"$divide": []interface{}{
                            bson.M{"$subtract": []interface{}{
                                bson.M{"$toLong": "$$NOW"},
                                bson.M{"$toLong": "$createdAt"},
                            }},
                            3600000,
                        }},
                        0.5,
                    }},
                },
            },
        }}},
        {{Key: "$sort", Value: bson.D{
            {Key: "engagementScore", Value: -1},
            {Key: "_id", Value: -1},
        }}},
        {{Key: "$limit", Value: int64(limit)}},
    }

    cursor, err := r.poems.Aggregate(ctx, pipeline)
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)

    var result []models.Poem
    if err := cursor.All(ctx, &result); err != nil {
        return nil, err
    }
    return result, nil
}
```

### File: `internal/features/feed/service.go`

Add to the existing `Service` interface and implement:

```go
// Add to interface:
GetAudioFeed(ctx context.Context, limit int, before string) (*models.FeedPage, error)

// Implementation:
func (s *service) GetAudioFeed(ctx context.Context, limit int, before string) (*models.FeedPage, error) {
    if limit <= 0 { limit = 20 }
    if limit > 50 { limit = 50 }

    var beforeID *bson.ObjectID
    if before != "" {
        id, err := bson.ObjectIDFromHex(before)
        if err != nil {
            return nil, errors.New("invalid before cursor")
        }
        beforeID = &id
    }

    poemDocs, err := s.repo.GetAudioFeed(ctx, limit+1, beforeID)
    if err != nil {
        return nil, err
    }

    hasMore := len(poemDocs) > limit
    if hasMore { poemDocs = poemDocs[:limit] }

    responses := make([]models.PoemResponse, 0, len(poemDocs))
    for _, p := range poemDocs {
        responses = append(responses, s.buildPoemResponse(ctx, &p))
    }

    return &models.FeedPage{Poems: responses, HasMore: hasMore}, nil
}
```

### File: `internal/features/feed/handler.go`

Add to the existing handler:

```go
// GET /api/v1/feed/audio?limit=20&before=<id>
// Auth optional.
func (h *Handler) GetAudioFeed(c *fiber.Ctx) error {
    limit := c.QueryInt("limit", 20)
    before := c.Query("before")
    page, err := h.service.GetAudioFeed(c.Context(), limit, before)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "Audio feed retrieved", page)
}
```

---

## Part 5 — Add GetUserByUsername to Users Repository

### File: `internal/features/users/repository.go`

The mention detection in comments requires looking up a user by username. Add this to the existing `Repository` interface and implement it:

```go
// Add to interface:
GetUserByUsername(ctx context.Context, username string) (*models.User, error)

// Implementation:
func (r *repository) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
    var user models.User
    err := r.users.FindOne(ctx, bson.M{"username": username}).Decode(&user)
    if err != nil {
        return nil, err
    }
    return &user, nil
}
```

---

## Part 6 — Wire Routes

### File: `internal/routes/routes.go`

Update `SetupRoutes` signature to accept social and updated feed handlers:

```go
socialHandler *social.Handler,  // NEW
```

Add these routes:

```go
// ── Social: Poem Likes ──
api.Post("/poems/:id/like", auth.Protect(), socialHandler.TogglePoemLike)
api.Get("/poems/:id/likes", auth.OptionalAuth(), socialHandler.GetPoemLikers)

// ── Social: Comments ──
api.Post("/poems/:id/comments", auth.Protect(), socialHandler.AddComment)
api.Get("/poems/:id/comments", auth.OptionalAuth(), socialHandler.GetComments)
api.Delete("/comments/:id", auth.Protect(), socialHandler.DeleteComment)
api.Post("/comments/:id/like", auth.Protect(), socialHandler.ToggleCommentLike)

// ── Social: Reposts ──
api.Post("/poems/:id/repost", auth.Protect(), socialHandler.ToggleRepost)
api.Get("/users/:id/reposts", auth.OptionalAuth(), socialHandler.GetUserReposts)

// ── Feed: Audio ──
api.Get("/feed/audio", auth.OptionalAuth(), feedHandler.GetAudioFeed)
```

### File: `cmd/api/main.go`

```go
socialRepo := social.NewRepository(db.Database)
socialService := social.NewService(socialRepo, userRepo, notifService, db.Database)
socialHandler := social.NewHandler(socialService)
```

Pass `socialHandler` to `SetupRoutes`.

---

## Part 7 — Wire Follow Notification

The follow toggle in `follows/service.go` needs to send a notification. Add this to the `ToggleFollow` method's follow branch (not the unfollow branch):

### File: `internal/features/follows/service.go`

Add `notifService notifications.Service` to the `service` struct and `NewService` parameters. Then in `ToggleFollow`, after the follow succeeds:

```go
// Inside the "Follow" branch, after IncrementFollowers goroutine:
go func() {
    _ = s.userRepo.IncrementFollowersCount(context.Background(), followingID)
    _ = s.userRepo.IncrementFollowingCount(context.Background(), followerID)

    // Notify the followed user
    if s.notifService != nil {
        follower, _ := s.userRepo.GetUserByID(context.Background(), followerID)
        name := "Someone"
        if follower != nil { name = follower.DisplayName }
        _ = s.notifService.Send(context.Background(), models.SendNotificationRequest{
            RecipientID:  followingID,
            ActorID:      followerID,
            Type:         models.NotifTypeFollowed,
            ResourceType: "user",
            ResourceID:   followerID.Hex(),
            Title:        name,
            Body:         "started following you",
        })
    }
}()
```

Update `NewService` in `follows/service.go`:
```go
func NewService(repo Repository, userRepo users.Repository, notifService notifications.Service) Service {
    return &service{repo: repo, userRepo: userRepo, notifService: notifService}
}
```

Update the call in `main.go` accordingly:
```go
followService := follows.NewService(followRepo, userRepo, notifService)
```

---

## Part 8 — Implementation Order

1. Add new models to `models/social.go`
2. Add `IsRepost`, `OriginalID` to `Poem` struct if missing
3. Add `IsLikedByMe`, `IsRepostedByMe`, `OriginalPoem`, `IsRepost` to `PoemResponse`
4. Add indexes for poem_likes, comments, comment_likes
5. Add `GetUserByUsername` to users repository
6. Create `features/social/` package (repository → service → handler)
7. Add `GetAudioFeed` to feed repository, service, and handler
8. Wire follow notification in `follows/service.go`
9. Update `routes/routes.go` and `main.go`
10. Run `go build ./...`

---

## API Summary

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | `/api/v1/poems/:id/like` | Required | Toggle like on poem |
| GET | `/api/v1/poems/:id/likes` | Optional | Who liked a poem |
| POST | `/api/v1/poems/:id/comments` | Required | Add comment |
| GET | `/api/v1/poems/:id/comments` | Optional | Get comments |
| DELETE | `/api/v1/comments/:id` | Required | Delete own comment |
| POST | `/api/v1/comments/:id/like` | Required | Toggle like on comment |
| POST | `/api/v1/poems/:id/repost` | Required | Toggle repost |
| GET | `/api/v1/users/:id/reposts` | Optional | User's reposts |
| GET | `/api/v1/feed/audio` | Optional | Audio poems feed |
