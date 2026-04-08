package connections

import (
	"github.com/gofiber/fiber/v2"
	"github.com/xyz-asif/renyra-backend/internal/models"
	"github.com/xyz-asif/renyra-backend/pkg/response"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) SendRequest(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	var req struct {
		ReceiverID string `json:"receiverId"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	if req.ReceiverID == "" {
		return response.BadRequest(c, "receiverId is required")
	}

	conn, err := h.service.SendRequest(c.Context(), user.ID.Hex(), req.ReceiverID)
	if err != nil {
		if err.Error() == "already connected" || err.Error() == "request already pending" {
			return response.Conflict(c, err.Error())
		}
		return response.BadRequest(c, err.Error())
	}

	return response.Created(c, "Connection request sent successfully", conn)
}

func (h *Handler) AcceptRequest(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	connID := c.Params("id")
	if connID == "" {
		return response.BadRequest(c, "connection id is required")
	}

	conn, err := h.service.AcceptRequest(c.Context(), user.ID.Hex(), connID)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Request accepted", conn)
}

func (h *Handler) RejectRequest(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	connID := c.Params("id")
	if connID == "" {
		return response.BadRequest(c, "connection id is required")
	}

	if err := h.service.RejectRequest(c.Context(), user.ID.Hex(), connID); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Request rejected", nil)
}

func (h *Handler) GetPendingRequests(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	requests, err := h.service.GetPendingRequests(c.Context(), user.ID.Hex())
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Pending requests retrieved", requests)
}

func (h *Handler) GetFriendsList(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	friends, err := h.service.GetFriendsList(c.Context(), user.ID.Hex())
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Friends retrieved", friends)
}

// CancelRequest allows the sender to cancel a pending request
func (h *Handler) CancelRequest(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	connID := c.Params("id")
	if connID == "" {
		return response.BadRequest(c, "connection id is required")
	}

	if err := h.service.CancelRequest(c.Context(), user.ID.Hex(), connID); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Request cancelled successfully", nil)
}

// RemoveConnection allows either party to remove an accepted connection
func (h *Handler) RemoveConnection(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	connID := c.Params("id")
	if connID == "" {
		return response.BadRequest(c, "connection id is required")
	}

	if err := h.service.RemoveConnection(c.Context(), user.ID.Hex(), connID); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Connection removed successfully", nil)
}
