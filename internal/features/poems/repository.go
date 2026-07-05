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
	GetByAuthor(ctx context.Context, authorID bson.ObjectID, limit int, beforeID *bson.ObjectID, includePrivate bool, visibilityFilter string) ([]models.Poem, error)
	CountPublicByAuthor(ctx context.Context, authorID bson.ObjectID) (int, error)
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
	FontFamily    string
	Mentions      []bson.ObjectID
	// PublishedAt is non-nil only on a private→public transition.
	// The repository sets it in the document only when provided.
	PublishedAt   *time.Time
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
	now := time.Now()
	poem.CreatedAt = now
	poem.UpdatedAt = now
	poem.IsDeleted = false
	poem.LikesCount = 0
	poem.CommentsCount = 0
	poem.RepostsCount = 0

	// Set publishedAt immediately for poems created directly as public so they
	// sort correctly alongside drafts that are later published.
	if poem.Visibility == models.PoemVisibilityPublic {
		poem.PublishedAt = &now
	}

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
	set := bson.M{
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
		"fontFamily":    fields.FontFamily,
		"mentions":      fields.Mentions,
		"updatedAt":     time.Now(),
	}

	// Only stamp publishedAt on the private→public transition.
	// The service sets this field; we must never overwrite an existing publishedAt.
	if fields.PublishedAt != nil {
		set["publishedAt"] = fields.PublishedAt
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var poem models.Poem
	err := r.poems.FindOneAndUpdate(ctx,
		bson.M{"_id": poemID, "isDeleted": false},
		bson.M{"$set": set},
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
//
// Sort order: publishedAt DESC, _id DESC.
// Poems without publishedAt (drafts, legacy data) sort after all published poems.
//
// The cursor is still the last poem's _id (API unchanged). We look up the cursor
// poem's publishedAt to build an accurate compound keyset filter.
func (r *repository) GetByAuthor(ctx context.Context, authorID bson.ObjectID, limit int, beforeID *bson.ObjectID, includePrivate bool, visibilityFilter string) ([]models.Poem, error) {
	filter := bson.M{
		"authorId":  authorID,
		"isDeleted": false,
		"isRepost":  bson.M{"$ne": true},
	}

	// An explicit visibility filter (e.g. "public" for the Posts tab, "private"
	// for the Drafts tab) takes precedence so each tab can paginate its own type
	// independently. Falls back to the includePrivate gate when not specified.
	if visibilityFilter != "" {
		filter["visibility"] = visibilityFilter
	} else if !includePrivate {
		filter["visibility"] = models.PoemVisibilityPublic
	}

	if beforeID != nil {
		// Look up the cursor poem to get its publishedAt.  One indexed read; cost
		// is negligible and keeps the public API (before=<id>) unchanged.
		var cursorPoem models.Poem
		lookupErr := r.poems.FindOne(ctx, bson.M{"_id": *beforeID}).Decode(&cursorPoem)

		if lookupErr == nil && cursorPoem.PublishedAt != nil {
			// Compound keyset: items that sort strictly after the cursor position.
			//   - publishedAt older than cursor's publishedAt   (includes null — see $not/$gte)
			//   - OR same publishedAt with a smaller _id        (tiebreaker)
			// Using $not/$gte so that null/missing publishedAt docs are captured by
			// the first branch (MongoDB excludes null from straight $lt comparisons).
			filter["$or"] = []bson.M{
				{"publishedAt": bson.M{"$not": bson.M{"$gte": *cursorPoem.PublishedAt}}},
				{"publishedAt": *cursorPoem.PublishedAt, "_id": bson.M{"$lt": *beforeID}},
			}
		} else {
			// Cursor poem has no publishedAt (draft or legacy data).  All items that
			// sort after it also have no publishedAt, so _id is the correct key.
			filter["_id"] = bson.M{"$lt": *beforeID}
		}
	}

	opts := options.Find().
		SetSort(bson.D{
			{Key: "publishedAt", Value: -1},
			{Key: "_id", Value: -1},
		}).
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

// CountPublicByAuthor returns the number of a user's published, non-repost poems.
// This is the "Poems" stat shown on profiles — computed live (like followers/
// following counts) so it stays accurate regardless of drafts, reposts, or how
// many pages the client has scrolled. Mirrors the GetByAuthor "public posts"
// filter exactly.
func (r *repository) CountPublicByAuthor(ctx context.Context, authorID bson.ObjectID) (int, error) {
	count, err := r.poems.CountDocuments(ctx, bson.M{
		"authorId":   authorID,
		"isDeleted":  false,
		"isRepost":   bson.M{"$ne": true},
		"visibility": models.PoemVisibilityPublic,
	})
	if err != nil {
		return 0, err
	}
	return int(count), nil
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
