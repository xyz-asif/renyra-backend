package feed

import (
	"context"
	"errors"

	"github.com/xyz-asif/gotodo/internal/features/follows"
	"github.com/xyz-asif/gotodo/internal/features/poems"
	"github.com/xyz-asif/gotodo/internal/features/social"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Service interface {
	GetHomeFeed(ctx context.Context, callerIDStr string, limit int, before string) (*models.FeedPage, error)
	GetExploreFeed(ctx context.Context, callerIDStr string, hashtag string, limit int, before string) (*models.FeedPage, error)
	GetAudioFeed(ctx context.Context, limit int, before string) (*models.FeedPage, error)
	SearchPoems(ctx context.Context, query string, limit int, before string) (*models.PoemSearchPage, error)
	SearchUsers(ctx context.Context, query string, callerIDStr string, limit int, offset int) (*models.UserSearchPage, error)
}

type service struct {
	repo        Repository
	followRepo  follows.Repository
	userRepo    users.Repository
	socialRepo  social.Repository
	poemService poems.Service // reuse poem service's toResponse helper indirectly if needed
}

func NewService(repo Repository, followRepo follows.Repository, userRepo users.Repository, socialRepo social.Repository) Service {
	return &service{
		repo:       repo,
		followRepo: followRepo,
		userRepo:   userRepo,
		socialRepo: socialRepo,
	}
}

func (s *service) buildPoemResponse(ctx context.Context, poem *models.Poem, isLiked, isReposted bool) models.PoemResponse {
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

	// Batch check like/repost status
	var likedMap map[string]bool
	var repostedMap map[string]bool
	if callerIDStr != "" {
		ids := make([]bson.ObjectID, 0, len(poemDocs))
		for _, p := range poemDocs {
			ids = append(ids, p.ID)
		}
		likedMap, _ = s.socialRepo.IsPoemLikedMany(ctx, callerID, ids)
		repostedMap, _ = s.socialRepo.IsPoemRepostedMany(ctx, callerID, ids)
	}

	responses := make([]models.PoemResponse, 0, len(poemDocs))
	for _, p := range poemDocs {
		responses = append(responses, s.buildPoemResponse(ctx, &p, likedMap[p.ID.Hex()], repostedMap[p.ID.Hex()]))
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

	// Batch check like/repost status
	var likedMap map[string]bool
	var repostedMap map[string]bool
	if callerIDStr != "" {
		callerID, err := bson.ObjectIDFromHex(callerIDStr)
		if err == nil {
			ids := make([]bson.ObjectID, 0, len(poemDocs))
			for _, p := range poemDocs {
				ids = append(ids, p.ID)
			}
			likedMap, _ = s.socialRepo.IsPoemLikedMany(ctx, callerID, ids)
			repostedMap, _ = s.socialRepo.IsPoemRepostedMany(ctx, callerID, ids)
		}
	}

	responses := make([]models.PoemResponse, 0, len(poemDocs))
	for _, p := range poemDocs {
		responses = append(responses, s.buildPoemResponse(ctx, &p, likedMap[p.ID.Hex()], repostedMap[p.ID.Hex()]))
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
		responses = append(responses, s.buildPoemResponse(ctx, &p, false, false)) // Search doesn't usually show status but we could add it
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

func (s *service) GetAudioFeed(ctx context.Context, limit int, before string) (*models.FeedPage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

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
	if hasMore {
		poemDocs = poemDocs[:limit]
	}

	responses := make([]models.PoemResponse, 0, len(poemDocs))
	for _, p := range poemDocs {
		responses = append(responses, s.buildPoemResponse(ctx, &p, false, false))
	}

	return &models.FeedPage{Poems: responses, HasMore: hasMore}, nil
}
