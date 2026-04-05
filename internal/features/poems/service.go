// package poems

// import (
// 	"context"
// 	"errors"
// 	"regexp"
// 	"strings"
// 	"time"

// 	"github.com/xyz-asif/gotodo/internal/features/notifications"
// 	"github.com/xyz-asif/gotodo/internal/features/social"
// 	"github.com/xyz-asif/gotodo/internal/features/users"
// 	"github.com/xyz-asif/gotodo/internal/models"
// 	"go.mongodb.org/mongo-driver/v2/bson"
// 	"go.mongodb.org/mongo-driver/v2/mongo"
// )

// type Service interface {
// 	Create(ctx context.Context, authorID string, req CreatePoemRequest) (*models.PoemResponse, error)
// 	GetByID(ctx context.Context, poemID string, callerID string) (*models.PoemResponse, error)
// 	Update(ctx context.Context, poemID string, authorID string, req UpdatePoemRequest) (*models.PoemResponse, error)
// 	Delete(ctx context.Context, poemID string, authorID string) error
// 	GetMyPoems(ctx context.Context, authorID string, limit int, before string) (*models.PoemsPage, error)
// 	GetUserPoems(ctx context.Context, targetUserID string, callerID string, limit int, before string) (*models.PoemsPage, error)
// }

// // CreatePoemRequest — body sent from the publish bottom sheet
// type CreatePoemRequest struct {
// 	Title         string   `json:"title"`
// 	ContentJSON   string   `json:"contentJson"`   // Quill Delta JSON
// 	PlainText     string   `json:"plainText"`     // stripped plain text
// 	Hashtags      []string `json:"hashtags"`      // combined: static chips + custom tags
// 	Mood          string   `json:"mood"`          // single mood from static chips
// 	IsOriginal    bool     `json:"isOriginal"`    // copyright checkbox
// 	Visibility    string   `json:"visibility"`    // "public" or "private"
// 	AudioURL      string   `json:"audioUrl"`      // Cloudinary URL, empty if no audio
// 	AudioDuration int      `json:"audioDuration"` // seconds
// 	CoverColor    string   `json:"coverColor"`    // hex from editor
// 	Description   string   `json:"description"`   // NEW: poem description with @mentions
// 	TextAlign     string   `json:"textAlign"`     // NEW: "left" | "center" | "right"
// }

// // UpdatePoemRequest — same fields, all optional (only changed fields need to be sent)
// type UpdatePoemRequest struct {
// 	Title         string   `json:"title"`
// 	ContentJSON   string   `json:"contentJson"`
// 	PlainText     string   `json:"plainText"`
// 	Hashtags      []string `json:"hashtags"`
// 	Mood          string   `json:"mood"`
// 	IsOriginal    bool     `json:"isOriginal"`
// 	Visibility    string   `json:"visibility"`
// 	AudioURL      string   `json:"audioUrl"`
// 	AudioDuration int      `json:"audioDuration"`
// 	CoverColor    string   `json:"coverColor"`
// 	Description   string   `json:"description"`   // NEW
// 	TextAlign     string   `json:"textAlign"`     // NEW
// }

// type service struct {
// 	repo          Repository
// 	userRepo      users.Repository
// 	socialRepo    social.Repository
// 	notifService  notifications.Service
// }

// func NewService(repo Repository, userRepo users.Repository, socialRepo social.Repository, notifService notifications.Service) Service {
// 	return &service{repo: repo, userRepo: userRepo, socialRepo: socialRepo, notifService: notifService}
// }

// // sanitizeHashtags normalises hashtags: lowercase, strip #, remove duplicates, max 10
// func sanitizeHashtags(tags []string) []string {
// 	seen := make(map[string]bool)
// 	var result []string
// 	for _, tag := range tags {
// 		tag = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(tag, "#")))
// 		if tag == "" || seen[tag] {
// 			continue
// 		}
// 		seen[tag] = true
// 		result = append(result, tag)
// 		if len(result) >= 10 {
// 			break
// 		}
// 	}
// 	return result
// }

// var mentionRegex = regexp.MustCompile(`@([a-zA-Z0-9_-]{3,20})`)

// // extractMentionedUsernames parses @username patterns from text.
// // Returns deduplicated, lowercased usernames.
// func extractMentionedUsernames(text string) []string {
// 	matches := mentionRegex.FindAllStringSubmatch(text, -1)
// 	seen := make(map[string]bool)
// 	usernames := make([]string, 0, len(matches))
// 	for _, m := range matches {
// 		username := strings.ToLower(m[1])
// 		if !seen[username] {
// 			seen[username] = true
// 			usernames = append(usernames, username)
// 		}
// 	}
// 	return usernames
// }

// // resolveMentions takes a description string, extracts @usernames,
// // looks up each in the DB, and returns the resolved ObjectIDs.
// // Non-existent usernames are silently skipped.
// func (s *service) resolveMentions(ctx context.Context, description string) []bson.ObjectID {
// 	usernames := extractMentionedUsernames(description)
// 	if len(usernames) == 0 {
// 		return nil
// 	}

// 	mentionIDs := make([]bson.ObjectID, 0, len(usernames))
// 	for _, username := range usernames {
// 		user, err := s.userRepo.GetUserByUsername(ctx, username)
// 		if err != nil {
// 			continue // skip non-existent users
// 		}
// 		mentionIDs = append(mentionIDs, user.ID)
// 	}
// 	return mentionIDs
// }

// // sendMentionNotifications sends a notification to each newly mentioned user.
// // oldMentions is nil for new poems, or the previous mentions list for updates.
// // IMPORTANT: This is called with `go s.sendMentionNotifications(...)` from the caller,
// // so it already runs in a goroutine. Do NOT spawn additional goroutines inside.
// func (s *service) sendMentionNotifications(author *models.User, poem *models.Poem, oldMentions []bson.ObjectID) {
// 	ctx := context.Background() // fresh context — the request context may be cancelled

// 	oldSet := make(map[bson.ObjectID]bool)
// 	for _, id := range oldMentions {
// 		oldSet[id] = true
// 	}

// 	for _, id := range poem.Mentions {
// 		// Skip if already mentioned before (on update)
// 		if oldSet[id] {
// 			continue
// 		}
// 		// Don't notify yourself
// 		if id == author.ID {
// 			continue
// 		}

// 		s.notifService.Send(ctx, models.SendNotificationRequest{
// 			RecipientID:  id,
// 			ActorID:      author.ID,
// 			Type:         models.NotifTypeMentioned,
// 			ResourceType: models.ResourceTypePoem,
// 			ResourceID:   poem.ID.Hex(),
// 			Title:        author.DisplayName + " mentioned you",
// 			Body:         `in "` + poem.Title + `"`,
// 			GroupKey:     "mention:" + poem.ID.Hex(),
// 		})
// 	}
// }

// func validateVisibility(v string) bool {
// 	return v == models.PoemVisibilityPublic || v == models.PoemVisibilityPrivate
// }

// func buildAuthor(user *models.User) models.PoemAuthor {
// 	if user == nil {
// 		return models.PoemAuthor{}
// 	}
// 	return models.PoemAuthor{
// 		ID:          user.ID.Hex(),
// 		DisplayName: user.DisplayName,
// 		Username:    user.Username,
// 		PhotoURL:    user.PhotoURL,
// 		IsEditor:    user.IsEditor,
// 	}
// }

// func (s *service) toResponse(poem *models.Poem, author *models.User) *models.PoemResponse {
// 	resp := &models.PoemResponse{
// 		ID:            poem.ID.Hex(),
// 		Author:        buildAuthor(author),
// 		Title:         poem.Title,
// 		ContentJSON:   poem.ContentJSON,
// 		PlainText:     poem.PlainText,
// 		Hashtags:      poem.Hashtags,
// 		Mood:          poem.Mood,
// 		IsOriginal:    poem.IsOriginal,
// 		Visibility:    poem.Visibility,
// 		AudioURL:      poem.AudioURL,
// 		AudioDuration: poem.AudioDuration,
// 		CoverColor:    poem.CoverColor,
// 		Description:   poem.Description,
// 		TextAlign:     poem.TextAlign,
// 		LikesCount:    poem.LikesCount,
// 		CommentsCount: poem.CommentsCount,
// 		RepostsCount:  poem.RepostsCount,
// 		CreatedAt:     poem.CreatedAt,
// 		UpdatedAt:     poem.UpdatedAt,
// 	}

// 	// Initialize mentions to empty slice (never nil — avoids JSON null)
// 	resp.Mentions = []models.MentionedUser{}

// 	// Populate if there are mentions
// 	if len(poem.Mentions) > 0 {
// 		for _, uid := range poem.Mentions {
// 			user, err := s.userRepo.GetUserByID(context.Background(), uid)
// 			if err != nil {
// 				continue // skip deleted/invalid users
// 			}
// 			resp.Mentions = append(resp.Mentions, models.MentionedUser{
// 				ID:          user.ID.Hex(),
// 				Username:    user.Username,
// 				DisplayName: user.DisplayName,
// 				PhotoURL:    user.PhotoURL,
// 			})
// 		}
// 	}

// 	if resp.Hashtags == nil {
// 		resp.Hashtags = []string{}
// 	}

// 	return resp
// }

// func (s *service) Create(ctx context.Context, authorIDStr string, req CreatePoemRequest) (*models.PoemResponse, error) {
// 	authorID, err := bson.ObjectIDFromHex(authorIDStr)
// 	if err != nil {
// 		return nil, errors.New("invalid author id")
// 	}

// 	// Validation
// 	req.Title = strings.TrimSpace(req.Title)
// 	if req.Title == "" {
// 		req.Title = "Untitled Poem"
// 	}
// 	if len(req.Title) > 200 {
// 		return nil, errors.New("title must be 200 characters or less")
// 	}
// 	if req.ContentJSON == "" {
// 		return nil, errors.New("poem content is required")
// 	}
// 	if !validateVisibility(req.Visibility) {
// 		req.Visibility = models.PoemVisibilityPublic
// 	}
// 	if req.Mood != "" && !models.ValidMoods[req.Mood] {
// 		return nil, errors.New("invalid mood value")
// 	}

// 	// Validate word count
// 	words := strings.Fields(strings.TrimSpace(req.PlainText))
// 	if len(words) > 150 {
// 		return nil, errors.New("poem body exceeds 150 word limit")
// 	}

// 	// Validate textAlign
// 	if req.TextAlign != "" && req.TextAlign != "left" && req.TextAlign != "center" && req.TextAlign != "right" {
// 		return nil, errors.New("textAlign must be left, center, or right")
// 	}

// 	// Validate description length
// 	if len([]rune(req.Description)) > 200 {
// 		return nil, errors.New("description exceeds 200 character limit")
// 	}

// 	hashtags := sanitizeHashtags(req.Hashtags)

// 	// Resolve @mentions from description
// 	mentionIDs := s.resolveMentions(ctx, req.Description)

// 	poem := &models.Poem{
// 		AuthorID:      authorID,
// 		Title:         req.Title,
// 		ContentJSON:   req.ContentJSON,
// 		PlainText:     req.PlainText,
// 		Hashtags:      hashtags,
// 		Mood:          req.Mood,
// 		IsOriginal:    req.IsOriginal,
// 		Visibility:    req.Visibility,
// 		AudioURL:      req.AudioURL,
// 		AudioDuration: req.AudioDuration,
// 		CoverColor:    req.CoverColor,
// 		Description:   req.Description,
// 		TextAlign:     req.TextAlign,
// 		Mentions:      mentionIDs,
// 	}

// 	if err := s.repo.Create(ctx, poem); err != nil {
// 		return nil, err
// 	}

// 	// Increment hashtag usage counts (fire and forget — non-blocking)
// 	if len(hashtags) > 0 {
// 		go func() {
// 			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
// 			defer cancel()
// 			_ = s.repo.UpsertHashtags(bgCtx, hashtags)
// 		}()
// 	}

// 	// Increment user's post count
// 	go func() {
// 		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
// 		defer cancel()
// 		_ = s.userRepo.IncrementPostsCount(bgCtx, authorID)
// 	}()

// 	// Fetch the author ONCE (single poem operation)
// 	author, _ := s.userRepo.GetUserByID(ctx, authorID)

// 	// Send mention notifications (async, don't block response)
// 	go s.sendMentionNotifications(author, poem, nil)

// 	return s.toResponse(poem, author), nil
// }

// func (s *service) GetByID(ctx context.Context, poemIDStr string, callerID string) (*models.PoemResponse, error) {
// 	poemID, err := bson.ObjectIDFromHex(poemIDStr)
// 	if err != nil {
// 		return nil, errors.New("invalid poem id")
// 	}

// 	poem, err := s.repo.GetByID(ctx, poemID)
// 	if err != nil {
// 		if err == mongo.ErrNoDocuments {
// 			return nil, errors.New("poem not found")
// 		}
// 		return nil, err
// 	}

// 	// Private poems can only be seen by their author
// 	if poem.Visibility == models.PoemVisibilityPrivate && poem.AuthorID.Hex() != callerID {
// 		return nil, errors.New("poem not found")
// 	}

// 	// Fetch the author once
// 	author, _ := s.userRepo.GetUserByID(ctx, poem.AuthorID)

// 	return s.toResponse(poem, author), nil
// }

// func (s *service) Update(ctx context.Context, poemIDStr string, authorIDStr string, req UpdatePoemRequest) (*models.PoemResponse, error) {
// 	poemID, err := bson.ObjectIDFromHex(poemIDStr)
// 	if err != nil {
// 		return nil, errors.New("invalid poem id")
// 	}
// 	authorID, err := bson.ObjectIDFromHex(authorIDStr)
// 	if err != nil {
// 		return nil, errors.New("invalid author id")
// 	}

// 	// Fetch existing poem to verify ownership and get old hashtags
// 	existing, err := s.repo.GetByID(ctx, poemID)
// 	if err != nil {
// 		return nil, errors.New("poem not found")
// 	}
// 	if existing.AuthorID != authorID {
// 		return nil, errors.New("unauthorized: you do not own this poem")
// 	}

// 	// Validation
// 	req.Title = strings.TrimSpace(req.Title)
// 	if req.Title == "" {
// 		req.Title = "Untitled Poem"
// 	}
// 	if !validateVisibility(req.Visibility) {
// 		req.Visibility = existing.Visibility
// 	}
// 	if req.Mood != "" && !models.ValidMoods[req.Mood] {
// 		return nil, errors.New("invalid mood value")
// 	}

// 	// Validate word count
// 	words := strings.Fields(strings.TrimSpace(req.PlainText))
// 	if len(words) > 150 {
// 		return nil, errors.New("poem body exceeds 150 word limit")
// 	}

// 	// Validate textAlign
// 	if req.TextAlign != "" && req.TextAlign != "left" && req.TextAlign != "center" && req.TextAlign != "right" {
// 		return nil, errors.New("textAlign must be left, center, or right")
// 	}

// 	// Validate description length
// 	if len([]rune(req.Description)) > 200 {
// 		return nil, errors.New("description exceeds 200 character limit")
// 	}

// 	// Capture old mentions before update
// 	oldMentions := existing.Mentions

// 	newHashtags := sanitizeHashtags(req.Hashtags)

// 	// Resolve new mentions
// 	newMentionIDs := s.resolveMentions(ctx, req.Description)

// 	updated, err := s.repo.Update(ctx, poemID, PoemUpdateFields{
// 		Title:         req.Title,
// 		ContentJSON:   req.ContentJSON,
// 		PlainText:     req.PlainText,
// 		Hashtags:      newHashtags,
// 		Mood:          req.Mood,
// 		IsOriginal:    req.IsOriginal,
// 		Visibility:    req.Visibility,
// 		AudioURL:      req.AudioURL,
// 		AudioDuration: req.AudioDuration,
// 		CoverColor:    req.CoverColor,
// 		Description:   req.Description,
// 		TextAlign:     req.TextAlign,
// 		Mentions:      newMentionIDs,
// 	})
// 	if err != nil {
// 		return nil, err
// 	}

// 	// Update hashtag counts async — decrement old, increment new
// 	go func() {
// 		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
// 		defer cancel()
// 		_ = s.repo.DecrementHashtags(bgCtx, existing.Hashtags)
// 		_ = s.repo.UpsertHashtags(bgCtx, newHashtags)
// 	}()

// 	author, _ := s.userRepo.GetUserByID(ctx, updated.AuthorID)

// 	// Send notifications only to newly mentioned users
// 	go s.sendMentionNotifications(author, updated, oldMentions)

// 	return s.toResponse(updated, author), nil
// }

// func (s *service) Delete(ctx context.Context, poemIDStr string, authorIDStr string) error {
// 	poemID, err := bson.ObjectIDFromHex(poemIDStr)
// 	if err != nil {
// 		return errors.New("invalid poem id")
// 	}
// 	authorID, err := bson.ObjectIDFromHex(authorIDStr)
// 	if err != nil {
// 		return errors.New("invalid author id")
// 	}

// 	// Fetch to get hashtags before deleting
// 	existing, err := s.repo.GetByID(ctx, poemID)
// 	if err != nil {
// 		return errors.New("poem not found")
// 	}
// 	if existing.AuthorID != authorID {
// 		return errors.New("unauthorized: you do not own this poem")
// 	}

// 	if err := s.repo.SoftDelete(ctx, poemID, authorID); err != nil {
// 		return err
// 	}

// 	// Decrement hashtags and postsCount async
// 	go func() {
// 		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
// 		defer cancel()
// 		_ = s.repo.DecrementHashtags(bgCtx, existing.Hashtags)
// 		_ = s.userRepo.DecrementPostsCount(bgCtx, authorID)
// 	}()

// 	return nil
// }

// func (s *service) GetMyPoems(ctx context.Context, authorIDStr string, limit int, beforeStr string) (*models.PoemsPage, error) {
// 	authorID, err := bson.ObjectIDFromHex(authorIDStr)
// 	if err != nil {
// 		return nil, errors.New("invalid author id")
// 	}

// 	if limit <= 0 {
// 		limit = 20
// 	}
// 	if limit > 50 {
// 		limit = 50
// 	}

// 	var beforeID *bson.ObjectID
// 	if beforeStr != "" {
// 		id, err := bson.ObjectIDFromHex(beforeStr)
// 		if err != nil {
// 			return nil, errors.New("invalid before cursor")
// 		}
// 		beforeID = &id
// 	}

// 	// Fetch limit+1 to determine hasMore
// 	poems, err := s.repo.GetByAuthor(ctx, authorID, limit+1, beforeID, true)
// 	if err != nil {
// 		return nil, err
// 	}

// 	hasMore := len(poems) > limit
// 	if hasMore {
// 		poems = poems[:limit]
// 	}

// 	responses := make([]models.PoemResponse, 0, len(poems))

// 	// Batch check like and repost status
// 	var likedMap map[string]bool
// 	var repostedMap map[string]bool
// 	if s.socialRepo != nil {
// 		ids := make([]bson.ObjectID, 0, len(poems))
// 		for _, p := range poems {
// 			ids = append(ids, p.ID)
// 		}
// 		likedMap, _ = s.socialRepo.IsPoemLikedMany(ctx, authorID, ids)
// 		repostedMap, _ = s.socialRepo.IsPoemRepostedMany(ctx, authorID, ids)
// 	}

// 	// Fetch the author ONCE (all poems have the same author)
// 	author, _ := s.userRepo.GetUserByID(ctx, authorID)

// 	for _, p := range poems {
// 		resp := s.toResponse(&p, author)
// 		if likedMap != nil {
// 			resp.IsLikedByMe = likedMap[p.ID.Hex()]
// 		}
// 		if repostedMap != nil {
// 			resp.IsRepostedByMe = repostedMap[p.ID.Hex()]
// 		}
// 		responses = append(responses, *resp)
// 	}

// 	return &models.PoemsPage{Poems: responses, HasMore: hasMore}, nil
// }

// func (s *service) GetUserPoems(ctx context.Context, targetUserIDStr string, callerID string, limit int, beforeStr string) (*models.PoemsPage, error) {
// 	targetUserID, err := bson.ObjectIDFromHex(targetUserIDStr)
// 	if err != nil {
// 		return nil, errors.New("invalid user id")
// 	}

// 	if limit <= 0 {
// 		limit = 20
// 	}
// 	if limit > 50 {
// 		limit = 50
// 	}

// 	var beforeID *bson.ObjectID
// 	if beforeStr != "" {
// 		id, err := bson.ObjectIDFromHex(beforeStr)
// 		if err != nil {
// 			return nil, errors.New("invalid before cursor")
// 		}
// 		beforeID = &id
// 	}

// 	// includePrivate only if the caller is viewing their own profile
// 	includePrivate := targetUserID.Hex() == callerID

// 	poems, err := s.repo.GetByAuthor(ctx, targetUserID, limit+1, beforeID, includePrivate)
// 	if err != nil {
// 		return nil, err
// 	}

// 	hasMore := len(poems) > limit
// 	if hasMore {
// 		poems = poems[:limit]
// 	}

// 	var likedMap map[string]bool
// 	var repostedMap map[string]bool
// 	if callerID != "" && s.socialRepo != nil {
// 		cID, err := bson.ObjectIDFromHex(callerID)
// 		if err == nil {
// 			ids := make([]bson.ObjectID, 0, len(poems))
// 			for _, p := range poems {
// 				ids = append(ids, p.ID)
// 			}
// 			likedMap, _ = s.socialRepo.IsPoemLikedMany(ctx, cID, ids)
// 			repostedMap, _ = s.socialRepo.IsPoemRepostedMany(ctx, cID, ids)
// 		}
// 	}

// 	// Fetch the author once
// 	author, _ := s.userRepo.GetUserByID(ctx, targetUserID)

// 	responses := make([]models.PoemResponse, 0, len(poems))
// 	for _, p := range poems {
// 		resp := s.toResponse(&p, author)
// 		if likedMap != nil {
// 			resp.IsLikedByMe = likedMap[p.ID.Hex()]
// 		}
// 		if repostedMap != nil {
// 			resp.IsRepostedByMe = repostedMap[p.ID.Hex()]
// 		}
// 		responses = append(responses, *resp)
// 	}

//		return &models.PoemsPage{Poems: responses, HasMore: hasMore}, nil
//	}
package poems

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/xyz-asif/gotodo/internal/features/notifications"
	"github.com/xyz-asif/gotodo/internal/features/social"
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
	Description   string   `json:"description"`   // NEW: poem description with @mentions
	TextAlign     string   `json:"textAlign"`     // NEW: "left" | "center" | "right"
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
	Description   string   `json:"description"` // NEW
	TextAlign     string   `json:"textAlign"`   // NEW
}

type service struct {
	repo         Repository
	userRepo     users.Repository
	socialRepo   social.Repository
	notifService notifications.Service
}

func NewService(repo Repository, userRepo users.Repository, socialRepo social.Repository, notifService notifications.Service) Service {
	return &service{repo: repo, userRepo: userRepo, socialRepo: socialRepo, notifService: notifService}
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
		user, err := s.userRepo.GetUserByUsername(ctx, username)
		if err != nil {
			continue // skip non-existent users
		}
		mentionIDs = append(mentionIDs, user.ID)
	}
	return mentionIDs
}

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

func validateVisibility(v string) bool {
	return v == models.PoemVisibilityPublic || v == models.PoemVisibilityPrivate
}

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
	resp := &models.PoemResponse{
		ID:            poem.ID.Hex(),
		Author:        buildAuthor(author),
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
		Description:   poem.Description,
		TextAlign:     poem.TextAlign,
		LikesCount:    poem.LikesCount,
		CommentsCount: poem.CommentsCount,
		RepostsCount:  poem.RepostsCount,
		CreatedAt:     poem.CreatedAt,
		UpdatedAt:     poem.UpdatedAt,
	}

	// Initialize mentions to empty slice (never nil — avoids JSON null)
	resp.Mentions = []models.MentionedUser{}

	// Populate if there are mentions
	if len(poem.Mentions) > 0 {
		for _, uid := range poem.Mentions {
			user, err := s.userRepo.GetUserByID(context.Background(), uid)
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

	if resp.Hashtags == nil {
		resp.Hashtags = []string{}
	}

	return resp
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

	hashtags := sanitizeHashtags(req.Hashtags)

	// Resolve @mentions from description
	mentionIDs := s.resolveMentions(ctx, req.Description)

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
		Description:   req.Description,
		TextAlign:     req.TextAlign,
		Mentions:      mentionIDs,
	}

	if err := s.repo.Create(ctx, poem); err != nil {
		return nil, err
	}

	// Increment hashtag usage counts (fire and forget — non-blocking)
	if len(hashtags) > 0 {
		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.repo.UpsertHashtags(bgCtx, hashtags)
		}()
	}

	// Increment user's post count
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.userRepo.IncrementPostsCount(bgCtx, authorID)
	}()

	// Fetch the author ONCE (single poem operation)
	author, _ := s.userRepo.GetUserByID(ctx, authorID)

	// Send mention notifications (async, don't block response)
	go s.sendMentionNotifications(author, poem, nil)

	return s.toResponse(poem, author), nil
}

// FIX: GetByID now checks the caller's like/repost state.
// Previously, callerID was only used for private-poem visibility check
// and isLikedByMe/isRepostedByMe were always returned as false.
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

	// Fetch the author once
	author, _ := s.userRepo.GetUserByID(ctx, poem.AuthorID)
	resp := s.toResponse(poem, author)

	// FIX: Check caller's like/repost state — same pattern as GetMyPoems and GetUserPoems.
	// Without this, the response always had isLikedByMe=false and isRepostedByMe=false,
	// which is what caused the grey heart on notification taps and deep links.
	if callerID != "" && s.socialRepo != nil {
		cID, err := bson.ObjectIDFromHex(callerID)
		if err == nil {
			liked, _ := s.socialRepo.IsPoemLiked(ctx, cID, poemID)
			resp.IsLikedByMe = liked

			repostedMap, _ := s.socialRepo.IsPoemRepostedMany(ctx, cID, []bson.ObjectID{poemID})
			resp.IsRepostedByMe = repostedMap[poemIDStr]
		}
	}

	return resp, nil
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

	// Capture old mentions before update
	oldMentions := existing.Mentions

	newHashtags := sanitizeHashtags(req.Hashtags)

	// Resolve new mentions
	newMentionIDs := s.resolveMentions(ctx, req.Description)

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
		Description:   req.Description,
		TextAlign:     req.TextAlign,
		Mentions:      newMentionIDs,
	})
	if err != nil {
		return nil, err
	}

	// Update hashtag counts async — decrement old, increment new
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repo.DecrementHashtags(bgCtx, existing.Hashtags)
		_ = s.repo.UpsertHashtags(bgCtx, newHashtags)
	}()

	author, _ := s.userRepo.GetUserByID(ctx, updated.AuthorID)

	// Send notifications only to newly mentioned users
	go s.sendMentionNotifications(author, updated, oldMentions)

	return s.toResponse(updated, author), nil
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
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repo.DecrementHashtags(bgCtx, existing.Hashtags)
		_ = s.userRepo.DecrementPostsCount(bgCtx, authorID)
	}()

	return nil
}

func (s *service) GetMyPoems(ctx context.Context, authorIDStr string, limit int, beforeStr string) (*models.PoemsPage, error) {
	authorID, err := bson.ObjectIDFromHex(authorIDStr)
	if err != nil {
		return nil, errors.New("invalid author id")
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

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

	// Batch check like and repost status
	var likedMap map[string]bool
	var repostedMap map[string]bool
	if s.socialRepo != nil {
		ids := make([]bson.ObjectID, 0, len(poems))
		for _, p := range poems {
			ids = append(ids, p.ID)
		}
		likedMap, _ = s.socialRepo.IsPoemLikedMany(ctx, authorID, ids)
		repostedMap, _ = s.socialRepo.IsPoemRepostedMany(ctx, authorID, ids)
	}

	// Fetch the author ONCE (all poems have the same author)
	author, _ := s.userRepo.GetUserByID(ctx, authorID)

	for _, p := range poems {
		resp := s.toResponse(&p, author)
		if likedMap != nil {
			resp.IsLikedByMe = likedMap[p.ID.Hex()]
		}
		if repostedMap != nil {
			resp.IsRepostedByMe = repostedMap[p.ID.Hex()]
		}
		responses = append(responses, *resp)
	}

	return &models.PoemsPage{Poems: responses, HasMore: hasMore}, nil
}

func (s *service) GetUserPoems(ctx context.Context, targetUserIDStr string, callerID string, limit int, beforeStr string) (*models.PoemsPage, error) {
	targetUserID, err := bson.ObjectIDFromHex(targetUserIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

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

	var likedMap map[string]bool
	var repostedMap map[string]bool
	if callerID != "" && s.socialRepo != nil {
		cID, err := bson.ObjectIDFromHex(callerID)
		if err == nil {
			ids := make([]bson.ObjectID, 0, len(poems))
			for _, p := range poems {
				ids = append(ids, p.ID)
			}
			likedMap, _ = s.socialRepo.IsPoemLikedMany(ctx, cID, ids)
			repostedMap, _ = s.socialRepo.IsPoemRepostedMany(ctx, cID, ids)
		}
	}

	// Fetch the author once
	author, _ := s.userRepo.GetUserByID(ctx, targetUserID)

	responses := make([]models.PoemResponse, 0, len(poems))
	for _, p := range poems {
		resp := s.toResponse(&p, author)
		if likedMap != nil {
			resp.IsLikedByMe = likedMap[p.ID.Hex()]
		}
		if repostedMap != nil {
			resp.IsRepostedByMe = repostedMap[p.ID.Hex()]
		}
		responses = append(responses, *resp)
	}

	return &models.PoemsPage{Poems: responses, HasMore: hasMore}, nil
}
