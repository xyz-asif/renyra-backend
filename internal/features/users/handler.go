package users

import (
	"github.com/gofiber/fiber/v2"
	"github.com/xyz-asif/renyra-backend/internal/models"
	pkgErrors "github.com/xyz-asif/renyra-backend/pkg/errors"
	"github.com/xyz-asif/renyra-backend/pkg/response"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

// MVP Launch: Get Current User - Completed
// GetMe retrieves the current user profile
func (h *Handler) GetMe(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	return response.OK(c, "User profile retrieved successfully", user)
}

// MVP Feature: User Profile Management - Completed
// UpdateProfile updates the current user profile
func (h *Handler) UpdateProfile(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	var updates map[string]interface{}
	if err := c.BodyParser(&updates); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	updatedUser, err := h.service.UpdateProfile(c.Context(), user.ID.Hex(), updates)
	if err != nil {
		// Check error type and return appropriate status code
		if pkgErrors.IsValidation(err) {
			return response.ValidationFailed(c, err.Error())
		}
		if pkgErrors.IsNotFound(err) {
			return response.NotFound(c, err.Error())
		}
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Profile updated successfully", updatedUser)
}



// Search Users
func (h *Handler) Search(c *fiber.Ctx) error {
	query := c.Query("q", "")
	limit := c.QueryInt("limit", 20)
	if limit > 50 {
		limit = 50
	}
	offset := c.QueryInt("offset", 0)

	users, err := h.service.SearchUsers(c.Context(), query, limit, offset)
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Users retrieved", users)
}

// SearchWithConnectionStatus searches for users and includes connection status
// If no query is provided, returns all users with pagination
// Endpoint: GET /api/v1/users/search-with-status?q=query&limit=20&offset=0
func (h *Handler) SearchWithConnectionStatus(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	query := c.Query("q", "") // Empty query returns all users
	limit := c.QueryInt("limit", 20)
	if limit > 50 {
		limit = 50
	}
	offset := c.QueryInt("offset", 0)

	result, err := h.service.SearchUsersWithConnectionStatus(c.Context(), user.ID.Hex(), query, limit, offset)
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Users retrieved with connection status", result)
}

// RegisterFCMToken saves the device's FCM token for push notifications.
func (h *Handler) RegisterFCMToken(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := c.BodyParser(&req); err != nil || req.Token == "" {
		return response.BadRequest(c, "token is required")
	}

	if err := h.service.RegisterFCMToken(c.Context(), user.ID.Hex(), req.Token); err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Token registered", nil)
}
