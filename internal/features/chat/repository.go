package chat

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"time"

	"github.com/xyz-asif/renyra-backend/internal/models"
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
	GetMessagesByIDs(ctx context.Context, messageIDs []bson.ObjectID) (map[bson.ObjectID]*models.Message, error)
	// GetMessagesByRoom returns up to limit messages in the room.
	// If beforeID is non-nil, only messages with _id < beforeID are returned
	// (cursor-based / keyset pagination — O(1) regardless of page depth).
	GetMessagesByRoom(ctx context.Context, roomID bson.ObjectID, limit int, beforeID *bson.ObjectID) ([]models.Message, error)
	UpdateRoomLastMessage(ctx context.Context, roomID bson.ObjectID, lastMessage, lastMessageType string, senderID bson.ObjectID) error
	// UpdateRoomLastMessagePreview updates only the cached last-message preview
	// text, without changing lastUpdated — used when a message is deleted/edited
	// so the room's preview changes but the room does not jump in the list.
	UpdateRoomLastMessagePreview(ctx context.Context, roomID bson.ObjectID, lastMessage string) error
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

	// AdvanceMessagesToDelivered bulk-updates all "sent" messages in the user's
	// rooms (where the user is NOT the sender) to "delivered".
	// Returns the updated messages so callers can broadcast status changes.
	AdvanceMessagesToDelivered(ctx context.Context, userID bson.ObjectID) ([]models.Message, error)
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
	log.Printf("[ROOM] GetDirectRoom: looking for room with user1=%s, user2=%s", user1ID.Hex(), user2ID.Hex())
	
	var room models.Room
	err := r.rooms.FindOne(ctx, bson.M{
		"type": models.RoomTypeDirect,
		"$and": []bson.M{
			{"participants": user1ID},
			{"participants": user2ID},
		},
	}).Decode(&room)
	
	if err != nil {
		if err == mongo.ErrNoDocuments {
			log.Printf("[ROOM] GetDirectRoom: no room found")
			return nil, nil
		}
		log.Printf("[ROOM] GetDirectRoom error: %v", err)
		return nil, err
	}
	
	log.Printf("[ROOM] GetDirectRoom: found room %s with participants %v", room.ID.Hex(), room.Participants)
	return &room, nil
}

// GetOrCreateDirectRoomAtomic finds or atomically creates a direct room between two users.
// Handles race conditions, duplicate key errors, and ensures data integrity.
func (r *repository) GetOrCreateDirectRoomAtomic(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Room, error) {
	// Sort participants for consistent ordering
	participants := []bson.ObjectID{user1ID, user2ID}
	if user1ID.Hex() > user2ID.Hex() {
		participants = []bson.ObjectID{user2ID, user1ID}
	}
	
	log.Printf("[ROOM] GetOrCreateDirectRoomAtomic: user1=%s, user2=%s, sorted=[%s, %s]",
		user1ID.Hex(), user2ID.Hex(), participants[0].Hex(), participants[1].Hex())

	// First, try to find existing room
	existing, err := r.GetDirectRoom(ctx, user1ID, user2ID)
	if err != nil {
		log.Printf("[ROOM] GetDirectRoom error: %v", err)
		return nil, err
	}
	if existing != nil {
		log.Printf("[ROOM] Found existing room: %s", existing.ID.Hex())
		return existing, nil
	}

	log.Printf("[ROOM] No existing room found, creating new one")

	// Create new room
	now := time.Now()
	newRoom := &models.Room{
		Type:         models.RoomTypeDirect,
		Participants: participants,
		CreatedAt:    now,
		LastUpdated:  now,
	}

	result, err := r.rooms.InsertOne(ctx, newRoom)
	if err != nil {
		log.Printf("[ROOM] InsertOne error: %v (isDuplicateKey: %v)", err, mongo.IsDuplicateKeyError(err))
		if mongo.IsDuplicateKeyError(err) {
			// Race condition — another request created the room. Fetch it.
			existing, fetchErr := r.GetDirectRoom(ctx, user1ID, user2ID)
			if fetchErr != nil {
				log.Printf("[ROOM] Fetch after duplicate error: %v", fetchErr)
				return nil, fmt.Errorf("room exists but fetch failed: %w", fetchErr)
			}
			if existing == nil {
				log.Printf("[ROOM] CRITICAL: duplicate key error but room not found!")
				return nil, errors.New("room exists (duplicate key) but could not be found")
			}
			return existing, nil
		}
		return nil, err
	}

	newRoom.ID = result.InsertedID.(bson.ObjectID)
	log.Printf("[ROOM] Created new room: %s", newRoom.ID.Hex())
	return newRoom, nil
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

func (r *repository) UpdateRoomLastMessagePreview(ctx context.Context, roomID bson.ObjectID, lastMessage string) error {
	update := bson.M{"$set": bson.M{"lastMessage": lastMessage}}
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

func (r *repository) GetMessagesByIDs(ctx context.Context, messageIDs []bson.ObjectID) (map[bson.ObjectID]*models.Message, error) {
	if len(messageIDs) == 0 {
		return make(map[bson.ObjectID]*models.Message), nil
	}
	cursor, err := r.messages.Find(ctx, bson.M{"_id": bson.M{"$in": messageIDs}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	result := make(map[bson.ObjectID]*models.Message, len(messageIDs))
	for cursor.Next(ctx) {
		var msg models.Message
		if err := cursor.Decode(&msg); err != nil {
			continue
		}
		result[msg.ID] = &msg
	}
	return result, cursor.Err()
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

// AdvanceMessagesToDelivered finds all "sent" messages in the user's rooms
// (where the user is a recipient, not the sender) and advances them to
// "delivered". Returns the updated messages for WS notification.
func (r *repository) AdvanceMessagesToDelivered(ctx context.Context, userID bson.ObjectID) ([]models.Message, error) {
	// Step 1: Get all room IDs the user participates in
	roomFilter := bson.M{"participants": userID}
	cursor, err := r.rooms.Find(ctx, roomFilter, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}
	var roomDocs []struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if err = cursor.All(ctx, &roomDocs); err != nil {
		cursor.Close(ctx)
		return nil, err
	}
	cursor.Close(ctx)

	if len(roomDocs) == 0 {
		return nil, nil
	}

	roomIDs := make([]bson.ObjectID, len(roomDocs))
	for i, rd := range roomDocs {
		roomIDs[i] = rd.ID
	}

	// Step 2: Find messages that are 'sent' and not from this user
	msgFilter := bson.M{
		"roomId":   bson.M{"$in": roomIDs},
		"senderId": bson.M{"$ne": userID},
		"status":   models.MessageStatusSent,
	}

	// Fetch the message IDs before updating (for WS broadcast)
	msgCursor, err := r.messages.Find(ctx, msgFilter)
	if err != nil {
		return nil, err
	}
	var msgs []models.Message
	if err = msgCursor.All(ctx, &msgs); err != nil {
		msgCursor.Close(ctx)
		return nil, err
	}
	msgCursor.Close(ctx)

	if len(msgs) == 0 {
		return nil, nil
	}

	// Step 3: Bulk update to 'delivered'
	update := bson.M{
		"$set": bson.M{
			"status":    models.MessageStatusDelivered,
			"updatedAt": time.Now(),
		},
	}
	result, err := r.messages.UpdateMany(ctx, msgFilter, update)
	if err != nil {
		return nil, err
	}

	log.Printf("[REPO] AdvanceMessagesToDelivered: user=%s, advanced %d messages to delivered",
		userID.Hex(), result.ModifiedCount)

	return msgs, nil
}
