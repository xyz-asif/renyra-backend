package social

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

// POST /api/v1/poems/:id/like
func (h *Handler) TogglePoemLike(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}
	poemID := c.Params("id")
	liked, count, err := h.service.TogglePoemLike(c.Context(), user.ID.Hex(), poemID)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "OK", fiber.Map{"liked": liked, "likesCount": count})
}

// GET /api/v1/poems/:id/likes?limit=20&before=<id>
func (h *Handler) GetPoemLikers(c *fiber.Ctx) error {
	poemID := c.Params("id")
	limit := c.QueryInt("limit", 20)
	if limit > 50 {
		limit = 50
	}
	before := c.Query("before")
	users, hasMore, err := h.service.GetPoemLikers(c.Context(), poemID, limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Likers retrieved", fiber.Map{"users": users, "hasMore": hasMore})
}

// POST /api/v1/poems/:id/comments
func (h *Handler) AddComment(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}
	poemID := c.Params("id")
	var req struct {
		Content string `json:"content"`
	}
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}
	comment, err := h.service.AddComment(c.Context(), user.ID.Hex(), poemID, req.Content)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.Created(c, "Comment added", comment)
}

// GET /api/v1/poems/:id/comments?limit=20&before=<id>
func (h *Handler) GetComments(c *fiber.Ctx) error {
	poemID := c.Params("id")
	limit := c.QueryInt("limit", 20)
	if limit > 50 {
		limit = 50
	}
	before := c.Query("before")
	callerID := ""
	if user, ok := c.Locals("user").(*models.User); ok {
		callerID = user.ID.Hex()
	}
	page, err := h.service.GetComments(c.Context(), poemID, callerID, limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Comments retrieved", page)
}

// DELETE /api/v1/comments/:id
func (h *Handler) DeleteComment(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}
	commentID := c.Params("id")
	if err := h.service.DeleteComment(c.Context(), user.ID.Hex(), commentID); err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Comment deleted", nil)
}

// POST /api/v1/comments/:id/like
func (h *Handler) ToggleCommentLike(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}
	commentID := c.Params("id")
	liked, count, err := h.service.ToggleCommentLike(c.Context(), user.ID.Hex(), commentID)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "OK", fiber.Map{"liked": liked, "likesCount": count})
}

// GET /api/v1/poems/:id/reposters?limit=20&before=<id>
func (h *Handler) GetPoemReposters(c *fiber.Ctx) error {
	poemID := c.Params("id")
	limit := c.QueryInt("limit", 20)
	if limit > 50 {
		limit = 50
	}
	before := c.Query("before")
	users, hasMore, err := h.service.GetPoemReposters(c.Context(), poemID, limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Reposters retrieved", fiber.Map{"users": users, "hasMore": hasMore})
}

// POST /api/v1/poems/:id/repost
func (h *Handler) ToggleRepost(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}
	poemID := c.Params("id")
	reposted, count, err := h.service.ToggleRepost(c.Context(), user.ID.Hex(), poemID)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "OK", fiber.Map{"reposted": reposted, "repostsCount": count})
}

// GET /api/v1/users/:id/reposts?limit=20&before=<id>
func (h *Handler) GetUserReposts(c *fiber.Ctx) error {
	userID := c.Params("id")
	limit := c.QueryInt("limit", 20)
	if limit > 50 {
		limit = 50
	}
	before := c.Query("before")
	callerID := ""
	if user, ok := c.Locals("user").(*models.User); ok {
		callerID = user.ID.Hex()
	}
	page, err := h.service.GetUserReposts(c.Context(), userID, callerID, limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}
	return response.OK(c, "Reposts retrieved", page)
}
