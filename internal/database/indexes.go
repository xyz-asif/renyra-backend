package database

import (
	"context"
	"log"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// CreateIndexes creates all necessary indexes for the Chat platform
func CreateIndexes(ctx context.Context, db *mongo.Database) error {
	log.Println("Creating MongoDB indexes...")

	// ── Users ──
	usersIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "firebaseUid", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true)},
		// Text index for user search
		{Keys: bson.D{{Key: "displayName", Value: "text"}, {Key: "email", Value: "text"}}},
		// Index for displayName regex search in chat list
		{Keys: bson.D{{Key: "displayName", Value: 1}}},
	}
	if _, err := db.Collection("users").Indexes().CreateMany(ctx, usersIndexes); err != nil {
		log.Printf("Warning: Users index creation issue: %v", err)
	}

	// ── Follows ──
	followsIndexes := []mongo.IndexModel{
		// Unique: one follow record per follower+following pair
		{
			Keys:    bson.D{{Key: "followerId", Value: 1}, {Key: "followingId", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		// Fetch all users that a person follows (for home feed)
		{Keys: bson.D{{Key: "followerId", Value: 1}, {Key: "_id", Value: -1}}},
		// Fetch all followers of a user (for profile followers list)
		{Keys: bson.D{{Key: "followingId", Value: 1}, {Key: "_id", Value: -1}}},
	}
	if _, err := db.Collection("follows").Indexes().CreateMany(ctx, followsIndexes); err != nil {
		log.Printf("Warning: Follows index creation issue: %v", err)
	}

	// ── Connections ──
	connectionsIndexes := []mongo.IndexModel{
		// Fast lookup: does a connection exist between these two users?
		{Keys: bson.D{{Key: "senderId", Value: 1}, {Key: "receiverId", Value: 1}}, Options: options.Index().SetUnique(true)},
		// Fast lookup: all connections for a given user (pending, friends list)
		{Keys: bson.D{{Key: "receiverId", Value: 1}, {Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "senderId", Value: 1}, {Key: "status", Value: 1}}},
	}
	if _, err := db.Collection("connections").Indexes().CreateMany(ctx, connectionsIndexes); err != nil {
		log.Printf("Warning: Connections index creation issue: %v", err)
	}

	// ── Chat Rooms ──
	roomsIndexes := []mongo.IndexModel{
		// Fast lookup: all rooms a user participates in, sorted by last activity
		{Keys: bson.D{{Key: "participants", Value: 1}, {Key: "lastUpdated", Value: -1}}},
		// Fast lookup: find existing direct room between two people
		{Keys: bson.D{{Key: "type", Value: 1}, {Key: "participants", Value: 1}}},
		// Enforce unique direct rooms between the same participants
		{
			Keys: bson.D{{Key: "type", Value: 1}, {Key: "participants", Value: 1}},
			Options: options.Index().SetUnique(true).SetPartialFilterExpression(bson.M{
				"type": "direct",
			}),
		},
		// Index for searching rooms by participant IDs (for $in queries)
		{Keys: bson.D{{Key: "participants", Value: 1}}},
	}
	if _, err := db.Collection("chat_rooms").Indexes().CreateMany(ctx, roomsIndexes); err != nil {
		log.Printf("Warning: Chat rooms index creation issue: %v", err)
	}

	// ── Chat Messages ──
	messagesIndexes := []mongo.IndexModel{
		// Primary query: cursor-based pagination uses _id as cursor (ObjectID is monotonic)
		{Keys: bson.D{{Key: "roomId", Value: 1}, {Key: "_id", Value: -1}}},
		// Bulk read-status update: MarkRoomMessagesAsRead filters by roomId + senderId + status
		{Keys: bson.D{{Key: "roomId", Value: 1}, {Key: "senderId", Value: 1}, {Key: "status", Value: 1}}},
	}
	if _, err := db.Collection("chat_messages").Indexes().CreateMany(ctx, messagesIndexes); err != nil {
		log.Printf("Warning: Chat messages index creation issue: %v", err)
	}

	// ── Notifications ──
	notifsIndexes := []mongo.IndexModel{
		// Primary query: paginated list for a user, newest first
		{Keys: bson.D{{Key: "recipientId", Value: 1}, {Key: "_id", Value: -1}}},
		// Unread count query
		{Keys: bson.D{{Key: "recipientId", Value: 1}, {Key: "isRead", Value: 1}}},
		// Grouping lookup (find existing unread notification with same groupKey)
		{Keys: bson.D{{Key: "recipientId", Value: 1}, {Key: "groupKey", Value: 1}, {Key: "isRead", Value: 1}}},
	}
	if _, err := db.Collection("notifications").Indexes().CreateMany(ctx, notifsIndexes); err != nil {
		log.Printf("Warning: Notifications index creation issue: %v", err)
	}

	// ── Users — username unique index ──
	usersUsernameIndex := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "username", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true), // sparse: allows multiple docs with no username
		},
	}
	if _, err := db.Collection("users").Indexes().CreateMany(ctx, usersUsernameIndex); err != nil {
		log.Printf("Warning: Users username index issue: %v", err)
	}

	// ── Poems ──
	poemsIndexes := []mongo.IndexModel{
		// Primary: fetch all poems by author, newest first
		{Keys: bson.D{{Key: "authorId", Value: 1}, {Key: "_id", Value: -1}}},
		// Filter by visibility (for explore feed later)
		{Keys: bson.D{{Key: "visibility", Value: 1}, {Key: "_id", Value: -1}}},
		// Filter by hashtag
		{Keys: bson.D{{Key: "hashtags", Value: 1}, {Key: "_id", Value: -1}}},
		// Filter by mood
		{Keys: bson.D{{Key: "mood", Value: 1}, {Key: "_id", Value: -1}}},
		// Full text search on title + plainText
		{Keys: bson.D{{Key: "title", Value: "text"}, {Key: "plainText", Value: "text"}}},
		// Soft delete filter
		{Keys: bson.D{{Key: "isDeleted", Value: 1}}},
	}
	if _, err := db.Collection("poems").Indexes().CreateMany(ctx, poemsIndexes); err != nil {
		log.Printf("Warning: Poems index issue: %v", err)
	}

	// ── Poems — additional indexes for feed and scoring ──
	poemsFeedIndexes := []mongo.IndexModel{
		// Explore feed: public poems with engagement score sort
		// score is computed at query time via $addFields — this index covers the base filter
		{Keys: bson.D{{Key: "visibility", Value: 1}, {Key: "isDeleted", Value: 1}, {Key: "createdAt", Value: -1}}},
		// Home feed: poems by multiple authors, cursor pagination
		// MongoDB will use the existing authorId index for $in queries
	}
	if _, err := db.Collection("poems").Indexes().CreateMany(ctx, poemsFeedIndexes); err != nil {
		log.Printf("Warning: Poems feed index issue: %v", err)
	}

	// ── Hashtags ──
	hashtagsIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "tag", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "usageCount", Value: -1}}},
	}
	if _, err := db.Collection("hashtags").Indexes().CreateMany(ctx, hashtagsIndexes); err != nil {
		log.Printf("Warning: Hashtags index issue: %v", err)
	}

	// ── Poem Likes ──
	poemLikesIndexes := []mongo.IndexModel{
		// Unique: one like per user per poem
		{
			Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "poemId", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		// All likes on a poem (for likers list)
		{Keys: bson.D{{Key: "poemId", Value: 1}, {Key: "_id", Value: -1}}},
		// All poems a user liked
		{Keys: bson.D{{Key: "userId", Value: 1}, {Key: "_id", Value: -1}}},
	}
	if _, err := db.Collection("poem_likes").Indexes().CreateMany(ctx, poemLikesIndexes); err != nil {
		log.Printf("Warning: Poem likes index issue: %v", err)
	}

	// ── Comments ──
	commentsIndexes := []mongo.IndexModel{
		// Primary: all comments on a poem, oldest first
		{Keys: bson.D{{Key: "poemId", Value: 1}, {Key: "_id", Value: 1}}},
		// Cursor pagination
		{Keys: bson.D{{Key: "poemId", Value: 1}, {Key: "_id", Value: -1}}},
	}
	if _, err := db.Collection("comments").Indexes().CreateMany(ctx, commentsIndexes); err != nil {
		log.Printf("Warning: Comments index issue: %v", err)
	}

	// ── Comment Likes ──
	commentLikesIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "userId", Value: 1}, {Key: "commentId", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{Keys: bson.D{{Key: "commentId", Value: 1}}},
	}
	if _, err := db.Collection("comment_likes").Indexes().CreateMany(ctx, commentLikesIndexes); err != nil {
		log.Printf("Warning: Comment likes index issue: %v", err)
	}

	log.Println("✅ All MongoDB indexes created successfully")
	return nil
}
