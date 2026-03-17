package connections

import (
	"context"
	"errors"
	"time"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type Repository interface {
	CreateConnection(ctx context.Context, conn *models.Connection) error
	GetConnectionByID(ctx context.Context, id bson.ObjectID) (*models.Connection, error)
	GetConnectionBetweenUsers(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Connection, error)
	GetConnectionsBetweenUserAndMany(ctx context.Context, userID bson.ObjectID, otherIDs []bson.ObjectID) (map[bson.ObjectID]*models.Connection, error)
	UpdateConnectionStatus(ctx context.Context, id bson.ObjectID, status string) error
	UpdateConnectionDirection(ctx context.Context, id bson.ObjectID, newSenderID, newReceiverID bson.ObjectID) error
	DeleteConnection(ctx context.Context, id bson.ObjectID) error
	GetUserConnections(ctx context.Context, userID bson.ObjectID, status string) ([]models.Connection, error)
}

type repository struct {
	db         *mongo.Database
	collection *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{
		db:         db,
		collection: db.Collection("connections"),
	}
}

func (r *repository) CreateConnection(ctx context.Context, conn *models.Connection) error {
	conn.CreatedAt = time.Now()
	conn.UpdatedAt = time.Now()

	res, err := r.collection.InsertOne(ctx, conn)
	if err != nil {
		return err
	}
	conn.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetConnectionByID(ctx context.Context, id bson.ObjectID) (*models.Connection, error) {
	var conn models.Connection
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&conn)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, errors.New("connection not found")
		}
		return nil, err
	}
	return &conn, nil
}

func (r *repository) GetConnectionBetweenUsers(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Connection, error) {
	var conn models.Connection
	// Check both directions
	filter := bson.M{
		"$or": []bson.M{
			{"senderId": user1ID, "receiverId": user2ID},
			{"senderId": user2ID, "receiverId": user1ID},
		},
	}

	err := r.collection.FindOne(ctx, filter).Decode(&conn)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil // Return nil, nil if no connection exists
		}
		return nil, err
	}
	return &conn, nil
}

// GetConnectionsBetweenUserAndMany batch-fetches connections between userID and each of otherIDs.
// Returns a map keyed by the OTHER user's ID.
func (r *repository) GetConnectionsBetweenUserAndMany(ctx context.Context, userID bson.ObjectID, otherIDs []bson.ObjectID) (map[bson.ObjectID]*models.Connection, error) {
	result := make(map[bson.ObjectID]*models.Connection, len(otherIDs))
	if len(otherIDs) == 0 {
		return result, nil
	}

	filter := bson.M{
		"$or": []bson.M{
			{"senderId": userID, "receiverId": bson.M{"$in": otherIDs}},
			{"receiverId": userID, "senderId": bson.M{"$in": otherIDs}},
		},
	}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var conns []models.Connection
	if err := cursor.All(ctx, &conns); err != nil {
		return nil, err
	}

	for i := range conns {
		c := &conns[i]
		if c.SenderID == userID {
			result[c.ReceiverID] = c
		} else {
			result[c.SenderID] = c
		}
	}

	return result, nil
}

func (r *repository) UpdateConnectionStatus(ctx context.Context, id bson.ObjectID, status string) error {
	update := bson.M{
		"$set": bson.M{
			"status":    status,
			"updatedAt": time.Now(),
		},
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	return err
}

func (r *repository) UpdateConnectionDirection(ctx context.Context, id bson.ObjectID, newSenderID, newReceiverID bson.ObjectID) error {
	update := bson.M{
		"$set": bson.M{
			"status":     models.ConnectionStatusPending,
			"senderId":   newSenderID,
			"receiverId": newReceiverID,
			"updatedAt":   time.Now(),
		},
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	return err
}

func (r *repository) DeleteConnection(ctx context.Context, id bson.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *repository) GetUserConnections(ctx context.Context, userID bson.ObjectID, status string) ([]models.Connection, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"senderId": userID},
			{"receiverId": userID},
		},
	}

	if status != "" {
		filter["status"] = status
	}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var connections []models.Connection
	if err := cursor.All(ctx, &connections); err != nil {
		return nil, err
	}

	if len(connections) > 0 {
		var userIDs []bson.ObjectID
		for _, conn := range connections {
			userIDs = append(userIDs, conn.SenderID, conn.ReceiverID)
		}

		usersColl := r.db.Collection("users")
		usersCursor, err := usersColl.Find(ctx, bson.M{"_id": bson.M{"$in": userIDs}})
		if err == nil {
			var users []models.User
			if err := usersCursor.All(ctx, &users); err == nil {
				userMap := make(map[bson.ObjectID]models.User)
				for _, u := range users {
					userMap[u.ID] = u
				}

				for i, conn := range connections {
					if sender, ok := userMap[conn.SenderID]; ok {
						connections[i].SenderDisplayName = sender.DisplayName
						connections[i].SenderEmail = sender.Email
						connections[i].SenderPhotoURL = sender.PhotoURL
					}
					if receiver, ok := userMap[conn.ReceiverID]; ok {
						connections[i].ReceiverDisplayName = receiver.DisplayName
						connections[i].ReceiverEmail = receiver.Email
						connections[i].ReceiverPhotoURL = receiver.PhotoURL
					}
				}
			}
		}
	}

	return connections, nil
}
