package poems

import (
	"context"
	"errors"
	"strings"
	"time"

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
	repo       Repository
	userRepo   users.Repository
	socialRepo social.Repository
}

func NewService(repo Repository, userRepo users.Repository, socialRepo social.Repository) Service {
	return &service{repo: repo, userRepo: userRepo, socialRepo: socialRepo}
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

	return s.toResponse(poem, author), nil
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

	// Fetch the author once
	author, _ := s.userRepo.GetUserByID(ctx, poem.AuthorID)

	return s.toResponse(poem, author), nil
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
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repo.DecrementHashtags(bgCtx, existing.Hashtags)
		_ = s.repo.UpsertHashtags(bgCtx, newHashtags)
	}()

	author, _ := s.userRepo.GetUserByID(ctx, updated.AuthorID)

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

	// Batch check like status for the caller's own poems
	var likedMap map[string]bool
	if s.socialRepo != nil {
		ids := make([]bson.ObjectID, 0, len(poems))
		for _, p := range poems {
			ids = append(ids, p.ID)
		}
		likedMap, _ = s.socialRepo.IsPoemLikedMany(ctx, authorID, ids)
	}

	// Fetch the author ONCE (all poems have the same author)
	author, _ := s.userRepo.GetUserByID(ctx, authorID)

	for _, p := range poems {
		resp := s.toResponse(&p, author)
		if likedMap != nil {
			resp.IsLikedByMe = likedMap[p.ID.Hex()]
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

	// Fetch the author once
	author, _ := s.userRepo.GetUserByID(ctx, targetUserID)

	responses := make([]models.PoemResponse, 0, len(poems))
	for _, p := range poems {
		responses = append(responses, *s.toResponse(&p, author))
	}

	return &models.PoemsPage{Poems: responses, HasMore: hasMore}, nil
}
