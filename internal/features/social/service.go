package social

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/xyz-asif/gotodo/internal/features/notifications"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"github.com/xyz-asif/gotodo/internal/models"
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
	GetUserReposts(ctx context.Context, userIDStr string, limit int, before string) (*models.FeedPage, error)
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
		go func() { _ = s.repo.DecrementPoemLikes(context.Background(), poemID) }()
		newCount := poem.LikesCount - 1
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

	go func() {
		_ = s.repo.IncrementPoemLikes(context.Background(), poemID)

		// Notify poem author — but not if liking own poem
		if poem.AuthorID != userID && s.notifService != nil {
			liker, _ := s.userRepo.GetUserByID(context.Background(), userID)
			name := "Someone"
			if liker != nil {
				name = liker.DisplayName
			}
			_ = s.notifService.Send(context.Background(), models.SendNotificationRequest{
				RecipientID:  poem.AuthorID,
				ActorID:      userID,
				Type:         models.NotifTypePoemLiked,
				ResourceType: "poem",
				ResourceID:   poemIDStr,
				Title:        name,
				Body:         "liked your poem \"" + poem.Title + "\"",
				GroupKey:     "like:" + poemIDStr,
			})
		}
	}()

	return true, poem.LikesCount + 1, nil
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
		_ = s.repo.IncrementPoemComments(context.Background(), poemID)

		commenter, _ := s.userRepo.GetUserByID(context.Background(), authorID)
		name := "Someone"
		if commenter != nil {
			name = commenter.DisplayName
		}

		// Notify poem author (if not self-comment)
		if poem.AuthorID != authorID && s.notifService != nil {
			_ = s.notifService.Send(context.Background(), models.SendNotificationRequest{
				RecipientID:  poem.AuthorID,
				ActorID:      authorID,
				Type:         models.NotifTypeCommented,
				ResourceType: "poem",
				ResourceID:   poemIDStr,
				Title:        name,
				Body:         "commented on your poem",
				GroupKey:     "comment:" + poemIDStr,
			})
		}

		// Detect @mentions and notify each mentioned user
		s.processMentions(context.Background(), content, authorID, poemIDStr, comment.ID.Hex())
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
			ResourceType: "comment",
			ResourceID:   commentIDStr,
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

func (s *service) DeleteComment(ctx context.Context, authorIDStr, commentIDStr string) error {
	authorID, err := bson.ObjectIDFromHex(authorIDStr)
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
	if comment.AuthorID != authorID {
		return errors.New("unauthorized: only the comment author can delete this")
	}

	if err := s.repo.SoftDeleteComment(ctx, commentID, authorID); err != nil {
		return err
	}

	go func() { _ = s.repo.DecrementPoemComments(context.Background(), comment.PoemID) }()
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
		go func() { _ = s.repo.DecrementCommentLikes(context.Background(), commentID) }()
		newCount := comment.LikesCount - 1
		if newCount < 0 {
			newCount = 0
		}
		return false, newCount, nil
	}

	if err := s.repo.LikeComment(ctx, userID, commentID); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return true, comment.LikesCount, nil
		}
		return false, 0, err
	}

	go func() {
		_ = s.repo.IncrementCommentLikes(context.Background(), commentID)

		// Notify comment author
		if comment.AuthorID != userID && s.notifService != nil {
			liker, _ := s.userRepo.GetUserByID(context.Background(), userID)
			name := "Someone"
			if liker != nil {
				name = liker.DisplayName
			}
			_ = s.notifService.Send(context.Background(), models.SendNotificationRequest{
				RecipientID:  comment.AuthorID,
				ActorID:      userID,
				Type:         models.NotifTypeCommentLiked,
				ResourceType: "comment",
				ResourceID:   commentIDStr,
				Title:        name,
				Body:         "liked your comment",
				GroupKey:     "clike:" + commentIDStr,
			})
		}
	}()

	return true, comment.LikesCount + 1, nil
}

// ── Reposts ──

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
		go func() { _ = s.repo.DecrementPoemReposts(context.Background(), poemID) }()
		newCount := original.RepostsCount - 1
		if newCount < 0 {
			newCount = 0
		}
		return false, newCount, nil
	}

	// Create repost document
	repost := &models.Poem{
		AuthorID:    userID,
		IsRepost:    true,
		OriginalID:  &poemID,
		Title:       original.Title,
		ContentJSON: original.ContentJSON,
		PlainText:   original.PlainText,
		Hashtags:    original.Hashtags,
		Visibility:  models.PoemVisibilityPublic,
	}
	repost.CreatedAt = time.Now()
	repost.UpdatedAt = time.Now()

	res, err := s.poemsCol.InsertOne(ctx, repost)
	if err != nil {
		return false, 0, err
	}
	repost.ID = res.InsertedID.(bson.ObjectID)

	go func() {
		_ = s.repo.IncrementPoemReposts(context.Background(), poemID)
		_ = s.userRepo.IncrementPostsCount(context.Background(), userID)

		// Notify original author
		if s.notifService != nil {
			reposter, _ := s.userRepo.GetUserByID(context.Background(), userID)
			name := "Someone"
			if reposter != nil {
				name = reposter.DisplayName
			}
			_ = s.notifService.Send(context.Background(), models.SendNotificationRequest{
				RecipientID:  original.AuthorID,
				ActorID:      userID,
				Type:         models.NotifTypeReposted,
				ResourceType: "poem",
				ResourceID:   poemIDStr,
				Title:        name,
				Body:         "reposted your poem \"" + original.Title + "\"",
				GroupKey:     "repost:" + poemIDStr,
			})
		}
	}()

	return true, original.RepostsCount + 1, nil
}

func (s *service) GetUserReposts(ctx context.Context, userIDStr string, limit int, before string) (*models.FeedPage, error) {
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

	// For each repost, fetch the original poem and embed it
	responses := make([]models.PoemResponse, 0, len(reposts))
	for _, rp := range reposts {
		resp := s.buildRepostResponse(ctx, &rp)
		responses = append(responses, resp)
	}

	return &models.FeedPage{Poems: responses, HasMore: hasMore}, nil
}

func (s *service) buildRepostResponse(ctx context.Context, rp *models.Poem) models.PoemResponse {
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
			originalResp := models.PoemResponse{
				ID:            original.ID.Hex(),
				Title:         original.Title,
				ContentJSON:   original.ContentJSON,
				PlainText:     original.PlainText,
				Hashtags:      original.Hashtags,
				Mood:          original.Mood,
				IsOriginal:    original.IsOriginal,
				Visibility:    original.Visibility,
				AudioURL:      original.AudioURL,
				AudioDuration: original.AudioDuration,
				LikesCount:    original.LikesCount,
				CommentsCount: original.CommentsCount,
				RepostsCount:  original.RepostsCount,
				CreatedAt:     original.CreatedAt,
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

	return resp
}
