package feed

import (
	"context"
	"errors"
	"log"

	"github.com/xyz-asif/gotodo/internal/features/follows"
	"github.com/xyz-asif/gotodo/internal/features/poems"
	"github.com/xyz-asif/gotodo/internal/features/social"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Service interface {
	GetHomeFeed(ctx context.Context, callerIDStr string, limit int, before string) (*models.FeedPage, error)
	GetExploreFeed(ctx context.Context, userID string, hashtag string, limit int, offset int) (*models.FeedPage, error)
	GetAudioFeed(ctx context.Context, limit int, offset int) (*models.FeedPage, error)
	SearchPoems(ctx context.Context, query string, limit int, before string) (*models.PoemSearchPage, error)
	SearchUsers(ctx context.Context, query string, callerIDStr string, limit int, offset int) (*models.UserSearchPage, error)
}

type service struct {
	repo        Repository
	followRepo  follows.Repository
	userRepo    users.Repository
	socialRepo  social.Repository
	poemService poems.Service
}

func NewService(repo Repository, followRepo follows.Repository, userRepo users.Repository, socialRepo social.Repository) Service {
	return &service{
		repo:       repo,
		followRepo: followRepo,
		userRepo:   userRepo,
		socialRepo: socialRepo,
	}
}

// FIX: buildPoemResponse now accepts likedMap and repostedMap so it can look up
// the correct social state for ANY poem, including originals nested inside reposts.
// Previously, isLiked/isReposted were passed as direct booleans, and the recursive
// call for originalPoem hardcoded them to false.
func (s *service) buildPoemResponse(
	ctx context.Context,
	poem *models.Poem,
	author *models.User,
	likedMap map[string]bool,
	repostedMap map[string]bool,
	originalPoem *models.Poem,
	originalAuthor *models.User,
) models.PoemResponse {
	poemIDHex := poem.ID.Hex()

	resp := models.PoemResponse{
		ID:             poemIDHex,
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
		Description:    poem.Description,
		TextAlign:      poem.TextAlign,
		LikesCount:     poem.LikesCount,
		CommentsCount:  poem.CommentsCount,
		RepostsCount:   poem.RepostsCount,
		IsLikedByMe:    likedMap[poemIDHex],
		IsRepostedByMe: repostedMap[poemIDHex],
		IsRepost:       poem.IsRepost,
		CreatedAt:      poem.CreatedAt,
		UpdatedAt:      poem.UpdatedAt,
	}

	// FIX: Embed original poem with CORRECT social state from the maps,
	// not hardcoded false. The maps now include original poem IDs too.
	if poem.IsRepost && originalPoem != nil {
		origResp := s.buildPoemResponse(ctx, originalPoem, originalAuthor, likedMap, repostedMap, nil, nil)
		resp.OriginalPoem = &origResp
	}

	// Populate author
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

	// Initialize mentions to empty slice (never nil — avoids JSON null)
	resp.Mentions = []models.MentionedUser{}

	// Populate if there are mentions
	if len(poem.Mentions) > 0 {
		for _, uid := range poem.Mentions {
			user, err := s.userRepo.GetUserByID(ctx, uid)
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

	return resp
}

func (s *service) GetHomeFeed(ctx context.Context, callerIDStr string, limit int, before string) (*models.FeedPage, error) {
	callerID, err := bson.ObjectIDFromHex(callerIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

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

	// Fetch original poems for reposts
	var originalIDs []bson.ObjectID
	for _, p := range poemDocs {
		if p.IsRepost && p.OriginalID != nil {
			originalIDs = append(originalIDs, *p.OriginalID)
		}
	}
	originalPoemsMap, _ := s.repo.GetPoemsByIDs(ctx, originalIDs)

	// FIX: Batch check like/repost status for BOTH top-level poems AND original
	// poems inside reposts. Previously only top-level IDs were checked, so
	// originalPoem always got isLikedByMe=false.
	var likedMap map[string]bool
	var repostedMap map[string]bool
	if callerIDStr != "" {
		// Collect all poem IDs that need social state: top-level + originals
		idSet := make(map[bson.ObjectID]bool)
		for _, p := range poemDocs {
			idSet[p.ID] = true
		}
		for _, origID := range originalIDs {
			idSet[origID] = true
		}
		ids := make([]bson.ObjectID, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		likedMap, _ = s.socialRepo.IsPoemLikedMany(ctx, callerID, ids)
		repostedMap, _ = s.socialRepo.IsPoemRepostedMany(ctx, callerID, ids)
	}

	// Batch fetch authors (including original authors)
	authorIDSet := make(map[bson.ObjectID]bool)
	for _, p := range poemDocs {
		authorIDSet[p.AuthorID] = true
	}
	for _, p := range originalPoemsMap {
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

	responses := make([]models.PoemResponse, 0, len(poemDocs))
	for _, p := range poemDocs {
		author := authorMap[p.AuthorID]
		var originalPoem *models.Poem
		var originalAuthor *models.User
		if p.IsRepost && p.OriginalID != nil {
			op, exists := originalPoemsMap[*p.OriginalID]
			if exists {
				originalPoem = &op
				originalAuthor = authorMap[op.AuthorID]
			}
		}
		// FIX: Pass maps instead of direct booleans
		responses = append(responses, s.buildPoemResponse(ctx, &p, author, likedMap, repostedMap, originalPoem, originalAuthor))
	}

	return &models.FeedPage{Poems: responses, HasMore: hasMore}, nil
}

// GetExploreFeed returns poems weighted by engagement score with offset pagination.
func (s *service) GetExploreFeed(ctx context.Context, userID string, hashtag string, limit int, offset int) (*models.FeedPage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	poemDocs, err := s.repo.GetExploreFeed(ctx, hashtag, limit+1, offset)
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
	if userID != "" {
		callerID, err := bson.ObjectIDFromHex(userID)
		if err == nil {
			ids := make([]bson.ObjectID, 0, len(poemDocs))
			for _, p := range poemDocs {
				ids = append(ids, p.ID)
			}
			likedMap, _ = s.socialRepo.IsPoemLikedMany(ctx, callerID, ids)
			repostedMap, _ = s.socialRepo.IsPoemRepostedMany(ctx, callerID, ids)
		}
	}

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

	// Initialize maps if nil (no authenticated user)
	if likedMap == nil {
		likedMap = make(map[string]bool)
	}
	if repostedMap == nil {
		repostedMap = make(map[string]bool)
	}

	responses := make([]models.PoemResponse, 0, len(poemDocs))
	for _, p := range poemDocs {
		author := authorMap[p.AuthorID]
		responses = append(responses, s.buildPoemResponse(ctx, &p, author, likedMap, repostedMap, nil, nil))
	}

	return &models.FeedPage{Poems: responses, HasMore: hasMore}, nil
}

// GetAudioFeed retrieves a timeline of audio poems.
func (s *service) GetAudioFeed(ctx context.Context, limit int, offset int) (*models.FeedPage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	poemDocs, err := s.repo.GetAudioFeed(ctx, limit+1, offset)
	if err != nil {
		return nil, err
	}

	hasMore := len(poemDocs) > limit
	if hasMore {
		poemDocs = poemDocs[:limit]
	}

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

	// Empty maps — audio feed has no auth context currently
	emptyLiked := make(map[string]bool)
	emptyReposted := make(map[string]bool)

	responses := make([]models.PoemResponse, 0, len(poemDocs))
	for _, p := range poemDocs {
		author := authorMap[p.AuthorID]
		responses = append(responses, s.buildPoemResponse(ctx, &p, author, emptyLiked, emptyReposted, nil, nil))
	}

	return &models.FeedPage{Poems: responses, HasMore: hasMore}, nil
}

func (s *service) SearchPoems(ctx context.Context, query string, limit int, before string) (*models.PoemSearchPage, error) {
	if query == "" {
		return &models.PoemSearchPage{Poems: []models.PoemResponse{}, HasMore: false}, nil
	}
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

	poemDocs, err := s.repo.SearchPoems(ctx, query, limit+1, beforeID)
	if err != nil {
		return nil, err
	}

	hasMore := len(poemDocs) > limit
	if hasMore {
		poemDocs = poemDocs[:limit]
	}

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

	emptyLiked := make(map[string]bool)
	emptyReposted := make(map[string]bool)

	responses := make([]models.PoemResponse, 0, len(poemDocs))
	for _, p := range poemDocs {
		author := authorMap[p.AuthorID]
		responses = append(responses, s.buildPoemResponse(ctx, &p, author, emptyLiked, emptyReposted, nil, nil))
	}

	return &models.PoemSearchPage{Poems: responses, HasMore: hasMore}, nil
}

func (s *service) SearchUsers(ctx context.Context, query string, callerIDStr string, limit int, offset int) (*models.UserSearchPage, error) {
	if query == "" {
		return &models.UserSearchPage{Users: []models.UserSearchResult{}, HasMore: false}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

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
