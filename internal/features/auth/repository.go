package auth

import (
	"context"
	"time"

	"github.com/xyz-asif/renyra-backend/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type Repository interface {
	SaveRefreshToken(ctx context.Context, userID bson.ObjectID, tokenHash string, expiresAt time.Time) error
	// ConsumeRefreshToken atomically finds AND deletes the token in a single
	// operation, returning the deleted document. Atomicity guarantees that when
	// two requests present the same token concurrently, exactly ONE consumes it
	// (the other gets mongo.ErrNoDocuments) — preventing double-issuance and
	// orphaned refresh tokens under a rotation race.
	ConsumeRefreshToken(ctx context.Context, tokenHash string) (*models.RefreshToken, error)
	DeleteRefreshToken(ctx context.Context, tokenHash string) error
	DeleteAllUserTokens(ctx context.Context, userID bson.ObjectID) error
}

type repository struct {
	col *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{col: db.Collection("refresh_tokens")}
}

func (r *repository) SaveRefreshToken(ctx context.Context, userID bson.ObjectID, tokenHash string, expiresAt time.Time) error {
	_, err := r.col.InsertOne(ctx, models.RefreshToken{
		ID:        bson.NewObjectID(),
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	})
	return err
}

func (r *repository) ConsumeRefreshToken(ctx context.Context, tokenHash string) (*models.RefreshToken, error) {
	var rt models.RefreshToken
	err := r.col.FindOneAndDelete(ctx, bson.M{"tokenHash": tokenHash}).Decode(&rt)
	if err != nil {
		return nil, err
	}
	return &rt, nil
}

func (r *repository) DeleteRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := r.col.DeleteOne(ctx, bson.M{"tokenHash": tokenHash})
	return err
}

func (r *repository) DeleteAllUserTokens(ctx context.Context, userID bson.ObjectID) error {
	_, err := r.col.DeleteMany(ctx, bson.M{"userId": userID})
	return err
}
