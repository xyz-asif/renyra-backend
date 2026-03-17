package chat

import (
	"context"
	"log"
	"regexp"
	"time"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Repository interface {
	CreateRoom(ctx context.Context, room *models.Room) error
	GetRoomByID(ctx context.Context, roomID bson.ObjectID) (*models.Room, error)
	GetDirectRoom(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Room, error)
	GetOrCreateDirectRoomAtomic(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Room, error)
	GetUserRooms(ctx context.Context, userID bson.ObjectID) ([]models.Room, error)
	// GetUserRoomsWithSearch returns paginated rooms with optional search by participant names or room name
	// searchQuery: optional text to search in room names or participant display names
	// limit: max rooms to return (capped at 50)
	// offset: number of rooms to skip for pagination
	GetUserRoomsWithSearch(ctx context.Context, userID bson.ObjectID, searchQuery string, limit, offset int) ([]models.Room, int64, error)
	// SearchRooms returns paginated rooms for a user, searching by room name.
	SearchRooms(ctx context.Context, userID bson.ObjectID, query string, limit, offset int) ([]models.Room, int64, error)

	SaveMessage(ctx context.Context, msg *models.Message) error
	GetMessageByID(ctx context.Context, messageID bson.ObjectID) (*models.Message, error)
	// GetMessagesByRoom returns up to limit messages in the room.
	// If beforeID is non-nil, only messages with _id < beforeID are returned
	// (cursor-based / keyset pagination — O(1) regardless of page depth).
	GetMessagesByRoom(ctx context.Context, roomID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Message, error)
	UpdateRoomLastMessage(ctx context.Context, roomID bson.ObjectID, lastMessage, lastMessageType string, senderID bson.ObjectID) error
	UpdateMessageStatus(ctx context.Context, messageID bson.ObjectID, status string) error
	UpdateMessageReaction(ctx context.Context, messageID bson.ObjectID, userID, emoji string) error
	UpdateMessageContent(ctx context.Context, messageID bson.ObjectID, content string) error
	SoftDeleteMessage(ctx context.Context, messageID bson.ObjectID) error

	// Unread count management
	// IncrementUnreadCounts bumps the unread counter for every participant except exceptUserID.
	// Callers must pass the already-known participants slice to avoid a redundant DB fetch.
	IncrementUnreadCounts(ctx context.Context, roomID bson.ObjectID, participants []bson.ObjectID, exceptUserID string) error
	ResetUnreadCount(ctx context.Context, roomID bson.ObjectID, userID string) error
	MarkRoomMessagesAsRead(ctx context.Context, roomID, senderID bson.ObjectID) error

	// Delete room and all associated messages
	DeleteRoom(ctx context.Context, roomID bson.ObjectID) error
	DeleteMessagesByRoom(ctx context.Context, roomID bson.ObjectID) error
}

type repository struct {
	db       *mongo.Database
	rooms    *mongo.Collection
	messages *mongo.Collection
}

func NewRepository(db *mongo.Database) Repository {
	return &repository{
		db:       db,
		rooms:    db.Collection("chat_rooms"),
		messages: db.Collection("chat_messages"),
	}
}

func (r *repository) CreateRoom(ctx context.Context, room *models.Room) error {
	room.CreatedAt = time.Now()
	room.LastUpdated = time.Now()
	if room.UnreadCounts == nil {
		room.UnreadCounts = make(map[string]int)
	}

	res, err := r.rooms.InsertOne(ctx, room)
	if err != nil {
		return err
	}
	room.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetRoomByID(ctx context.Context, roomID bson.ObjectID) (*models.Room, error) {
	var room models.Room
	if err := r.rooms.FindOne(ctx, bson.M{"_id": roomID}).Decode(&room); err != nil {
		return nil, err
	}
	return &room, nil
}

func (r *repository) GetDirectRoom(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Room, error) {
	var room models.Room
	filter := bson.M{
		"type": models.RoomTypeDirect,
		"participants": bson.M{
			"$all":  []bson.ObjectID{user1ID, user2ID},
			"$size": 2,
		},
	}

	err := r.rooms.FindOne(ctx, filter).Decode(&room)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &room, nil
}

// GetOrCreateDirectRoomAtomic finds or atomically creates a direct room between two users.
// Handles race conditions, duplicate key errors, and ensures data integrity.
func (r *repository) GetOrCreateDirectRoomAtomic(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Room, error) {
	// First, try to find the existing direct room
	room, err := r.GetDirectRoom(ctx, user1ID, user2ID)
	if err != nil {
		return nil, err
	}
	if room != nil {
		// Verify room has both participants (data integrity check)
		if len(room.Participants) != 2 {
			log.Printf("WARNING: Room %s has %d participants, expected 2. Fixing...", room.ID.Hex(), len(room.Participants))
			// Try to fix the room by ensuring both users are participants
			room.Participants = []bson.ObjectID{user1ID, user2ID}
			_, _ = r.rooms.UpdateOne(ctx, bson.M{"_id": room.ID}, bson.M{
				"$set": bson.M{"participants": room.Participants},
			})
		}
		return room, nil
	}

	// Create a new direct room if it doesn't exist
	now := time.Now()

	// Sort participants to ensure the unique index works flawlessly (array order matters)
	p1, p2 := user1ID, user2ID
	if p1.Hex() > p2.Hex() {
		p1, p2 = p2, p1
	}

	newRoom := &models.Room{
		Type:         models.RoomTypeDirect,
		Participants: []bson.ObjectID{p1, p2},
		UnreadCounts: map[string]int{
			user1ID.Hex(): 0,
			user2ID.Hex(): 0,
		},
		CreatedAt:   now,
		LastUpdated: now,
	}

	if err := r.CreateRoom(ctx, newRoom); err != nil {
		// Check if room was created by concurrent request (unique index violation)
		if mongo.IsDuplicateKeyError(err) {
			// Room was created by another concurrent request, fetch it
			return r.GetDirectRoom(ctx, user1ID, user2ID)
		}
		return nil, err
	}

	// Verify the room was created correctly by re-fetching it
	// This ensures read-after-write consistency
	createdRoom, err := r.GetRoomByID(ctx, newRoom.ID)
	if err != nil {
		log.Printf("WARNING: Failed to fetch created room %s: %v", newRoom.ID.Hex(), err)
		// Return the in-memory room as fallback
		return newRoom, nil
	}

	// Verify participants are correct
	if len(createdRoom.Participants) != 2 {
		log.Printf("WARNING: Created room %s has %d participants, expected 2", createdRoom.ID.Hex(), len(createdRoom.Participants))
		// Fix the room participants
		createdRoom.Participants = []bson.ObjectID{user1ID, user2ID}
	}

	return createdRoom, nil
}

// SearchRooms returns paginated rooms for a user, searching by room name.
func (r *repository) SearchRooms(ctx context.Context, userID bson.ObjectID, query string, limit, offset int) ([]models.Room, int64, error) {
	// Validate and cap limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	// Escape special regex characters in the query
	safeQuery := regexp.QuoteMeta(query)

	filter := bson.M{
		"participants": userID,
		"name":         bson.M{"$regex": safeQuery, "$options": "i"},
	}

	// Get total count for pagination metadata
	totalCount, err := r.rooms.CountDocuments(ctx, filter)
	if err != nil {
		log.Printf("[REPO] Error counting rooms for search: %v", err)
		totalCount = 0
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "lastUpdated", Value: -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(offset))

	cursor, err := r.rooms.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var rooms []models.Room
	if err = cursor.All(ctx, &rooms); err != nil {
		return nil, 0, err
	}

	log.Printf("[REPO] SearchRooms: user=%s, query=%q, limit=%d, offset=%d, found=%d, total=%d",
		userID.Hex(), query, limit, offset, len(rooms), totalCount)

	return rooms, totalCount, nil
}

func (r *repository) GetUserRooms(ctx context.Context, userID bson.ObjectID) ([]models.Room, error) {
	filter := bson.M{"participants": userID}
	opts := options.Find().
		SetSort(bson.D{{Key: "lastUpdated", Value: -1}}).
		SetLimit(50)

	cursor, err := r.rooms.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rooms []models.Room
	if err = cursor.All(ctx, &rooms); err != nil {
		return nil, err
	}

	return rooms, nil
}

// GetUserRoomsWithSearch returns paginated rooms with optional search.
// Search works by:
// 1. For empty search: returns all rooms for user (sorted by lastUpdated desc)
// 2. For search query: searches room names (for groups) and participant display names
// Returns rooms, total count, and error
func (r *repository) GetUserRoomsWithSearch(ctx context.Context, userID bson.ObjectID, searchQuery string, limit, offset int) ([]models.Room, int64, error) {
	// Validate and cap limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	// Base filter: user must be a participant
	filter := bson.M{"participants": userID}

	// If search query provided, we need to find matching users first
	if searchQuery != "" && searchQuery != "*" {
		safeQuery := regexp.QuoteMeta(searchQuery)

		// Search for users whose display name matches the query
		// We'll do a case-insensitive regex search
		userFilter := bson.M{
			"displayName": bson.M{
				"$regex":   safeQuery,
				"$options": "i",
			},
		}

		// Find matching user IDs
		usersColl := r.db.Collection("users")
		cursor, err := usersColl.Find(ctx, userFilter)
		if err != nil {
			log.Printf("[REPO] Error searching users: %v", err)
			// Continue with empty search - will just filter by room name
		} else {
			var matchingUsers []struct {
				ID bson.ObjectID `bson:"_id"`
			}
			if err = cursor.All(ctx, &matchingUsers); err != nil {
				log.Printf("[REPO] Error decoding users: %v", err)
			}
			cursor.Close(ctx)

			// Build $or filter: room name matches OR participant matches
			var userIDs []bson.ObjectID
			for _, u := range matchingUsers {
				// Exclude the requesting user themselves
				if u.ID != userID {
					userIDs = append(userIDs, u.ID)
				}
			}

			orConditions := []bson.M{}

			// Condition 1: Room name matches search (for groups)
			orConditions = append(orConditions, bson.M{
				"name": bson.M{
					"$regex":   safeQuery,
					"$options": "i",
				},
			})

			// Condition 2: Room has a matching participant
			if len(userIDs) > 0 {
				orConditions = append(orConditions, bson.M{
					"participants": bson.M{"$in": userIDs},
				})
			}

			if len(orConditions) > 0 {
				filter = bson.M{
					"$and": []bson.M{
						{"participants": userID}, // Must be participant
						{"$or": orConditions},     // And match search
					},
				}
			}
		}
	}

	// Get total count for pagination metadata
	totalCount, err := r.rooms.CountDocuments(ctx, filter)
	if err != nil {
		log.Printf("[REPO] Error counting rooms: %v", err)
		totalCount = 0
	}

	// Build find options with pagination
	opts := options.Find().
		SetSort(bson.D{{Key: "lastUpdated", Value: -1}}).
		SetLimit(int64(limit)).
		SetSkip(int64(offset))

	cursor, err := r.rooms.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var rooms []models.Room
	if err = cursor.All(ctx, &rooms); err != nil {
		return nil, 0, err
	}

	log.Printf("[REPO] GetUserRoomsWithSearch: user=%s, query=%q, limit=%d, offset=%d, found=%d, total=%d",
		userID.Hex(), searchQuery, limit, offset, len(rooms), totalCount)

	return rooms, totalCount, nil
}

func (r *repository) SaveMessage(ctx context.Context, msg *models.Message) error {
	msg.CreatedAt = time.Now()
	res, err := r.messages.InsertOne(ctx, msg)
	if err != nil {
		return err
	}
	msg.ID = res.InsertedID.(bson.ObjectID)
	return nil
}

func (r *repository) GetMessagesByRoom(ctx context.Context, roomID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Message, error) {
	filter := bson.M{"roomId": roomID}
	if beforeID != nil {
		filter["_id"] = bson.M{"$lt": *beforeID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: "_id", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.messages.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var msgs []models.Message
	if err = cursor.All(ctx, &msgs); err != nil {
		return nil, err
	}

	return msgs, nil
}

func (r *repository) UpdateRoomLastMessage(ctx context.Context, roomID bson.ObjectID, lastMessage, lastMessageType string, senderID bson.ObjectID) error {
	update := bson.M{
		"$set": bson.M{
			"lastMessage":         lastMessage,
			"lastMessageType":     lastMessageType,
			"lastMessageSenderId": senderID,
			"lastUpdated":         time.Now(),
		},
	}
	_, err := r.rooms.UpdateOne(ctx, bson.M{"_id": roomID}, update)
	return err
}

func (r *repository) GetMessageByID(ctx context.Context, messageID bson.ObjectID) (*models.Message, error) {
	var msg models.Message
	if err := r.messages.FindOne(ctx, bson.M{"_id": messageID}).Decode(&msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (r *repository) UpdateMessageStatus(ctx context.Context, messageID bson.ObjectID, status string) error {
	update := bson.M{
		"$set": bson.M{
			"status":    status,
			"updatedAt": time.Now(),
		},
	}
	_, err := r.messages.UpdateOne(ctx, bson.M{"_id": messageID}, update)
	return err
}

func (r *repository) UpdateMessageReaction(ctx context.Context, messageID bson.ObjectID, userID, emoji string) error {
	var update bson.M
	if emoji == "" {
		update = bson.M{
			"$unset": bson.M{"reactions." + userID: ""},
			"$set":   bson.M{"updatedAt": time.Now()},
		}
	} else {
		update = bson.M{
			"$set": bson.M{
				"reactions." + userID: emoji,
				"updatedAt":           time.Now(),
			},
		}
	}
	_, err := r.messages.UpdateOne(ctx, bson.M{"_id": messageID}, update)
	return err
}

func (r *repository) UpdateMessageContent(ctx context.Context, messageID bson.ObjectID, content string) error {
	update := bson.M{
		"$set": bson.M{
			"content":   content,
			"isEdited":  true,
			"updatedAt": time.Now(),
		},
	}
	_, err := r.messages.UpdateOne(ctx, bson.M{"_id": messageID}, update)
	return err
}

func (r *repository) SoftDeleteMessage(ctx context.Context, messageID bson.ObjectID) error {
	update := bson.M{
		"$set": bson.M{
			"content":   "This message was deleted",
			"isDeleted": true,
			"updatedAt": time.Now(),
		},
		// Clear reactions and metadata — deleted messages should not show media or emojis
		"$unset": bson.M{
			"reactions": "",
			"metadata":  "",
		},
	}
	_, err := r.messages.UpdateOne(ctx, bson.M{"_id": messageID}, update)
	return err
}

// IncrementUnreadCounts bumps unread count for all participants EXCEPT the sender.
// participants is passed in by the caller — no extra DB fetch needed.
func (r *repository) IncrementUnreadCounts(ctx context.Context, roomID bson.ObjectID, participants []bson.ObjectID, exceptUserID string) error {
	incMap := bson.M{}
	for _, p := range participants {
		if hex := p.Hex(); hex != exceptUserID {
			incMap["unreadCounts."+hex] = 1
		}
	}

	if len(incMap) == 0 {
		return nil
	}

	_, err := r.rooms.UpdateOne(ctx, bson.M{"_id": roomID}, bson.M{"$inc": incMap})
	return err
}

// ResetUnreadCount sets a user's unread count back to 0
func (r *repository) ResetUnreadCount(ctx context.Context, roomID bson.ObjectID, userID string) error {
	update := bson.M{
		"$set": bson.M{
			"unreadCounts." + userID: 0,
		},
	}
	_, err := r.rooms.UpdateOne(ctx, bson.M{"_id": roomID}, update)
	return err
}

// MarkRoomMessagesAsRead marks all messages from other senders as "read" in bulk
func (r *repository) MarkRoomMessagesAsRead(ctx context.Context, roomID, readerID bson.ObjectID) error {
	filter := bson.M{
		"roomId":   roomID,
		"senderId": bson.M{"$ne": readerID}, // Only mark messages from OTHER users
		"status":   bson.M{"$ne": models.MessageStatusRead},
	}
	update := bson.M{
		"$set": bson.M{
			"status":    models.MessageStatusRead,
			"updatedAt": time.Now(),
		},
	}
	_, err := r.messages.UpdateMany(ctx, filter, update)
	return err
}

// DeleteRoom deletes a room by ID
func (r *repository) DeleteRoom(ctx context.Context, roomID bson.ObjectID) error {
	_, err := r.rooms.DeleteOne(ctx, bson.M{"_id": roomID})
	return err
}

// DeleteMessagesByRoom deletes all messages in a room
func (r *repository) DeleteMessagesByRoom(ctx context.Context, roomID bson.ObjectID) error {
	_, err := r.messages.DeleteMany(ctx, bson.M{"roomId": roomID})
	return err
}
