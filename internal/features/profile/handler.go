package profile

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

// POST /api/v1/users/setup
// Called once after first login to complete profile.
// Auth required.
func (h *Handler) SetupProfile(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	var req ProfileSetupRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	updatedUser, err := h.service.SetupProfile(c.Context(), user.ID.Hex(), req)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Profile setup complete", updatedUser)
}

// GET /api/v1/users/username/check?username=asif_writes
// No auth required. Called on every keystroke (debounced on frontend).
func (h *Handler) CheckUsername(c *fiber.Ctx) error {
	username := c.Query("username")
	if username == "" {
		return response.BadRequest(c, "username query param is required")
	}

	result, err := h.service.CheckUsername(c.Context(), username)
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Username check result", result)
}

// POST /api/v1/users/username
// Auth required. Called once when user confirms their chosen username.
func (h *Handler) SetUsername(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	var req struct {
		Username string `json:"username"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	updatedUser, err := h.service.SetUsername(c.Context(), user.ID.Hex(), req.Username)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Username set successfully", updatedUser)
}
