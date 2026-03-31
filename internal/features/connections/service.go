package connections

import (
	"context"
	"errors"
	"log"

	"github.com/xyz-asif/gotodo/internal/features/notifications"
	"github.com/xyz-asif/gotodo/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type HubBroadcaster interface {
	SendToUsers(userIDs []string, msg models.WSMessage) error
	SendMessage(userID string, msg models.WSMessage) error
}

type ChatRoomCreator interface {
	GetOrCreateDirectRoom(ctx context.Context, user1ID, user2ID string) (*models.RoomResponse, error)
}

type Service interface {
	SendRequest(ctx context.Context, senderID, receiverID string) (*models.Connection, error)
	AcceptRequest(ctx context.Context, userID, connectionID string) (*models.Connection, error)
	RejectRequest(ctx context.Context, userID, connectionID string) error
	CancelRequest(ctx context.Context, userID, connectionID string) error
	RemoveConnection(ctx context.Context, userID, connectionID string) error
	GetPendingRequests(ctx context.Context, userID string) ([]models.Connection, error)
	GetFriendsList(ctx context.Context, userID string) ([]models.Connection, error)
	GetConnectionStatus(ctx context.Context, userID, targetUserID string) (*models.Connection, error)
	SetHub(hub HubBroadcaster)
	SetChatService(chat ChatRoomCreator)
}

type service struct {
	repo         Repository
	hub          HubBroadcaster
	chat         ChatRoomCreator
	notifService notifications.Service
	userLookup   notifications.UserLookup
}

func NewService(repo Repository, notifService notifications.Service, userLookup notifications.UserLookup) Service {
	svc := &service{
		repo:         repo,
		notifService: notifService,
		userLookup:   userLookup,
	}
	return svc
}

func (s *service) SetHub(hub HubBroadcaster) {
	s.hub = hub
}

func (s *service) SetChatService(chat ChatRoomCreator) {
	s.chat = chat
}

func (s *service) SendRequest(ctx context.Context, senderIDStr, receiverIDStr string) (*models.Connection, error) {
	senderID, err := bson.ObjectIDFromHex(senderIDStr)
	if err != nil {
		return nil, errors.New("invalid sender id")
	}
	receiverID, err := bson.ObjectIDFromHex(receiverIDStr)
	if err != nil {
		return nil, errors.New("invalid receiver id")
	}

	if senderID == receiverID {
		return nil, errors.New("cannot send request to yourself")
	}

	// Check if connection already exists
	existingConn, err := s.repo.GetConnectionBetweenUsers(ctx, senderID, receiverID)
	if err != nil {
		return nil, err
	}
	if existingConn != nil {
		if existingConn.Status == models.ConnectionStatusPending {
			return nil, errors.New("request already pending")
		}
		if existingConn.Status == models.ConnectionStatusAccepted {
			return nil, errors.New("already connected")
		}
		if existingConn.Status == models.ConnectionStatusBlocked {
			return nil, errors.New("cannot send request")
		}

		// If rejected, update both status and direction in a single DB write
		if existingConn.Status == models.ConnectionStatusRejected {
			// Ensure the new sender is the one initiating again (might have been rejected by the other party)
			if err := s.repo.UpdateConnectionDirection(ctx, existingConn.ID, senderID, receiverID); err != nil {
				return nil, err
			}
			existingConn.Status = models.ConnectionStatusPending
			existingConn.SenderID = senderID
			existingConn.ReceiverID = receiverID
			return existingConn, nil
		}
	}

	// Create new connection
	conn := &models.Connection{
		SenderID:   senderID,
		ReceiverID: receiverID,
		Status:     models.ConnectionStatusPending,
	}

	if err := s.repo.CreateConnection(ctx, conn); err != nil {
		return nil, err
	}

	// Send notification to receiver
	if s.notifService != nil {
		senderName := "Someone"
		if sender, err := s.userLookup.GetUserByID(ctx, senderID); err == nil && sender != nil {
			senderName = sender.DisplayName
		}
		_ = s.notifService.Send(ctx, models.SendNotificationRequest{
			RecipientID:  receiverID,
			ActorID:      senderID,
			Type:         models.NotifTypeConnectionRequest,
			ResourceType: models.ResourceTypeConnection,
			ResourceID:   conn.ID.Hex(),
			Title:        "New friend request",
			Body:         senderName + " sent you a friend request",
		})
	}

	return conn, nil
}

func (s *service) AcceptRequest(ctx context.Context, userIDStr, connectionIDStr string) (*models.Connection, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	connID, err := bson.ObjectIDFromHex(connectionIDStr)
	if err != nil {
		return nil, errors.New("invalid connection id")
	}

	conn, err := s.repo.GetConnectionByID(ctx, connID)
	if err != nil {
		return nil, err
	}

	// Ensure the user accepting is the receiver
	if conn.ReceiverID != userID {
		return nil, errors.New("unauthorized to accept this request")
	}

	if conn.Status != models.ConnectionStatusPending {
		return nil, errors.New("request is not pending")
	}

	if err := s.repo.UpdateConnectionStatus(ctx, connID, models.ConnectionStatusAccepted); err != nil {
		return nil, err
	}

	conn.Status = models.ConnectionStatusAccepted

	// Send notification to original sender that their request was accepted
	if s.notifService != nil {
		acceptorName := "Someone"
		if acceptor, err := s.userLookup.GetUserByID(ctx, userID); err == nil && acceptor != nil {
			acceptorName = acceptor.DisplayName
		}
		_ = s.notifService.Send(ctx, models.SendNotificationRequest{
			RecipientID:  conn.SenderID,
			ActorID:      userID,
			Type:         models.NotifTypeConnectionAccepted,
			ResourceType: models.ResourceTypeConnection,
			ResourceID:   conn.ID.Hex(),
			Title:        "Request accepted",
			Body:         acceptorName + " accepted your friend request",
		})
	}

	// Create the chat room immediately when connection is accepted
	var senderRoom, receiverRoom *models.RoomResponse
	if s.chat != nil {
		senderID := conn.SenderID.Hex()
		receiverID := conn.ReceiverID.Hex()
		
		// Create room and get sender's perspective
		sRoom, err := s.chat.GetOrCreateDirectRoom(ctx, senderID, receiverID)
		if err != nil {
			log.Printf("[CONN] Failed to create room for accepted connection: %v", err)
		} else {
			senderRoom = sRoom
		}
		
		// Get receiver's perspective (same room, different viewer)
		rRoom, err := s.chat.GetOrCreateDirectRoom(ctx, receiverID, senderID)
		if err != nil {
			log.Printf("[CONN] Failed to get room for receiver: %v", err)
		} else {
			receiverRoom = rRoom
		}
	}

	// Broadcast to both users via WebSocket that they are now connected.
	// Only send if the room was successfully created — sending with a null room
	// causes the frontend to silently drop the event, leaving the user unable
	// to see the new chat until a manual refresh.
	if s.hub != nil {
		senderID := conn.SenderID.Hex()
		receiverID := conn.ReceiverID.Hex()

		if senderRoom != nil {
			senderPayload := map[string]interface{}{
				"connectionId": conn.ID.Hex(),
				"senderId":     senderID,
				"receiverId":   receiverID,
				"status":       "accepted",
				"message":      "You are now connected! Start chatting.",
				"room":         senderRoom,
			}
			_ = s.hub.SendMessage(senderID, models.WSMessage{
				Type:    "connection_accepted",
				Payload: senderPayload,
			})
		}

		if receiverRoom != nil {
			receiverPayload := map[string]interface{}{
				"connectionId": conn.ID.Hex(),
				"senderId":     senderID,
				"receiverId":   receiverID,
				"status":       "accepted",
				"message":      "You are now connected! Start chatting.",
				"room":         receiverRoom,
			}
			_ = s.hub.SendMessage(receiverID, models.WSMessage{
				Type:    "connection_accepted",
				Payload: receiverPayload,
			})
		}

		log.Printf("[CONN] Broadcasted connection_accepted to sender %s and receiver %s", senderID, receiverID)
	}

	return conn, nil
}

func (s *service) RejectRequest(ctx context.Context, userIDStr, connectionIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}
	connID, err := bson.ObjectIDFromHex(connectionIDStr)
	if err != nil {
		return errors.New("invalid connection id")
	}

	conn, err := s.repo.GetConnectionByID(ctx, connID)
	if err != nil {
		return err
	}

	// Ensure the user rejecting is the receiver
	if conn.ReceiverID != userID {
		return errors.New("unauthorized to reject this request")
	}

	if conn.Status != models.ConnectionStatusPending {
		return errors.New("request is not pending")
	}

	return s.repo.UpdateConnectionStatus(ctx, connID, models.ConnectionStatusRejected)
}

func (s *service) GetPendingRequests(ctx context.Context, userIDStr string) ([]models.Connection, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}

	// Get all connections where this user is involved
	allConns, err := s.repo.GetUserConnections(ctx, userID, models.ConnectionStatusPending)
	if err != nil {
		return nil, err
	}

	// Filter to only requests WHERE the user is the RECEIVER
	var pendingRequests []models.Connection
	for _, conn := range allConns {
		if conn.ReceiverID == userID {
			pendingRequests = append(pendingRequests, conn)
		}
	}

	return pendingRequests, nil
}

func (s *service) GetFriendsList(ctx context.Context, userIDStr string) ([]models.Connection, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}

	return s.repo.GetUserConnections(ctx, userID, models.ConnectionStatusAccepted)
}

// CancelRequest allows a user to cancel a pending request they sent
func (s *service) CancelRequest(ctx context.Context, userIDStr, connectionIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}
	connID, err := bson.ObjectIDFromHex(connectionIDStr)
	if err != nil {
		return errors.New("invalid connection id")
	}

	conn, err := s.repo.GetConnectionByID(ctx, connID)
	if err != nil {
		return err
	}

	// Ensure the user cancelling is the SENDER (only sender can cancel)
	if conn.SenderID != userID {
		return errors.New("unauthorized: only sender can cancel request")
	}

	if conn.Status != models.ConnectionStatusPending {
		return errors.New("request is not pending")
	}

	// Delete the connection entirely (cancel the request)
	return s.repo.DeleteConnection(ctx, connID)
}

// RemoveConnection allows a user to remove/accept/decline an accepted or pending connection
func (s *service) RemoveConnection(ctx context.Context, userIDStr, connectionIDStr string) error {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return errors.New("invalid user id")
	}
	connID, err := bson.ObjectIDFromHex(connectionIDStr)
	if err != nil {
		return errors.New("invalid connection id")
	}

	conn, err := s.repo.GetConnectionByID(ctx, connID)
	if err != nil {
		return err
	}

	// Ensure the user is part of this connection
	if conn.SenderID != userID && conn.ReceiverID != userID {
		return errors.New("unauthorized: not part of this connection")
	}

	// Can remove accepted connections or reject pending ones
	if conn.Status != models.ConnectionStatusAccepted && conn.Status != models.ConnectionStatusPending {
		return errors.New("can only remove accepted connections or cancel pending requests")
	}

	// Delete the connection
	return s.repo.DeleteConnection(ctx, connID)
}

// GetConnectionStatus returns the connection status between two users
func (s *service) GetConnectionStatus(ctx context.Context, userIDStr, targetUserIDStr string) (*models.Connection, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	targetUserID, err := bson.ObjectIDFromHex(targetUserIDStr)
	if err != nil {
		return nil, errors.New("invalid target user id")
	}

	// Get connection between users (checks both directions)
	conn, err := s.repo.GetConnectionBetweenUsers(ctx, userID, targetUserID)
	if err != nil {
		return nil, err
	}

	return conn, nil
}
