# ChatBee v1 — Backend Implementation Spec

*March 22, 2026*

---

## What's changing

Three new fields on the Poem: `description`, `textAlign`, `mentions`. The backend parses @usernames from the description, resolves them to user IDs, stores them, and sends mention notifications. Also adds word-count validation (150 max) and description length validation (200 chars max).

---

## Current state (what exists)

**Poem model** (`internal/models/poem.go`):
- Has: title, contentJson, plainText, hashtags, mood, isOriginal, visibility, audioUrl, audioDuration, coverColor, likesCount, commentsCount, repostsCount, isRepost, originalId, repostNote, isDeleted, createdAt, updatedAt
- Missing: description, textAlign, mentions

**PoemResponse** (`internal/models/poem.go`):
- Has: all above + author (PoemAuthor), isLikedByMe, isRepostedByMe, originalPoem
- Missing: description, textAlign, mentions (populated)

**Poem service** (`internal/features/poems/service.go`):
- Wired with: `poemRepo`, `userRepo`, `socialRepo`
- `userRepo` gives access to user lookups (needed for resolving @usernames)

**Notification constants** (`internal/models/notification.go`):
- Has: `NotifTypeConnectionRequest`, `NotifTypeConnectionAccepted`, `NotifTypeNewMessage`
- Missing: `ResourceTypePoem`

**Social notification constants** (`internal/models/social.go`):
- Already has: `NotifTypeMentioned = "mentioned"` (unused so far)
- Can reuse this for mention notifications

**User model** (`internal/models/user.go`):
- Has `Username` field with unique sparse index on the `users` collection

**Username regex** (`pkg/validator/validator.go`):
- `usernameRegex = regexp.MustCompile("^[a-zA-Z0-9_-]{3,20}$")`
- Usernames are 3-20 chars, alphanumeric + underscore + hyphen

**Poem service does NOT currently have access to notification service.** The wiring in `main.go` is:
```go
poemService := poems.NewService(poemRepo, userRepo, socialRepo)
```
This needs to change to include `notifService`.

---

## Changes by file

### 1. `internal/models/poem.go` — Add 3 fields

**Poem struct** — add after `CoverColor`:

```go
Description string           `bson:"description,omitempty" json:"description,omitempty"`
TextAlign   string           `bson:"textAlign,omitempty"   json:"textAlign,omitempty"`   // "left" | "center" | "right"
Mentions    []bson.ObjectID  `bson:"mentions,omitempty"    json:"mentions,omitempty"`    // user IDs parsed from description @mentions
```

**PoemResponse struct** — add after `CoverColor`:

```go
Description string         `json:"description,omitempty"`
TextAlign   string         `json:"textAlign,omitempty"`
Mentions    []MentionedUser `json:"mentions"`
```

**Add new struct** (after PoemAuthor):

```go
// MentionedUser — embedded mention info in poem response
// so the frontend never needs a separate user lookup for mentions
type MentionedUser struct {
    ID          string `json:"id"`
    Username    string `json:"username"`
    DisplayName string `json:"displayName"`
    PhotoURL    string `json:"photoURL"`
}
```

**Why `Mentions` is `[]bson.ObjectID` on the DB model but `[]MentionedUser` on the response**: The DB stores just IDs (compact, no denormalization drift). The response populates full user info at read time, same pattern as `Author`/`PoemAuthor`.

---

### 2. `internal/models/notification.go` — Add ResourceTypePoem

```go
const (
    ResourceTypeConnection = "connection"
    ResourceTypeChatRoom   = "chat_room"
    ResourceTypePoem       = "poem"        // ← ADD THIS
)
```

`NotifTypeMentioned` already exists in `social.go` as `"mentioned"` — no new type constant needed. However, note that `social.go` is in `package models`, so you reference it as `models.NotifTypeMentioned`.

---

### 3. `internal/features/poems/service.go` — Core logic changes

#### 3a. Add `notifService` dependency

Current constructor signature:
```go
func NewService(poemRepo Repository, userRepo users.Repository, socialRepo social.Repository) Service
```

Change to:
```go
func NewService(poemRepo Repository, userRepo users.Repository, socialRepo social.Repository, notifService notifications.Service) Service
```

Store `notifService` on the service struct.

#### 3b. Add mention extraction helper

Add this function to the service (or as a package-level helper):

```go
import "regexp"

var mentionRegex = regexp.MustCompile(`@([a-zA-Z0-9_-]{3,20})`)

// extractMentionedUsernames parses @username patterns from text.
// Returns deduplicated, lowercased usernames.
func extractMentionedUsernames(text string) []string {
    matches := mentionRegex.FindAllStringSubmatch(text, -1)
    seen := make(map[string]bool)
    usernames := make([]string, 0, len(matches))
    for _, m := range matches {
        username := strings.ToLower(m[1])
        if !seen[username] {
            seen[username] = true
            usernames = append(usernames, username)
        }
    }
    return usernames
}
```

The regex `@([a-zA-Z0-9_-]{3,20})` matches the same pattern as the existing `usernameRegex` in `validator.go`. This means it won't false-positive on email addresses like `user@domain.com` because domain parts contain dots (which aren't in the character class). It will correctly match `@asif`, `@writers_network`, `@lockhart-red`.

#### 3c. Add mention resolution helper

```go
// resolveMentions takes a description string, extracts @usernames,
// looks up each in the DB, and returns the resolved ObjectIDs.
// Non-existent usernames are silently skipped.
func (s *service) resolveMentions(ctx context.Context, description string) []bson.ObjectID {
    usernames := extractMentionedUsernames(description)
    if len(usernames) == 0 {
        return nil
    }

    mentionIDs := make([]bson.ObjectID, 0, len(usernames))
    for _, username := range usernames {
        user, err := s.userRepo.FindByUsername(ctx, username)
        if err != nil {
            continue // skip non-existent users
        }
        mentionIDs = append(mentionIDs, user.ID)
    }
    return mentionIDs
}
```

**This requires `userRepo.FindByUsername(ctx, username)`** — see section 5 below.

#### 3d. Add mention notification helper

```go
// sendMentionNotifications sends a notification to each newly mentioned user.
// oldMentions is nil for new poems, or the previous mentions list for updates.
// IMPORTANT: This is called with `go s.sendMentionNotifications(...)` from the caller,
// so it already runs in a goroutine. Do NOT spawn additional goroutines inside.
func (s *service) sendMentionNotifications(author *models.User, poem *models.Poem, oldMentions []bson.ObjectID) {
    ctx := context.Background() // fresh context — the request context may be cancelled
    
    oldSet := make(map[bson.ObjectID]bool)
    for _, id := range oldMentions {
        oldSet[id] = true
    }

    for _, id := range poem.Mentions {
        // Skip if already mentioned before (on update)
        if oldSet[id] {
            continue
        }
        // Don't notify yourself
        if id == author.ID {
            continue
        }

        s.notifService.Send(ctx, models.SendNotificationRequest{
            RecipientID:  id,
            ActorID:      author.ID,
            Type:         models.NotifTypeMentioned,
            ResourceType: models.ResourceTypePoem,
            ResourceID:   poem.ID.Hex(),
            Title:        author.DisplayName + " mentioned you",
            Body:         `in "` + poem.Title + `"`,
            GroupKey:     "mention:" + poem.ID.Hex(),
        })
    }
}
```

#### 3e. Update CreatePoem

In the CreatePoem method, after validating the request and before inserting:

```go
// Validate word count
words := strings.Fields(strings.TrimSpace(req.PlainText))
if len(words) > 150 {
    return nil, errors.New("poem body exceeds 150 word limit")
}

// Validate textAlign
if req.TextAlign != "" && req.TextAlign != "left" && req.TextAlign != "center" && req.TextAlign != "right" {
    return nil, errors.New("textAlign must be left, center, or right")
}

// Validate description length
if len([]rune(req.Description)) > 200 {
    return nil, errors.New("description exceeds 200 character limit")
}

// Resolve @mentions from description
mentionIDs := s.resolveMentions(ctx, req.Description)

// Build the poem document
poem := &models.Poem{
    // ... existing fields ...
    Description: req.Description,
    TextAlign:   req.TextAlign,
    Mentions:    mentionIDs,
}
```

After successful insert:

```go
// Send mention notifications (async, don't block response)
go s.sendMentionNotifications(author, poem, nil)
```

#### 3f. Update UpdatePoem

Same validations as create. Additionally, diff old vs new mentions:

```go
// Capture old mentions before update
oldMentions := existingPoem.Mentions

// Resolve new mentions
newMentionIDs := s.resolveMentions(ctx, req.Description)

// Update the poem
existingPoem.Description = req.Description
existingPoem.TextAlign = req.TextAlign
existingPoem.Mentions = newMentionIDs
```

After successful update:

```go
// Send notifications only to newly mentioned users
go s.sendMentionNotifications(author, existingPoem, oldMentions)
```

---

### 4. `internal/features/poems/handler.go` — Accept new fields

Update the create/update request struct (whatever it's called in your handler):

```go
type createPoemRequest struct {
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
    Description   string   `json:"description"`   // ← NEW
    TextAlign     string   `json:"textAlign"`      // ← NEW
}
```

No other handler changes — the handler just passes these to the service. All validation and mention logic lives in the service.

---

### 5. `internal/features/users/repository.go` — Add FindByUsername

Check if this method already exists. If not, add:

```go
func (r *repository) FindByUsername(ctx context.Context, username string) (*models.User, error) {
    var user models.User
    // Case-insensitive lookup using regex
    filter := bson.M{
        "username": bson.M{
            "$regex":   "^" + regexp.QuoteMeta(username) + "$",
            "$options": "i",
        },
    }
    err := r.collection.FindOne(ctx, filter).Decode(&user)
    if err != nil {
        return nil, err
    }
    return &user, nil
}
```

Also add to the `Repository` interface if it's defined as an interface:

```go
type Repository interface {
    // ... existing methods ...
    FindByUsername(ctx context.Context, username string) (*models.User, error)
}
```

**Index**: The `username` field already has a unique sparse index (in `indexes.go`). The regex query with `^...$` anchors will use this index efficiently with case-insensitive option.

---

### 6. `internal/features/poems/service.go` or wherever `buildPoemResponse` lives — Populate mentions

In the function that converts `Poem` → `PoemResponse`, add description/textAlign/mentions population:

```go
// Always set description and textAlign
resp.Description = poem.Description
resp.TextAlign = poem.TextAlign

// Initialize mentions to empty slice (never nil — avoids JSON null)
resp.Mentions = []models.MentionedUser{}

// Populate if there are mentions
if len(poem.Mentions) > 0 {
    for _, uid := range poem.Mentions {
        user, err := s.userRepo.FindByID(ctx, uid)
        if err != nil {
            continue // skip deleted/invalid users
        }
        resp.Mentions = append(resp.Mentions, models.MentionedUser{
            ID:          user.ID.Hex(),
            Username:    user.Username,
            DisplayName: user.DisplayName,
            PhotoURL:    user.PhotoURL,
        })
    }
}
```

**Performance note**: For v1 this does N individual user lookups where N = number of mentions per poem (typically 0-3). This is fine. For v2, if needed, batch with `$in`:

```go
// v2 optimization: batch lookup
users, _ := s.userRepo.FindByIDs(ctx, poem.Mentions)
```

---

### 7. `main.go` — Update poem service wiring

Change:
```go
poemService := poems.NewService(poemRepo, userRepo, socialRepo)
```

To:
```go
poemService := poems.NewService(poemRepo, userRepo, socialRepo, notifService)
```

---

### 8. No index changes needed

The `mentions` field on poems is only written on create/update and read per-poem. It's never queried across poems (no "show all poems mentioning me" feature in v1). No new index needed.

If you add that feature in v2, add:
```go
{Keys: bson.D{{Key: "mentions", Value: 1}, {Key: "_id", Value: -1}}}
```

---

## Feed and all poem endpoints — automatically covered

Every endpoint that returns poems uses `PoemResponse` and goes through `buildPoemResponse`:

| Endpoint | Response type | Affected? |
|----------|--------------|-----------|
| `GET /poems/:id` | `PoemResponse` | Yes — single poem |
| `GET /poems/me` | `PoemsPage { poems: []PoemResponse }` | Yes — my poems |
| `GET /poems/user/:userId` | `PoemsPage { poems: []PoemResponse }` | Yes — user's poems |
| `POST /poems/` | `PoemResponse` | Yes — create returns the poem |
| `PATCH /poems/:id` | `PoemResponse` | Yes — update returns the poem |
| `GET /feed/home` | `FeedPage { poems: []PoemResponse }` | Yes — home feed |
| `GET /feed/explore` | `FeedPage { poems: []PoemResponse }` | Yes — explore feed |
| `GET /search/poems` | `PoemSearchPage { poems: []PoemResponse }` | Yes — search |

Since all of these go through the same `buildPoemResponse` function, adding description/textAlign/mentions population there covers **every endpoint automatically**. No per-endpoint changes needed.

**Repost handling**: When a poem is a repost, `OriginalPoem *PoemResponse` is populated by calling `buildPoemResponse` recursively on the original poem. This means the original poem's description/mentions are also populated correctly.

---

## JSON null vs empty array — important

**Problem**: In Go, `[]MentionedUser(nil)` serializes to `null` in JSON, not `[]`. The Flutter frontend expects `[]` (empty list), not `null`, otherwise `json_serializable` may crash or return null instead of an empty list.

**Fix in `buildPoemResponse`**: Always initialize to empty slice, not nil:

```go
// Initialize mentions to empty slice (never nil — avoids JSON null)
resp.Mentions = []models.MentionedUser{}

// Then populate if there are mentions
if len(poem.Mentions) > 0 {
    for _, uid := range poem.Mentions {
        user, err := s.userRepo.FindByID(ctx, uid)
        if err != nil {
            continue
        }
        resp.Mentions = append(resp.Mentions, models.MentionedUser{
            ID:          user.ID.Hex(),
            Username:    user.Username,
            DisplayName: user.DisplayName,
            PhotoURL:    user.PhotoURL,
        })
    }
}
```

Same pattern as how you likely handle `Hashtags` — `[]string{}` not `nil`.

---

## Request/response shape changes

### Create/Update request body (what frontend sends)

Before:
```json
{
  "title": "Celestial Heart",
  "contentJson": "[{\"insert\":\"Two stars...\"}]",
  "plainText": "Two stars decided to align...",
  "hashtags": ["love", "spiritual"],
  "mood": "love",
  "isOriginal": true,
  "visibility": "public",
  "audioUrl": "",
  "audioDuration": 0,
  "coverColor": "#7F77DD"
}
```

After (two new fields):
```json
{
  "title": "Celestial Heart",
  "contentJson": "[{\"insert\":\"Two stars...\"}]",
  "plainText": "Two stars decided to align...",
  "hashtags": ["love", "spiritual"],
  "mood": "love",
  "isOriginal": true,
  "visibility": "public",
  "audioUrl": "",
  "audioDuration": 0,
  "coverColor": "#7F77DD",
  "description": "Thanks for the support @writersnetwork and special thanks to @god",
  "textAlign": "center"
}
```

### Poem response body (what frontend receives)

New fields added to every poem response:
```json
{
  "id": "...",
  "author": { "id": "...", "displayName": "bobbycneis", "username": "bobbycneis", "photoURL": "..." },
  "title": "Celestial Heart",
  "contentJson": "...",
  "plainText": "...",
  "description": "Thanks for the support @writersnetwork and special thanks to @god",
  "textAlign": "center",
  "mentions": [
    { "id": "abc123", "username": "writersnetwork", "displayName": "Writers Network", "photoURL": "..." },
    { "id": "def456", "username": "god", "displayName": "Asif", "photoURL": "..." }
  ],
  "hashtags": ["love", "spiritual"],
  "isOriginal": true,
  "...": "rest unchanged"
}
```

For poems with no description/mentions, these fields are empty/null:
```json
{
  "description": "",
  "textAlign": "",
  "mentions": []
}
```

---

## Validation summary

| Field | Rule | Error message |
|-------|------|---------------|
| `plainText` | `len(strings.Fields(text)) <= 150` | "poem body exceeds 150 word limit" |
| `textAlign` | Must be "" or "left" or "center" or "right" | "textAlign must be left, center, or right" |
| `description` | `len([]rune(text)) <= 200` | "description exceeds 200 character limit" |
| `@mentions` | Non-existent usernames silently skipped | No error — just not included in mentions array |

**Note**: Use `[]rune` for description length to count characters correctly for unicode/emoji.

---

## Edge cases to handle

1. **User changes username after being mentioned**: The description stores plain text with the old `@username`. On next read, `buildPoemResponse` resolves mention IDs (which haven't changed) to current usernames. The card will show the correct current username from the `mentions` array. The description text itself still shows the old `@username` — this is acceptable for v1. Fix in v2 by storing mention metadata separately.

2. **Mentioned user deletes account**: `buildPoemResponse` skips deleted users in the mentions array (the `FindByID` will fail). The description text still shows `@deleteduser` but it won't be tappable since it's not in the resolved mentions list.

3. **Self-mention**: If a user mentions themselves (`@myown`), the ID is stored in mentions but no notification is sent (filtered in `sendMentionNotifications`).

4. **Duplicate mentions**: If description contains `@asif` twice, `extractMentionedUsernames` deduplicates. Only one ID stored, one notification sent.

5. **Edit removes a mention**: Old user loses their mention but does NOT get a "you were unmentioned" notification. This is standard behavior (Instagram, Twitter don't notify on mention removal either).

---

## Files changed summary

| File | What changes |
|------|-------------|
| `internal/models/poem.go` | Add Description, TextAlign, Mentions to Poem struct. Add Description, TextAlign, Mentions (as `[]MentionedUser`) to PoemResponse. Add MentionedUser struct. |
| `internal/models/notification.go` | Add `ResourceTypePoem = "poem"` constant |
| `internal/features/poems/service.go` | Add notifService dep to constructor + struct. Add `extractMentionedUsernames`, `resolveMentions`, `sendMentionNotifications` helpers. Add validation (word count, textAlign, description length) in CreatePoem + UpdatePoem. Populate description/textAlign/mentions in `toResponse`. Add nil-check for Hashtags in `toResponse`. |
| `internal/features/poems/handler.go` | Add `Description string` + `TextAlign string` to create/update request struct |
| `internal/features/feed/service.go` | Add Description, TextAlign, Mentions population to `buildPoemResponse`. Add nil-check for Hashtags (already exists but verify). |
| `internal/features/users/repository.go` | `GetUserByUsername` already exists — no changes needed |
| `main.go` | Pass `notifService` to `poems.NewService(...)` |

**No changes needed:**

| File | Why |
|------|-----|
| `indexes.go` | No new indexes for v1 |
| `routes.go` | Same endpoints, just new fields in request/response body |
| `internal/models/social.go` | `NotifTypeMentioned` already defined |
| `internal/models/feed.go` | `FeedPage` uses `[]PoemResponse` — struct already updated |
| Poem handler | `CreatePoemRequest`/`UpdatePoemRequest` structs defined in service.go, handler auto-parses — no handler changes |