package notifications

import (
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

// GetNotifications returns paginated notification list.
// Query params: limit (default 20, max 50), before (cursor: notification ID)
func (h *Handler) GetNotifications(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	limit := c.QueryInt("limit", 20)
	if limit > 50 {
		limit = 50
	}
	before := c.Query("before")

	notifs, hasMore, err := h.service.GetNotifications(c.Context(), user.ID.Hex(), limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Notifications retrieved", fiber.Map{
		"notifications": notifs,
		"hasMore":       hasMore,
	})
}

// GetUnreadCount returns the count of unread notifications (for badge).
func (h *Handler) GetUnreadCount(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	count, err := h.service.GetUnreadCount(c.Context(), user.ID.Hex())
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Unread count", fiber.Map{"count": count})
}

// MarkAsRead marks a single notification as read.
func (h *Handler) MarkAsRead(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	notifID := c.Params("id")
	if notifID == "" {
		return response.BadRequest(c, "notification ID required")
	}

	if err := h.service.MarkAsRead(c.Context(), user.ID.Hex(), notifID); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Marked as read", nil)
}

// MarkAllAsRead marks all notifications as read.
func (h *Handler) MarkAllAsRead(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	if err := h.service.MarkAllAsRead(c.Context(), user.ID.Hex()); err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "All marked as read", nil)
}
