package notifications

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// HubSender sends WebSocket messages (satisfied by chat.Hub)
type HubSender interface {
	SendMessage(userID string, msg models.WSMessage) error
	IsUserOnline(userID string) bool
}

// UserLookup fetches user info for populating notification display data
type UserLookup interface {
	GetUserByID(ctx context.Context, id bson.ObjectID) (*models.User, error)
	GetUsersByIDs(ctx context.Context, ids []bson.ObjectID) (map[bson.ObjectID]*models.User, error)
	RemoveFCMTokens(ctx context.Context, userID bson.ObjectID, tokens []string) error
}

// FCMSender sends push notifications (implement this interface)
type FCMSender interface {
	SendPush(ctx context.Context, tokens []string, title, body string, data map[string]string) ([]string, error)
}

type Service interface {
	Send(ctx context.Context, req models.SendNotificationRequest) error
	GetNotifications(ctx context.Context, userID string, limit int, before string) ([]models.NotificationResponse, bool, error)
	GetUnreadCount(ctx context.Context, userID string) (int64, error)
	MarkAsRead(ctx context.Context, userID, notifID string) error
	MarkAllAsRead(ctx context.Context, userID string) error
}

type service struct {
	repo       Repository
	userLookup UserLookup
	hub        HubSender
	fcm        FCMSender // nil if FCM not configured — push is skipped gracefully
}

func NewService(repo Repository, userLookup UserLookup, hub HubSender, fcm FCMSender) Service {
	return &service{
		repo:       repo,
		userLookup: userLookup,
		hub:        hub,
		fcm:        fcm,
	}
}

// Send creates a notification, delivers it via WebSocket if online, or FCM push if offline.
// This is the method all other features call.
func (s *service) Send(ctx context.Context, req models.SendNotificationRequest) error {
	// Don't notify yourself
	if req.RecipientID == req.ActorID {
		return nil
	}

	// Look up actor info for display
	actor, err := s.userLookup.GetUserByID(ctx, req.ActorID)
	if err != nil {
		return fmt.Errorf("failed to look up actor: %w", err)
	}

	// Handle grouping: if a GroupKey is set and an unread notification with the
	// same key exists, update it instead of creating a duplicate.
	// Example: multiple messages from Alice → "Alice sent 3 messages" instead of 3 separate notifs.
	if req.GroupKey != "" {
		existing, err := s.repo.FindByGroupKey(ctx, req.RecipientID, req.GroupKey)
		if err != nil {
			log.Printf("notification grouping lookup failed: %v", err)
			// Fall through to create a new one
		}
		if existing != nil {
			if err := s.repo.UpdateGroupedNotification(ctx, existing.ID, req.Title, req.Body); err != nil {
				log.Printf("notification grouping update failed: %v", err)
			} else {
				// Deliver the updated notification via WebSocket
				s.deliverRealtime(existing.ID.Hex(), req, actor)
				return nil
			}
		}
	}

	// Create new notification
	notif := &models.Notification{
		RecipientID:  req.RecipientID,
		ActorID:      req.ActorID,
		Type:         req.Type,
		ResourceType: req.ResourceType,
		ResourceID:   req.ResourceID,
		Title:        req.Title,
		Body:         req.Body,
		ImageURL:     actor.PhotoURL,
		GroupKey:     req.GroupKey,
	}

	if err := s.repo.Create(ctx, notif); err != nil {
		return fmt.Errorf("failed to create notification: %w", err)
	}

	// Real-time delivery via WebSocket
	s.deliverRealtime(notif.ID.Hex(), req, actor)

	// FCM push if recipient is offline
	recipientHex := req.RecipientID.Hex()
	if s.fcm != nil && !s.hub.IsUserOnline(recipientHex) {
		go s.sendPush(req.RecipientID, req, actor)
	}

	return nil
}

// deliverRealtime sends the notification over WebSocket for instant UI update
func (s *service) deliverRealtime(notifID string, req models.SendNotificationRequest, actor *models.User) {
	recipientHex := req.RecipientID.Hex()

	_ = s.hub.SendMessage(recipientHex, models.WSMessage{
		Type: "notification",
		Payload: map[string]interface{}{
			"id":            notifID,
			"type":          req.Type,
			"resourceType":  req.ResourceType,
			"resourceId":    req.ResourceID,
			"title":         req.Title,
			"body":          req.Body,
			"actorId":       req.ActorID.Hex(),
			"actorName":     actor.DisplayName,
			"actorPhotoUrl": actor.PhotoURL,
		},
	})
}

// sendPush sends an FCM notification to offline users
func (s *service) sendPush(recipientID bson.ObjectID, req models.SendNotificationRequest, actor *models.User) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	recipient, err := s.userLookup.GetUserByID(ctx, recipientID)
	if err != nil || recipient == nil || len(recipient.FCMTokens) == 0 {
		return
	}

	data := map[string]string{
		"type":         req.Type,
		"resourceType": req.ResourceType,
		"resourceId":   req.ResourceID,
	}

	staleTokens, err := s.fcm.SendPush(ctx, recipient.FCMTokens, req.Title, req.Body, data)
	if err != nil {
		log.Printf("FCM push failed for user %s: %v", recipientID.Hex(), err)
	}

	if len(staleTokens) > 0 {
		log.Printf("FCM: Removing %d stale tokens for user %s", len(staleTokens), recipientID.Hex())
		if err := s.userLookup.RemoveFCMTokens(ctx, recipientID, staleTokens); err != nil {
			log.Printf("FCM: Failed to remove stale tokens for user %s: %v", recipientID.Hex(), err)
		}
	}
}

func (s *service) GetNotifications(ctx context.Context, userIDStr string, limit int, beforeStr string) ([]models.NotificationResponse, bool, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, false, errors.New("invalid user id")
	}

	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	var beforeID *bson.ObjectID
	if beforeStr != "" {
		id, err := bson.ObjectIDFromHex(beforeStr)
		if err != nil {
			return nil, false, errors.New("invalid before cursor")
		}
		beforeID = &id
	}

	// Fetch one extra to determine hasMore
	notifs, err := s.repo.GetByRecipient(ctx, userID, limit+1, beforeID)
	if err != nil {
		return nil, false, err
	}

	hasMore := len(notifs) > limit
	if hasMore {
		notifs = notifs[:limit]
	}

	// Build responses with actor info
	// Batch fetch all actors
	actorIDSet := make(map[bson.ObjectID]bool)
	for _, n := range notifs {
		actorIDSet[n.ActorID] = true
	}
	actorIDs := make([]bson.ObjectID, 0, len(actorIDSet))
	for id := range actorIDSet {
		actorIDs = append(actorIDs, id)
	}
	actorMap := make(map[bson.ObjectID]*models.User)
	if len(actorIDs) > 0 {
		if m, err := s.userLookup.GetUsersByIDs(ctx, actorIDs); err == nil {
			actorMap = m
		}
	}

	responses := make([]models.NotificationResponse, 0, len(notifs))
	for _, n := range notifs {
		resp := models.NotificationResponse{
			ID:            n.ID.Hex(),
			Type:          n.Type,
			ResourceType:  n.ResourceType,
			ResourceID:    n.ResourceID,
			Title:         n.Title,
			Body:          n.Body,
			ActorID:       n.ActorID.Hex(),
			IsRead:        n.IsRead,
			CreatedAt:     n.CreatedAt,
			ActorPhotoURL: n.ImageURL, // stored at creation time
		}

		if actor, ok := actorMap[n.ActorID]; ok && actor != nil {
			resp.ActorName = actor.DisplayName
		}

		responses = append(responses, resp)
	}

	return responses, hasMore, nil
}

func (s *service) GetUnreadCount(ctx context.Context, userIDStr string) (int64, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return 0, errors.New("invalid user id")
	}
	return s.repo.GetUnreadCount(ctx, userID)
}

func (s *service) MarkAsRead(ctx context.Context, userIDStr, notifIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}
	notifID, err := bson.ObjectIDFromHex(notifIDStr)
	if err != nil {
		return errors.New("invalid notification id")
	}
	return s.repo.MarkAsRead(ctx, notifID, userID)
}

func (s *service) MarkAllAsRead(ctx context.Context, userIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}
	return s.repo.MarkAllAsRead(ctx, userID)
}
