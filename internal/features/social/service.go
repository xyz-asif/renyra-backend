package social

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/xyz-asif/renyra-backend/internal/features/notifications"
	"github.com/xyz-asif/renyra-backend/internal/features/users"
	"github.com/xyz-asif/renyra-backend/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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
	GetUserReposts(ctx context.Context, userIDStr string, callerIDStr string, limit int, before string) (*models.FeedPage, error)
	GetPoemReposters(ctx context.Context, poemIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error)
}

type service struct {
	repo         Repository
	userRepo     users.Repository
	notifService notifications.Service
	poemsCol     *mongo.Collection // direct access for repost creation
}

func NewService(repo Repository, userRepo users.Repository, notifService notifications.Service, db *mongo.Database) Service {
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
		// Wait for decrement to finish to return accurate count
		_ = s.repo.DecrementPoemLikes(ctx, poemID)

		var updated models.Poem
		_ = s.poemsCol.FindOne(ctx, bson.M{"_id": poemID, "isDeleted": false}).Decode(&updated)

		newCount := updated.LikesCount
		if newCount < 0 {
			newCount = 0
		}
		return false, newCount, nil
	}

	if err := s.repo.LikePoem(ctx, userID, poemID); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return true, poem.LikesCount, nil
		}
		return false, 0, err
	}

	// Wait for increment to finish to return accurate count
	_ = s.repo.IncrementPoemLikes(ctx, poemID)

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Notify poem author — but not if liking own poem
		if poem.AuthorID != userID && s.notifService != nil {
			liker, _ := s.userRepo.GetUserByID(bgCtx, userID)
			name := "Someone"
			if liker != nil {
				name = liker.DisplayName
			}
			_ = s.notifService.Send(bgCtx, models.SendNotificationRequest{
				RecipientID:  poem.AuthorID,
				ActorID:      userID,
				Type:         models.NotifTypePoemLiked,
				ResourceType: "poem",
				ResourceID:   poemIDStr,
				Title:        name,
				Body:         "liked your post \"" + poem.Title + "\"",
				GroupKey:     "like:" + poemIDStr,
			})
		}
	}()

	var updated models.Poem
	_ = s.poemsCol.FindOne(ctx, bson.M{"_id": poemID, "isDeleted": false}).Decode(&updated)

	return true, updated.LikesCount, nil
}

func (s *service) GetPoemLikers(ctx context.Context, poemIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error) {
	poemID, err := bson.ObjectIDFromHex(poemIDStr)
	if err != nil {
		return nil, false, errors.New("invalid poem id")
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
			return nil, false, errors.New("invalid before cursor")
		}
		beforeID = &id
	}

	likes, err := s.repo.GetPoemLikers(ctx, poemID, limit+1, beforeID)
	if err != nil {
		return nil, false, err
	}

	hasMore := len(likes) > limit
	if hasMore {
		likes = likes[:limit]
	}

	ids := make([]bson.ObjectID, 0, len(likes))
	for _, l := range likes {
		ids = append(ids, l.UserID)
	}

	userMap, err := s.userRepo.GetUsersByIDs(ctx, ids)
	if err != nil {
		return nil, false, err
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
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repo.IncrementPoemComments(bgCtx, poemID)

		commenter, _ := s.userRepo.GetUserByID(bgCtx, authorID)
		name := "Someone"
		if commenter != nil {
			name = commenter.DisplayName
		}

		// Notify poem author (if not self-comment)
		if poem.AuthorID != authorID && s.notifService != nil {
			_ = s.notifService.Send(bgCtx, models.SendNotificationRequest{
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
		s.processMentions(bgCtx, content, authorID, poemIDStr, comment.ID.Hex())
	}()

	author, _ := s.userRepo.GetUserByID(ctx, authorID)
	resp := &models.CommentResponse{
		ID:        comment.ID.Hex(),
		PoemID:    poemIDStr,
		Content:   content,
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
		if len(match) < 2 {
			continue
		}
		username := match[1]
		if notified[username] {
			continue
		}
		notified[username] = true

		// Look up user by username
		mentionedUser, err := s.userRepo.GetUserByUsername(ctx, username)
		if err != nil || mentionedUser == nil {
			continue
		}
		if mentionedUser.ID == authorID {
			continue
		} // don't notify self-mention

		commenter, _ := s.userRepo.GetUserByID(ctx, authorID)
		name := "Someone"
		if commenter != nil {
			name = commenter.DisplayName
		}

		_ = s.notifService.Send(ctx, models.SendNotificationRequest{
			RecipientID:  mentionedUser.ID,
			ActorID:      authorID,
			Type:         models.NotifTypeMentioned,
			ResourceType: "poem",
			ResourceID:   poemIDStr,
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

	comments, err := s.repo.GetCommentsByPoem(ctx, poemID, limit+1, beforeID)
	if err != nil {
		return nil, err
	}

	hasMore := len(comments) > limit
	if hasMore {
		comments = comments[:limit]
	}

	// Batch check which comments the caller has liked
	var likedMap map[string]bool
	if callerIDStr != "" {
		callerID, err := bson.ObjectIDFromHex(callerIDStr)
		if err == nil {
			ids := make([]bson.ObjectID, 0, len(comments))
			for _, c := range comments {
				ids = append(ids, c.ID)
			}
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

func (s *service) DeleteComment(ctx context.Context, requesterIDStr, commentIDStr string) error {
	requesterID, err := bson.ObjectIDFromHex(requesterIDStr)
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

	isCommentAuthor := comment.AuthorID == requesterID

	if !isCommentAuthor {
		// Check if the requester is the poem author — poem authors can remove
		// any comment from their own poem.
		var poem models.Poem
		if findErr := s.poemsCol.FindOne(ctx, bson.M{"_id": comment.PoemID, "isDeleted": false}).Decode(&poem); findErr != nil {
			return errors.New("unauthorized: you can only delete your own comments")
		}
		if poem.AuthorID != requesterID {
			return errors.New("unauthorized: you can only delete your own comments")
		}
		// Poem author path — delete without author filter
		if err := s.repo.ForceDeleteComment(ctx, commentID); err != nil {
			return err
		}
	} else {
		// Comment author path — delete with author filter
		if err := s.repo.SoftDeleteComment(ctx, commentID, requesterID); err != nil {
			return err
		}
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.repo.DecrementPoemComments(bgCtx, comment.PoemID)
	}()
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
		_ = s.repo.DecrementCommentLikes(ctx, commentID)

		updated, _ := s.repo.GetCommentByID(ctx, commentID)
		newCount := 0
		if updated != nil {
			newCount = updated.LikesCount
		}
		return false, newCount, nil
	}

	if err := s.repo.LikeComment(ctx, userID, commentID); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return true, comment.LikesCount, nil
		}
		return false, 0, err
	}

	_ = s.repo.IncrementCommentLikes(ctx, commentID)

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// Notify comment author
		if comment.AuthorID != userID && s.notifService != nil {
			liker, _ := s.userRepo.GetUserByID(bgCtx, userID)
			name := "Someone"
			if liker != nil {
				name = liker.DisplayName
			}
			_ = s.notifService.Send(bgCtx, models.SendNotificationRequest{
				RecipientID:  comment.AuthorID,
				ActorID:      userID,
				Type:         models.NotifTypeCommentLiked,
				ResourceType: "poem",
				ResourceID:   comment.PoemID.Hex(),
				// ResourceType: "comment",
				// ResourceID:   commentIDStr,
				Title:    name,
				Body:     "liked your comment",
				GroupKey: "clike:" + commentIDStr,
			})
		}
	}()

	updated, _ := s.repo.GetCommentByID(ctx, commentID)
	newCount := comment.LikesCount + 1
	if updated != nil {
		newCount = updated.LikesCount
	}

	return true, newCount, nil
}

// ── Reposts ──

func (s *service) GetPoemReposters(ctx context.Context, poemIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error) {
	poemID, err := bson.ObjectIDFromHex(poemIDStr)
	if err != nil {
		return nil, false, errors.New("invalid poem id")
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
			return nil, false, errors.New("invalid before cursor")
		}
		beforeID = &id
	}

	reposts, err := s.repo.GetPoemReposters(ctx, poemID, limit+1, beforeID)
	if err != nil {
		return nil, false, err
	}

	hasMore := len(reposts) > limit
	if hasMore {
		reposts = reposts[:limit]
	}

	// Extract unique author IDs (a user can only repost once, but be safe)
	seen := make(map[bson.ObjectID]bool, len(reposts))
	ids := make([]bson.ObjectID, 0, len(reposts))
	for _, rp := range reposts {
		if !seen[rp.AuthorID] {
			seen[rp.AuthorID] = true
			ids = append(ids, rp.AuthorID)
		}
	}

	userMap, err := s.userRepo.GetUsersByIDs(ctx, ids)
	if err != nil {
		return nil, false, err
	}

	results := make([]models.UserSearchResult, 0, len(ids))
	for _, id := range ids {
		user, ok := userMap[id]
		if !ok {
			continue // skip deleted / deactivated users
		}
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
		"authorId":   userID,
		"isRepost":   true,
		"originalId": poemID,
		"isDeleted":  false,
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
		_ = s.repo.DecrementPoemReposts(ctx, poemID)

		var updated models.Poem
		_ = s.poemsCol.FindOne(ctx, bson.M{"_id": poemID, "isDeleted": false}).Decode(&updated)

		newCount := updated.RepostsCount
		if newCount < 0 {
			newCount = 0
		}
		return false, newCount, nil
	}

	// Create repost document
	now := time.Now()
	repost := &models.Poem{
		AuthorID:    userID,
		IsRepost:    true,
		OriginalID:  &poemID,
		Title:       original.Title,
		ContentJSON: original.ContentJSON,
		PlainText:   original.PlainText,
		Hashtags:    original.Hashtags,
		Visibility:  models.PoemVisibilityPublic,
		// A repost is public the moment it's created. PublishedAt is the home
		// feed sort key (sorted DESC with no createdAt fallback), so without it
		// the repost sorts below every post that has one and never surfaces.
		PublishedAt: &now,
	}
	repost.CreatedAt = now
	repost.UpdatedAt = now

	res, err := s.poemsCol.InsertOne(ctx, repost)
	if err != nil {
		return false, 0, err
	}
	repost.ID = res.InsertedID.(bson.ObjectID)

	_ = s.repo.IncrementPoemReposts(ctx, poemID)

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.userRepo.IncrementPostsCount(bgCtx, userID)

		// Notify original author
		if s.notifService != nil {
			reposter, _ := s.userRepo.GetUserByID(bgCtx, userID)
			name := "Someone"
			if reposter != nil {
				name = reposter.DisplayName
			}
			_ = s.notifService.Send(bgCtx, models.SendNotificationRequest{
				RecipientID:  original.AuthorID,
				ActorID:      userID,
				Type:         models.NotifTypeReposted,
				ResourceType: "poem",
				ResourceID:   poemIDStr,
				Title:        name,
				Body:         "reposted your post \"" + original.Title + "\"",
				GroupKey:     "repost:" + poemIDStr,
			})
		}
	}()

	var updated models.Poem
	_ = s.poemsCol.FindOne(ctx, bson.M{"_id": poemID, "isDeleted": false}).Decode(&updated)

	return true, updated.RepostsCount, nil
}

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

	var likedMap map[string]bool
	var repostedMap map[string]bool
	if callerIDStr != "" {
		callerID, err := bson.ObjectIDFromHex(callerIDStr)
		if err == nil {
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

	// For each repost, fetch the original poem and embed it
	responses := make([]models.PoemResponse, 0, len(reposts))
	for _, rp := range reposts {
		resp := s.buildRepostResponse(ctx, &rp, likedMap, repostedMap)
		responses = append(responses, resp)
	}

	return &models.FeedPage{Poems: responses, HasMore: hasMore}, nil
}

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

	if resp.Hashtags == nil {
		resp.Hashtags = []string{}
	}
	resp.Mentions = append([]models.MentionedUser{}, resp.Mentions...)

	return resp
}
