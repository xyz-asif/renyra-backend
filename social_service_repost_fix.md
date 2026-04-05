# Fix: Social package — GetUserReposts missing caller context

## Problem

`GetUserReposts` and `buildRepostResponse` in the social `service.go` never check
`isLikedByMe` / `isRepostedByMe` for the original poem inside reposts. The handler
doesn't even extract the caller's ID from auth. So when you visit someone's profile
and look at their reposts tab, your own liked poems show as un-liked.

## Changes needed (2 files in the social package)

### 1. handler.go — Extract callerID and pass it

```go
// GET /api/v1/users/:id/reposts?limit=20&before=<id>
func (h *Handler) GetUserReposts(c *fiber.Ctx) error {
	userID := c.Params("id")
	limit := c.QueryInt("limit", 20)
	if limit > 50 {
		limit = 50
	}
	before := c.Query("before")

	// FIX: Extract caller ID so we can check isLikedByMe on original poems
	callerID := ""
	if user, ok := c.Locals("user").(*models.User); ok {
		callerID = user.ID.Hex()
	}

	page, err := h.service.GetUserReposts(c.Context(), userID, callerID, limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Reposts retrieved", page)
}
```

### 2. service.go — Update interface, GetUserReposts, and buildRepostResponse

**Interface change:**
```go
// In the Service interface, change:
GetUserReposts(ctx context.Context, userIDStr string, limit int, before string) (*models.FeedPage, error)
// To:
GetUserReposts(ctx context.Context, userIDStr string, callerIDStr string, limit int, before string) (*models.FeedPage, error)
```

**GetUserReposts — add batch like/repost check:**
```go
func (s *service) GetUserReposts(ctx context.Context, userIDStr string, callerIDStr string, limit int, before string) (*models.FeedPage, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

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
	if hasMore {
		reposts = reposts[:limit]
	}

	// FIX: Batch check like/repost status for original poem IDs
	var likedMap map[string]bool
	var repostedMap map[string]bool
	if callerIDStr != "" {
		callerID, err := bson.ObjectIDFromHex(callerIDStr)
		if err == nil {
			// Collect original poem IDs
			var originalIDs []bson.ObjectID
			for _, rp := range reposts {
				if rp.OriginalID != nil {
					originalIDs = append(originalIDs, *rp.OriginalID)
				}
			}
			if len(originalIDs) > 0 {
				likedMap, _ = s.repo.IsPoemLikedMany(ctx, callerID, originalIDs)
				repostedMap, _ = s.repo.IsPoemRepostedMany(ctx, callerID, originalIDs)
			}
		}
	}
	if likedMap == nil {
		likedMap = make(map[string]bool)
	}
	if repostedMap == nil {
		repostedMap = make(map[string]bool)
	}

	responses := make([]models.PoemResponse, 0, len(reposts))
	for _, rp := range reposts {
		// FIX: Pass maps to buildRepostResponse
		resp := s.buildRepostResponse(ctx, &rp, likedMap, repostedMap)
		responses = append(responses, resp)
	}

	return &models.FeedPage{Poems: responses, HasMore: hasMore}, nil
}
```

**buildRepostResponse — accept and use the maps:**
```go
func (s *service) buildRepostResponse(ctx context.Context, rp *models.Poem, likedMap map[string]bool, repostedMap map[string]bool) models.PoemResponse {
	resp := models.PoemResponse{
		ID:        rp.ID.Hex(),
		IsRepost:  true,
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
			origIDHex := original.ID.Hex()
			originalResp := models.PoemResponse{
				ID:             origIDHex,
				Title:          original.Title,
				ContentJSON:    original.ContentJSON,
				PlainText:      original.PlainText,
				Hashtags:       original.Hashtags,
				Mood:           original.Mood,
				IsOriginal:     original.IsOriginal,
				Visibility:     original.Visibility,
				AudioURL:       original.AudioURL,
				AudioDuration:  original.AudioDuration,
				LikesCount:     original.LikesCount,
				CommentsCount:  original.CommentsCount,
				RepostsCount:   original.RepostsCount,
				// FIX: Look up caller's like/repost state from the batch maps
				IsLikedByMe:    likedMap[origIDHex],
				IsRepostedByMe: repostedMap[origIDHex],
				CreatedAt:      original.CreatedAt,
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

	// Ensure non-nil slices
	if resp.Hashtags == nil {
		resp.Hashtags = []string{}
	}
	resp.Mentions = []models.MentionedUser{}

	return resp
}
```

## Frontend change needed

The Flutter `social_repo.dart` `getUserReposts` method doesn't pass any caller context,
but it doesn't need to — the backend auth middleware already provides the caller from
the JWT token via `c.Locals("user")`. The handler just needs to read it (which the fix above does).

No Flutter changes needed for this fix.

## Also check: poems service GetUserPoems

The same pattern likely exists in the poems `service.go` for the `GetUserPoems` endpoint
(used on the "Poems" tab of profiles). Check if it also hardcodes `false` for
`isLikedByMe` on the original poems. If so, apply the same fix there.
