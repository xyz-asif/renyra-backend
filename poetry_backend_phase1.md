# ChatBee Poetry App — Backend Phase 1
### Go + MongoDB + Fiber — Extends Existing Codebase

---

## Overview

This document covers exactly two flows:
1. Post-auth profile setup (profile details + username)
2. Poem CRUD (create, read, update, delete, list)

Everything here builds on the existing codebase. The existing `users` collection, auth middleware, Cloudinary service, and Fiber router are all reused. Do not create new infrastructure — extend what exists.

---

## Part 1 — Extend the User Model

### File: `internal/models/user.go`

Add the following fields to the existing `User` struct. Do not remove or rename any existing fields.

```go
// Profile setup fields — add these to the existing User struct
Username       string `bson:"username,omitempty"       json:"username,omitempty"`
Bio            string `bson:"bio,omitempty"            json:"bio,omitempty"`
ExternalLink   string `bson:"externalLink,omitempty"   json:"externalLink,omitempty"`
CoverImageURL  string `bson:"coverImageURL,omitempty"  json:"coverImageURL,omitempty"`
IsProfileSetup bool   `bson:"isProfileSetup"           json:"isProfileSetup"`
IsEditor       bool   `bson:"isEditor"                 json:"isEditor"`
PostsCount     int    `bson:"postsCount"               json:"postsCount"`
FollowersCount int    `bson:"followersCount"           json:"followersCount"`
FollowingCount int    `bson:"followingCount"           json:"followingCount"`
```

`IsProfileSetup` defaults to `false` on new user creation. The frontend uses this flag to decide whether to redirect to the setup flow after login.

---

## Part 2 — New Models

### File: `internal/models/poem.go`

```go
package models

import (
    "time"
    "go.mongodb.org/mongo-driver/v2/bson"
)

// Poem visibility options
const (
    PoemVisibilityPublic   = "public"
    PoemVisibilityPrivate  = "private" // draft state
)

// Poem moods — fixed set matching the static chips on the frontend
var ValidMoods = map[string]bool{
    "love": true, "grief": true, "nature": true, "nostalgia": true,
    "hope": true, "dark": true, "spiritual": true, "humour": true,
    "life": true, "longing": true,
}

type Poem struct {
    ID            bson.ObjectID  `bson:"_id,omitempty"        json:"id"`
    AuthorID      bson.ObjectID  `bson:"authorId"             json:"authorId"`
    Title         string         `bson:"title"                json:"title"`
    ContentJSON   string         `bson:"contentJson"          json:"contentJson"`   // Quill Delta JSON string
    PlainText     string         `bson:"plainText"            json:"plainText"`     // stripped plain text for search
    Hashtags      []string       `bson:"hashtags"             json:"hashtags"`      // lowercase, no #, max 10
    Mood          string         `bson:"mood,omitempty"       json:"mood,omitempty"` // single value from ValidMoods
    IsOriginal    bool           `bson:"isOriginal"           json:"isOriginal"`    // copyright checkbox
    Visibility    string         `bson:"visibility"           json:"visibility"`    // public | private
    AudioURL      string         `bson:"audioUrl,omitempty"   json:"audioUrl,omitempty"` // Cloudinary URL
    AudioDuration int            `bson:"audioDuration"        json:"audioDuration"` // seconds, 0 if no audio
    CoverColor    string         `bson:"coverColor,omitempty" json:"coverColor,omitempty"` // hex color from editor
    LikesCount    int            `bson:"likesCount"           json:"likesCount"`
    CommentsCount int            `bson:"commentsCount"        json:"commentsCount"`
    RepostsCount  int            `bson:"repostsCount"         json:"repostsCount"`
    IsDeleted     bool           `bson:"isDeleted"            json:"isDeleted"`
    CreatedAt     time.Time      `bson:"createdAt"            json:"createdAt"`
    UpdatedAt     time.Time      `bson:"updatedAt"            json:"updatedAt"`
}

// PoemResponse is what the API returns — includes author info inline
// so the frontend never needs a second user lookup
type PoemResponse struct {
    ID            string      `json:"id"`
    Author        PoemAuthor  `json:"author"`
    Title         string      `json:"title"`
    ContentJSON   string      `json:"contentJson"`
    PlainText     string      `json:"plainText"`
    Hashtags      []string    `json:"hashtags"`
    Mood          string      `json:"mood,omitempty"`
    IsOriginal    bool        `json:"isOriginal"`
    Visibility    string      `json:"visibility"`
    AudioURL      string      `json:"audioUrl,omitempty"`
    AudioDuration int         `json:"audioDuration"`
    CoverColor    string      `json:"coverColor,omitempty"`
    LikesCount    int         `json:"likesCount"`
    CommentsCount int         `json:"commentsCount"`
    RepostsCount  int         `json:"repostsCount"`
    CreatedAt     time.Time   `json:"createdAt"`
    UpdatedAt     time.Time   `json:"updatedAt"`
}

// PoemAuthor — embedded author info in every poem response
type PoemAuthor struct {
    ID           string `json:"id"`
    DisplayName  string `json:"displayName"`
    Username     string `json:"username"`
    PhotoURL     string `json:"photoURL"`
    IsEditor     bool   `json:"isEditor"`
}

// PoemsPage — paginated list response
type PoemsPage struct {
    Poems   []PoemResponse `json:"poems"`
    HasMore bool           `json:"hasMore"`
}
```

### File: `internal/models/hashtag.go`

```go
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
```

---

## Part 3 — MongoDB Indexes

### File: `internal/database/indexes.go`

Add the following index blocks to the existing `CreateIndexes` function. Add them after the existing index groups — do not modify existing indexes.

```go
// ── Users — username unique index ──
usersUsernameIndex := []mongo.IndexModel{
    {
        Keys:    bson.D{{Key: "username", Value: 1}},
        Options: options.Index().SetUnique(true).SetSparse(true), // sparse: allows multiple docs with no username
    },
}
if _, err := db.Collection("users").Indexes().CreateMany(ctx, usersUsernameIndex); err != nil {
    log.Printf("Warning: Users username index issue: %v", err)
}

// ── Poems ──
poemsIndexes := []mongo.IndexModel{
    // Primary: fetch all poems by author, newest first
    {Keys: bson.D{{Key: "authorId", Value: 1}, {Key: "_id", Value: -1}}},
    // Filter by visibility (for explore feed later)
    {Keys: bson.D{{Key: "visibility", Value: 1}, {Key: "_id", Value: -1}}},
    // Filter by hashtag
    {Keys: bson.D{{Key: "hashtags", Value: 1}, {Key: "_id", Value: -1}}},
    // Filter by mood
    {Keys: bson.D{{Key: "mood", Value: 1}, {Key: "_id", Value: -1}}},
    // Full text search on title + plainText
    {Keys: bson.D{{Key: "title", Value: "text"}, {Key: "plainText", Value: "text"}}},
    // Soft delete filter
    {Keys: bson.D{{Key: "isDeleted", Value: 1}}},
}
if _, err := db.Collection("poems").Indexes().CreateMany(ctx, poemsIndexes); err != nil {
    log.Printf("Warning: Poems index issue: %v", err)
}

// ── Hashtags ──
hashtagsIndexes := []mongo.IndexModel{
    {Keys: bson.D{{Key: "tag", Value: 1}}, Options: options.Index().SetUnique(true)},
    {Keys: bson.D{{Key: "usageCount", Value: -1}}},
}
if _, err := db.Collection("hashtags").Indexes().CreateMany(ctx, hashtagsIndexes); err != nil {
    log.Printf("Warning: Hashtags index issue: %v", err)
}
```

---

## Part 4 — User Profile Setup & Username APIs

### New feature package: `internal/features/profile`

Create this as a new package. Structure:

```
internal/features/profile/
    handler.go
    service.go
    repository.go   (reuses users collection — no new collection needed)
```

### File: `internal/features/profile/repository.go`

```go
package profile

import (
    "context"
    "errors"
    "time"

    "go.mongodb.org/mongo-driver/v2/bson"
    "go.mongodb.org/mongo-driver/v2/mongo"
    "go.mongodb.org/mongo-driver/v2/mongo/options"
    "github.com/xyz-asif/gotodo/internal/models"
)

type Repository interface {
    UpdateProfile(ctx context.Context, userID bson.ObjectID, update ProfileUpdateRequest) (*models.User, error)
    SetUsername(ctx context.Context, userID bson.ObjectID, username string) (*models.User, error)
    IsUsernameTaken(ctx context.Context, username string) (bool, error)
    GetByID(ctx context.Context, userID bson.ObjectID) (*models.User, error)
}

type repository struct {
    users *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
    return &repository{
        users: db.Collection("users"),
    }
}

// ProfileUpdateRequest — fields allowed in setup
type ProfileUpdateRequest struct {
    DisplayName   string
    Bio           string
    ExternalLink  string
    PhotoURL      string
    CoverImageURL string
}

func (r *repository) UpdateProfile(ctx context.Context, userID bson.ObjectID, req ProfileUpdateRequest) (*models.User, error) {
    update := bson.M{
        "$set": bson.M{
            "displayName":    req.DisplayName,
            "bio":            req.Bio,
            "externalLink":   req.ExternalLink,
            "photoURL":       req.PhotoURL,
            "coverImageURL":  req.CoverImageURL,
            "isProfileSetup": true,
            "updatedAt":      time.Now(),
        },
    }

    opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
    var user models.User
    err := r.users.FindOneAndUpdate(ctx, bson.M{"_id": userID}, update, opts).Decode(&user)
    if err != nil {
        return nil, err
    }
    return &user, nil
}

func (r *repository) SetUsername(ctx context.Context, userID bson.ObjectID, username string) (*models.User, error) {
    // Only allow setting username if not already set
    filter := bson.M{
        "_id":      userID,
        "username": bson.M{"$exists": false}, // prevents overwrite — username is permanent once set
    }
    update := bson.M{
        "$set": bson.M{
            "username":  username,
            "updatedAt": time.Now(),
        },
    }

    opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
    var user models.User
    err := r.users.FindOneAndUpdate(ctx, filter, update, opts).Decode(&user)
    if err != nil {
        if err == mongo.ErrNoDocuments {
            return nil, errors.New("username already set or user not found")
        }
        if mongo.IsDuplicateKeyError(err) {
            return nil, errors.New("username is already taken")
        }
        return nil, err
    }
    return &user, nil
}

func (r *repository) IsUsernameTaken(ctx context.Context, username string) (bool, error) {
    count, err := r.users.CountDocuments(ctx, bson.M{"username": username})
    if err != nil {
        return false, err
    }
    return count > 0, nil
}

func (r *repository) GetByID(ctx context.Context, userID bson.ObjectID) (*models.User, error) {
    var user models.User
    err := r.users.FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
    if err != nil {
        return nil, err
    }
    return &user, nil
}
```

### File: `internal/features/profile/service.go`

```go
package profile

import (
    "context"
    "errors"
    "regexp"
    "strings"

    "github.com/xyz-asif/gotodo/internal/models"
    "go.mongodb.org/mongo-driver/v2/bson"
)

// Reserved usernames that cannot be registered
var reservedUsernames = map[string]bool{
    "admin": true, "support": true, "editor": true, "chatbee": true,
    "poetry": true, "official": true, "moderator": true, "help": true,
    "me": true, "settings": true, "explore": true, "feed": true,
    "search": true, "notifications": true, "profile": true,
}

// username format: lowercase, alphanumeric + underscore, 3–30 chars
var usernameRegex = regexp.MustCompile(`^[a-z0-9_]{3,30}$`)

type Service interface {
    SetupProfile(ctx context.Context, userID string, req ProfileSetupRequest) (*models.User, error)
    CheckUsername(ctx context.Context, username string) (CheckUsernameResult, error)
    SetUsername(ctx context.Context, userID string, username string) (*models.User, error)
}

type ProfileSetupRequest struct {
    DisplayName   string `json:"displayName"`
    Bio           string `json:"bio"`
    ExternalLink  string `json:"externalLink"`
    PhotoURL      string `json:"photoURL"`
    CoverImageURL string `json:"coverImageURL"`
}

type CheckUsernameResult struct {
    Username  string `json:"username"`
    Available bool   `json:"available"`
    Reason    string `json:"reason,omitempty"` // "taken" | "invalid_format" | "reserved" | ""
}

type service struct {
    repo Repository
}

func NewService(repo Repository) Service {
    return &service{repo: repo}
}

func (s *service) SetupProfile(ctx context.Context, userIDStr string, req ProfileSetupRequest) (*models.User, error) {
    userID, err := bson.ObjectIDFromHex(userIDStr)
    if err != nil {
        return nil, errors.New("invalid user id")
    }

    // Validate display name
    req.DisplayName = strings.TrimSpace(req.DisplayName)
    if req.DisplayName == "" {
        return nil, errors.New("display name is required")
    }
    if len(req.DisplayName) > 50 {
        return nil, errors.New("display name must be 50 characters or less")
    }

    // Validate bio
    if len(req.Bio) > 200 {
        return nil, errors.New("bio must be 200 characters or less")
    }

    // Validate external link (basic check)
    if req.ExternalLink != "" {
        if !strings.HasPrefix(req.ExternalLink, "http://") && !strings.HasPrefix(req.ExternalLink, "https://") {
            return nil, errors.New("external link must start with http:// or https://")
        }
    }

    return s.repo.UpdateProfile(ctx, userID, ProfileUpdateRequest{
        DisplayName:   req.DisplayName,
        Bio:           req.Bio,
        ExternalLink:  req.ExternalLink,
        PhotoURL:      req.PhotoURL,
        CoverImageURL: req.CoverImageURL,
    })
}

func (s *service) CheckUsername(ctx context.Context, username string) (CheckUsernameResult, error) {
    username = strings.ToLower(strings.TrimSpace(username))

    result := CheckUsernameResult{Username: username}

    // Format validation
    if !usernameRegex.MatchString(username) {
        result.Available = false
        result.Reason = "invalid_format"
        return result, nil
    }

    // Reserved check
    if reservedUsernames[username] {
        result.Available = false
        result.Reason = "reserved"
        return result, nil
    }

    // DB availability check
    taken, err := s.repo.IsUsernameTaken(ctx, username)
    if err != nil {
        return result, err
    }

    if taken {
        result.Available = false
        result.Reason = "taken"
    } else {
        result.Available = true
    }

    return result, nil
}

func (s *service) SetUsername(ctx context.Context, userIDStr string, username string) (*models.User, error) {
    userID, err := bson.ObjectIDFromHex(userIDStr)
    if err != nil {
        return nil, errors.New("invalid user id")
    }

    username = strings.ToLower(strings.TrimSpace(username))

    // Re-validate everything before writing
    if !usernameRegex.MatchString(username) {
        return nil, errors.New("invalid username format: use 3-30 lowercase letters, numbers, or underscores")
    }
    if reservedUsernames[username] {
        return nil, errors.New("this username is reserved")
    }

    // One final availability check right before write (race condition guard)
    taken, err := s.repo.IsUsernameTaken(ctx, username)
    if err != nil {
        return nil, err
    }
    if taken {
        return nil, errors.New("username is already taken")
    }

    return s.repo.SetUsername(ctx, userID, username)
}
```

### File: `internal/features/profile/handler.go`

```go
package profile

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

// POST /api/v1/users/setup
// Called once after first login to complete profile.
// Auth required.
func (h *Handler) SetupProfile(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }

    var req ProfileSetupRequest
    if err := c.BodyParser(&req); err != nil {
        return response.BadRequest(c, "Invalid request body")
    }

    updatedUser, err := h.service.SetupProfile(c.Context(), user.ID.Hex(), req)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }

    return response.OK(c, "Profile setup complete", updatedUser)
}

// GET /api/v1/users/username/check?username=asif_writes
// No auth required. Called on every keystroke (debounced on frontend).
func (h *Handler) CheckUsername(c *fiber.Ctx) error {
    username := c.Query("username")
    if username == "" {
        return response.BadRequest(c, "username query param is required")
    }

    result, err := h.service.CheckUsername(c.Context(), username)
    if err != nil {
        return response.InternalError(c, err.Error())
    }

    return response.OK(c, "Username check result", result)
}

// POST /api/v1/users/username
// Auth required. Called once when user confirms their chosen username.
func (h *Handler) SetUsername(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }

    var req struct {
        Username string `json:"username"`
    }
    if err := c.BodyParser(&req); err != nil {
        return response.BadRequest(c, "Invalid request body")
    }

    updatedUser, err := h.service.SetUsername(c.Context(), user.ID.Hex(), req.Username)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }

    return response.OK(c, "Username set successfully", updatedUser)
}
```

---

## Part 5 — Poem CRUD

### New feature package: `internal/features/poems`

```
internal/features/poems/
    handler.go
    service.go
    repository.go
```

### File: `internal/features/poems/repository.go`

```go
package poems

import (
    "context"
    "time"

    "github.com/xyz-asif/gotodo/internal/models"
    "go.mongodb.org/mongo-driver/v2/bson"
    "go.mongodb.org/mongo-driver/v2/mongo"
    "go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
    Create(ctx context.Context, poem *models.Poem) error
    GetByID(ctx context.Context, poemID bson.ObjectID) (*models.Poem, error)
    Update(ctx context.Context, poemID bson.ObjectID, update PoemUpdateFields) (*models.Poem, error)
    SoftDelete(ctx context.Context, poemID, authorID bson.ObjectID) error
    GetByAuthor(ctx context.Context, authorID bson.ObjectID, limit int, beforeID *bson.ObjectID, includePrivate bool) ([]models.Poem, error)
    UpsertHashtags(ctx context.Context, tags []string) error
    DecrementHashtags(ctx context.Context, tags []string) error
}

type PoemUpdateFields struct {
    Title         string
    ContentJSON   string
    PlainText     string
    Hashtags      []string
    Mood          string
    IsOriginal    bool
    Visibility    string
    AudioURL      string
    AudioDuration int
    CoverColor    string
}

type repository struct {
    poems    *mongo.Collection
    hashtags *mongo.Collection
    users    *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
    return &repository{
        poems:    db.Collection("poems"),
        hashtags: db.Collection("hashtags"),
        users:    db.Collection("users"),
    }
}

func (r *repository) Create(ctx context.Context, poem *models.Poem) error {
    poem.CreatedAt = time.Now()
    poem.UpdatedAt = time.Now()
    poem.IsDeleted = false
    poem.LikesCount = 0
    poem.CommentsCount = 0
    poem.RepostsCount = 0

    res, err := r.poems.InsertOne(ctx, poem)
    if err != nil {
        return err
    }
    poem.ID = res.InsertedID.(bson.ObjectID)
    return nil
}

func (r *repository) GetByID(ctx context.Context, poemID bson.ObjectID) (*models.Poem, error) {
    var poem models.Poem
    err := r.poems.FindOne(ctx, bson.M{"_id": poemID, "isDeleted": false}).Decode(&poem)
    if err != nil {
        return nil, err
    }
    return &poem, nil
}

func (r *repository) Update(ctx context.Context, poemID bson.ObjectID, fields PoemUpdateFields) (*models.Poem, error) {
    update := bson.M{
        "$set": bson.M{
            "title":         fields.Title,
            "contentJson":   fields.ContentJSON,
            "plainText":     fields.PlainText,
            "hashtags":      fields.Hashtags,
            "mood":          fields.Mood,
            "isOriginal":    fields.IsOriginal,
            "visibility":    fields.Visibility,
            "audioUrl":      fields.AudioURL,
            "audioDuration": fields.AudioDuration,
            "coverColor":    fields.CoverColor,
            "updatedAt":     time.Now(),
        },
    }

    opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
    var poem models.Poem
    err := r.poems.FindOneAndUpdate(ctx,
        bson.M{"_id": poemID, "isDeleted": false},
        update,
        opts,
    ).Decode(&poem)
    if err != nil {
        return nil, err
    }
    return &poem, nil
}

func (r *repository) SoftDelete(ctx context.Context, poemID, authorID bson.ObjectID) error {
    _, err := r.poems.UpdateOne(ctx,
        bson.M{"_id": poemID, "authorId": authorID, "isDeleted": false},
        bson.M{"$set": bson.M{"isDeleted": true, "updatedAt": time.Now()}},
    )
    return err
}

// GetByAuthor returns poems by a specific author with cursor-based pagination.
// includePrivate = true only when the caller IS the author (my poems endpoint).
// includePrivate = false for public profile view (only returns public poems).
func (r *repository) GetByAuthor(ctx context.Context, authorID bson.ObjectID, limit int, beforeID *bson.ObjectID, includePrivate bool) ([]models.Poem, error) {
    filter := bson.M{
        "authorId":  authorID,
        "isDeleted": false,
    }

    if !includePrivate {
        filter["visibility"] = models.PoemVisibilityPublic
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

// UpsertHashtags increments usageCount for each tag, creating the document if it doesn't exist.
func (r *repository) UpsertHashtags(ctx context.Context, tags []string) error {
    for _, tag := range tags {
        if tag == "" {
            continue
        }
        _, err := r.hashtags.UpdateOne(ctx,
            bson.M{"tag": tag},
            bson.M{
                "$inc": bson.M{"usageCount": 1},
                "$set": bson.M{"updatedAt": time.Now()},
                "$setOnInsert": bson.M{"tag": tag},
            },
            options.UpdateOne().SetUpsert(true),
        )
        if err != nil {
            return err
        }
    }
    return nil
}

// DecrementHashtags decrements usageCount when a poem is deleted or hashtags are removed.
// Never goes below 0.
func (r *repository) DecrementHashtags(ctx context.Context, tags []string) error {
    for _, tag := range tags {
        if tag == "" {
            continue
        }
        // Only decrement if usageCount > 0
        _, err := r.hashtags.UpdateOne(ctx,
            bson.M{"tag": tag, "usageCount": bson.M{"$gt": 0}},
            bson.M{
                "$inc": bson.M{"usageCount": -1},
                "$set": bson.M{"updatedAt": time.Now()},
            },
        )
        if err != nil {
            return err
        }
    }
    return nil
}
```

### File: `internal/features/poems/service.go`

```go
package poems

import (
    "context"
    "errors"
    "strings"

    "github.com/xyz-asif/gotodo/internal/features/users"
    "github.com/xyz-asif/gotodo/internal/models"
    "go.mongodb.org/mongo-driver/v2/bson"
    "go.mongodb.org/mongo-driver/v2/mongo"
)

type Service interface {
    Create(ctx context.Context, authorID string, req CreatePoemRequest) (*models.PoemResponse, error)
    GetByID(ctx context.Context, poemID string, callerID string) (*models.PoemResponse, error)
    Update(ctx context.Context, poemID string, authorID string, req UpdatePoemRequest) (*models.PoemResponse, error)
    Delete(ctx context.Context, poemID string, authorID string) error
    GetMyPoems(ctx context.Context, authorID string, limit int, before string) (*models.PoemsPage, error)
    GetUserPoems(ctx context.Context, targetUserID string, callerID string, limit int, before string) (*models.PoemsPage, error)
}

// CreatePoemRequest — body sent from the publish bottom sheet
type CreatePoemRequest struct {
    Title         string   `json:"title"`
    ContentJSON   string   `json:"contentJson"`   // Quill Delta JSON
    PlainText     string   `json:"plainText"`     // stripped plain text
    Hashtags      []string `json:"hashtags"`      // combined: static chips + custom tags
    Mood          string   `json:"mood"`          // single mood from static chips
    IsOriginal    bool     `json:"isOriginal"`    // copyright checkbox
    Visibility    string   `json:"visibility"`    // "public" or "private"
    AudioURL      string   `json:"audioUrl"`      // Cloudinary URL, empty if no audio
    AudioDuration int      `json:"audioDuration"` // seconds
    CoverColor    string   `json:"coverColor"`    // hex from editor
}

// UpdatePoemRequest — same fields, all optional (only changed fields need to be sent)
type UpdatePoemRequest struct {
    Title         string   `json:"title"`
    ContentJSON   string   `json:"contentJson"`
    PlainText     string   `json:"plainText"`
    Hashtags      []string `json:"hashtags"`
    Mood          string   `json:"mood"`
    IsOriginal    bool     `json:"isOriginal"`
    Visibility    string   `json:"visibility"`
    AudioURL      string   `json:"audioUrl"`
    AudioDuration int      `json:"audioDuration"`
    CoverColor    string   `json:"coverColor"`
}

type service struct {
    repo     Repository
    userRepo users.Repository // reuse existing user repo for author lookups
}

func NewService(repo Repository, userRepo users.Repository) Service {
    return &service{repo: repo, userRepo: userRepo}
}

// sanitizeHashtags normalises hashtags: lowercase, strip #, remove duplicates, max 10
func sanitizeHashtags(tags []string) []string {
    seen := make(map[string]bool)
    var result []string
    for _, tag := range tags {
        tag = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(tag, "#")))
        if tag == "" || seen[tag] {
            continue
        }
        seen[tag] = true
        result = append(result, tag)
        if len(result) >= 10 {
            break
        }
    }
    return result
}

func validateVisibility(v string) bool {
    return v == models.PoemVisibilityPublic || v == models.PoemVisibilityPrivate
}

func (s *service) buildAuthor(ctx context.Context, authorID bson.ObjectID) models.PoemAuthor {
    author := models.PoemAuthor{ID: authorID.Hex()}
    user, err := s.userRepo.GetUserByID(ctx, authorID)
    if err == nil && user != nil {
        author.DisplayName = user.DisplayName
        author.Username = user.Username
        author.PhotoURL = user.PhotoURL
        author.IsEditor = user.IsEditor
    }
    return author
}

func (s *service) toResponse(ctx context.Context, poem *models.Poem) *models.PoemResponse {
    return &models.PoemResponse{
        ID:            poem.ID.Hex(),
        Author:        s.buildAuthor(ctx, poem.AuthorID),
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
}

func (s *service) Create(ctx context.Context, authorIDStr string, req CreatePoemRequest) (*models.PoemResponse, error) {
    authorID, err := bson.ObjectIDFromHex(authorIDStr)
    if err != nil {
        return nil, errors.New("invalid author id")
    }

    // Validation
    req.Title = strings.TrimSpace(req.Title)
    if req.Title == "" {
        req.Title = "Untitled Poem"
    }
    if len(req.Title) > 200 {
        return nil, errors.New("title must be 200 characters or less")
    }
    if req.ContentJSON == "" {
        return nil, errors.New("poem content is required")
    }
    if !validateVisibility(req.Visibility) {
        req.Visibility = models.PoemVisibilityPublic
    }
    if req.Mood != "" && !models.ValidMoods[req.Mood] {
        return nil, errors.New("invalid mood value")
    }

    hashtags := sanitizeHashtags(req.Hashtags)

    poem := &models.Poem{
        AuthorID:      authorID,
        Title:         req.Title,
        ContentJSON:   req.ContentJSON,
        PlainText:     req.PlainText,
        Hashtags:      hashtags,
        Mood:          req.Mood,
        IsOriginal:    req.IsOriginal,
        Visibility:    req.Visibility,
        AudioURL:      req.AudioURL,
        AudioDuration: req.AudioDuration,
        CoverColor:    req.CoverColor,
    }

    if err := s.repo.Create(ctx, poem); err != nil {
        return nil, err
    }

    // Increment hashtag usage counts (fire and forget — non-blocking)
    if len(hashtags) > 0 {
        go func() {
            _ = s.repo.UpsertHashtags(context.Background(), hashtags)
        }()
    }

    // Increment user's post count
    go func() {
        _ = s.userRepo.IncrementPostsCount(context.Background(), authorID)
    }()

    return s.toResponse(ctx, poem), nil
}

func (s *service) GetByID(ctx context.Context, poemIDStr string, callerID string) (*models.PoemResponse, error) {
    poemID, err := bson.ObjectIDFromHex(poemIDStr)
    if err != nil {
        return nil, errors.New("invalid poem id")
    }

    poem, err := s.repo.GetByID(ctx, poemID)
    if err != nil {
        if err == mongo.ErrNoDocuments {
            return nil, errors.New("poem not found")
        }
        return nil, err
    }

    // Private poems can only be seen by their author
    if poem.Visibility == models.PoemVisibilityPrivate && poem.AuthorID.Hex() != callerID {
        return nil, errors.New("poem not found")
    }

    return s.toResponse(ctx, poem), nil
}

func (s *service) Update(ctx context.Context, poemIDStr string, authorIDStr string, req UpdatePoemRequest) (*models.PoemResponse, error) {
    poemID, err := bson.ObjectIDFromHex(poemIDStr)
    if err != nil {
        return nil, errors.New("invalid poem id")
    }
    authorID, err := bson.ObjectIDFromHex(authorIDStr)
    if err != nil {
        return nil, errors.New("invalid author id")
    }

    // Fetch existing poem to verify ownership and get old hashtags
    existing, err := s.repo.GetByID(ctx, poemID)
    if err != nil {
        return nil, errors.New("poem not found")
    }
    if existing.AuthorID != authorID {
        return nil, errors.New("unauthorized: you do not own this poem")
    }

    // Validation
    req.Title = strings.TrimSpace(req.Title)
    if req.Title == "" {
        req.Title = "Untitled Poem"
    }
    if !validateVisibility(req.Visibility) {
        req.Visibility = existing.Visibility
    }
    if req.Mood != "" && !models.ValidMoods[req.Mood] {
        return nil, errors.New("invalid mood value")
    }

    newHashtags := sanitizeHashtags(req.Hashtags)

    updated, err := s.repo.Update(ctx, poemID, PoemUpdateFields{
        Title:         req.Title,
        ContentJSON:   req.ContentJSON,
        PlainText:     req.PlainText,
        Hashtags:      newHashtags,
        Mood:          req.Mood,
        IsOriginal:    req.IsOriginal,
        Visibility:    req.Visibility,
        AudioURL:      req.AudioURL,
        AudioDuration: req.AudioDuration,
        CoverColor:    req.CoverColor,
    })
    if err != nil {
        return nil, err
    }

    // Update hashtag counts async — decrement old, increment new
    go func() {
        _ = s.repo.DecrementHashtags(context.Background(), existing.Hashtags)
        _ = s.repo.UpsertHashtags(context.Background(), newHashtags)
    }()

    return s.toResponse(ctx, updated), nil
}

func (s *service) Delete(ctx context.Context, poemIDStr string, authorIDStr string) error {
    poemID, err := bson.ObjectIDFromHex(poemIDStr)
    if err != nil {
        return errors.New("invalid poem id")
    }
    authorID, err := bson.ObjectIDFromHex(authorIDStr)
    if err != nil {
        return errors.New("invalid author id")
    }

    // Fetch to get hashtags before deleting
    existing, err := s.repo.GetByID(ctx, poemID)
    if err != nil {
        return errors.New("poem not found")
    }
    if existing.AuthorID != authorID {
        return errors.New("unauthorized: you do not own this poem")
    }

    if err := s.repo.SoftDelete(ctx, poemID, authorID); err != nil {
        return err
    }

    // Decrement hashtags and postsCount async
    go func() {
        _ = s.repo.DecrementHashtags(context.Background(), existing.Hashtags)
        _ = s.userRepo.DecrementPostsCount(context.Background(), authorID)
    }()

    return nil
}

func (s *service) GetMyPoems(ctx context.Context, authorIDStr string, limit int, beforeStr string) (*models.PoemsPage, error) {
    authorID, err := bson.ObjectIDFromHex(authorIDStr)
    if err != nil {
        return nil, errors.New("invalid author id")
    }

    if limit <= 0 { limit = 20 }
    if limit > 50 { limit = 50 }

    var beforeID *bson.ObjectID
    if beforeStr != "" {
        id, err := bson.ObjectIDFromHex(beforeStr)
        if err != nil {
            return nil, errors.New("invalid before cursor")
        }
        beforeID = &id
    }

    // Fetch limit+1 to determine hasMore
    poems, err := s.repo.GetByAuthor(ctx, authorID, limit+1, beforeID, true)
    if err != nil {
        return nil, err
    }

    hasMore := len(poems) > limit
    if hasMore {
        poems = poems[:limit]
    }

    responses := make([]models.PoemResponse, 0, len(poems))
    for _, p := range poems {
        responses = append(responses, *s.toResponse(ctx, &p))
    }

    return &models.PoemsPage{Poems: responses, HasMore: hasMore}, nil
}

func (s *service) GetUserPoems(ctx context.Context, targetUserIDStr string, callerID string, limit int, beforeStr string) (*models.PoemsPage, error) {
    targetUserID, err := bson.ObjectIDFromHex(targetUserIDStr)
    if err != nil {
        return nil, errors.New("invalid user id")
    }

    if limit <= 0 { limit = 20 }
    if limit > 50 { limit = 50 }

    var beforeID *bson.ObjectID
    if beforeStr != "" {
        id, err := bson.ObjectIDFromHex(beforeStr)
        if err != nil {
            return nil, errors.New("invalid before cursor")
        }
        beforeID = &id
    }

    // includePrivate only if the caller is viewing their own profile
    includePrivate := targetUserID.Hex() == callerID

    poems, err := s.repo.GetByAuthor(ctx, targetUserID, limit+1, beforeID, includePrivate)
    if err != nil {
        return nil, err
    }

    hasMore := len(poems) > limit
    if hasMore {
        poems = poems[:limit]
    }

    responses := make([]models.PoemResponse, 0, len(poems))
    for _, p := range poems {
        responses = append(responses, *s.toResponse(ctx, &p))
    }

    return &models.PoemsPage{Poems: responses, HasMore: hasMore}, nil
}
```

### File: `internal/features/poems/handler.go`

```go
package poems

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

// POST /api/v1/poems
func (h *Handler) CreatePoem(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }

    var req CreatePoemRequest
    if err := c.BodyParser(&req); err != nil {
        return response.BadRequest(c, "Invalid request body")
    }

    poem, err := h.service.Create(c.Context(), user.ID.Hex(), req)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }

    return response.Created(c, "Poem created", poem)
}

// GET /api/v1/poems/:id
func (h *Handler) GetPoem(c *fiber.Ctx) error {
    poemID := c.Params("id")

    // Caller ID is optional (unauthenticated users can read public poems)
    callerID := ""
    if user, ok := c.Locals("user").(*models.User); ok {
        callerID = user.ID.Hex()
    }

    poem, err := h.service.GetByID(c.Context(), poemID, callerID)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }

    return response.OK(c, "Poem retrieved", poem)
}

// PATCH /api/v1/poems/:id
func (h *Handler) UpdatePoem(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }

    poemID := c.Params("id")
    var req UpdatePoemRequest
    if err := c.BodyParser(&req); err != nil {
        return response.BadRequest(c, "Invalid request body")
    }

    poem, err := h.service.Update(c.Context(), poemID, user.ID.Hex(), req)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }

    return response.OK(c, "Poem updated", poem)
}

// DELETE /api/v1/poems/:id
func (h *Handler) DeletePoem(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }

    poemID := c.Params("id")
    if err := h.service.Delete(c.Context(), poemID, user.ID.Hex()); err != nil {
        return response.BadRequest(c, err.Error())
    }

    return response.OK(c, "Poem deleted", nil)
}

// GET /api/v1/poems/me?limit=20&before=<id>
func (h *Handler) GetMyPoems(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }

    limit := c.QueryInt("limit", 20)
    before := c.Query("before")

    page, err := h.service.GetMyPoems(c.Context(), user.ID.Hex(), limit, before)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }

    return response.OK(c, "My poems retrieved", page)
}

// GET /api/v1/poems/user/:userId?limit=20&before=<id>
func (h *Handler) GetUserPoems(c *fiber.Ctx) error {
    targetUserID := c.Params("userId")
    limit := c.QueryInt("limit", 20)
    before := c.Query("before")

    callerID := ""
    if user, ok := c.Locals("user").(*models.User); ok {
        callerID = user.ID.Hex()
    }

    page, err := h.service.GetUserPoems(c.Context(), targetUserID, callerID, limit, before)
    if err != nil {
        return response.BadRequest(c, err.Error())
    }

    return response.OK(c, "User poems retrieved", page)
}
```

---

## Part 6 — Add to Existing User Repository

### File: `internal/features/users/repository.go`

Add these two methods to the existing `Repository` interface and `repository` struct. Do not change any existing methods.

Add to the interface:
```go
IncrementPostsCount(ctx context.Context, userID bson.ObjectID) error
DecrementPostsCount(ctx context.Context, userID bson.ObjectID) error
```

Add the implementations:
```go
func (r *repository) IncrementPostsCount(ctx context.Context, userID bson.ObjectID) error {
    _, err := r.users.UpdateOne(ctx,
        bson.M{"_id": userID},
        bson.M{"$inc": bson.M{"postsCount": 1}},
    )
    return err
}

func (r *repository) DecrementPostsCount(ctx context.Context, userID bson.ObjectID) error {
    _, err := r.users.UpdateOne(ctx,
        bson.M{"_id": userID, "postsCount": bson.M{"$gt": 0}},
        bson.M{"$inc": bson.M{"postsCount": -1}},
    )
    return err
}
```

Also add `Username`, `IsEditor`, `PostsCount` to the User struct as specified in Part 1.

---

## Part 7 — Wire Routes

### File: `internal/routes/routes.go`

Add the new handlers to the existing `SetupRoutes` function signature and register the routes. 

Update the function signature to accept the new handlers:
```go
func SetupRoutes(
    app *fiber.App,
    auth *middleware.AuthMiddleware,
    userHandler *users.Handler,
    connectionHandler *connections.Handler,
    chatHandler *chat.Handler,
    notifHandler *notifications.Handler,
    profileHandler *profile.Handler,  // NEW
    poemHandler *poems.Handler,        // NEW
)
```

Add these route groups inside the function:

```go
// ── Profile Setup ──
api.Post("/users/setup", auth.Protect(), profileHandler.SetupProfile)
api.Get("/users/username/check", profileHandler.CheckUsername) // no auth — public
api.Post("/users/username", auth.Protect(), profileHandler.SetUsername)

// ── Poems ──
poemRoutes := api.Group("/poems", auth.OptionalAuth()) // OptionalAuth: read works without token
poemRoutes.Post("/", auth.Protect(), poemHandler.CreatePoem)
poemRoutes.Get("/me", auth.Protect(), poemHandler.GetMyPoems)
poemRoutes.Get("/user/:userId", poemHandler.GetUserPoems)
poemRoutes.Get("/:id", poemHandler.GetPoem)
poemRoutes.Patch("/:id", auth.Protect(), poemHandler.UpdatePoem)
poemRoutes.Delete("/:id", auth.Protect(), poemHandler.DeletePoem)
```

Note: `auth.OptionalAuth()` is a middleware that sets `user` in locals if a valid token is present but does NOT reject the request if no token is provided. If this doesn't exist in the codebase, create it in `internal/middleware/auth.go`:

```go
// OptionalAuth — sets user in locals if token present, continues regardless
func (m *AuthMiddleware) OptionalAuth() fiber.Handler {
    return func(c *fiber.Ctx) error {
        token := extractToken(c)
        if token != "" {
            if user, err := m.verifyAndGetUser(c.Context(), token); err == nil {
                c.Locals("user", user)
            }
        }
        return c.Next()
    }
}
```

### File: `cmd/api/main.go`

Wire the new packages:

```go
// Add after existing service/handler setup:
profileRepo := profile.NewRepository(db.Database)
profileService := profile.NewService(profileRepo)
profileHandler := profile.NewHandler(profileService)

poemRepo := poems.NewRepository(db.Database)
poemService := poems.NewService(poemRepo, userRepo) // reuses existing userRepo
poemHandler := poems.NewHandler(poemService)

// Pass to SetupRoutes
routes.SetupRoutes(app, authMiddleware, userHandler, connectionHandler,
    chatHandler, notifHandler, profileHandler, poemHandler)
```

---

## Part 8 — Implementation Order for Windsurf

Follow this exact order. Do not jump ahead.

1. Add new fields to `User` struct in `models/user.go`
2. Create `models/poem.go` and `models/hashtag.go`
3. Add new indexes to `database/indexes.go`
4. Add `IncrementPostsCount` and `DecrementPostsCount` to users repository
5. Create `features/profile/` package (repository → service → handler)
6. Create `features/poems/` package (repository → service → handler)
7. Add `OptionalAuth` middleware if it doesn't exist
8. Update `routes/routes.go` to register all new routes
9. Wire everything in `main.go`
10. Run `go build ./...` and fix any compile errors before testing

---

## API Summary

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | `/api/v1/users/setup` | Required | Complete profile after first login |
| GET | `/api/v1/users/username/check?username=x` | None | Check username availability |
| POST | `/api/v1/users/username` | Required | Set username permanently |
| POST | `/api/v1/poems` | Required | Create poem (publish or draft) |
| GET | `/api/v1/poems/me` | Required | Get own poems (includes drafts) |
| GET | `/api/v1/poems/user/:userId` | Optional | Get user's public poems |
| GET | `/api/v1/poems/:id` | Optional | Get single poem |
| PATCH | `/api/v1/poems/:id` | Required | Update poem |
| DELETE | `/api/v1/poems/:id` | Required | Soft delete poem |
