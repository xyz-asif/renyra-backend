package users

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/xyz-asif/renyra-backend/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// HubSender defines the interface for sending WebSocket messages
type HubSender interface {
	SendToUsers(userIDs []string, msg models.WSMessage) error
}

type Service interface {
	GetOrCreateUser(ctx context.Context, firebaseUID, email, displayName, photoURL string) (*models.User, error)
	GetUserByID(ctx context.Context, userID string) (*models.User, error)
	GetUsersByIDs(ctx context.Context, userIDs []string) (map[string]*models.User, error)
	UpdateProfile(ctx context.Context, userID string, updates map[string]interface{}) (*models.User, error)
	SearchUsers(ctx context.Context, query string, limit, offset int) ([]models.User, error)
	SearchUsersWithConnectionStatus(ctx context.Context, currentUserID, query string, limit, offset int) (*UserSearchResult, error)
	ListAllUsers(ctx context.Context, query string, limit, offset int, sortBy, sortDir string) (*AdminUserListResult, error)
	GetFeed(ctx context.Context, userID string) ([]interface{}, error)
	RegisterFCMToken(ctx context.Context, userID, token string) error
	DeleteAccount(ctx context.Context, userID string, reason string) error
}

type service struct {
	repo     Repository
	hub      HubSender
	connRepo ConnectionRepository
	chatRepo ChatRepository
}

type ConnectionRepository interface {
	GetUserConnections(ctx context.Context, userID bson.ObjectID, status string) ([]models.Connection, error)
	GetConnectionBetweenUsers(ctx context.Context, user1ID, user2ID bson.ObjectID) (*models.Connection, error)
	GetConnectionsBetweenUserAndMany(ctx context.Context, userID bson.ObjectID, otherIDs []bson.ObjectID) (map[bson.ObjectID]*models.Connection, error)
}

type ChatRepository interface {
	GetUserRooms(ctx context.Context, userID bson.ObjectID) ([]models.Room, error)
}

func NewService(repo Repository, hub HubSender, connRepo ConnectionRepository, chatRepo ChatRepository) Service {
	return &service{
		repo:     repo,
		hub:      hub,
		connRepo: connRepo,
		chatRepo: chatRepo,
	}
}

// MVP Feature: Authentication - Completed
func (s *service) GetOrCreateUser(ctx context.Context, uid, email, name, photoURL string) (*models.User, error) {
	user, err := s.repo.GetUserByFirebaseUID(ctx, uid)
	if err != nil {
		return nil, err
	}

	// User doesn't exist, create new
	if user == nil {
		newUser := &models.User{
			FirebaseUID: uid,
			Email:       email,
			DisplayName: name,
			PhotoURL:    photoURL,
		}
		if err := s.repo.CreateUser(ctx, newUser); err != nil {
			return nil, err
		}
		return newUser, nil
	}
	return user, nil
}

// GetUsersByIDs fetches multiple users by their string IDs in a single batch
func (s *service) GetUsersByIDs(ctx context.Context, userIDs []string) (map[string]*models.User, error) {
	// Convert string IDs to ObjectIDs
	objectIDs := make([]bson.ObjectID, len(userIDs))
	for i, idStr := range userIDs {
		id, err := bson.ObjectIDFromHex(idStr)
		if err != nil {
			return nil, errors.New("invalid user ID: " + idStr)
		}
		objectIDs[i] = id
	}

	// Fetch users in batch
	userMap, err := s.repo.GetUsersByIDs(ctx, objectIDs)
	if err != nil {
		return nil, err
	}

	// Convert back to string map for frontend compatibility
	result := make(map[string]*models.User)
	for objID, user := range userMap {
		result[objID.Hex()] = user
	}

	return result, nil
}

// MVP Launch: Get user by ID
func (s *service) GetUserByID(ctx context.Context, userID string) (*models.User, error) {
	uID, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}
	return s.repo.GetUserByID(ctx, uID)
}

// MVP Feature: User Profile Management - Completed
func (s *service) UpdateProfile(ctx context.Context, userID string, updates map[string]interface{}) (*models.User, error) {
	uID, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}

	// Validate allowed fields
	allowedFields := map[string]bool{
		"displayName":   true,
		"photoURL":      true,
		"bio":           true,
		"preferences":   true,
		"coverImageURL": true,
		"externalLink":  true,
	}

	filteredUpdates := make(map[string]interface{})
	for key, value := range updates {
		if allowedFields[key] {
			filteredUpdates[key] = value
		}
	}

	if len(filteredUpdates) == 0 {
		return nil, errors.New("no valid fields to update")
	}

	if err := s.repo.UpdateUser(ctx, uID, filteredUpdates); err != nil {
		return nil, err
	}

	updatedUser, err := s.repo.GetUserByID(ctx, uID)
	if err != nil {
		return nil, err
	}

	// Broadcast profile update to friends and chat participants asynchronously
	// This doesn't affect the API response - fire and forget
	go s.broadcastProfileUpdate(userID, filteredUpdates, updatedUser)

	return updatedUser, nil
}

// broadcastProfileUpdate sends profile changes to all users who have a connection or chat with this user
func (s *service) broadcastProfileUpdate(userID string, updates map[string]interface{}, user *models.User) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return
	}

	// Collect unique recipient IDs
	recipients := make(map[string]bool)

	// 1. Get all friends (connections) with accepted status
	if s.connRepo != nil {
		connections, err := s.connRepo.GetUserConnections(ctx, uid, models.ConnectionStatusAccepted)
		if err != nil {
			log.Printf("broadcastProfileUpdate: failed to get friends for user %s: %v", userID, err)
		} else {
			for _, conn := range connections {
				// Determine the other user in the connection
				var friendID string
				if conn.SenderID == uid {
					friendID = conn.ReceiverID.Hex()
				} else {
					friendID = conn.SenderID.Hex()
				}
				if friendID != userID {
					recipients[friendID] = true
				}
			}
		}
	}

	// 2. Get all chat room participants
	if s.chatRepo != nil {
		rooms, err := s.chatRepo.GetUserRooms(ctx, uid)
		if err != nil {
			log.Printf("broadcastProfileUpdate: failed to get rooms for user %s: %v", userID, err)
		} else {
			for _, room := range rooms {
				for _, p := range room.Participants {
					pHex := p.Hex()
					if pHex != userID {
						recipients[pHex] = true
					}
				}
			}
		}
	}

	// If no recipients, nothing to broadcast
	if len(recipients) == 0 {
		return
	}

	// Convert map to slice
	recipientList := make([]string, 0, len(recipients))
	for id := range recipients {
		recipientList = append(recipientList, id)
	}

	// Build payload with only changed fields + user ID
	payload := map[string]interface{}{
		"userId": userID,
	}
	for key, value := range updates {
		payload[key] = value
	}
	// Always include current displayName and photoURL for consistency
	payload["displayName"] = user.DisplayName
	payload["photoURL"] = user.PhotoURL

	// Send WebSocket message
	if s.hub != nil {
		_ = s.hub.SendToUsers(recipientList, models.WSMessage{
			Type:    "profile_updated",
			Payload: payload,
		})
	}
}

func (s *service) SearchUsers(ctx context.Context, query string, limit, offset int) ([]models.User, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	if query == "" {
		return []models.User{}, nil
	}

	return s.repo.SearchUsers(ctx, query, limit, offset)
}

// Placeholder for feed
func (s *service) GetFeed(ctx context.Context, userID string) ([]interface{}, error) {
	return []interface{}{}, nil
}

func (s *service) RegisterFCMToken(ctx context.Context, userID, token string) error {
	uid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return err
	}
	return s.repo.AddFCMToken(ctx, uid, token)
}

// UserWithConnection represents a user with their connection status to the current user
type UserWithConnection struct {
	models.User
	ConnectionStatus string `json:"connectionStatus"` // none, pending_sent, pending_received, accepted, rejected, blocked
	ConnectionID     string `json:"connectionId,omitempty"`
	IsSender         bool   `json:"isSender,omitempty"` // true if current user sent the request
}

// UserSearchResult contains the paginated search results
type UserSearchResult struct {
	Users      []UserWithConnection `json:"users"`
	TotalCount int                  `json:"totalCount"`
	HasMore    bool                 `json:"hasMore"`
}

// SearchUsersWithConnectionStatus searches for users and includes connection status
// If query is empty, returns all users (excluding the current user) with pagination
func (s *service) SearchUsersWithConnectionStatus(ctx context.Context, currentUserID, query string, limit, offset int) (*UserSearchResult, error) {
	// Validate pagination
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	// Convert current user ID
	currentUserObjID, err := bson.ObjectIDFromHex(currentUserID)
	if err != nil {
		return nil, errors.New("invalid current user id")
	}

	// Search users
	users, err := s.repo.SearchUsers(ctx, query, limit+1, offset) // +1 to check if there's more
	if err != nil {
		return nil, err
	}

	hasMore := len(users) > limit
	if hasMore {
		users = users[:limit] // Remove the extra item
	}

	// Build result with connection status
	result := &UserSearchResult{
		Users:      make([]UserWithConnection, 0, len(users)),
		TotalCount: 0, // Will be set if we implement count query
		HasMore:    hasMore,
	}

	// Batch fetch connection status
	otherIDs := make([]bson.ObjectID, 0, len(users))
	for _, user := range users {
		if user.ID != currentUserObjID {
			otherIDs = append(otherIDs, user.ID)
		}
	}
	connMap, err := s.connRepo.GetConnectionsBetweenUserAndMany(ctx, currentUserObjID, otherIDs)
	if err != nil {
		log.Printf("Error batch getting connection status: %v", err)
		connMap = make(map[bson.ObjectID]*models.Connection)
	}

	for _, user := range users {
		// Skip the current user
		if user.ID == currentUserObjID {
			continue
		}

		userWithConn := UserWithConnection{
			User:             user,
			ConnectionStatus: "none",
		}

		// Check if there's a connection between current user and this user from batch map
		if conn, ok := connMap[user.ID]; ok && conn != nil {
			userWithConn.ConnectionStatus = conn.Status
			userWithConn.ConnectionID = conn.ID.Hex()

			// Determine if current user is the sender
			if conn.SenderID == currentUserObjID {
				userWithConn.IsSender = true
				if conn.Status == models.ConnectionStatusPending {
					userWithConn.ConnectionStatus = "pending_sent"
				}
			} else if conn.ReceiverID == currentUserObjID {
				userWithConn.IsSender = false
				if conn.Status == models.ConnectionStatusPending {
					userWithConn.ConnectionStatus = "pending_received"
				}
			}
		}

		result.Users = append(result.Users, userWithConn)
	}

	return result, nil
}

// AdminUserItem is the curated, public-safe view of a user returned by the
// admin user-listing endpoint. It deliberately omits sensitive fields such as
// FCM device tokens and the Firebase UID.
type AdminUserItem struct {
	ID             string           `json:"id"`
	Email          string           `json:"email"`
	DisplayName    string           `json:"displayName"`
	Username       string           `json:"username,omitempty"`
	PhotoURL       string           `json:"photoURL"`
	CoverImageURL  string           `json:"coverImageURL,omitempty"`
	Bio            string           `json:"bio,omitempty"`
	ExternalLink   string           `json:"externalLink,omitempty"`
	IsProfileSetup bool             `json:"isProfileSetup"`
	IsEditor       bool             `json:"isEditor"`
	IsActive       bool             `json:"isActive"`
	IsBanned       bool             `json:"isBanned"`
	BannedReason   *string          `json:"bannedReason,omitempty"`
	PostsCount     int              `json:"postsCount"`
	FollowersCount int              `json:"followersCount"`
	FollowingCount int              `json:"followingCount"`
	Stats          models.UserStats `json:"stats"`
	JoinedAt       time.Time        `json:"joinedAt"` // createdAt — date of joining
	LastLoginAt    time.Time        `json:"lastLoginAt"`
	UpdatedAt      time.Time        `json:"updatedAt"`
}

// AdminUserListResult is the paginated response for the admin user-listing endpoint.
type AdminUserListResult struct {
	Users      []AdminUserItem `json:"users"`
	TotalCount int64           `json:"totalCount"`
	Limit      int             `json:"limit"`
	Offset     int             `json:"offset"`
	HasMore    bool            `json:"hasMore"`
}

func toAdminUserItem(u models.User) AdminUserItem {
	return AdminUserItem{
		ID:             u.ID.Hex(),
		Email:          u.Email,
		DisplayName:    u.DisplayName,
		Username:       u.Username,
		PhotoURL:       u.PhotoURL,
		CoverImageURL:  u.CoverImageURL,
		Bio:            u.Bio,
		ExternalLink:   u.ExternalLink,
		IsProfileSetup: u.IsProfileSetup,
		IsEditor:       u.IsEditor,
		IsActive:       u.IsActive,
		IsBanned:       u.IsBanned,
		BannedReason:   u.BannedReason,
		PostsCount:     u.PostsCount,
		FollowersCount: u.FollowersCount,
		FollowingCount: u.FollowingCount,
		Stats:          u.Stats,
		JoinedAt:       u.CreatedAt,
		LastLoginAt:    u.LastLoginAt,
		UpdatedAt:      u.UpdatedAt,
	}
}

// ListAllUsers returns a paginated list of all users for the admin app.
// No authentication is required for this endpoint.
func (s *service) ListAllUsers(ctx context.Context, query string, limit, offset int, sortBy, sortDir string) (*AdminUserListResult, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	users, err := s.repo.ListAllUsers(ctx, query, limit, offset, sortBy, sortDir)
	if err != nil {
		return nil, err
	}

	total, err := s.repo.CountAllUsers(ctx, query)
	if err != nil {
		return nil, err
	}

	items := make([]AdminUserItem, 0, len(users))
	for _, u := range users {
		items = append(items, toAdminUserItem(u))
	}

	return &AdminUserListResult{
		Users:      items,
		TotalCount: total,
		Limit:      limit,
		Offset:     offset,
		HasMore:    int64(offset+len(items)) < total,
	}, nil
}

func (s *service) DeleteAccount(ctx context.Context, userID string, reason string) error {
	uid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return errors.New("invalid user ID")
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		return errors.New("deletion reason is required")
	}
	if len(reason) > 1000 {
		return errors.New("deletion reason is too long (max 1000 characters)")
	}

	// Fetch user details for the audit log
	user, err := s.repo.GetUserByID(ctx, uid)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("user not found")
	}

	// 1. Create audit log
	deletionLog := &models.AccountDeletion{
		UserID:   uid,
		Email:    user.Email,
		UserName: user.DisplayName, // recording their display name for the audit
		Reason:   reason,
	}
	if err := s.repo.LogAccountDeletion(ctx, deletionLog); err != nil {
		log.Printf("Failed to log account deletion for %s: %v", userID, err)
		// Proceed anyway
	}

	// 2. Perform the hard delete
	if err := s.repo.DeleteUser(ctx, uid); err != nil {
		return err
	}

	return nil
}
