package feed

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

// GET /api/v1/feed?limit=20&before=<id>
// Auth required.
func (h *Handler) GetHomeFeed(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}
	limit := c.QueryInt("limit", 20)
	before := c.Query("before")

	page, err := h.service.GetHomeFeed(c.Context(), user.ID.Hex(), limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Home feed retrieved", page)
}

// GET /api/v1/feed/explore?limit=20&before=<id>&hashtag=love
// Auth optional.
func (h *Handler) GetExploreFeed(c *fiber.Ctx) error {
	callerID := ""
	if user, ok := c.Locals("user").(*models.User); ok {
		callerID = user.ID.Hex()
	}
	limit := c.QueryInt("limit", 20)
	before := c.Query("before")
	hashtag := c.Query("hashtag")

	page, err := h.service.GetExploreFeed(c.Context(), callerID, hashtag, limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Explore feed retrieved", page)
}

// GET /api/v1/search/poems?q=rain&limit=20&before=<id>
// Auth optional.
func (h *Handler) SearchPoems(c *fiber.Ctx) error {
	query := c.Query("q")
	limit := c.QueryInt("limit", 20)
	before := c.Query("before")

	page, err := h.service.SearchPoems(c.Context(), query, limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Poems found", page)
}

// GET /api/v1/search/users?q=asif&limit=20&offset=0
// Auth optional.
func (h *Handler) SearchUsers(c *fiber.Ctx) error {
	callerID := ""
	if user, ok := c.Locals("user").(*models.User); ok {
		callerID = user.ID.Hex()
	}
	query := c.Query("q")
	limit := c.QueryInt("limit", 20)
	offset := c.QueryInt("offset", 0)

	page, err := h.service.SearchUsers(c.Context(), query, callerID, limit, offset)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Users found", page)
}

// GET /api/v1/feed/audio?limit=20&before=<id>
// Auth optional.
func (h *Handler) GetAudioFeed(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 20)
	before := c.Query("before")
	page, err := h.service.GetAudioFeed(c.Context(), limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Audio feed retrieved", page)
}
