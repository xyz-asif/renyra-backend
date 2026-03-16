# ChatBee Poetry App — Backend Phase 2
### Go + MongoDB + Fiber — Extends Existing Codebase

---

## Overview

This document covers:
1. Remove connection guard from chat room creation
2. Follow/unfollow system
3. Public user profile endpoint
4. Home feed (poems from followed users)
5. Explore feed (all public poems, scored by engagement + recency)
6. Poem search and user search endpoints
7. Poem detail endpoint (single poem with full author info)

Everything builds on the existing codebase. Do not touch any existing chat, WebSocket, notification, or poem CRUD code unless explicitly stated.

---

## Part 1 — Remove Connection Guard from Chat

### File: `internal/features/chat/service.go`

In the `GetOrCreateDirectRoom` method, find and **delete** these lines:

```go
// DELETE THESE LINES — remove entirely
conn, err := s.connRepo.GetConnectionBetweenUsers(ctx, user1ID, user2ID)
if err != nil {
    return nil, err
}
if conn == nil || conn.Status != models.ConnectionStatusAccepted {
    return nil, errors.New("you must be connected (friends) with this user before chatting")
}
```

After deletion, the method goes directly from validating the two user IDs to calling `GetOrCreateDirectRoomAtomic`. Anyone can now start a chat with anyone.

Do not change anything else in this method or file.

---

## Part 2 — New Models

### File: `internal/models/follow.go`

```go
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
```

### File: `internal/models/user.go` — add to existing User struct

Add these two fields to the existing `User` struct. Do not remove or rename any existing fields:

```go
FollowersCount int `bson:"followersCount" json:"followersCount"`
FollowingCount int `bson:"followingCount" json:"followingCount"`
```

These were already specified in Phase 1 — add them now if not already present.

### File: `internal/models/feed.go`

```go
package models

// PublicProfileResponse — returned for any user's public profile
type PublicProfileResponse struct {
    ID             string `json:"id"`
    DisplayName    string `json:"displayName"`
    Username       string `json:"username"`
    PhotoURL       string `json:"photoURL"`
    CoverImageURL  string `json:"coverImageURL"`
    Bio            string `json:"bio"`
    ExternalLink   string `json:"externalLink"`
    IsEditor       bool   `json:"isEditor"`
    PostsCount     int    `json:"postsCount"`
    FollowersCount int    `json:"followersCount"`
    FollowingCount int    `json:"followingCount"`
    IsFollowedByMe bool   `json:"isFollowedByMe"` // true if the caller follows this user
    IsMe           bool   `json:"isMe"`           // true if caller == this user
}

// FeedPage — paginated poem feed response
type FeedPage struct {
    Poems   []PoemResponse `json:"poems"`
    HasMore bool           `json:"hasMore"`
}

// UserSearchResult — user result in search
type UserSearchResult struct {
    ID          string `json:"id"`
    DisplayName string `json:"displayName"`
    Username    string `json:"username"`
    PhotoURL    string `json:"photoURL"`
    IsEditor    bool   `json:"isEditor"`
    IsFollowing bool   `json:"isFollowing"` // true if caller follows this user
}

// UserSearchPage — paginated user search response
type UserSearchPage struct {
    Users   []UserSearchResult `json:"users"`
    HasMore bool               `json:"hasMore"`
}

// PoemSearchPage — paginated poem search response
type PoemSearchPage struct {
    Poems   []PoemResponse `json:"poems"`
    HasMore bool           `json:"hasMore"`
}
```

---

## Part 3 — MongoDB Indexes

### File: `internal/database/indexes.go`

Add these index groups to the existing `CreateIndexes` function. Add after the existing groups:

```go
// ── Follows ──
followsIndexes := []mongo.IndexModel{
    // Unique: one follow record per follower+following pair
    {
        Keys:    bson.D{{Key: "followerId", Value: 1}, {Key: "followingId", Value: 1}},
        Options: options.Index().SetUnique(true),
    },
    // Fetch all users that a person follows (for home feed)
    {Keys: bson.D{{Key: "followerId", Value: 1}, {Key: "_id", Value: -1}}},
    // Fetch all followers of a user (for profile followers list)
    {Keys: bson.D{{Key: "followingId", Value: 1}, {Key: "_id", Value: -1}}},
}
if _, err := db.Collection("follows").Indexes().CreateMany(ctx, followsIndexes); err != nil {
    log.Printf("Warning: Follows index issue: %v", err)
}

// ── Poems — additional indexes for feed and scoring ──
poemsFeedIndexes := []mongo.IndexModel{
    // Explore feed: public poems with engagement score sort
    // score is computed at query time via $addFields — this index covers the base filter
    {Keys: bson.D{{Key: "visibility", Value: 1}, {Key: "isDeleted", Value: 1}, {Key: "createdAt", Value: -1}}},
    // Home feed: poems by multiple authors, cursor pagination
    // MongoDB will use the existing authorId index for $in queries
}
if _, err := db.Collection("poems").Indexes().CreateMany(ctx, poemsFeedIndexes); err != nil {
    log.Printf("Warning: Poems feed index issue: %v", err)
}
```

---

## Part 4 — Existing User Repository Additions

### File: `internal/features/users/repository.go`

Add these methods to the existing `Repository` interface and implement them. Do not change any existing methods:

Add to interface:
```go
IncrementFollowersCount(ctx context.Context, userID bson.ObjectID) error
DecrementFollowersCount(ctx context.Context, userID bson.ObjectID) error
IncrementFollowingCount(ctx context.Context, userID bson.ObjectID) error
DecrementFollowingCount(ctx context.Context, userID bson.ObjectID) error
GetUsersByIDs(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]*models.User, error)
```

Add implementations:
```go
func (r *repository) IncrementFollowersCount(ctx context.Context, userID bson.ObjectID) error {
    _, err := r.users.UpdateOne(ctx,
        bson.M{"_id": userID},
        bson.M{"$inc": bson.M{"followersCount": 1}},
    )
    return err
}

func (r *repository) DecrementFollowersCount(ctx context.Context, userID bson.ObjectID) error {
    _, err := r.users.UpdateOne(ctx,
        bson.M{"_id": userID, "followersCount": bson.M{"$gt": 0}},
        bson.M{"$inc": bson.M{"followersCount": -1}},
    )
    return err
}

func (r *repository) IncrementFollowingCount(ctx context.Context, userID bson.ObjectID) error {
    _, err := r.users.UpdateOne(ctx,
        bson.M{"_id": userID},
        bson.M{"$inc": bson.M{"followingCount": 1}},
    )
    return err
}

func (r *repository) DecrementFollowingCount(ctx context.Context, userID bson.ObjectID) error {
    _, err := r.users.UpdateOne(ctx,
        bson.M{"_id": userID, "followingCount": bson.M{"$gt": 0}},
        bson.M{"$inc": bson.M{"followingCount": -1}},
    )
    return err
}

func (r *repository) GetUsersByIDs(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]*models.User, error) {
    cursor, err := r.users.Find(ctx, bson.M{"_id": bson.M{"$in": ids}})
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)

    result := make(map[bson.ObjectID]*models.User)
    var users []models.User
    if err := cursor.All(ctx, &users); err != nil {
        return nil, err
    }
    for i := range users {
        result[users[i].ID] = &users[i]
    }
    return result, nil
}
```

---

## Part 5 — Follow Feature Package

### New package: `internal/features/follows`

```
internal/features/follows/
    repository.go
    service.go
    handler.go
```

### File: `internal/features/follows/repository.go`

```go
package follows

import (
    "context"
    "time"

    "github.com/xyz-asif/gotodo/internal/models"
    "go.mongodb.org/mongo-driver/v2/bson"
    "go.mongodb.org/mongo-driver/v2/mongo"
    "go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
    Follow(ctx context.Context, followerID, followingID bson.ObjectID) error
    Unfollow(ctx context.Context, followerID, followingID bson.ObjectID) error
    IsFollowing(ctx context.Context, followerID, followingID bson.ObjectID) (bool, error)
    GetFollowers(ctx context.Context, userID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Follow, error)
    GetFollowing(ctx context.Context, userID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Follow, error)
    GetFollowingIDs(ctx context.Context, followerID bson.ObjectID) ([]bson.ObjectID, error)
    IsFollowingMany(ctx context.Context, followerID bson.ObjectID, targetIDs []bson.ObjectID) (map[string]bool, error)
}

type repository struct {
    follows *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
    return &repository{follows: db.Collection("follows")}
}

func (r *repository) Follow(ctx context.Context, followerID, followingID bson.ObjectID) error {
    follow := models.Follow{
        FollowerID:  followerID,
        FollowingID: followingID,
        CreatedAt:   time.Now(),
    }
    _, err := r.follows.InsertOne(ctx, follow)
    return err
}

func (r *repository) Unfollow(ctx context.Context, followerID, followingID bson.ObjectID) error {
    _, err := r.follows.DeleteOne(ctx, bson.M{
        "followerId":  followerID,
        "followingId": followingID,
    })
    return err
}

func (r *repository) IsFollowing(ctx context.Context, followerID, followingID bson.ObjectID) (bool, error) {
    count, err := r.follows.CountDocuments(ctx, bson.M{
        "followerId":  followerID,
        "followingId": followingID,
    })
    if err != nil {
        return false, err
    }
    return count > 0, nil
}

func (r *repository) GetFollowers(ctx context.Context, userID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Follow, error) {
    filter := bson.M{"followingId": userID}
    if beforeID != nil {
        filter["_id"] = bson.M{"$lt": *beforeID}
    }

    opts := options.Find().
        SetSort(bson.D{{Key: "_id", Value: -1}}).
        SetLimit(int64(limit))

    cursor, err := r.follows.Find(ctx, filter, opts)
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)

    var result []models.Follow
    if err := cursor.All(ctx, &result); err != nil {
        return nil, err
    }
    return result, nil
}

func (r *repository) GetFollowing(ctx context.Context, userID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Follow, error) {
    filter := bson.M{"followerId": userID}
    if beforeID != nil {
        filter["_id"] = bson.M{"$lt": *beforeID}
    }

    opts := options.Find().
        SetSort(bson.D{{Key: "_id", Value: -1}}).
        SetLimit(int64(limit))

    cursor, err := r.follows.Find(ctx, filter, opts)
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)

    var result []models.Follow
    if err := cursor.All(ctx, &result); err != nil {
        return nil, err
    }
    return result, nil
}

// GetFollowingIDs returns all user IDs that a given user follows.
// Used to build the home feed query.
// For users following many people this could be large — acceptable for Phase 2.
func (r *repository) GetFollowingIDs(ctx context.Context, followerID bson.ObjectID) ([]bson.ObjectID, error) {
    cursor, err := r.follows.Find(ctx,
        bson.M{"followerId": followerID},
        options.Find().SetProjection(bson.M{"followingId": 1}),
    )
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)

    var follows []models.Follow
    if err := cursor.All(ctx, &follows); err != nil {
        return nil, err
    }

    ids := make([]bson.ObjectID, 0, len(follows))
    for _, f := range follows {
        ids = append(ids, f.FollowingID)
    }
    return ids, nil
}

// IsFollowingMany returns a map of targetUserID.Hex() → bool for efficient batch checks.
func (r *repository) IsFollowingMany(ctx context.Context, followerID bson.ObjectID, targetIDs []bson.ObjectID) (map[string]bool, error) {
    cursor, err := r.follows.Find(ctx, bson.M{
        "followerId":  followerID,
        "followingId": bson.M{"$in": targetIDs},
    })
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)

    var follows []models.Follow
    if err := cursor.All(ctx, &follows); err != nil {
        return nil, err
    }

    result := make(map[string]bool)
    for _, f := range follows {
        result[f.FollowingID.Hex()] = true
    }
    return result, nil
}
```

### File: `internal/features/follows/service.go`

```go
package follows

import (
    "context"
    "errors"

    "github.com/xyz-asif/gotodo/internal/features/users"
    "github.com/xyz-asif/gotodo/internal/models"
    "go.mongodb.org/mongo-driver/v2/bson"
    "go.mongodb.org/mongo-driver/v2/mongo"
)

type Service interface {
    ToggleFollow(ctx context.Context, followerIDStr, followingIDStr string) (bool, error)
    GetPublicProfile(ctx context.Context, targetUserIDStr, callerIDStr string) (*models.PublicProfileResponse, error)
    GetFollowers(ctx context.Context, userIDStr, callerIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error)
    GetFollowing(ctx context.Context, userIDStr, callerIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error)
}

type service struct {
    repo     Repository
    userRepo users.Repository
}

func NewService(repo Repository, userRepo users.Repository) Service {
    return &service{repo: repo, userRepo: userRepo}
}

// ToggleFollow follows or unfollows a user. Returns true if now following, false if unfollowed.
func (s *service) ToggleFollow(ctx context.Context, followerIDStr, followingIDStr string) (bool, error) {
    if followerIDStr == followingIDStr {
        return false, errors.New("cannot follow yourself")
    }

    followerID, err := bson.ObjectIDFromHex(followerIDStr)
    if err != nil {
        return false, errors.New("invalid follower id")
    }
    followingID, err := bson.ObjectIDFromHex(followingIDStr)
    if err != nil {
        return false, errors.New("invalid following id")
    }

    // Check if target user exists
    targetUser, err := s.userRepo.GetUserByID(ctx, followingID)
    if err != nil || targetUser == nil {
        return false, errors.New("user not found")
    }

    alreadyFollowing, err := s.repo.IsFollowing(ctx, followerID, followingID)
    if err != nil {
        return false, err
    }

    if alreadyFollowing {
        // Unfollow
        if err := s.repo.Unfollow(ctx, followerID, followingID); err != nil {
            return false, err
        }
        // Decrement counts atomically
        go func() {
            _ = s.userRepo.DecrementFollowersCount(context.Background(), followingID)
            _ = s.userRepo.DecrementFollowingCount(context.Background(), followerID)
        }()
        return false, nil
    }

    // Follow
    if err := s.repo.Follow(ctx, followerID, followingID); err != nil {
        if mongo.IsDuplicateKeyError(err) {
            return true, nil // already following due to race — idempotent
        }
        return false, err
    }
    // Increment counts atomically
    go func() {
        _ = s.userRepo.IncrementFollowersCount(context.Background(), followingID)
        _ = s.userRepo.IncrementFollowingCount(context.Background(), followerID)
    }()
    return true, nil
}

// GetPublicProfile returns a user's public profile with isFollowedByMe flag.
func (s *service) GetPublicProfile(ctx context.Context, targetUserIDStr, callerIDStr string) (*models.PublicProfileResponse, error) {
    targetUserID, err := bson.ObjectIDFromHex(targetUserIDStr)
    if err != nil {
        return nil, errors.New("invalid user id")
    }

    user, err := s.userRepo.GetUserByID(ctx, targetUserID)
    if err != nil || user == nil {
        return nil, errors.New("user not found")
    }

    resp := &models.PublicProfileResponse{
        ID:             user.ID.Hex(),
        DisplayName:    user.DisplayName,
        Username:       user.Username,
        PhotoURL:       user.PhotoURL,
        CoverImageURL:  user.CoverImageURL,
        Bio:            user.Bio,
        ExternalLink:   user.ExternalLink,
        IsEditor:       user.IsEditor,
        PostsCount:     user.PostsCount,
        FollowersCount: user.FollowersCount,
        FollowingCount: user.FollowingCount,
        IsMe:           callerIDStr == targetUserIDStr,
    }

    // Check isFollowedByMe only if caller is authenticated and not viewing own profile
    if callerIDStr != "" && callerIDStr != targetUserIDStr {
        callerID, err := bson.ObjectIDFromHex(callerIDStr)
        if err == nil {
            isFollowing, _ := s.repo.IsFollowing(ctx, callerID, targetUserID)
            resp.IsFollowedByMe = isFollowing
        }
    }

    return resp, nil
}

// GetFollowers returns paginated list of a user's followers with isFollowing flags for the caller.
func (s *service) GetFollowers(ctx context.Context, userIDStr, callerIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error) {
    userID, err := bson.ObjectIDFromHex(userIDStr)
    if err != nil {
        return nil, false, errors.New("invalid user id")
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

    follows, err := s.repo.GetFollowers(ctx, userID, limit+1, beforeID)
    if err != nil {
        return nil, false, err
    }

    hasMore := len(follows) > limit
    if hasMore {
        follows = follows[:limit]
    }

    return s.buildUserResults(ctx, follows, true, callerIDStr)
    // true = extract followerID (we're listing followers — the "follower" column)
}

// GetFollowing returns paginated list of users that a user follows.
func (s *service) GetFollowing(ctx context.Context, userIDStr, callerIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error) {
    userID, err := bson.ObjectIDFromHex(userIDStr)
    if err != nil {
        return nil, false, errors.New("invalid user id")
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

    follows, err := s.repo.GetFollowing(ctx, userID, limit+1, beforeID)
    if err != nil {
        return nil, false, err
    }

    hasMore := len(follows) > limit
    if hasMore {
        follows = follows[:limit]
    }

    return s.buildUserResults(ctx, follows, false, callerIDStr)
    // false = extract followingID
}

// buildUserResults resolves user IDs from follow records into UserSearchResult objects.
// isFollowers: true = use FollowerID, false = use FollowingID
func (s *service) buildUserResults(ctx context.Context, follows []models.Follow, isFollowers bool, callerIDStr string) ([]models.UserSearchResult, bool, error) {
    if len(follows) == 0 {
        return []models.UserSearchResult{}, false, nil
    }

    ids := make([]bson.ObjectID, 0, len(follows))
    for _, f := range follows {
        if isFollowers {
            ids = append(ids, f.FollowerID)
        } else {
            ids = append(ids, f.FollowingID)
        }
    }

    userMap, err := s.userRepo.GetUsersByIDs(ctx, ids)
    if err != nil {
        return nil, false, err
    }

    // Batch check which ones the caller follows
    var followingMap map[string]bool
    if callerIDStr != "" {
        callerID, err := bson.ObjectIDFromHex(callerIDStr)
        if err == nil {
            followingMap, _ = s.repo.IsFollowingMany(ctx, callerID, ids)
        }
    }

    results := make([]models.UserSearchResult, 0, len(ids))
    for _, id := range ids {
        user, ok := userMap[id]
        if !ok {
            continue
        }
        results = append(results, models.UserSearchResult{
            ID:          user.ID.Hex(),
            DisplayName: user.DisplayName,
            Username:    user.Username,
            PhotoURL:    user.PhotoURL,
            IsEditor:    user.IsEditor,
            IsFollowing: followingMap[user.ID.Hex()],
        })
    }

    return results, false, nil
}
```

### File: `internal/features/follows/handler.go`

```go
package follows

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

// POST /api/v1/users/:id/follow
// Auth required. Toggles follow/unfollow.
func (h *Handler) ToggleFollow(c *fiber.Ctx) error {
    caller, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }

    targetID := c.Params("id")
    isFollowing, err := h.service.ToggleFollow(c.Context(), caller.ID.Hex(), targetID)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }

    msg := "Unfollowed"
    if isFollowing {
        msg = "Following"
    }
    return response.OK(c, msg, fiber.Map{"following": isFollowing})
}

// GET /api/v1/users/:id/profile
// Auth optional. Returns public profile with isFollowedByMe flag.
func (h *Handler) GetPublicProfile(c *fiber.Ctx) error {
    targetID := c.Params("id")
    callerID := ""
    if user, ok := c.Locals("user").(*models.User); ok {
        callerID = user.ID.Hex()
    }

    profile, err := h.service.GetPublicProfile(c.Context(), targetID, callerID)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "Profile retrieved", profile)
}

// GET /api/v1/users/:id/followers?limit=20&before=<id>
// Auth optional.
func (h *Handler) GetFollowers(c *fiber.Ctx) error {
    targetID := c.Params("id")
    limit := c.QueryInt("limit", 20)
    before := c.Query("before")
    callerID := ""
    if user, ok := c.Locals("user").(*models.User); ok {
        callerID = user.ID.Hex()
    }

    users, hasMore, err := h.service.GetFollowers(c.Context(), targetID, callerID, limit, before)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "Followers retrieved", fiber.Map{"users": users, "hasMore": hasMore})
}

// GET /api/v1/users/:id/following?limit=20&before=<id>
// Auth optional.
func (h *Handler) GetFollowing(c *fiber.Ctx) error {
    targetID := c.Params("id")
    limit := c.QueryInt("limit", 20)
    before := c.Query("before")
    callerID := ""
    if user, ok := c.Locals("user").(*models.User); ok {
        callerID = user.ID.Hex()
    }

    users, hasMore, err := h.service.GetFollowing(c.Context(), targetID, callerID, limit, before)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "Following retrieved", fiber.Map{"users": users, "hasMore": hasMore})
}
```

---

## Part 6 — Feed Feature Package

### New package: `internal/features/feed`

```
internal/features/feed/
    repository.go
    service.go
    handler.go
```

### File: `internal/features/feed/repository.go`

```go
package feed

import (
    "context"

    "github.com/xyz-asif/gotodo/internal/models"
    "go.mongodb.org/mongo-driver/v2/bson"
    "go.mongodb.org/mongo-driver/v2/mongo"
    "go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
    GetHomeFeed(ctx context.Context, authorIDs []bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Poem, error)
    GetExploreFeed(ctx context.Context, hashtag string, limit int, beforeID *bson.ObjectID) ([]models.Poem, error)
    SearchPoems(ctx context.Context, query string, limit int, beforeID *bson.ObjectID) ([]models.Poem, error)
    SearchUsers(ctx context.Context, query string, limit int, skip int) ([]models.User, int64, error)
}

type repository struct {
    poems *mongo.Collection
    users *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
    return &repository{
        poems: db.Collection("poems"),
        users: db.Collection("users"),
    }
}

// GetHomeFeed returns poems from a list of author IDs, cursor-paginated, newest first.
func (r *repository) GetHomeFeed(ctx context.Context, authorIDs []bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Poem, error) {
    if len(authorIDs) == 0 {
        return []models.Poem{}, nil
    }

    filter := bson.M{
        "authorId":   bson.M{"$in": authorIDs},
        "visibility": models.PoemVisibilityPublic,
        "isDeleted":  false,
    }
    if beforeID != nil {
        filter["_id"] = bson.M{"$lt": *beforeID}
    }

    opts := options.Find().
        SetSort(bson.D{{Key: "_id", Value: -1}}).
        SetLimit(int64(limit))

    cursor, err := r.poems.Find(ctx, filter, opts)
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

// GetExploreFeed returns public poems scored by engagement + recency.
//
// Scoring formula (Reddit/HN inspired):
//   score = (likes × 3) + (comments × 2) + (reposts × 1.5) - (hoursSincePosted × 0.5)
//
// This is computed server-side using MongoDB $addFields aggregation.
// Cursor pagination uses a compound (score, _id) cursor approach:
// for simplicity in Phase 2, we use offset-style skip on the score sort.
// beforeID is used as a secondary cursor when provided.
func (r *repository) GetExploreFeed(ctx context.Context, hashtag string, limit int, beforeID *bson.ObjectID) ([]models.Poem, error) {
    matchFilter := bson.M{
        "visibility": models.PoemVisibilityPublic,
        "isDeleted":  false,
    }
    if hashtag != "" {
        matchFilter["hashtags"] = hashtag
    }
    if beforeID != nil {
        matchFilter["_id"] = bson.M{"$lt": *beforeID}
    }

    // Aggregation pipeline: filter → compute score → sort by score desc → limit
    pipeline := mongo.Pipeline{
        // Stage 1: filter
        {{Key: "$match", Value: matchFilter}},

        // Stage 2: compute engagement score
        // hoursSincePosted = (now_unix - createdAt_unix) / 3600
        // score = (likes*3) + (comments*2) + (reposts*1.5) - (hoursSince * 0.5)
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
                            3600000, // ms → hours
                        }},
                        0.5,
                    }},
                },
            },
        }}},

        // Stage 3: sort by score descending, then by _id descending for stable pagination
        {{Key: "$sort", Value: bson.D{
            {Key: "engagementScore", Value: -1},
            {Key: "_id", Value: -1},
        }}},

        // Stage 4: limit
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

// SearchPoems searches poem title and plainText using MongoDB text index.
func (r *repository) SearchPoems(ctx context.Context, query string, limit int, beforeID *bson.ObjectID) ([]models.Poem, error) {
    filter := bson.M{
        "$text":      bson.M{"$search": query},
        "visibility": models.PoemVisibilityPublic,
        "isDeleted":  false,
    }
    if beforeID != nil {
        filter["_id"] = bson.M{"$lt": *beforeID}
    }

    opts := options.Find().
        SetSort(bson.D{{Key: "score", Value: bson.M{"$meta": "textScore"}}, {Key: "_id", Value: -1}}).
        SetProjection(bson.M{"score": bson.M{"$meta": "textScore"}}).
        SetLimit(int64(limit))

    cursor, err := r.poems.Find(ctx, filter, opts)
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

// SearchUsers searches users by displayName or username using case-insensitive regex.
func (r *repository) SearchUsers(ctx context.Context, query string, limit int, skip int) ([]models.User, int64, error) {
    filter := bson.M{
        "$or": []bson.M{
            {"displayName": bson.M{"$regex": query, "$options": "i"}},
            {"username": bson.M{"$regex": query, "$options": "i"}},
        },
    }

    total, _ := r.users.CountDocuments(ctx, filter)

    opts := options.Find().
        SetLimit(int64(limit)).
        SetSkip(int64(skip)).
        SetSort(bson.D{{Key: "followersCount", Value: -1}}) // most-followed first

    cursor, err := r.users.Find(ctx, filter, opts)
    if err != nil {
        return nil, 0, err
    }
    defer cursor.Close(ctx)

    var result []models.User
    if err := cursor.All(ctx, &result); err != nil {
        return nil, 0, err
    }
    return result, total, nil
}
```

### File: `internal/features/feed/service.go`

```go
package feed

import (
    "context"
    "errors"

    "github.com/xyz-asif/gotodo/internal/features/follows"
    "github.com/xyz-asif/gotodo/internal/features/poems"
    "github.com/xyz-asif/gotodo/internal/features/users"
    "github.com/xyz-asif/gotodo/internal/models"
    "go.mongodb.org/mongo-driver/v2/bson"
)

type Service interface {
    GetHomeFeed(ctx context.Context, callerIDStr string, limit int, before string) (*models.FeedPage, error)
    GetExploreFeed(ctx context.Context, callerIDStr string, hashtag string, limit int, before string) (*models.FeedPage, error)
    SearchPoems(ctx context.Context, query string, limit int, before string) (*models.PoemSearchPage, error)
    SearchUsers(ctx context.Context, query string, callerIDStr string, limit int, offset int) (*models.UserSearchPage, error)
}

type service struct {
    repo        Repository
    followRepo  follows.Repository
    userRepo    users.Repository
    poemService poems.Service // reuse poem service's toResponse helper indirectly
}

func NewService(repo Repository, followRepo follows.Repository, userRepo users.Repository) Service {
    return &service{
        repo:       repo,
        followRepo: followRepo,
        userRepo:   userRepo,
    }
}

func (s *service) buildPoemResponse(ctx context.Context, poem *models.Poem) models.PoemResponse {
    resp := models.PoemResponse{
        ID:            poem.ID.Hex(),
        Title:         poem.Title,
        ContentJSON:   poem.ContentJSON,
        PlainText:     poem.PlainText,
        Hashtags:      poem.Hashtags,
        Mood:          poem.Mood,
        IsOriginal:    poem.IsOriginal,
        Visibility:    poem.Visibility,
        AudioURL:      poem.AudioURL,
        AudioDuration: poem.AudioDuration,
        CoverColor:    poem.CoverColor,
        LikesCount:    poem.LikesCount,
        CommentsCount: poem.CommentsCount,
        RepostsCount:  poem.RepostsCount,
        CreatedAt:     poem.CreatedAt,
        UpdatedAt:     poem.UpdatedAt,
    }

    // Populate author
    author, err := s.userRepo.GetUserByID(ctx, poem.AuthorID)
    if err == nil && author != nil {
        resp.Author = models.PoemAuthor{
            ID:          author.ID.Hex(),
            DisplayName: author.DisplayName,
            Username:    author.Username,
            PhotoURL:    author.PhotoURL,
            IsEditor:    author.IsEditor,
        }
    }

    if resp.Hashtags == nil {
        resp.Hashtags = []string{}
    }

    return resp
}

func (s *service) GetHomeFeed(ctx context.Context, callerIDStr string, limit int, before string) (*models.FeedPage, error) {
    callerID, err := bson.ObjectIDFromHex(callerIDStr)
    if err != nil {
        return nil, errors.New("invalid user id")
    }
    if limit <= 0 { limit = 20 }
    if limit > 50 { limit = 50 }

    // Get all user IDs that the caller follows
    followingIDs, err := s.followRepo.GetFollowingIDs(ctx, callerID)
    if err != nil {
        return nil, err
    }

    // Include the caller's own poems in their home feed
    followingIDs = append(followingIDs, callerID)

    var beforeID *bson.ObjectID
    if before != "" {
        id, err := bson.ObjectIDFromHex(before)
        if err != nil {
            return nil, errors.New("invalid before cursor")
        }
        beforeID = &id
    }

    poemDocs, err := s.repo.GetHomeFeed(ctx, followingIDs, limit+1, beforeID)
    if err != nil {
        return nil, err
    }

    hasMore := len(poemDocs) > limit
    if hasMore {
        poemDocs = poemDocs[:limit]
    }

    responses := make([]models.PoemResponse, 0, len(poemDocs))
    for _, p := range poemDocs {
        responses = append(responses, s.buildPoemResponse(ctx, &p))
    }

    return &models.FeedPage{Poems: responses, HasMore: hasMore}, nil
}

func (s *service) GetExploreFeed(ctx context.Context, callerIDStr string, hashtag string, limit int, before string) (*models.FeedPage, error) {
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

    poemDocs, err := s.repo.GetExploreFeed(ctx, hashtag, limit+1, beforeID)
    if err != nil {
        return nil, err
    }

    hasMore := len(poemDocs) > limit
    if hasMore {
        poemDocs = poemDocs[:limit]
    }

    responses := make([]models.PoemResponse, 0, len(poemDocs))
    for _, p := range poemDocs {
        responses = append(responses, s.buildPoemResponse(ctx, &p))
    }

    return &models.FeedPage{Poems: responses, HasMore: hasMore}, nil
}

func (s *service) SearchPoems(ctx context.Context, query string, limit int, before string) (*models.PoemSearchPage, error) {
    if query == "" {
        return &models.PoemSearchPage{Poems: []models.PoemResponse{}, HasMore: false}, nil
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

    poemDocs, err := s.repo.SearchPoems(ctx, query, limit+1, beforeID)
    if err != nil {
        return nil, err
    }

    hasMore := len(poemDocs) > limit
    if hasMore {
        poemDocs = poemDocs[:limit]
    }

    responses := make([]models.PoemResponse, 0, len(poemDocs))
    for _, p := range poemDocs {
        responses = append(responses, s.buildPoemResponse(ctx, &p))
    }

    return &models.PoemSearchPage{Poems: responses, HasMore: hasMore}, nil
}

func (s *service) SearchUsers(ctx context.Context, query string, callerIDStr string, limit int, offset int) (*models.UserSearchPage, error) {
    if query == "" {
        return &models.UserSearchPage{Users: []models.UserSearchResult{}, HasMore: false}, nil
    }
    if limit <= 0 { limit = 20 }
    if limit > 50 { limit = 50 }

    userDocs, total, err := s.repo.SearchUsers(ctx, query, limit+1, offset)
    if err != nil {
        return nil, err
    }

    hasMore := int64(offset+len(userDocs)) < total
    if len(userDocs) > limit {
        userDocs = userDocs[:limit]
    }

    // Batch check which ones the caller follows
    var followingMap map[string]bool
    if callerIDStr != "" {
        callerID, err := bson.ObjectIDFromHex(callerIDStr)
        if err == nil {
            ids := make([]bson.ObjectID, 0, len(userDocs))
            for _, u := range userDocs {
                ids = append(ids, u.ID)
            }
            followingMap, _ = s.followRepo.IsFollowingMany(ctx, callerID, ids)
        }
    }

    results := make([]models.UserSearchResult, 0, len(userDocs))
    for _, u := range userDocs {
        results = append(results, models.UserSearchResult{
            ID:          u.ID.Hex(),
            DisplayName: u.DisplayName,
            Username:    u.Username,
            PhotoURL:    u.PhotoURL,
            IsEditor:    u.IsEditor,
            IsFollowing: followingMap[u.ID.Hex()],
        })
    }

    return &models.UserSearchPage{Users: results, HasMore: hasMore}, nil
}
```

### File: `internal/features/feed/handler.go`

```go
package feed

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

// GET /api/v1/feed?limit=20&before=<id>
// Auth required.
func (h *Handler) GetHomeFeed(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }
    limit := c.QueryInt("limit", 20)
    before := c.Query("before")

    page, err := h.service.GetHomeFeed(c.Context(), user.ID.Hex(), limit, before)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "Home feed retrieved", page)
}

// GET /api/v1/feed/explore?limit=20&before=<id>&hashtag=love
// Auth optional.
func (h *Handler) GetExploreFeed(c *fiber.Ctx) error {
    callerID := ""
    if user, ok := c.Locals("user").(*models.User); ok {
        callerID = user.ID.Hex()
    }
    limit := c.QueryInt("limit", 20)
    before := c.Query("before")
    hashtag := c.Query("hashtag")

    page, err := h.service.GetExploreFeed(c.Context(), callerID, hashtag, limit, before)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "Explore feed retrieved", page)
}

// GET /api/v1/search/poems?q=rain&limit=20&before=<id>
// Auth optional.
func (h *Handler) SearchPoems(c *fiber.Ctx) error {
    query := c.Query("q")
    limit := c.QueryInt("limit", 20)
    before := c.Query("before")

    page, err := h.service.SearchPoems(c.Context(), query, limit, before)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "Poems found", page)
}

// GET /api/v1/search/users?q=asif&limit=20&offset=0
// Auth optional.
func (h *Handler) SearchUsers(c *fiber.Ctx) error {
    callerID := ""
    if user, ok := c.Locals("user").(*models.User); ok {
        callerID = user.ID.Hex()
    }
    query := c.Query("q")
    limit := c.QueryInt("limit", 20)
    offset := c.QueryInt("offset", 0)

    page, err := h.service.SearchUsers(c.Context(), query, callerID, limit, offset)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }
    return response.OK(c, "Users found", page)
}
```

---

## Part 7 — Wire Routes

### File: `internal/routes/routes.go`

Update `SetupRoutes` to accept new handlers:

```go
func SetupRoutes(
    app *fiber.App,
    auth *middleware.AuthMiddleware,
    userHandler *users.Handler,
    connectionHandler *connections.Handler,
    chatHandler *chat.Handler,
    notifHandler *notifications.Handler,
    profileHandler *profile.Handler,
    poemHandler *poems.Handler,
    followHandler *follows.Handler,   // NEW
    feedHandler *feed.Handler,         // NEW
)
```

Add these route groups:

```go
// ── Follow / Profile ──
api.Post("/users/:id/follow", auth.Protect(), followHandler.ToggleFollow)
api.Get("/users/:id/profile", auth.OptionalAuth(), followHandler.GetPublicProfile)
api.Get("/users/:id/followers", auth.OptionalAuth(), followHandler.GetFollowers)
api.Get("/users/:id/following", auth.OptionalAuth(), followHandler.GetFollowing)

// ── Feed ──
api.Get("/feed", auth.Protect(), feedHandler.GetHomeFeed)
api.Get("/feed/explore", auth.OptionalAuth(), feedHandler.GetExploreFeed)

// ── Search ──
api.Get("/search/poems", auth.OptionalAuth(), feedHandler.SearchPoems)
api.Get("/search/users", auth.OptionalAuth(), feedHandler.SearchUsers)
```

### File: `cmd/api/main.go`

Add after existing handler setup:

```go
followRepo := follows.NewRepository(db.Database)
followService := follows.NewService(followRepo, userRepo)
followHandler := follows.NewHandler(followService)

feedRepo := feed.NewRepository(db.Database)
feedService := feed.NewService(feedRepo, followRepo, userRepo)
feedHandler := feed.NewHandler(feedService)
```

Pass to `SetupRoutes`.

---

## Part 8 — Implementation Order

1. Add `Follow` model to `models/follow.go`
2. Add `FollowersCount`, `FollowingCount` to `User` struct if not already present
3. Add `PublicProfileResponse`, `FeedPage`, `UserSearchResult`, `UserSearchPage`, `PoemSearchPage` to `models/feed.go`
4. Add follow-related indexes to `database/indexes.go`
5. Add 5 new methods to users repository
6. Create `features/follows/` package (repository → service → handler)
7. Create `features/feed/` package (repository → service → handler)
8. Remove connection guard from `chat/service.go` `GetOrCreateDirectRoom`
9. Update `routes/routes.go` and `main.go`
10. Run `go build ./...` and fix compile errors before testing

---

## API Summary

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | `/api/v1/users/:id/follow` | Required | Toggle follow/unfollow |
| GET | `/api/v1/users/:id/profile` | Optional | Public profile + isFollowedByMe |
| GET | `/api/v1/users/:id/followers` | Optional | Followers list |
| GET | `/api/v1/users/:id/following` | Optional | Following list |
| GET | `/api/v1/feed` | Required | Home feed (followed users' poems) |
| GET | `/api/v1/feed/explore` | Optional | Explore feed (scored, filterable) |
| GET | `/api/v1/search/poems` | Optional | Full text poem search |
| GET | `/api/v1/search/users` | Optional | User search |
