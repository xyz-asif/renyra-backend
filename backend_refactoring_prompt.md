# Backend Refactoring Guide — Poetry Social Platform (ChatBee)

> **Target IDE**: Antigravity / Windsurf / Cursor
> **Stack**: Go 1.21+ · Fiber v2 · MongoDB (driver v2) · Firebase Auth · WebSocket
> **Pattern**: handler → service → repository (per feature package)
> **Goal**: Fix all audit issues so the backend can reliably serve 1 million users

---

## How to use this prompt

Work through each numbered section **in order**. Each section is self-contained: it tells you which file(s) to touch, what to change, and why. After completing each section, run `go build ./...` to verify compilation before moving to the next.

Do **not** skip CRITICAL fixes to work on lower-priority items — later fixes assume earlier ones are already done.

---

## CRITICAL FIXES — Do these first

---

### 1. Remove the Duplicate Follow System

**Problem**: Two completely separate follow implementations exist. The `follows` package (`internal/features/follows/`) has a `ToggleFollow` method that handles follow/unfollow. But the `users` package (`internal/features/users/`) has its own `FollowUser`/`UnfollowUser` methods that also write to the same `follows` MongoDB collection and update the same `followersCount`/`followingCount` fields on the user document. Having two systems writing to the same data causes count drift and confusion.

**The `follows` package is the correct one to keep** — it's what the routes in `routes.go` actually point to (`followHandler.ToggleFollow`). The `users` package version is legacy dead code.

**Files to modify:**

#### `internal/features/users/repository.go`

Delete these methods entirely:

```go
// DELETE: FollowUser (the entire method, ~30 lines, uses transactions)
// DELETE: UnfollowUser (the entire method, ~25 lines, uses transactions)
// DELETE: GetFollowedUsers (the entire method, ~15 lines)
```

Also remove `FollowUser`, `UnfollowUser` from the `Repository` interface at the top of the file.

Remove the `followsColl` field from the `repository` struct and its initialization in `NewRepository`:

```go
// BEFORE
type repository struct {
    db          *mongo.Database
    client      *mongo.Client
    collection  *mongo.Collection
    followsColl *mongo.Collection  // DELETE this line
}

func NewRepository(db *mongo.Database) Repository {
    return &repository{
        db:          db,
        client:      db.Client(),
        collection:  db.Collection("users"),
        followsColl: db.Collection("follows"),  // DELETE this line
    }
}
```

#### `internal/features/users/service.go`

Delete these methods:

```go
// DELETE: func (s *service) FollowUser(...)
// DELETE: func (s *service) UnfollowUser(...)
```

Remove `FollowUser` and `UnfollowUser` from the `Service` interface:

```go
type Service interface {
    GetOrCreateUser(ctx context.Context, firebaseUID, email, displayName, photoURL string) (*models.User, error)
    GetUserByID(ctx context.Context, userID string) (*models.User, error)
    GetUsersByIDs(ctx context.Context, userIDs []string) (map[string]*models.User, error)
    UpdateProfile(ctx context.Context, userID string, updates map[string]interface{}) (*models.User, error)
    // DELETE: FollowUser(ctx context.Context, followerID, followedUserID string) error
    // DELETE: UnfollowUser(ctx context.Context, followerID, followedUserID string) error
    SearchUsers(ctx context.Context, query string, limit, offset int) ([]models.User, error)
    SearchUsersWithConnectionStatus(ctx context.Context, currentUserID, query string, limit, offset int) (*UserSearchResult, error)
    GetFeed(ctx context.Context, userID string) ([]interface{}, error)
    RegisterFCMToken(ctx context.Context, userID, token string) error
}
```

#### `internal/features/users/handler.go`

Delete these handler methods entirely:

```go
// DELETE: func (h *Handler) FollowUser(c *fiber.Ctx) error { ... }
// DELETE: func (h *Handler) UnfollowUser(c *fiber.Ctx) error { ... }
```

#### Verification

After deletion, run `go build ./...`. If anything references the deleted methods, you'll get compile errors — track them down and remove those references too. The routes in `routes.go` should NOT reference these deleted methods (they use `followHandler.ToggleFollow` instead).

---

### 2. Fix `follows.service.ToggleFollow` to Use Transactions

**Problem**: The current `ToggleFollow` does three separate operations: (1) insert/delete the follow record, (2) increment/decrement follower count, (3) increment/decrement following count. If any step fails, the data is inconsistent. Errors from steps 2-3 are silently discarded with `_ =`.

**Files to modify:**

#### `internal/features/follows/service.go`

First, add `*mongo.Client` to the service struct and constructor:

```go
type service struct {
    repo         Repository
    userRepo     users.Repository
    notifService notifications.Service
    mongoClient  *mongo.Client  // ADD THIS
}

func NewService(repo Repository, userRepo users.Repository, notifService notifications.Service, mongoClient *mongo.Client) Service {
    return &service{
        repo:         repo,
        userRepo:     userRepo,
        notifService: notifService,
        mongoClient:  mongoClient,  // ADD THIS
    }
}
```

Then rewrite the follow/unfollow logic inside `ToggleFollow` to use transactions:

```go
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
        // === UNFOLLOW (in transaction) ===
        session, err := s.mongoClient.StartSession()
        if err != nil {
            return false, fmt.Errorf("failed to start session: %w", err)
        }
        defer session.EndSession(ctx)

        _, err = session.WithTransaction(ctx, func(sessCtx context.Context) (interface{}, error) {
            if err := s.repo.Unfollow(sessCtx, followerID, followingID); err != nil {
                return nil, err
            }
            if err := s.userRepo.DecrementFollowersCount(sessCtx, followingID); err != nil {
                return nil, fmt.Errorf("failed to decrement followers count: %w", err)
            }
            if err := s.userRepo.DecrementFollowingCount(sessCtx, followerID); err != nil {
                return nil, fmt.Errorf("failed to decrement following count: %w", err)
            }
            return nil, nil
        })
        if err != nil {
            return false, fmt.Errorf("unfollow transaction failed: %w", err)
        }
        return false, nil
    }

    // === FOLLOW (in transaction) ===
    session, err := s.mongoClient.StartSession()
    if err != nil {
        return false, fmt.Errorf("failed to start session: %w", err)
    }
    defer session.EndSession(ctx)

    _, err = session.WithTransaction(ctx, func(sessCtx context.Context) (interface{}, error) {
        if err := s.repo.Follow(sessCtx, followerID, followingID); err != nil {
            if mongo.IsDuplicateKeyError(err) {
                return nil, nil // already following due to race — idempotent
            }
            return nil, err
        }
        if err := s.userRepo.IncrementFollowersCount(sessCtx, followingID); err != nil {
            return nil, fmt.Errorf("failed to increment followers count: %w", err)
        }
        if err := s.userRepo.IncrementFollowingCount(sessCtx, followerID); err != nil {
            return nil, fmt.Errorf("failed to increment following count: %w", err)
        }
        return nil, nil
    })
    if err != nil {
        return false, fmt.Errorf("follow transaction failed: %w", err)
    }

    // Notify the followed user (non-critical, async with timeout)
    go func() {
        nCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        if s.notifService != nil {
            follower, _ := s.userRepo.GetUserByID(nCtx, followerID)
            name := "Someone"
            if follower != nil {
                name = follower.DisplayName
            }
            _ = s.notifService.Send(nCtx, models.SendNotificationRequest{
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
    return true, nil
}
```

Add `"fmt"` to the import block if not already present.

#### `cmd/main.go` (or wherever `main.go` is)

Update the `NewService` call for follows to pass the mongo client:

```go
// BEFORE
followService := follows.NewService(followRepo, userRepo, notifService)

// AFTER
followService := follows.NewService(followRepo, userRepo, notifService, db.Client)
```

Where `db` is the `*database.MongoDB` instance. `db.Client` is the `*mongo.Client`.

---

### 3. Fix ALL N+1 Query Patterns

This is the single biggest performance issue. There are 6 locations where the code fetches users one-by-one inside a loop instead of batching.

#### 3A. Chat — `buildRoomResponse` fetches each participant individually

**File**: `internal/features/chat/service.go`

**Current problem**: `buildRoomResponse` calls `s.userRepo.GetUserByID(ctx, p)` for each participant in a loop (line ~1038). `GetUserRooms` (line ~279) already batch-fetches users into a map but then calls `buildRoomResponse` which ignores that map and fetches again.

**Fix**: Change `buildRoomResponse` to accept an optional pre-loaded user map:

```go
func (s *service) buildRoomResponse(ctx context.Context, room *models.Room, forUserID string, userMap map[bson.ObjectID]*models.User) (*models.RoomResponse, error) {
    resp := &models.RoomResponse{
        ID:              room.ID.Hex(),
        Type:            room.Type,
        Name:            room.Name,
        LastMessage:     room.LastMessage,
        LastMessageType: room.LastMessageType,
        LastUpdated:     room.LastUpdated,
        Participants:    make([]models.ParticipantInfo, 0, len(room.Participants)),
    }

    if room.UnreadCounts != nil {
        resp.UnreadCount = room.UnreadCounts[forUserID]
    }

    // Helper to get user from map or DB as fallback
    getUser := func(id bson.ObjectID) *models.User {
        if userMap != nil {
            if u, ok := userMap[id]; ok {
                return u
            }
        }
        u, err := s.userRepo.GetUserByID(ctx, id)
        if err != nil {
            return nil
        }
        return u
    }

    // Resolve last message sender name
    if room.LastMessageSenderID != nil {
        if sender := getUser(*room.LastMessageSenderID); sender != nil {
            resp.LastMessageSenderName = sender.DisplayName
        }
    }

    // Build participant info
    for _, p := range room.Participants {
        if user := getUser(p); user != nil {
            resp.Participants = append(resp.Participants, models.ParticipantInfo{
                ID:          user.ID.Hex(),
                DisplayName: user.DisplayName,
                PhotoURL:    user.PhotoURL,
                Email:       user.Email,
                IsOnline:    s.hub.IsUserOnline(user.ID.Hex()),
            })
        }
    }

    return resp, nil
}
```

Then update `GetUserRooms` to pass the map it already builds:

```go
func (s *service) GetUserRooms(ctx context.Context, userIDStr string) ([]models.RoomResponse, error) {
    userID, err := bson.ObjectIDFromHex(userIDStr)
    if err != nil {
        return nil, errors.New("invalid user id")
    }

    rooms, err := s.repo.GetUserRooms(ctx, userID)
    if err != nil {
        return nil, err
    }

    // Collect all unique user IDs
    userIDSet := make(map[bson.ObjectID]bool)
    for _, r := range rooms {
        for _, p := range r.Participants {
            userIDSet[p] = true
        }
        if r.LastMessageSenderID != nil {
            userIDSet[*r.LastMessageSenderID] = true
        }
    }
    userIDs := make([]bson.ObjectID, 0, len(userIDSet))
    for id := range userIDSet {
        userIDs = append(userIDs, id)
    }

    // Single batch query
    userMap, err := s.userRepo.GetUsersByIDs(ctx, userIDs)
    if err != nil {
        return nil, err
    }

    // Build responses with the preloaded map
    responses := make([]models.RoomResponse, 0, len(rooms))
    for _, r := range rooms {
        resp, err := s.buildRoomResponse(ctx, &r, userIDStr, userMap)
        if err != nil {
            log.Printf("GetUserRooms: failed to build room response for room %s: %v", r.ID.Hex(), err)
            continue
        }
        responses = append(responses, *resp)
    }

    return responses, nil
}
```

Do the same for `GetUserRoomsWithSearch` — add the batch fetch before the loop:

```go
func (s *service) GetUserRoomsWithSearch(ctx context.Context, userIDStr, searchQuery string, limit, offset int) ([]models.RoomResponse, int64, bool, error) {
    // ... existing validation code ...

    rooms, totalCount, err := s.repo.GetUserRoomsWithSearch(ctx, userID, searchQuery, limit, offset)
    if err != nil {
        return nil, 0, false, err
    }

    hasMore := int64(offset+len(rooms)) < totalCount
    if len(rooms) == 0 {
        return []models.RoomResponse{}, totalCount, hasMore, nil
    }

    // Batch fetch all users referenced in these rooms
    userIDSet := make(map[bson.ObjectID]bool)
    for _, r := range rooms {
        for _, p := range r.Participants {
            userIDSet[p] = true
        }
        if r.LastMessageSenderID != nil {
            userIDSet[*r.LastMessageSenderID] = true
        }
    }
    userIDs := make([]bson.ObjectID, 0, len(userIDSet))
    for id := range userIDSet {
        userIDs = append(userIDs, id)
    }
    userMap, err := s.userRepo.GetUsersByIDs(ctx, userIDs)
    if err != nil {
        log.Printf("[SERVICE] Failed to batch fetch users: %v", err)
        userMap = make(map[bson.ObjectID]*models.User) // fallback to empty
    }

    responses := make([]models.RoomResponse, 0, len(rooms))
    for _, r := range rooms {
        resp, err := s.buildRoomResponse(ctx, &r, userIDStr, userMap)
        if err != nil {
            continue
        }
        responses = append(responses, *resp)
    }

    return responses, totalCount, hasMore, nil
}
```

Update **all other callers** of `buildRoomResponse` (like `GetOrCreateDirectRoom`, `SendMessage` room creation in connections, etc.) to pass `nil` for the userMap parameter when a pre-loaded map is not available — the fallback inside the function handles it.

#### 3B. Chat — `buildMessageResponse` fetches sender per message

**File**: `internal/features/chat/service.go`

**Fix**: Change `buildMessageResponse` to accept a user map, then batch-fetch in `GetRoomMessages`:

```go
func (s *service) buildMessageResponse(ctx context.Context, msg *models.Message, userMap map[bson.ObjectID]*models.User) *models.MessageResponse {
    resp := &models.MessageResponse{
        ID:        msg.ID.Hex(),
        RoomID:    msg.RoomID.Hex(),
        SenderID:  msg.SenderID.Hex(),
        Type:      msg.Type,
        Content:   msg.Content,
        Metadata:  msg.Metadata,
        Status:    msg.Status,
        Reactions: msg.Reactions,
        IsEdited:  msg.IsEdited,
        IsDeleted: msg.IsDeleted,
        CreatedAt: msg.CreatedAt,
        UpdatedAt: msg.UpdatedAt,
    }

    if resp.Type == "" {
        resp.Type = models.MessageTypeText
    }

    // Use map first, fallback to individual fetch
    getUser := func(id bson.ObjectID) *models.User {
        if userMap != nil {
            if u, ok := userMap[id]; ok {
                return u
            }
        }
        u, _ := s.userRepo.GetUserByID(ctx, id)
        return u
    }

    if sender := getUser(msg.SenderID); sender != nil {
        resp.SenderName = sender.DisplayName
        resp.SenderPhotoURL = sender.PhotoURL
    }

    // Reply-to (one level deep)
    if msg.ReplyToID != nil {
        if replyMsg, err := s.repo.GetMessageByID(ctx, *msg.ReplyToID); err == nil {
            replyResp := &models.MessageResponse{
                ID:        replyMsg.ID.Hex(),
                RoomID:    replyMsg.RoomID.Hex(),
                SenderID:  replyMsg.SenderID.Hex(),
                Type:      replyMsg.Type,
                Content:   replyMsg.Content,
                Metadata:  replyMsg.Metadata,
                Status:    replyMsg.Status,
                IsDeleted: replyMsg.IsDeleted,
                CreatedAt: replyMsg.CreatedAt,
            }
            if replyResp.Type == "" {
                replyResp.Type = models.MessageTypeText
            }
            if replySender := getUser(replyMsg.SenderID); replySender != nil {
                replyResp.SenderName = replySender.DisplayName
                replyResp.SenderPhotoURL = replySender.PhotoURL
            }
            resp.ReplyTo = replyResp
        }
    }

    return resp
}
```

Then update `GetRoomMessages` to batch-fetch users before the loop:

```go
func (s *service) GetRoomMessages(ctx context.Context, userIDStr, roomIDStr string, limit int, beforeIDStr string) (*models.MessagesPage, error) {
    // ... existing validation and fetch code up to getting msgs ...

    hasMore := len(msgs) > limit
    if hasMore {
        msgs = msgs[:limit]
    }

    // Batch fetch all unique user IDs (senders + reply senders)
    userIDSet := make(map[bson.ObjectID]bool)
    replyIDs := make([]bson.ObjectID, 0)
    for _, m := range msgs {
        userIDSet[m.SenderID] = true
        if m.ReplyToID != nil {
            replyIDs = append(replyIDs, *m.ReplyToID)
        }
    }

    // Fetch reply messages in batch to get their sender IDs too
    // (this avoids N individual GetMessageByID calls)
    for _, rid := range replyIDs {
        if replyMsg, err := s.repo.GetMessageByID(ctx, rid); err == nil {
            userIDSet[replyMsg.SenderID] = true
        }
    }

    userIDs := make([]bson.ObjectID, 0, len(userIDSet))
    for id := range userIDSet {
        userIDs = append(userIDs, id)
    }
    userMap, err := s.userRepo.GetUsersByIDs(ctx, userIDs)
    if err != nil {
        log.Printf("GetRoomMessages: failed to batch fetch users: %v", err)
        userMap = make(map[bson.ObjectID]*models.User)
    }

    responses := make([]models.MessageResponse, 0, len(msgs))
    for _, m := range msgs {
        responses = append(responses, *s.buildMessageResponse(ctx, &m, userMap))
    }

    // Reverse to chronological order
    for i, j := 0, len(responses)-1; i < j; i, j = i+1, j-1 {
        responses[i], responses[j] = responses[j], responses[i]
    }

    return &models.MessagesPage{Messages: responses, HasMore: hasMore}, nil
}
```

Update **all other callers** of `buildMessageResponse` (like `SendMessage`) to pass `nil` for the userMap.

#### 3C. Feed — `buildPoemResponse` fetches author per poem

**File**: `internal/features/feed/service.go`

**Fix**: Change `buildPoemResponse` to accept the author directly:

```go
func (s *service) buildPoemResponse(ctx context.Context, poem *models.Poem, author *models.User, isLiked, isReposted bool) models.PoemResponse {
    resp := models.PoemResponse{
        ID:             poem.ID.Hex(),
        Title:          poem.Title,
        ContentJSON:    poem.ContentJSON,
        PlainText:      poem.PlainText,
        Hashtags:       poem.Hashtags,
        Mood:           poem.Mood,
        IsOriginal:     poem.IsOriginal,
        Visibility:     poem.Visibility,
        AudioURL:       poem.AudioURL,
        AudioDuration:  poem.AudioDuration,
        CoverColor:     poem.CoverColor,
        LikesCount:     poem.LikesCount,
        CommentsCount:  poem.CommentsCount,
        RepostsCount:   poem.RepostsCount,
        IsLikedByMe:    isLiked,
        IsRepostedByMe: isReposted,
        CreatedAt:      poem.CreatedAt,
        UpdatedAt:      poem.UpdatedAt,
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

    if resp.Hashtags == nil {
        resp.Hashtags = []string{}
    }

    return resp
}
```

Then update every caller (GetHomeFeed, GetExploreFeed, GetAudioFeed, SearchPoems) to batch-fetch authors. Here is the pattern — apply it to **all four methods**:

```go
// After fetching poemDocs and trimming to limit...

// Batch fetch authors
authorIDSet := make(map[bson.ObjectID]bool)
for _, p := range poemDocs {
    authorIDSet[p.AuthorID] = true
}
authorIDs := make([]bson.ObjectID, 0, len(authorIDSet))
for id := range authorIDSet {
    authorIDs = append(authorIDs, id)
}
authorMap, err := s.userRepo.GetUsersByIDs(ctx, authorIDs)
if err != nil {
    log.Printf("Failed to batch fetch authors: %v", err)
    authorMap = make(map[bson.ObjectID]*models.User)
}

// Build responses
responses := make([]models.PoemResponse, 0, len(poemDocs))
for _, p := range poemDocs {
    author := authorMap[p.AuthorID]
    responses = append(responses, s.buildPoemResponse(ctx, &p, author, likedMap[p.ID.Hex()], repostedMap[p.ID.Hex()]))
}
```

#### 3D. Poems — `toResponse` / `buildAuthor` fetch author per poem

**File**: `internal/features/poems/service.go`

**Fix**: Change `buildAuthor` to accept a `*models.User` and change `toResponse` to accept it too:

```go
func buildAuthor(user *models.User) models.PoemAuthor {
    if user == nil {
        return models.PoemAuthor{}
    }
    return models.PoemAuthor{
        ID:          user.ID.Hex(),
        DisplayName: user.DisplayName,
        Username:    user.Username,
        PhotoURL:    user.PhotoURL,
        IsEditor:    user.IsEditor,
    }
}

func (s *service) toResponse(poem *models.Poem, author *models.User) *models.PoemResponse {
    return &models.PoemResponse{
        ID:            poem.ID.Hex(),
        Author:        buildAuthor(author),
        Title:         poem.Title,
        // ... all other fields same as before ...
        CreatedAt:     poem.CreatedAt,
        UpdatedAt:     poem.UpdatedAt,
    }
}
```

Update `GetMyPoems` — all poems have the same author, so fetch once:

```go
func (s *service) GetMyPoems(ctx context.Context, authorIDStr string, limit int, beforeStr string) (*models.PoemsPage, error) {
    authorID, err := bson.ObjectIDFromHex(authorIDStr)
    if err != nil {
        return nil, errors.New("invalid author id")
    }
    // ... existing pagination code ...

    poems, err := s.repo.GetByAuthor(ctx, authorID, limit+1, beforeID, true)
    if err != nil {
        return nil, err
    }

    hasMore := len(poems) > limit
    if hasMore {
        poems = poems[:limit]
    }

    // Fetch the author ONCE (all poems have the same author)
    author, _ := s.userRepo.GetUserByID(ctx, authorID)

    // Batch check likes
    var likedMap map[string]bool
    if s.socialRepo != nil {
        ids := make([]bson.ObjectID, 0, len(poems))
        for _, p := range poems {
            ids = append(ids, p.ID)
        }
        likedMap, _ = s.socialRepo.IsPoemLikedMany(ctx, authorID, ids)
    }

    responses := make([]models.PoemResponse, 0, len(poems))
    for _, p := range poems {
        resp := s.toResponse(&p, author)
        if likedMap != nil {
            resp.IsLikedByMe = likedMap[p.ID.Hex()]
        }
        responses = append(responses, *resp)
    }

    return &models.PoemsPage{Poems: responses, HasMore: hasMore}, nil
}
```

Update `GetUserPoems` with batch fetch:

```go
func (s *service) GetUserPoems(ctx context.Context, targetUserIDStr string, callerID string, limit int, beforeStr string) (*models.PoemsPage, error) {
    // ... existing code up to fetching poems ...

    // Fetch the author once
    author, _ := s.userRepo.GetUserByID(ctx, targetUserID)

    responses := make([]models.PoemResponse, 0, len(poems))
    for _, p := range poems {
        responses = append(responses, *s.toResponse(&p, author))
    }

    return &models.PoemsPage{Poems: responses, HasMore: hasMore}, nil
}
```

Update `Create`, `GetByID`, `Update` — these deal with single poems, so a single fetch is fine:

```go
// In Create, after repo.Create:
author, _ := s.userRepo.GetUserByID(ctx, authorID)
return s.toResponse(poem, author), nil

// In GetByID:
author, _ := s.userRepo.GetUserByID(ctx, poem.AuthorID)
return s.toResponse(poem, author), nil

// In Update:
author, _ := s.userRepo.GetUserByID(ctx, updated.AuthorID)
return s.toResponse(updated, author), nil
```

#### 3E. Notifications — `GetNotifications` fetches actor per notification

**File**: `internal/features/notifications/service.go`

**Fix**: Batch-fetch all actors before the loop in `GetNotifications`:

```go
func (s *service) GetNotifications(ctx context.Context, userIDStr string, limit int, beforeStr string) ([]models.NotificationResponse, bool, error) {
    // ... existing code up to getting notifs slice ...

    hasMore := len(notifs) > limit
    if hasMore {
        notifs = notifs[:limit]
    }

    // Batch fetch all actors
    actorIDSet := make(map[bson.ObjectID]bool)
    for _, n := range notifs {
        actorIDSet[n.ActorID] = true
    }
    actorIDs := make([]bson.ObjectID, 0, len(actorIDSet))
    for id := range actorIDSet {
        actorIDs = append(actorIDs, id)
    }

    // Use the GetUsersByIDs method that returns map[bson.ObjectID]*models.User
    actorMap := make(map[bson.ObjectID]*models.User)
    if userLookupRepo, ok := s.userLookup.(interface {
        GetUsersByIDs(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]*models.User, error)
    }); ok && len(actorIDs) > 0 {
        if m, err := userLookupRepo.GetUsersByIDs(ctx, actorIDs); err == nil {
            actorMap = m
        }
    }

    responses := make([]models.NotificationResponse, 0, len(notifs))
    for _, n := range notifs {
        resp := models.NotificationResponse{
            ID:            n.ID.Hex(),
            Type:          n.Type,
            ResourceType:  n.ResourceType,
            ResourceID:    n.ResourceID,
            Title:         n.Title,
            Body:          n.Body,
            ActorID:       n.ActorID.Hex(),
            IsRead:        n.IsRead,
            CreatedAt:     n.CreatedAt,
            ActorPhotoURL: n.ImageURL,
        }

        if actor, ok := actorMap[n.ActorID]; ok && actor != nil {
            resp.ActorName = actor.DisplayName
        }

        responses = append(responses, resp)
    }

    return responses, hasMore, nil
}
```

**Alternative** (if the type assertion feels fragile): Add `GetUsersByIDs` to the `UserLookup` interface in `notifications/service.go`:

```go
type UserLookup interface {
    GetUserByID(ctx context.Context, id bson.ObjectID) (*models.User, error)
    GetUsersByIDs(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]*models.User, error)  // ADD
    RemoveFCMTokens(ctx context.Context, userID bson.ObjectID, tokens []string) error
}
```

The `users.Repository` already implements `GetUsersByIDs`, so this just makes the interface aware of it.

#### 3F. Users — `SearchUsersWithConnectionStatus` queries connections one-by-one

**File**: `internal/features/connections/repository.go`

Add a new batch method:

```go
// GetConnectionsBetweenUserAndMany returns all connections between userID and any of targetIDs.
// Returns a map keyed by the OTHER user's ID for easy lookup.
func (r *repository) GetConnectionsBetweenUserAndMany(ctx context.Context, userID bson.ObjectID, targetIDs []bson.ObjectID) (map[bson.ObjectID]*models.Connection, error) {
    if len(targetIDs) == 0 {
        return make(map[bson.ObjectID]*models.Connection), nil
    }

    filter := bson.M{
        "$or": []bson.M{
            {"senderId": userID, "receiverId": bson.M{"$in": targetIDs}},
            {"receiverId": userID, "senderId": bson.M{"$in": targetIDs}},
        },
    }

    cursor, err := r.collection.Find(ctx, filter)
    if err != nil {
        return nil, err
    }
    defer cursor.Close(ctx)

    var connections []models.Connection
    if err := cursor.All(ctx, &connections); err != nil {
        return nil, err
    }

    result := make(map[bson.ObjectID]*models.Connection, len(connections))
    for i, conn := range connections {
        if conn.SenderID == userID {
            result[conn.ReceiverID] = &connections[i]
        } else {
            result[conn.SenderID] = &connections[i]
        }
    }
    return result, nil
}
```

Add it to the `connections.Repository` interface:

```go
type Repository interface {
    // ... existing methods ...
    GetConnectionsBetweenUserAndMany(ctx context.Context, userID bson.ObjectID, targetIDs []bson.ObjectID) (map[bson.ObjectID]*models.Connection, error)
}
```

**File**: `internal/features/users/service.go`

Update the `ConnectionRepository` interface to include the new method:

```go
type ConnectionRepository interface {
    GetUserConnections(ctx context.Context, userID bson.ObjectID, status string) ([]models.Connection, error)
    GetConnectionBetweenUsers(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Connection, error)
    GetConnectionsBetweenUserAndMany(ctx context.Context, userID bson.ObjectID, targetIDs []bson.ObjectID) (map[bson.ObjectID]*models.Connection, error)  // ADD
}
```

Then rewrite the loop in `SearchUsersWithConnectionStatus`:

```go
// REPLACE the per-user loop with:

// Collect target user IDs
targetIDs := make([]bson.ObjectID, 0, len(users))
for _, user := range users {
    if user.ID != currentUserObjID {
        targetIDs = append(targetIDs, user.ID)
    }
}

// Batch fetch all connections
connMap, err := s.connRepo.GetConnectionsBetweenUserAndMany(ctx, currentUserObjID, targetIDs)
if err != nil {
    log.Printf("Error batch fetching connections: %v", err)
    connMap = make(map[bson.ObjectID]*models.Connection)
}

for _, user := range users {
    if user.ID == currentUserObjID {
        continue
    }

    userWithConn := UserWithConnection{
        User:             user,
        ConnectionStatus: "none",
    }

    if conn, ok := connMap[user.ID]; ok && conn != nil {
        userWithConn.ConnectionStatus = conn.Status
        userWithConn.ConnectionID = conn.ID.Hex()
        if conn.SenderID == currentUserObjID {
            userWithConn.IsSender = true
            if conn.Status == models.ConnectionStatusPending {
                userWithConn.ConnectionStatus = "pending_sent"
            }
        } else if conn.ReceiverID == currentUserObjID {
            userWithConn.IsSender = false
            if conn.Status == models.ConnectionStatusPending {
                userWithConn.ConnectionStatus = "pending_received"
            }
        }
    }

    result.Users = append(result.Users, userWithConn)
}
```

---

### 4. Sanitize Regex Search Inputs

**Problem**: User-provided search queries are passed directly into MongoDB `$regex` operators. A malicious user can submit a regex like `.*` or `(a{999999})` to cause ReDoS (Regular Expression Denial of Service).

**Files to modify and exact changes:**

#### `internal/features/users/repository.go` — `SearchUsers`

```go
import "regexp"

func (r *repository) SearchUsers(ctx context.Context, query string, limit, offset int) ([]models.User, error) {
    escapedQuery := regexp.QuoteMeta(query)  // ADD THIS LINE

    filter := bson.M{
        "$or": []bson.M{
            {"displayName": bson.M{"$regex": escapedQuery, "$options": "i"}},  // USE escapedQuery
            {"email": bson.M{"$regex": escapedQuery, "$options": "i"}},        // USE escapedQuery
        },
        "isActive": true,
    }
    // ... rest unchanged ...
}
```

#### `internal/features/feed/repository.go` — `SearchUsers`

```go
import "regexp"

func (r *repository) SearchUsers(ctx context.Context, query string, limit int, skip int) ([]models.User, int64, error) {
    escapedQuery := regexp.QuoteMeta(query)  // ADD THIS LINE

    filter := bson.M{
        "$or": []bson.M{
            {"displayName": bson.M{"$regex": escapedQuery, "$options": "i"}},  // USE escapedQuery
            {"username": bson.M{"$regex": escapedQuery, "$options": "i"}},     // USE escapedQuery
        },
    }
    // ... rest unchanged ...
}
```

#### `internal/features/chat/repository.go` — `GetUserRoomsWithSearch`

```go
import "regexp"

// Inside GetUserRoomsWithSearch, where the search filter is built:
if searchQuery != "" && searchQuery != "*" {
    escapedQuery := regexp.QuoteMeta(searchQuery)  // ADD THIS LINE

    userFilter := bson.M{
        "displayName": bson.M{
            "$regex":   escapedQuery,  // USE escapedQuery
            "$options": "i",
        },
    }
    // ... and further down where room name is searched:
    orConditions = append(orConditions, bson.M{
        "name": bson.M{
            "$regex":   escapedQuery,  // USE escapedQuery
            "$options": "i",
        },
    })
}
```

---

### 5. Add Unique Compound Index for Direct Chat Rooms

**Problem**: `GetOrCreateDirectRoomAtomic` handles `DuplicateKeyError` to deal with race conditions, but there is no unique index that would actually trigger this error. Two concurrent requests will create duplicate direct rooms.

**File**: `internal/database/indexes.go`

Add this index to the `roomsIndexes` slice:

```go
roomsIndexes := []mongo.IndexModel{
    // ... existing indexes ...

    // Unique index for direct rooms — prevents duplicate rooms between the same two users
    // The partial filter ensures this only applies to "direct" type rooms
    {
        Keys: bson.D{{Key: "type", Value: 1}, {Key: "participants", Value: 1}},
        Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{"type": "direct"}),
    },
}
```

**Important note**: This index works because MongoDB considers the entire `participants` array value for uniqueness. For direct rooms, participants always have exactly 2 entries. However, the order of participants in the array matters for uniqueness. Ensure that when creating a direct room, participants are always sorted consistently:

**File**: `internal/features/chat/repository.go` — `GetOrCreateDirectRoomAtomic`

```go
// Before creating the room, sort participant IDs for consistent ordering:
participants := []bson.ObjectID{user1ID, user2ID}
if user1ID.Hex() > user2ID.Hex() {
    participants = []bson.ObjectID{user2ID, user1ID}
}

newRoom := &models.Room{
    Type:         models.RoomTypeDirect,
    Participants: participants,  // USE sorted participants
    // ... rest unchanged ...
}
```

Also update `GetDirectRoom` to search with sorted participants:

```go
func (r *repository) GetDirectRoom(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Room, error) {
    // Sort for consistent lookup
    p1, p2 := user1ID, user2ID
    if p1.Hex() > p2.Hex() {
        p1, p2 = p2, p1
    }

    var room models.Room
    filter := bson.M{
        "type":         models.RoomTypeDirect,
        "participants": bson.A{p1, p2},  // Exact array match in sorted order
    }
    // ... rest unchanged ...
}
```

---

### 6. Fix SetUsername — Remove TOCTOU Race Condition

**Problem**: `profile.service.SetUsername` checks `IsUsernameTaken` and then calls `repo.SetUsername` as separate steps. Two users can both pass the check simultaneously. The unique index catches the second write, but the error message is confusing.

**File**: `internal/features/profile/service.go`

Replace the `SetUsername` method:

```go
func (s *service) SetUsername(ctx context.Context, userIDStr string, username string) (*models.User, error) {
    userID, err := bson.ObjectIDFromHex(userIDStr)
    if err != nil {
        return nil, errors.New("invalid user id")
    }

    username = strings.ToLower(strings.TrimSpace(username))

    // Validate format
    if !usernameRegex.MatchString(username) {
        return nil, errors.New("invalid username format: use 3-30 lowercase letters, numbers, or underscores")
    }
    if reservedUsernames[username] {
        return nil, errors.New("this username is reserved")
    }

    // Just try to set it — the unique index handles concurrency
    // NO IsUsernameTaken check needed (that was a TOCTOU race)
    user, err := s.repo.SetUsername(ctx, userID, username)
    if err != nil {
        errMsg := err.Error()
        if strings.Contains(errMsg, "already taken") || strings.Contains(errMsg, "already set") {
            return nil, err // repo already returns clear messages
        }
        // Check for duplicate key error from the unique index
        if mongo.IsDuplicateKeyError(err) {
            return nil, errors.New("username is already taken")
        }
        return nil, err
    }
    return user, nil
}
```

Add `"go.mongodb.org/mongo-driver/v2/mongo"` to imports if not already present.

---

## HIGH PRIORITY FIXES

---

### 7. Delete All Dead Code

**Delete these entire files/directories:**

```
pkg/ratelimit/ratelimit.go      — In-memory rate limiter (uses Gin, not Fiber)
pkg/ratelimit/middleware.go      — Gin middleware for rate limiting (dead code)
pkg/token/token.go               — JWT generation/validation (unused, you use Firebase Auth)
pkg/pagination/pagination.go     — Offset-based pagination (unused, you use cursor-based)
```

If these packages have their own directories, delete the entire directories.

After deletion, search the entire codebase for any imports of these packages and remove them:

```
grep -r "gotodo/pkg/ratelimit" --include="*.go"
grep -r "gotodo/pkg/token" --include="*.go"
grep -r "gotodo/pkg/pagination" --include="*.go"
```

---

### 8. Add Pagination to GetUserRooms (Remove Unlimited Query)

**Problem**: `chat.repository.GetUserRooms` fetches ALL rooms for a user with no limit. A power user with 500 rooms triggers 500+ user lookups.

**File**: `internal/features/chat/repository.go`

Either delete `GetUserRooms` and use only `GetUserRoomsWithSearch`, or add a default limit:

```go
func (r *repository) GetUserRooms(ctx context.Context, userID bson.ObjectID) ([]models.Room, error) {
    filter := bson.M{"participants": userID}
    opts := options.Find().
        SetSort(bson.D{{Key: "lastUpdated", Value: -1}}).
        SetLimit(50)  // ADD THIS — never unbounded

    cursor, err := r.rooms.Find(ctx, filter, opts)
    // ... rest unchanged ...
}
```

**File**: `internal/features/chat/handler.go`

Update `GetUserRooms` handler to always use the paginated version:

```go
func (h *Handler) GetUserRooms(c *fiber.Ctx) error {
    user, ok := c.Locals("user").(*models.User)
    if !ok {
        return response.Unauthorized(c, "Unauthorized")
    }

    searchQuery := c.Query("q", "")
    limit := c.QueryInt("limit", 20)
    offset := c.QueryInt("offset", 0)

    if limit > 50 { limit = 50 }
    if limit <= 0 { limit = 20 }
    if offset < 0 { offset = 0 }

    // Always use paginated version
    rooms, totalCount, hasMore, err := h.service.GetUserRoomsWithSearch(c.Context(), user.ID.Hex(), searchQuery, limit, offset)
    if err != nil {
        return response.InternalError(c, err.Error())
    }

    return response.OK(c, "Rooms retrieved", fiber.Map{
        "rooms":      rooms,
        "totalCount": totalCount,
        "hasMore":    hasMore,
        "limit":      limit,
        "offset":     offset,
    })
}
```

---

### 9. Fix Explore Feed Pagination

**Problem**: The explore feed sorts by computed `engagementScore` (an aggregation-computed field) but uses `_id`-based cursor (`beforeID`) for pagination. Since `_id` order != score order, pages will have inconsistent results.

**File**: `internal/features/feed/repository.go` — `GetExploreFeed`

Change to offset/skip pagination for scored feeds:

```go
func (r *repository) GetExploreFeed(ctx context.Context, hashtag string, limit int, skip int) ([]models.Poem, error) {
    matchFilter := bson.M{
        "visibility": models.PoemVisibilityPublic,
        "isDeleted":  false,
    }
    if hashtag != "" {
        matchFilter["hashtags"] = hashtag
    }

    pipeline := mongo.Pipeline{
        {{Key: "$match", Value: matchFilter}},
        // ... existing $addFields for engagementScore ...
        {{Key: "$sort", Value: bson.D{
            {Key: "engagementScore", Value: -1},
            {Key: "_id", Value: -1},
        }}},
        {{Key: "$skip", Value: int64(skip)}},   // USE skip instead of _id cursor
        {{Key: "$limit", Value: int64(limit)}},
    }
    // ... rest unchanged ...
}
```

**File**: `internal/features/feed/repository.go` — update the `Repository` interface:

```go
type Repository interface {
    GetHomeFeed(ctx context.Context, authorIDs []bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Poem, error)
    GetExploreFeed(ctx context.Context, hashtag string, limit int, skip int) ([]models.Poem, error)  // CHANGED: beforeID → skip
    GetAudioFeed(ctx context.Context, limit int, skip int) ([]models.Poem, error)                    // CHANGED: same
    // ... rest unchanged ...
}
```

**File**: `internal/features/feed/service.go` — update callers:

```go
func (s *service) GetExploreFeed(ctx context.Context, callerIDStr string, hashtag string, limit int, before string) (*models.FeedPage, error) {
    if limit <= 0 { limit = 20 }
    if limit > 50 { limit = 50 }

    // Parse offset from "before" param (repurpose as offset for scored feeds)
    skip := 0
    if before != "" {
        // For explore feed, "before" is now an offset number, not an ObjectID
        if n, err := strconv.Atoi(before); err == nil {
            skip = n
        }
    }

    poemDocs, err := s.repo.GetExploreFeed(ctx, hashtag, limit+1, skip)
    // ... rest of the method stays the same ...
}
```

Add `"strconv"` to imports.

**File**: `internal/features/feed/handler.go` — update explore handler to use offset:

```go
func (h *Handler) GetExploreFeed(c *fiber.Ctx) error {
    // ... existing code ...
    limit := c.QueryInt("limit", 20)
    offset := c.QueryInt("offset", 0)  // USE offset instead of before cursor
    hashtag := c.Query("hashtag")

    page, err := h.service.GetExploreFeed(c.Context(), callerID, hashtag, limit, strconv.Itoa(offset))
    // ... rest unchanged ...
}
```

Do the same for `GetAudioFeed` (same scoring approach).

---

### 10. Add Graceful Shutdown to `main.go`

**File**: `main.go`

Replace the simple `app.Listen` call with signal-aware shutdown:

```go
import (
    "os"
    "os/signal"
    "syscall"
    "time"
)

// At the end of main(), replace:
//   if err := app.Listen(":" + cfg.Port); err != nil { ... }
// With:

// Start server in a goroutine
go func() {
    if err := app.Listen(":" + cfg.Port); err != nil {
        log.Fatalf("Server error: %v", err)
    }
}()

log.Printf("Server started on port %s", cfg.Port)

// Wait for interrupt signal
quit := make(chan os.Signal, 1)
signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
<-quit

log.Println("Shutting down server...")

// Shutdown with timeout
shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
defer shutdownCancel()

if err := app.Shutdown(); err != nil {
    log.Printf("Server shutdown error: %v", err)
}

if err := db.Disconnect(shutdownCtx); err != nil {
    log.Printf("Database disconnect error: %v", err)
}

log.Println("Server stopped gracefully")
```

---

### 11. Add Fiber Timeouts

**File**: `main.go`

```go
// BEFORE
app := fiber.New(fiber.Config{
    AppName: "Chat API v1.0",
})

// AFTER
app := fiber.New(fiber.Config{
    AppName:      "Chat API v1.0",
    ReadTimeout:  30 * time.Second,
    WriteTimeout: 30 * time.Second,
    IdleTimeout:  120 * time.Second,
})
```

Add `"time"` to imports if not already present.

---

### 12. Share Firebase App Instance

**Problem**: Firebase App is initialized twice — once in `middleware.NewAuthMiddleware` and once in `notifications.NewFirebaseFCM`. Each creates a separate `firebase.App`. This wastes resources and can cause issues.

**File**: `main.go`

Create the Firebase App once and pass it:

```go
import (
    firebase "firebase.google.com/go/v4"
    "google.golang.org/api/option"
)

// After loading config, before creating middleware:
var firebaseApp *firebase.App
{
    ctx := context.Background()
    var opts []option.ClientOption
    if cfg.FirebaseCredsPath != "" {
        opts = append(opts, option.WithCredentialsFile(cfg.FirebaseCredsPath))
    }
    fbConfig := &firebase.Config{ProjectID: cfg.FirebaseProjectID}
    app, err := firebase.NewApp(ctx, fbConfig, opts...)
    if err != nil {
        log.Printf("Warning: Firebase not initialized: %v", err)
    } else {
        firebaseApp = app
    }
}
```

Then modify `NewAuthMiddleware` and `NewFirebaseFCM` to accept `*firebase.App` instead of credential paths.

**File**: `internal/middleware/auth.go`

```go
func NewAuthMiddleware(app *firebase.App, userService users.Service) (*AuthMiddleware, error) {
    if app == nil {
        return nil, errors.New("firebase app is nil")
    }
    return &AuthMiddleware{App: app, userService: userService}, nil
}
```

**File**: `internal/features/notifications/fcm.go`

```go
func NewFirebaseFCM(app *firebase.App) *FirebaseFCM {
    if app == nil {
        log.Println("FCM: No Firebase app, push notifications disabled")
        return nil
    }
    client, err := app.Messaging(context.Background())
    if err != nil {
        log.Printf("FCM: Failed to init messaging client: %v", err)
        return nil
    }
    log.Println("FCM: Push notifications enabled")
    return &FirebaseFCM{client: client}
}
```

Update `main.go` callers:

```go
authMiddleware, err := middleware.NewAuthMiddleware(firebaseApp, userService)
fcmSender := notifications.NewFirebaseFCM(firebaseApp)
```

---

## MEDIUM PRIORITY FIXES

---

### 13. Batch Hashtag Operations

**File**: `internal/features/poems/repository.go`

Replace the loop in `UpsertHashtags` with `BulkWrite`:

```go
func (r *repository) UpsertHashtags(ctx context.Context, tags []string) error {
    if len(tags) == 0 {
        return nil
    }

    models := make([]mongo.WriteModel, 0, len(tags))
    now := time.Now()
    for _, tag := range tags {
        if tag == "" {
            continue
        }
        models = append(models, mongo.NewUpdateOneModel().
            SetFilter(bson.M{"tag": tag}).
            SetUpdate(bson.M{
                "$inc":         bson.M{"usageCount": 1},
                "$set":         bson.M{"updatedAt": now},
                "$setOnInsert": bson.M{"tag": tag},
            }).
            SetUpsert(true),
        )
    }

    if len(models) == 0 {
        return nil
    }

    _, err := r.hashtags.BulkWrite(ctx, models)
    return err
}
```

Same pattern for `DecrementHashtags`:

```go
func (r *repository) DecrementHashtags(ctx context.Context, tags []string) error {
    if len(tags) == 0 {
        return nil
    }

    models := make([]mongo.WriteModel, 0, len(tags))
    now := time.Now()
    for _, tag := range tags {
        if tag == "" {
            continue
        }
        models = append(models, mongo.NewUpdateOneModel().
            SetFilter(bson.M{"tag": tag, "usageCount": bson.M{"$gt": 0}}).
            SetUpdate(bson.M{
                "$inc": bson.M{"usageCount": -1},
                "$set": bson.M{"updatedAt": now},
            }),
        )
    }

    if len(models) == 0 {
        return nil
    }

    _, err := r.hashtags.BulkWrite(ctx, models)
    return err
}
```

---

### 14. Fix Validator Regex Compilation

**File**: `pkg/validator/validator.go`

Move the regex out of the function body to a package-level var:

```go
// At the top with the other package-level regexes:
var (
    emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
    phoneRegex    = regexp.MustCompile(`^\+?[1-9]\d{1,14}$`)
    usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,20}$`)
    urlRegex      = regexp.MustCompile(`^https?:\/\/(www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b([-a-zA-Z0-9()@:%_\+.~#?&//=]*)$`)
    nameRegex     = regexp.MustCompile(`^[a-zA-Z\s\-'\.]+$`)  // ADD THIS
)

func IsValidName(name string) bool {
    if strings.TrimSpace(name) == "" {
        return false
    }
    // Use the package-level nameRegex instead of compiling a new one
    return nameRegex.MatchString(name) && len(name) >= 2
}
```

---

### 15. Fix AddFCMToken Unnecessary Query

**File**: `internal/features/users/repository.go`

The first `UpdateOne` that initializes `fcmTokens` to an empty array is unnecessary because MongoDB's `$addToSet` operator creates the array if it doesn't exist.

```go
// BEFORE
func (r *repository) AddFCMToken(ctx context.Context, userID bson.ObjectID, token string) error {
    _, _ = r.collection.UpdateOne(ctx,
        bson.M{"_id": userID, "fcmTokens": nil},
        bson.M{"$set": bson.M{"fcmTokens": []string{}}},
    )
    _, err := r.collection.UpdateOne(ctx,
        bson.M{"_id": userID},
        bson.M{"$addToSet": bson.M{"fcmTokens": token}},
    )
    return err
}

// AFTER
func (r *repository) AddFCMToken(ctx context.Context, userID bson.ObjectID, token string) error {
    _, err := r.collection.UpdateOne(ctx,
        bson.M{"_id": userID},
        bson.M{"$addToSet": bson.M{"fcmTokens": token}},
    )
    return err
}
```

---

### 16. Add Context Timeouts to All Background Goroutines

Search for `go func()` across the codebase. Every background goroutine that performs database operations must use `context.WithTimeout`.

**Pattern to apply everywhere:**

```go
// BEFORE (found in multiple places)
go func() {
    _ = s.repo.UpsertHashtags(context.Background(), hashtags)
}()

// AFTER
go func() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    if err := s.repo.UpsertHashtags(ctx, hashtags); err != nil {
        log.Printf("Failed to upsert hashtags: %v", err)
    }
}()
```

**Files to check (non-exhaustive — search for all `go func()` and `context.Background()`):**

- `internal/features/poems/service.go` — hashtag upsert, hashtag decrement, posts count increment/decrement
- `internal/features/follows/service.go` — notification send (already has timeout after our fix)
- `internal/features/users/service.go` — `broadcastProfileUpdate` (already has 5s timeout)
- `internal/features/chat/service.go` — presence broadcast, notification sends

---

### 17. Apply Rate Limiters in Routes

**Problem**: `middleware/rate_limiter.go` defines `StrictRateLimiter()` (20 req/min for writes) and `GenerousRateLimiter()` (300 req/min for reads), but they are never used in `routes.go`.

**File**: `internal/routes/routes.go`

Apply rate limiters to route groups:

```go
func SetupRoutes(
    app *fiber.App,
    authMiddleware *middleware.AuthMiddleware,
    // ... existing params ...
) {
    api := app.Group("/api/v1")

    // Apply generous rate limiter to all API routes as a baseline
    api.Use(middleware.RateLimiterConfig())

    // ... existing route setup ...

    // For write-heavy endpoints, add strict limiter:
    // Example: social interactions
    api.Post("/poems/:id/like", authMiddleware.Protect(), middleware.StrictRateLimiter(), socialHandler.TogglePoemLike)
    api.Post("/poems/:id/comments", authMiddleware.Protect(), middleware.StrictRateLimiter(), socialHandler.AddComment)
    api.Post("/comments/:id/like", authMiddleware.Protect(), middleware.StrictRateLimiter(), socialHandler.ToggleCommentLike)
    api.Post("/poems/:id/repost", authMiddleware.Protect(), middleware.StrictRateLimiter(), socialHandler.ToggleRepost)
    api.Post("/users/:id/follow", authMiddleware.Protect(), middleware.StrictRateLimiter(), followHandler.ToggleFollow)

    // For read-heavy endpoints, use generous limiter (already the baseline, but can be explicit):
    api.Get("/feed", authMiddleware.Protect(), middleware.GenerousRateLimiter(), feedHandler.GetHomeFeed)
    api.Get("/feed/explore", authMiddleware.OptionalAuth(), middleware.GenerousRateLimiter(), feedHandler.GetExploreFeed)
    // ... etc ...
}
```

---

### 18. Add MongoDB Health Check

**File**: `internal/routes/routes.go` (or wherever the health endpoint is)

Pass the database client to routes and update the health check:

```go
api.Get("/health", func(c *fiber.Ctx) error {
    ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
    defer cancel()
    if err := db.Client.Ping(ctx, nil); err != nil {
        return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
            "status": "unhealthy",
            "error":  "database unreachable",
        })
    }
    return c.JSON(fiber.Map{"status": "ok"})
})
```

You'll need to pass `db` (the `*database.MongoDB` instance) to `SetupRoutes` or define the health endpoint in `main.go` directly.

---

## LOW PRIORITY FIXES

---

### 19. Reduce WebSocket Logging

**File**: `internal/features/chat/service.go`

Gate verbose logs behind a condition or remove them:

```go
// REMOVE or comment out these log lines:
// log.Printf("[WS] Pong received from user %s", userID)
// log.Printf("[WS] Received message #%d from user %s: type=%s", ...)
// log.Printf("[NOTIF DEBUG] User %s isOnline=%v ...")

// KEEP these (important for debugging):
// log.Printf("[WS] New WebSocket connection for user %s ...")
// log.Printf("[WS CLOSE] ...")
// log.Printf("[WS ERROR] ...")
```

**File**: `internal/features/chat/hub.go`

```go
// REMOVE or gate behind a debug flag:
// log.Printf("[HUB] User %s registered (connections: %d, wasOffline: %v)", ...)
// log.Printf("User %s grace period cancelled", ...)
```

---

### 20. Add Missing Response Helper Methods

**File**: `pkg/response/response.go`

The codebase calls `response.OK`, `response.Created`, `response.BadRequest`, `response.Unauthorized`, `response.NotFound`, `response.Conflict`, `response.ValidationFailed`, and `response.InternalError`, but the file only defines `SendError`, `SendSuccess`, `SendData`, `SendCreated`, and `SendValidationError`.

Add the missing helpers:

```go
func OK(c *fiber.Ctx, message string, data interface{}) error {
    return c.JSON(SuccessResponse{Message: message, Data: data})
}

func Created(c *fiber.Ctx, message string, data interface{}) error {
    return c.Status(fiber.StatusCreated).JSON(SuccessResponse{Message: message, Data: data})
}

func BadRequest(c *fiber.Ctx, message string) error {
    return c.Status(fiber.StatusBadRequest).JSON(ErrorResponse{Error: "bad_request", Message: message})
}

func Unauthorized(c *fiber.Ctx, message string) error {
    return c.Status(fiber.StatusUnauthorized).JSON(ErrorResponse{Error: "unauthorized", Message: message})
}

func NotFound(c *fiber.Ctx, message string) error {
    return c.Status(fiber.StatusNotFound).JSON(ErrorResponse{Error: "not_found", Message: message})
}

func Conflict(c *fiber.Ctx, message string) error {
    return c.Status(fiber.StatusConflict).JSON(ErrorResponse{Error: "conflict", Message: message})
}

func ValidationFailed(c *fiber.Ctx, message string) error {
    return c.Status(fiber.StatusUnprocessableEntity).JSON(ErrorResponse{Error: "validation_failed", Message: message})
}

func InternalError(c *fiber.Ctx, message string) error {
    return c.Status(fiber.StatusInternalServerError).JSON(ErrorResponse{Error: "internal_error", Message: message})
}
```

---

## FINAL VERIFICATION CHECKLIST

After completing all fixes:

1. **Compile check**: `go build ./...` — must pass with zero errors
2. **Import cycles**: `go vet ./...` — check for circular imports (especially after adding `*mongo.Client` to follows service)
3. **Search for swallowed errors**: `grep -rn "_ =" --include="*.go" internal/` — every result should either be intentional (like a type assertion) or changed to log the error
4. **Search for `context.Background()` in goroutines**: `grep -rn "context.Background()" --include="*.go" internal/` — every instance inside a `go func()` should have a timeout
5. **Search for remaining dead code**: `grep -rn "gin\." --include="*.go"` — should return zero results after deleting pkg/ratelimit
6. **Verify no duplicate firebase init**: `grep -rn "firebase.NewApp" --include="*.go"` — should only appear once (in main.go)
