package follows

import (
	"context"
	"errors"

	"github.com/xyz-asif/gotodo/internal/features/notifications"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type Service interface {
	ToggleFollow(ctx context.Context, followerIDStr, followingIDStr string) (bool, error)
	GetPublicProfile(ctx context.Context, targetUserIDStr, callerIDStr string) (*models.PublicProfileResponse, error)
	GetFollowers(ctx context.Context, userIDStr, callerIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error)
	GetFollowing(ctx context.Context, userIDStr, callerIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error)
}

type service struct {
	repo         Repository
	userRepo     users.Repository
	notifService notifications.Service
}

func NewService(repo Repository, userRepo users.Repository, notifService notifications.Service) Service {
	return &service{repo: repo, userRepo: userRepo, notifService: notifService}
}

// ToggleFollow follows or unfollows a user. Returns true if now following, false if unfollowed.
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
		// Unfollow
		if err := s.repo.Unfollow(ctx, followerID, followingID); err != nil {
			return false, err
		}
		// Decrement counts synchronously so the values are correct immediately
		_ = s.userRepo.DecrementFollowersCount(ctx, followingID)
		_ = s.userRepo.DecrementFollowingCount(ctx, followerID)
		return false, nil
	}

	// Follow
	if err := s.repo.Follow(ctx, followerID, followingID); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return true, nil // already following due to race — idempotent
		}
		return false, err
	}
	// Increment counts synchronously so the values are correct immediately
	_ = s.userRepo.IncrementFollowersCount(ctx, followingID)
	_ = s.userRepo.IncrementFollowingCount(ctx, followerID)

	// Notify the followed user (non-critical, keep async)
	go func() {
		if s.notifService != nil {
			follower, _ := s.userRepo.GetUserByID(context.Background(), followerID)
			name := "Someone"
			if follower != nil {
				name = follower.DisplayName
			}
			_ = s.notifService.Send(context.Background(), models.SendNotificationRequest{
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

// GetPublicProfile returns a user's public profile with isFollowedByMe flag.
func (s *service) GetPublicProfile(ctx context.Context, targetUserIDStr, callerIDStr string) (*models.PublicProfileResponse, error) {
	targetUserID, err := bson.ObjectIDFromHex(targetUserIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}

	user, err := s.userRepo.GetUserByID(ctx, targetUserID)
	if err != nil || user == nil {
		return nil, errors.New("user not found")
	}

	// Compute live counts from the follows collection (source of truth)
	followersCount, _ := s.repo.CountFollowers(ctx, targetUserID)
	followingCount, _ := s.repo.CountFollowing(ctx, targetUserID)

	resp := &models.PublicProfileResponse{
		ID:             user.ID.Hex(),
		DisplayName:    user.DisplayName,
		Username:       user.Username,
		PhotoURL:       user.PhotoURL,
		CoverImageURL:  user.CoverImageURL,
		Bio:            user.Bio,
		ExternalLink:   user.ExternalLink,
		IsEditor:       user.IsEditor,
		PostsCount:     user.PostsCount,
		FollowersCount: followersCount,
		FollowingCount: followingCount,
		IsMe:           callerIDStr == targetUserIDStr,
	}

	// Check isFollowedByMe only if caller is authenticated and not viewing own profile
	if callerIDStr != "" && callerIDStr != targetUserIDStr {
		callerID, err := bson.ObjectIDFromHex(callerIDStr)
		if err == nil {
			isFollowing, _ := s.repo.IsFollowing(ctx, callerID, targetUserID)
			resp.IsFollowedByMe = isFollowing
		}
	}

	return resp, nil
}

// GetFollowers returns paginated list of a user's followers with isFollowing flags for the caller.
func (s *service) GetFollowers(ctx context.Context, userIDStr, callerIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, false, errors.New("invalid user id")
	}
	if limit <= 0 { limit = 20 }
	if limit > 50 { limit = 50 }

	var beforeID *bson.ObjectID
	if before != "" {
		id, err := bson.ObjectIDFromHex(before)
		if err != nil {
			return nil, false, errors.New("invalid before cursor")
		}
		beforeID = &id
	}

	follows, err := s.repo.GetFollowers(ctx, userID, limit+1, beforeID)
	if err != nil {
		return nil, false, err
	}

	hasMore := len(follows) > limit
	if hasMore {
		follows = follows[:limit]
	}

	return s.buildUserResults(ctx, follows, true, callerIDStr)
	// true = extract followerID (we're listing followers — the "follower" column)
}

// GetFollowing returns paginated list of users that a user follows.
func (s *service) GetFollowing(ctx context.Context, userIDStr, callerIDStr string, limit int, before string) ([]models.UserSearchResult, bool, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, false, errors.New("invalid user id")
	}
	if limit <= 0 { limit = 20 }
	if limit > 50 { limit = 50 }

	var beforeID *bson.ObjectID
	if before != "" {
		id, err := bson.ObjectIDFromHex(before)
		if err != nil {
			return nil, false, errors.New("invalid before cursor")
		}
		beforeID = &id
	}

	follows, err := s.repo.GetFollowing(ctx, userID, limit+1, beforeID)
	if err != nil {
		return nil, false, err
	}

	hasMore := len(follows) > limit
	if hasMore {
		follows = follows[:limit]
	}

	return s.buildUserResults(ctx, follows, false, callerIDStr)
	// false = extract followingID
}

// buildUserResults resolves user IDs from follow records into UserSearchResult objects.
// isFollowers: true = use FollowerID, false = use FollowingID
func (s *service) buildUserResults(ctx context.Context, follows []models.Follow, isFollowers bool, callerIDStr string) ([]models.UserSearchResult, bool, error) {
	if len(follows) == 0 {
		return []models.UserSearchResult{}, false, nil
	}

	ids := make([]bson.ObjectID, 0, len(follows))
	for _, f := range follows {
		if isFollowers {
			ids = append(ids, f.FollowerID)
		} else {
			ids = append(ids, f.FollowingID)
		}
	}

	userMap, err := s.userRepo.GetUsersByIDs(ctx, ids)
	if err != nil {
		return nil, false, err
	}

	// Batch check which ones the caller follows
	var followingMap map[string]bool
	if callerIDStr != "" {
		callerID, err := bson.ObjectIDFromHex(callerIDStr)
		if err == nil {
			followingMap, _ = s.repo.IsFollowingMany(ctx, callerID, ids)
		}
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
			IsFollowing: followingMap[user.ID.Hex()],
		})
	}

	return results, false, nil
}
