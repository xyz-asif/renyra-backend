package social

import (
	"context"
	"time"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
	// Poem likes
	LikePoem(ctx context.Context, userID, poemID bson.ObjectID) error
	UnlikePoem(ctx context.Context, userID, poemID bson.ObjectID) error
	IsPoemLiked(ctx context.Context, userID, poemID bson.ObjectID) (bool, error)
	IsPoemLikedMany(ctx context.Context, userID bson.ObjectID, poemIDs []bson.ObjectID) (map[string]bool, error)
	IsPoemRepostedMany(ctx context.Context, userID bson.ObjectID, poemIDs []bson.ObjectID) (map[string]bool, error)
	GetPoemLikers(ctx context.Context, poemID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.PoemLike, error)

	// Comments
	CreateComment(ctx context.Context, comment *models.Comment) error
	GetCommentByID(ctx context.Context, commentID bson.ObjectID) (*models.Comment, error)
	GetCommentsByPoem(ctx context.Context, poemID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Comment, error)
	SoftDeleteComment(ctx context.Context, commentID, authorID bson.ObjectID) error
	ForceDeleteComment(ctx context.Context, commentID bson.ObjectID) error
	IncrementCommentLikes(ctx context.Context, commentID bson.ObjectID) error
	DecrementCommentLikes(ctx context.Context, commentID bson.ObjectID) error

	// Comment likes
	LikeComment(ctx context.Context, userID, commentID bson.ObjectID) error
	UnlikeComment(ctx context.Context, userID, commentID bson.ObjectID) error
	IsCommentLiked(ctx context.Context, userID, commentID bson.ObjectID) (bool, error)
	IsCommentLikedMany(ctx context.Context, userID bson.ObjectID, commentIDs []bson.ObjectID) (map[string]bool, error)

	// Poem counters (used when toggling likes/comments/reposts)
	IncrementPoemLikes(ctx context.Context, poemID bson.ObjectID) error
	DecrementPoemLikes(ctx context.Context, poemID bson.ObjectID) error
	IncrementPoemComments(ctx context.Context, poemID bson.ObjectID) error
	DecrementPoemComments(ctx context.Context, poemID bson.ObjectID) error
	IncrementPoemReposts(ctx context.Context, poemID bson.ObjectID) error
	DecrementPoemReposts(ctx context.Context, poemID bson.ObjectID) error
}

type repository struct {
	poemLikes    *mongo.Collection
	comments     *mongo.Collection
	commentLikes *mongo.Collection
	poems        *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{
		poemLikes:    db.Collection("poem_likes"),
		comments:     db.Collection("comments"),
		commentLikes: db.Collection("comment_likes"),
		poems:        db.Collection("poems"),
	}
}

// ── Poem likes ──

func (r *repository) LikePoem(ctx context.Context, userID, poemID bson.ObjectID) error {
	_, err := r.poemLikes.InsertOne(ctx, models.PoemLike{
		UserID: userID, PoemID: poemID, CreatedAt: time.Now(),
	})
	return err
}

func (r *repository) UnlikePoem(ctx context.Context, userID, poemID bson.ObjectID) error {
	_, err := r.poemLikes.DeleteOne(ctx, bson.M{"userId": userID, "poemId": poemID})
	return err
}

func (r *repository) IsPoemLiked(ctx context.Context, userID, poemID bson.ObjectID) (bool, error) {
	count, err := r.poemLikes.CountDocuments(ctx, bson.M{"userId": userID, "poemId": poemID})
	return count > 0, err
}

func (r *repository) IsPoemLikedMany(ctx context.Context, userID bson.ObjectID, poemIDs []bson.ObjectID) (map[string]bool, error) {
	cursor, err := r.poemLikes.Find(ctx, bson.M{
		"userId": userID,
		"poemId": bson.M{"$in": poemIDs},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var likes []models.PoemLike
	if err := cursor.All(ctx, &likes); err != nil {
		return nil, err
	}
	result := make(map[string]bool)
	for _, l := range likes {
		result[l.PoemID.Hex()] = true
	}
	return result, nil
}

func (r *repository) IsPoemRepostedMany(ctx context.Context, userID bson.ObjectID, poemIDs []bson.ObjectID) (map[string]bool, error) {
	cursor, err := r.poems.Find(ctx, bson.M{
		"authorId":   userID,
		"isRepost":   true,
		"originalId": bson.M{"$in": poemIDs},
		"isDeleted":  false,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var reposts []models.Poem
	if err := cursor.All(ctx, &reposts); err != nil {
		return nil, err
	}
	result := make(map[string]bool)
	for _, rp := range reposts {
		if rp.OriginalID != nil {
			result[rp.OriginalID.Hex()] = true
		}
	}
	return result, nil
}

func (r *repository) GetPoemLikers(ctx context.Context, poemID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.PoemLike, error) {
	filter := bson.M{"poemId": poemID}
	if beforeID != nil {
		filter["_id"] = bson.M{"$lt": *beforeID}
	}
	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit))
	cursor, err := r.poemLikes.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var result []models.PoemLike
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// ── Comments ──

func (r *repository) CreateComment(ctx context.Context, comment *models.Comment) error {
	comment.CreatedAt = time.Now()
	comment.UpdatedAt = time.Now()
	comment.IsDeleted = false
	comment.LikesCount = 0
	res, err := r.comments.InsertOne(ctx, comment)
	if err != nil {
		return err
	}
	comment.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetCommentByID(ctx context.Context, commentID bson.ObjectID) (*models.Comment, error) {
	var c models.Comment
	err := r.comments.FindOne(ctx, bson.M{"_id": commentID, "isDeleted": false}).Decode(&c)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *repository) GetCommentsByPoem(ctx context.Context, poemID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Comment, error) {
	filter := bson.M{"poemId": poemID, "isDeleted": false}
	if beforeID != nil {
		filter["_id"] = bson.M{"$lt": *beforeID}
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}). // newest first
		SetLimit(int64(limit))
	cursor, err := r.comments.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var result []models.Comment
	if err := cursor.All(ctx, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *repository) SoftDeleteComment(ctx context.Context, commentID, authorID bson.ObjectID) error {
	_, err := r.comments.UpdateOne(ctx,
		bson.M{"_id": commentID, "authorId": authorID},
		bson.M{"$set": bson.M{"isDeleted": true, "content": "This comment was deleted", "updatedAt": time.Now()}},
	)
	return err
}

// ForceDeleteComment soft-deletes a comment without checking authorId.
// Used when the poem author (not the comment author) removes a comment from their poem.
func (r *repository) ForceDeleteComment(ctx context.Context, commentID bson.ObjectID) error {
	_, err := r.comments.UpdateOne(ctx,
		bson.M{"_id": commentID},
		bson.M{"$set": bson.M{"isDeleted": true, "content": "This comment was deleted", "updatedAt": time.Now()}},
	)
	return err
}

func (r *repository) IncrementCommentLikes(ctx context.Context, commentID bson.ObjectID) error {
	_, err := r.comments.UpdateOne(ctx, bson.M{"_id": commentID}, bson.M{"$inc": bson.M{"likesCount": 1}})
	return err
}

func (r *repository) DecrementCommentLikes(ctx context.Context, commentID bson.ObjectID) error {
	_, err := r.comments.UpdateOne(ctx,
		bson.M{"_id": commentID, "likesCount": bson.M{"$gt": 0}},
		bson.M{"$inc": bson.M{"likesCount": -1}},
	)
	return err
}

// ── Comment likes ──

func (r *repository) LikeComment(ctx context.Context, userID, commentID bson.ObjectID) error {
	_, err := r.commentLikes.InsertOne(ctx, models.CommentLike{
		UserID: userID, CommentID: commentID, CreatedAt: time.Now(),
	})
	return err
}

func (r *repository) UnlikeComment(ctx context.Context, userID, commentID bson.ObjectID) error {
	_, err := r.commentLikes.DeleteOne(ctx, bson.M{"userId": userID, "commentId": commentID})
	return err
}

func (r *repository) IsCommentLiked(ctx context.Context, userID, commentID bson.ObjectID) (bool, error) {
	count, err := r.commentLikes.CountDocuments(ctx, bson.M{"userId": userID, "commentId": commentID})
	return count > 0, err
}

func (r *repository) IsCommentLikedMany(ctx context.Context, userID bson.ObjectID, commentIDs []bson.ObjectID) (map[string]bool, error) {
	cursor, err := r.commentLikes.Find(ctx, bson.M{
		"userId":    userID,
		"commentId": bson.M{"$in": commentIDs},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var likes []models.CommentLike
	if err := cursor.All(ctx, &likes); err != nil {
		return nil, err
	}
	result := make(map[string]bool)
	for _, l := range likes {
		result[l.CommentID.Hex()] = true
	}
	return result, nil
}

// ── Poem counters ──

func (r *repository) IncrementPoemLikes(ctx context.Context, poemID bson.ObjectID) error {
	_, err := r.poems.UpdateOne(ctx, bson.M{"_id": poemID}, bson.M{"$inc": bson.M{"likesCount": 1}})
	return err
}

func (r *repository) DecrementPoemLikes(ctx context.Context, poemID bson.ObjectID) error {
	_, err := r.poems.UpdateOne(ctx, bson.M{"_id": poemID, "likesCount": bson.M{"$gt": 0}}, bson.M{"$inc": bson.M{"likesCount": -1}})
	return err
}

func (r *repository) IncrementPoemComments(ctx context.Context, poemID bson.ObjectID) error {
	_, err := r.poems.UpdateOne(ctx, bson.M{"_id": poemID}, bson.M{"$inc": bson.M{"commentsCount": 1}})
	return err
}

func (r *repository) DecrementPoemComments(ctx context.Context, poemID bson.ObjectID) error {
	_, err := r.poems.UpdateOne(ctx, bson.M{"_id": poemID, "commentsCount": bson.M{"$gt": 0}}, bson.M{"$inc": bson.M{"commentsCount": -1}})
	return err
}

func (r *repository) IncrementPoemReposts(ctx context.Context, poemID bson.ObjectID) error {
	_, err := r.poems.UpdateOne(ctx, bson.M{"_id": poemID}, bson.M{"$inc": bson.M{"repostsCount": 1}})
	return err
}

func (r *repository) DecrementPoemReposts(ctx context.Context, poemID bson.ObjectID) error {
	_, err := r.poems.UpdateOne(ctx, bson.M{"_id": poemID, "repostsCount": bson.M{"$gt": 0}}, bson.M{"$inc": bson.M{"repostsCount": -1}})
	return err
}
