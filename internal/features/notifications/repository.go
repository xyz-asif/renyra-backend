package notifications

import (
	"context"
	"time"

	"github.com/xyz-asif/renyra-backend/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
	Create(ctx context.Context, notif *models.Notification) error
	GetByRecipient(ctx context.Context, recipientID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Notification, error)
	GetUnreadCount(ctx context.Context, recipientID bson.ObjectID) (int64, error)
	MarkAsRead(ctx context.Context, notifID, recipientID bson.ObjectID) error
	MarkAllAsRead(ctx context.Context, recipientID bson.ObjectID) error
	FindByGroupKey(ctx context.Context, recipientID bson.ObjectID, groupKey string) (*models.Notification, error)
	UpdateGroupedNotification(ctx context.Context, notifID bson.ObjectID, title, body string) error
}

type repository struct {
	collection *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{
		collection: db.Collection("notifications"),
	}
}

func (r *repository) Create(ctx context.Context, notif *models.Notification) error {
	notif.CreatedAt = time.Now()
	notif.IsRead = false

	res, err := r.collection.InsertOne(ctx, notif)
	if err != nil {
		return err
	}
	notif.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetByRecipient(ctx context.Context, recipientID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Notification, error) {
	filter := bson.M{"recipientId": recipientID}
	if beforeID != nil {
		filter["_id"] = bson.M{"$lt": *beforeID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var notifs []models.Notification
	if err := cursor.All(ctx, &notifs); err != nil {
		return nil, err
	}
	return notifs, nil
}

func (r *repository) GetUnreadCount(ctx context.Context, recipientID bson.ObjectID) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{
		"recipientId": recipientID,
		"isRead":      false,
	})
}

func (r *repository) MarkAsRead(ctx context.Context, notifID, recipientID bson.ObjectID) error {
	// Ensure the notification belongs to the recipient (security check)
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": notifID, "recipientId": recipientID},
		bson.M{"$set": bson.M{"isRead": true}},
	)
	return err
}

func (r *repository) MarkAllAsRead(ctx context.Context, recipientID bson.ObjectID) error {
	_, err := r.collection.UpdateMany(ctx,
		bson.M{"recipientId": recipientID, "isRead": false},
		bson.M{"$set": bson.M{"isRead": true}},
	)
	return err
}

func (r *repository) FindByGroupKey(ctx context.Context, recipientID bson.ObjectID, groupKey string) (*models.Notification, error) {
	var notif models.Notification
	err := r.collection.FindOne(ctx, bson.M{
		"recipientId": recipientID,
		"groupKey":    groupKey,
		"isRead":      false, // only group with unread notifications
	}).Decode(&notif)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &notif, nil
}

func (r *repository) UpdateGroupedNotification(ctx context.Context, notifID bson.ObjectID, title, body string) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": notifID},
		bson.M{"$set": bson.M{
			"title":     title,
			"body":      body,
			"createdAt": time.Now(), // bump to top of list
		}},
	)
	return err
}
