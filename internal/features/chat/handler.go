package chat

import (
	"log"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/xyz-asif/gotodo/internal/models"
	"github.com/xyz-asif/gotodo/pkg/response"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// WsUpgrade handles the initial HTTP request and upgrades it to a WebSocket connection
func (h *Handler) WsUpgrade(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized access to websocket")
	}

	if websocket.IsWebSocketUpgrade(c) {
		c.Locals("userID", user.ID.Hex())
		return c.Next()
	}
	return fiber.ErrUpgradeRequired
}

// WebSocketHandle is the Fiber WebSocket handler itself
func (h *Handler) WebSocketHandle(c *websocket.Conn) {
	userID, ok := c.Locals("userID").(string)
	if !ok {
		log.Println("WS Error: User ID missing in websocket context")
		return
	}
	h.service.HandleWebSocket(c, userID)
}

// GetOrCreateDirectRoom HTTP Endpoint to start a chat with someone
func (h *Handler) GetOrCreateDirectRoom(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	targetUserId := c.Params("id")
	if targetUserId == "" {
		return response.BadRequest(c, "target user ID is required")
	}

	room, err := h.service.GetOrCreateDirectRoom(c.Context(), user.ID.Hex(), targetUserId)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Room retrieved successfully", room)
}

// GetUserRooms HTTP Endpoint to list all chats with optional search and pagination
// Query params:
//   - q      string search query (optional, searches participant names and room names)
//   - limit  int    (default 20, max 50)
//   - offset int    (default 0, for pagination)
func (h *Handler) GetUserRooms(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	// Parse query parameters
	searchQuery := c.Query("q", "")
	limit := c.QueryInt("limit", 20)
	offset := c.QueryInt("offset", 0)

	if limit > 50 {
		limit = 50
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// Always use GetUserRoomsWithSearch — it's paginated, bounded, and supports search
	rooms, totalCount, hasMore, err := h.service.GetUserRoomsWithSearch(c.Context(), user.ID.Hex(), searchQuery, limit, offset)
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Rooms retrieved", fiber.Map{
		"rooms":      rooms,
		"totalCount": totalCount,
		"hasMore":    hasMore,
		"limit":      limit,
		"offset":     offset,
	})
}

// GetRoomMessages HTTP Endpoint to fetch history (cursor-based pagination)
// Query params:
//   - limit  int    (default 50, max 100)
//   - before string message ID — return messages older than this ID
func (h *Handler) GetRoomMessages(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	roomID := c.Params("roomId")
	limit := c.QueryInt("limit", 50)
	if limit > 50 {
		limit = 50
	}
	before := c.Query("before") // cursor: ObjectID hex of the oldest message currently shown

	page, err := h.service.GetRoomMessages(c.Context(), user.ID.Hex(), roomID, limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Messages retrieved", page)
}

// SendMessage HTTP Endpoint
func (h *Handler) SendMessage(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	roomID := c.Params("roomId")
	var req struct {
		Content   string                `json:"content"`
		Type      string                `json:"type"`
		Metadata  *models.MediaMetadata `json:"metadata,omitempty"`
		ReplyToID string                `json:"replyToId,omitempty"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	if req.Type == "" {
		req.Type = models.MessageTypeText
	}

	msg, err := h.service.SendMessage(c.Context(), user.ID.Hex(), roomID, req.Content, req.Type, req.Metadata, req.ReplyToID)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.Created(c, "Message sent", msg)
}

// UpdateMessageStatus Endpoint (`PATCH /api/v1/chat/messages/:messageId/status`)
func (h *Handler) UpdateMessageStatus(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	messageID := c.Params("messageId")
	var req struct {
		Status string `json:"status"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	if err := h.service.UpdateMessageStatus(c.Context(), user.ID.Hex(), messageID, req.Status); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Message status updated", nil)
}

// UpdateMessageReaction Endpoint (`PUT /api/v1/chat/messages/:messageId/reactions`)
func (h *Handler) UpdateMessageReaction(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	messageID := c.Params("messageId")
	var req struct {
		Emoji string `json:"emoji"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	if err := h.service.UpdateMessageReaction(c.Context(), user.ID.Hex(), messageID, req.Emoji); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Message reaction updated", nil)
}

// GetUserPresence Endpoint (`GET /api/v1/chat/users/:id/presence`)
func (h *Handler) GetUserPresence(c *fiber.Ctx) error {
	_, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	targetUserId := c.Params("id")
	if targetUserId == "" {
		return response.BadRequest(c, "target user ID is required")
	}

	presence, err := h.service.GetUserPresence(c.Context(), targetUserId)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Presence retrieved", presence)
}

// MarkRoomAsRead Endpoint (`POST /api/v1/chat/rooms/:roomId/read`)
func (h *Handler) MarkRoomAsRead(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	roomID := c.Params("roomId")
	if err := h.service.MarkRoomAsRead(c.Context(), user.ID.Hex(), roomID); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Room marked as read", nil)
}

// EditMessage Endpoint (`PATCH /api/v1/chat/messages/:messageId`)
func (h *Handler) EditMessage(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	messageID := c.Params("messageId")
	var req struct {
		Content string `json:"content"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	if err := h.service.EditMessage(c.Context(), user.ID.Hex(), messageID, req.Content); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Message edited", nil)
}

// DeleteMessage Endpoint (`DELETE /api/v1/chat/messages/:messageId`)
func (h *Handler) DeleteMessage(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	messageID := c.Params("messageId")
	if err := h.service.DeleteMessage(c.Context(), user.ID.Hex(), messageID); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Message deleted", nil)
}

// DeleteChat Endpoint (`DELETE /api/v1/chat/rooms/:roomId`)
// Deletes the chat room, all messages, and the connection (for direct chats)
func (h *Handler) DeleteChat(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	roomID := c.Params("roomId")
	if roomID == "" {
		return response.BadRequest(c, "roomId is required")
	}

	if err := h.service.DeleteChat(c.Context(), user.ID.Hex(), roomID); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Chat deleted successfully", nil)
}

// Disconnect Endpoint (`POST /api/v1/chat/disconnect`)
// Call this when app goes to background or terminates to immediately mark user offline
func (h *Handler) Disconnect(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	log.Printf("[WS] Manual disconnect requested for user %s", user.ID.Hex())
	
	// Broadcast offline status immediately via HTTP (for when WS is already closed)
	go h.service.ForceDisconnect(user.ID.Hex())

	return response.OK(c, "Disconnect signal sent", nil)
}
