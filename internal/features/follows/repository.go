package follows

import (
	"context"
	"time"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
	Follow(ctx context.Context, followerID, followingID bson.ObjectID) error
	Unfollow(ctx context.Context, followerID, followingID bson.ObjectID) error
	IsFollowing(ctx context.Context, followerID, followingID bson.ObjectID) (bool, error)
	GetFollowers(ctx context.Context, userID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Follow, error)
	GetFollowing(ctx context.Context, userID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Follow, error)
	GetFollowingIDs(ctx context.Context, followerID bson.ObjectID) ([]bson.ObjectID, error)
	IsFollowingMany(ctx context.Context, followerID bson.ObjectID, targetIDs []bson.ObjectID) (map[string]bool, error)
	CountFollowers(ctx context.Context, userID bson.ObjectID) (int, error)
	CountFollowing(ctx context.Context, userID bson.ObjectID) (int, error)
}

type repository struct {
	follows *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{follows: db.Collection("follows")}
}

func (r *repository) Follow(ctx context.Context, followerID, followingID bson.ObjectID) error {
	follow := models.Follow{
		FollowerID:  followerID,
		FollowingID: followingID,
		CreatedAt:   time.Now(),
	}
	_, err := r.follows.InsertOne(ctx, follow)
	return err
}

func (r *repository) Unfollow(ctx context.Context, followerID, followingID bson.ObjectID) error {
	_, err := r.follows.DeleteOne(ctx, bson.M{
		"followerId":  followerID,
		"followingId": followingID,
	})
	return err
}

func (r *repository) IsFollowing(ctx context.Context, followerID, followingID bson.ObjectID) (bool, error) {
	count, err := r.follows.CountDocuments(ctx, bson.M{
		"followerId":  followerID,
		"followingId": followingID,
	})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *repository) GetFollowers(ctx context.Context, userID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Follow, error) {
	filter := bson.M{"followingId": userID}
	if beforeID != nil {
		filter["_id"] = bson.M{"$lt": *beforeID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.follows.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var result []models.Follow
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *repository) GetFollowing(ctx context.Context, userID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Follow, error) {
	filter := bson.M{"followerId": userID}
	if beforeID != nil {
		filter["_id"] = bson.M{"$lt": *beforeID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.follows.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var result []models.Follow
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetFollowingIDs returns all user IDs that a given user follows.
func (r *repository) GetFollowingIDs(ctx context.Context, followerID bson.ObjectID) ([]bson.ObjectID, error) {
	cursor, err := r.follows.Find(ctx,
		bson.M{"followerId": followerID},
		options.Find().SetProjection(bson.M{"followingId": 1}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var follows []models.Follow
	if err := cursor.All(ctx, &follows); err != nil {
		return nil, err
	}

	ids := make([]bson.ObjectID, 0, len(follows))
	for _, f := range follows {
		ids = append(ids, f.FollowingID)
	}
	return ids, nil
}

// IsFollowingMany returns a map of targetUserID.Hex() → bool for efficient batch checks.
func (r *repository) IsFollowingMany(ctx context.Context, followerID bson.ObjectID, targetIDs []bson.ObjectID) (map[string]bool, error) {
	cursor, err := r.follows.Find(ctx, bson.M{
		"followerId":  followerID,
		"followingId": bson.M{"$in": targetIDs},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var follows []models.Follow
	if err := cursor.All(ctx, &follows); err != nil {
		return nil, err
	}

	result := make(map[string]bool)
	for _, f := range follows {
		result[f.FollowingID.Hex()] = true
	}
	return result, nil
}

// CountFollowers counts the number of followers for a user from the follows collection.
func (r *repository) CountFollowers(ctx context.Context, userID bson.ObjectID) (int, error) {
	count, err := r.follows.CountDocuments(ctx, bson.M{"followingId": userID})
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

// CountFollowing counts the number of users that a given user follows.
func (r *repository) CountFollowing(ctx context.Context, userID bson.ObjectID) (int, error) {
	count, err := r.follows.CountDocuments(ctx, bson.M{"followerId": userID})
	if err != nil {
		return 0, err
	}
	return int(count), nil
}
