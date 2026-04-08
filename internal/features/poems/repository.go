package poems

import (
	"context"
	"time"

	"github.com/xyz-asif/renyra-backend/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
	Create(ctx context.Context, poem *models.Poem) error
	GetByID(ctx context.Context, poemID bson.ObjectID) (*models.Poem, error)
	Update(ctx context.Context, poemID bson.ObjectID, update PoemUpdateFields) (*models.Poem, error)
	SoftDelete(ctx context.Context, poemID, authorID bson.ObjectID) error
	GetByAuthor(ctx context.Context, authorID bson.ObjectID, limit int, beforeID *bson.ObjectID, includePrivate bool) ([]models.Poem, error)
	UpsertHashtags(ctx context.Context, tags []string) error
	DecrementHashtags(ctx context.Context, tags []string) error
}

type PoemUpdateFields struct {
	Title         string
	ContentJSON   string
	PlainText     string
	Hashtags      []string
	Mood          string
	IsOriginal    bool
	Visibility    string
	AudioURL      string
	AudioDuration int
	CoverColor    string
	Description   string
	TextAlign     string
	Mentions      []bson.ObjectID
}

type repository struct {
	poems    *mongo.Collection
	hashtags *mongo.Collection
	users    *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{
		poems:    db.Collection("poems"),
		hashtags: db.Collection("hashtags"),
		users:    db.Collection("users"),
	}
}

func (r *repository) Create(ctx context.Context, poem *models.Poem) error {
	poem.CreatedAt = time.Now()
	poem.UpdatedAt = time.Now()
	poem.IsDeleted = false
	poem.LikesCount = 0
	poem.CommentsCount = 0
	poem.RepostsCount = 0

	res, err := r.poems.InsertOne(ctx, poem)
	if err != nil {
		return err
	}
	poem.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetByID(ctx context.Context, poemID bson.ObjectID) (*models.Poem, error) {
	var poem models.Poem
	err := r.poems.FindOne(ctx, bson.M{"_id": poemID, "isDeleted": false}).Decode(&poem)
	if err != nil {
		return nil, err
	}
	return &poem, nil
}

func (r *repository) Update(ctx context.Context, poemID bson.ObjectID, fields PoemUpdateFields) (*models.Poem, error) {
	update := bson.M{
		"$set": bson.M{
			"title":         fields.Title,
			"contentJson":   fields.ContentJSON,
			"plainText":     fields.PlainText,
			"hashtags":      fields.Hashtags,
			"mood":          fields.Mood,
			"isOriginal":    fields.IsOriginal,
			"visibility":    fields.Visibility,
			"audioUrl":      fields.AudioURL,
			"audioDuration": fields.AudioDuration,
			"coverColor":    fields.CoverColor,
			"description":   fields.Description,
			"textAlign":     fields.TextAlign,
			"mentions":      fields.Mentions,
			"updatedAt":     time.Now(),
		},
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var poem models.Poem
	err := r.poems.FindOneAndUpdate(ctx,
		bson.M{"_id": poemID, "isDeleted": false},
		update,
		opts,
	).Decode(&poem)
	if err != nil {
		return nil, err
	}
	return &poem, nil
}

func (r *repository) SoftDelete(ctx context.Context, poemID, authorID bson.ObjectID) error {
	_, err := r.poems.UpdateOne(ctx,
		bson.M{"_id": poemID, "authorId": authorID, "isDeleted": false},
		bson.M{"$set": bson.M{"isDeleted": true, "updatedAt": time.Now()}},
	)
	return err
}

// GetByAuthor returns poems by a specific author with cursor-based pagination.
// includePrivate = true only when the caller IS the author (my poems endpoint).
// includePrivate = false for public profile view (only returns public poems).
func (r *repository) GetByAuthor(ctx context.Context, authorID bson.ObjectID, limit int, beforeID *bson.ObjectID, includePrivate bool) ([]models.Poem, error) {
	filter := bson.M{
		"authorId":  authorID,
		"isDeleted": false,
		"isRepost":  bson.M{"$ne": true},
	}

	if !includePrivate {
		filter["visibility"] = models.PoemVisibilityPublic
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

// UpsertHashtags increments usageCount for each tag, creating the document if it doesn't exist.
func (r *repository) UpsertHashtags(ctx context.Context, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	var models []mongo.WriteModel
	now := time.Now()

	for _, tag := range tags {
		if tag == "" {
			continue
		}
		
		model := mongo.NewUpdateOneModel().
			SetFilter(bson.M{"tag": tag}).
			SetUpdate(bson.M{
				"$inc": bson.M{"usageCount": 1},
				"$set": bson.M{"updatedAt": now},
				"$setOnInsert": bson.M{"tag": tag},
			}).
			SetUpsert(true)
			
		models = append(models, model)
	}

	if len(models) == 0 {
		return nil
	}

	_, err := r.hashtags.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	return err
}

// DecrementHashtags decrements usageCount when a poem is deleted or hashtags are removed.
// Never goes below 0.
func (r *repository) DecrementHashtags(ctx context.Context, tags []string) error {
	if len(tags) == 0 {
		return nil
	}

	var models []mongo.WriteModel
	now := time.Now()

	for _, tag := range tags {
		if tag == "" {
			continue
		}
		
		// Only decrement if usageCount > 0
		model := mongo.NewUpdateOneModel().
			SetFilter(bson.M{"tag": tag, "usageCount": bson.M{"$gt": 0}}).
			SetUpdate(bson.M{
				"$inc": bson.M{"usageCount": -1},
				"$set": bson.M{"updatedAt": now},
			})
			
		models = append(models, model)
	}

	if len(models) == 0 {
		return nil
	}

	_, err := r.hashtags.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false))
	return err
}
