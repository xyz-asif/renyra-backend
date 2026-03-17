package feed

import (
	"context"
	"regexp"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
	GetHomeFeed(ctx context.Context, authorIDs []bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Poem, error)
	GetExploreFeed(ctx context.Context, hashtag string, limit int, offset int) ([]models.Poem, error)
	GetAudioFeed(ctx context.Context, limit int, offset int) ([]models.Poem, error)
	SearchPoems(ctx context.Context, query string, limit int, beforeID *bson.ObjectID) ([]models.Poem, error)
	SearchUsers(ctx context.Context, query string, limit int, skip int) ([]models.User, int64, error)
}

type repository struct {
	poems *mongo.Collection
	users *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{
		poems: db.Collection("poems"),
		users: db.Collection("users"),
	}
}

// GetHomeFeed returns poems from a list of author IDs, cursor-paginated, newest first.
func (r *repository) GetHomeFeed(ctx context.Context, authorIDs []bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Poem, error) {
	if len(authorIDs) == 0 {
		return []models.Poem{}, nil
	}

	filter := bson.M{
		"authorId":   bson.M{"$in": authorIDs},
		"visibility": models.PoemVisibilityPublic,
		"isDeleted":  false,
	}
	if beforeID != nil {
		filter["_id"] = bson.M{"$lt": *beforeID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.poems.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var result []models.Poem
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// GetExploreFeed returns public poems scored by engagement + recency.
//
// Scoring formula (Reddit/HN inspired):
//   score = (likes × 3) + (comments × 2) + (reposts × 1.5) - (hoursSincePosted × 0.5)
//
// This is computed server-side using MongoDB $addFields aggregation.
func (r *repository) GetExploreFeed(ctx context.Context, hashtag string, limit int, offset int) ([]models.Poem, error) {
	matchFilter := bson.M{
		"visibility": models.PoemVisibilityPublic,
		"isDeleted":  false,
	}
	if hashtag != "" {
		matchFilter["hashtags"] = hashtag
	}

	// Aggregation pipeline: filter → compute score → sort by score desc → limit
	pipeline := mongo.Pipeline{
		// Stage 1: filter
		{{Key: "$match", Value: matchFilter}},

		// Stage 2: compute engagement score
		// hoursSincePosted = (now_unix - createdAt_unix) / 3600
		// score = (likes*3) + (comments*2) + (reposts*1.5) - (hoursSince * 0.5)
		{{Key: "$addFields", Value: bson.M{
			"engagementScore": bson.M{
				"$subtract": []interface{}{
					bson.M{"$add": []interface{}{
						bson.M{"$multiply": []interface{}{"$likesCount", 3}},
						bson.M{"$multiply": []interface{}{"$commentsCount", 2}},
						bson.M{"$multiply": []interface{}{"$repostsCount", 1.5}},
					}},
					bson.M{"$multiply": []interface{}{
						bson.M{"$divide": []interface{}{
							bson.M{"$subtract": []interface{}{
								bson.M{"$toLong": "$$NOW"},
								bson.M{"$toLong": "$createdAt"},
							}},
							3600000, // ms → hours
						}},
						0.5,
					}},
				},
			},
		}}},

		// Stage 3: sort by score descending, then by _id descending for stable pagination
		{{Key: "$sort", Value: bson.D{
			{Key: "engagementScore", Value: -1},
			{Key: "_id", Value: -1},
		}}},

		// Stage 4: skip for offset pagination
		{{Key: "$skip", Value: int64(offset)}},

		// Stage 5: limit
		{{Key: "$limit", Value: int64(limit)}},
	}

	cursor, err := r.poems.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var result []models.Poem
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SearchPoems searches poem title and plainText using MongoDB text index.
func (r *repository) SearchPoems(ctx context.Context, query string, limit int, beforeID *bson.ObjectID) ([]models.Poem, error) {
	filter := bson.M{
		"$text":      bson.M{"$search": query},
		"visibility": models.PoemVisibilityPublic,
		"isDeleted":  false,
	}
	if beforeID != nil {
		filter["_id"] = bson.M{"$lt": *beforeID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "score", Value: bson.M{"$meta": "textScore"}}, {Key: "_id", Value: -1}}).
		SetProjection(bson.M{"score": bson.M{"$meta": "textScore"}}).
		SetLimit(int64(limit))

	cursor, err := r.poems.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var result []models.Poem
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// SearchUsers searches users by displayName or username using case-insensitive regex.
func (r *repository) SearchUsers(ctx context.Context, query string, limit int, skip int) ([]models.User, int64, error) {
	safeQuery := regexp.QuoteMeta(query)
	filter := bson.M{
		"$or": []bson.M{
			{"displayName": bson.M{"$regex": safeQuery, "$options": "i"}},
			{"username": bson.M{"$regex": safeQuery, "$options": "i"}},
		},
	}

	total, _ := r.users.CountDocuments(ctx, filter)

	opts := options.Find().
		SetLimit(int64(limit)).
		SetSkip(int64(skip)).
		SetSort(bson.D{{Key: "followersCount", Value: -1}}) // most-followed first

	cursor, err := r.users.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var result []models.User
	if err := cursor.All(ctx, &result); err != nil {
		return nil, 0, err
	}
	return result, total, nil
}

// GetAudioFeed returns public poems that have audio, scored by engagement + recency.
func (r *repository) GetAudioFeed(ctx context.Context, limit int, offset int) ([]models.Poem, error) {
	matchFilter := bson.M{
		"visibility": models.PoemVisibilityPublic,
		"isDeleted":  false,
		"audioUrl":   bson.M{"$exists": true, "$ne": ""},
	}

	// Same engagement scoring as explore feed
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: matchFilter}},
		{{Key: "$addFields", Value: bson.M{
			"engagementScore": bson.M{
				"$subtract": []interface{}{
					bson.M{"$add": []interface{}{
						bson.M{"$multiply": []interface{}{"$likesCount", 3}},
						bson.M{"$multiply": []interface{}{"$commentsCount", 2}},
						bson.M{"$multiply": []interface{}{"$repostsCount", 1.5}},
					}},
					bson.M{"$multiply": []interface{}{
						bson.M{"$divide": []interface{}{
							bson.M{"$subtract": []interface{}{
								bson.M{"$toLong": "$$NOW"},
								bson.M{"$toLong": "$createdAt"},
							}},
							3600000,
						}},
						0.5,
					}},
				},
			},
		}}},
		{{Key: "$sort", Value: bson.D{
			{Key: "engagementScore", Value: -1},
			{Key: "_id", Value: -1},
		}}},
		{{Key: "$skip", Value: int64(offset)}},
		{{Key: "$limit", Value: int64(limit)}},
	}

	cursor, err := r.poems.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var result []models.Poem
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}
