package auth

import (
	"github.com/gofiber/fiber/v2"
	"github.com/xyz-asif/gotodo/pkg/response"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

type exchangeRequest struct {
	FirebaseToken string `json:"firebaseToken"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type logoutRequest struct {
	RefreshToken string `json:"refreshToken"`
}

// Exchange verifies a Firebase ID token and returns a JWT access + refresh token pair.
// POST /api/v1/auth/exchange
func (h *Handler) Exchange(c *fiber.Ctx) error {
	var req exchangeRequest
	if err := c.BodyParser(&req); err != nil || req.FirebaseToken == "" {
		return response.BadRequest(c, "firebaseToken is required")
	}

	accessToken, refreshToken, err := h.service.Exchange(c.Context(), req.FirebaseToken)
	if err != nil {
		return response.Unauthorized(c, err.Error())
	}

	return response.OK(c, "tokens issued", fiber.Map{
		"accessToken":  accessToken,
		"refreshToken": refreshToken,
	})
}

// Refresh rotates a refresh token and returns a new access + refresh token pair.
// POST /api/v1/auth/refresh
func (h *Handler) Refresh(c *fiber.Ctx) error {
	var req refreshRequest
	if err := c.BodyParser(&req); err != nil || req.RefreshToken == "" {
		return response.BadRequest(c, "refreshToken is required")
	}

	newAccess, newRefresh, err := h.service.Refresh(c.Context(), req.RefreshToken)
	if err != nil {
		return response.Unauthorized(c, err.Error())
	}

	return response.OK(c, "token refreshed", fiber.Map{
		"accessToken":  newAccess,
		"refreshToken": newRefresh,
	})
}

// Logout revokes the refresh token server-side.
// POST /api/v1/auth/logout
func (h *Handler) Logout(c *fiber.Ctx) error {
	var req logoutRequest
	if err := c.BodyParser(&req); err != nil || req.RefreshToken == "" {
		return response.BadRequest(c, "refreshToken is required")
	}

	_ = h.service.Logout(c.Context(), req.RefreshToken) // best-effort
	return response.OK(c, "logged out", nil)
}
