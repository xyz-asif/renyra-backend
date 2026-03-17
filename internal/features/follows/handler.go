package follows

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

// POST /api/v1/users/:id/follow
// Auth required. Toggles follow/unfollow.
func (h *Handler) ToggleFollow(c *fiber.Ctx) error {
	caller, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	targetID := c.Params("id")
	isFollowing, err := h.service.ToggleFollow(c.Context(), caller.ID.Hex(), targetID)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	msg := "Unfollowed"
	if isFollowing {
		msg = "Following"
	}
	return response.OK(c, msg, fiber.Map{"following": isFollowing})
}

// GET /api/v1/users/:id/profile
// Auth optional. Returns public profile with isFollowedByMe flag.
func (h *Handler) GetPublicProfile(c *fiber.Ctx) error {
	targetID := c.Params("id")
	callerID := ""
	if user, ok := c.Locals("user").(*models.User); ok {
		callerID = user.ID.Hex()
	}

	profile, err := h.service.GetPublicProfile(c.Context(), targetID, callerID)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Profile retrieved", profile) // fiber automatically handles pointer dereferencing in JSON output
}

// GET /api/v1/users/:id/followers?limit=20&before=<id>
// Auth optional.
func (h *Handler) GetFollowers(c *fiber.Ctx) error {
	targetID := c.Params("id")
	limit := c.QueryInt("limit", 20)
	if limit > 50 {
		limit = 50
	}
	before := c.Query("before")
	callerID := ""
	if user, ok := c.Locals("user").(*models.User); ok {
		callerID = user.ID.Hex()
	}

	users, hasMore, err := h.service.GetFollowers(c.Context(), targetID, callerID, limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Followers retrieved", fiber.Map{"users": users, "hasMore": hasMore})
}

// GET /api/v1/users/:id/following?limit=20&before=<id>
// Auth optional.
func (h *Handler) GetFollowing(c *fiber.Ctx) error {
	targetID := c.Params("id")
	limit := c.QueryInt("limit", 20)
	if limit > 50 {
		limit = 50
	}
	before := c.Query("before")
	callerID := ""
	if user, ok := c.Locals("user").(*models.User); ok {
		callerID = user.ID.Hex()
	}

	users, hasMore, err := h.service.GetFollowing(c.Context(), targetID, callerID, limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Following retrieved", fiber.Map{"users": users, "hasMore": hasMore})
}
