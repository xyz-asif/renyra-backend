package profile

import (
	"context"
	"errors"
	"time"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
	UpdateProfile(ctx context.Context, userID bson.ObjectID, update ProfileUpdateRequest) (*models.User, error)
	SetUsername(ctx context.Context, userID bson.ObjectID, username string) (*models.User, error)
	IsUsernameTaken(ctx context.Context, username string) (bool, error)
	GetByID(ctx context.Context, userID bson.ObjectID) (*models.User, error)
}

type repository struct {
	users *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{
		users: db.Collection("users"),
	}
}

// ProfileUpdateRequest — fields allowed in setup
type ProfileUpdateRequest struct {
	DisplayName   string
	Bio           string
	ExternalLink  string
	PhotoURL      string
	CoverImageURL string
}

func (r *repository) UpdateProfile(ctx context.Context, userID bson.ObjectID, req ProfileUpdateRequest) (*models.User, error) {
	update := bson.M{
		"$set": bson.M{
			"displayName":    req.DisplayName,
			"bio":            req.Bio,
			"externalLink":   req.ExternalLink,
			"photoURL":       req.PhotoURL,
			"coverImageURL":  req.CoverImageURL,
			"isProfileSetup": true,
			"updatedAt":      time.Now(),
		},
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var user models.User
	err := r.users.FindOneAndUpdate(ctx, bson.M{"_id": userID}, update, opts).Decode(&user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *repository) SetUsername(ctx context.Context, userID bson.ObjectID, username string) (*models.User, error) {
	// Only allow setting username if not already set
	filter := bson.M{
		"_id":      userID,
		"username": bson.M{"$exists": false}, // prevents overwrite — username is permanent once set
	}
	update := bson.M{
		"$set": bson.M{
			"username":  username,
			"updatedAt": time.Now(),
		},
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var user models.User
	err := r.users.FindOneAndUpdate(ctx, filter, update, opts).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, errors.New("username already set or user not found")
		}
		if mongo.IsDuplicateKeyError(err) {
			return nil, errors.New("username is already taken")
		}
		return nil, err
	}
	return &user, nil
}

func (r *repository) IsUsernameTaken(ctx context.Context, username string) (bool, error) {
	count, err := r.users.CountDocuments(ctx, bson.M{"username": username})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *repository) GetByID(ctx context.Context, userID bson.ObjectID) (*models.User, error) {
	var user models.User
	err := r.users.FindOne(ctx, bson.M{"_id": userID}).Decode(&user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}
