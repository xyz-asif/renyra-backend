package poems

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

// POST /api/v1/poems
func (h *Handler) CreatePoem(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	var req CreatePoemRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	poem, err := h.service.Create(c.Context(), user.ID.Hex(), req)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.Created(c, "Poem created", poem)
}

// GET /api/v1/poems/:id
func (h *Handler) GetPoem(c *fiber.Ctx) error {
	poemID := c.Params("id")

	// Caller ID is optional (unauthenticated users can read public poems)
	callerID := ""
	if user, ok := c.Locals("user").(*models.User); ok {
		callerID = user.ID.Hex()
	}

	poem, err := h.service.GetByID(c.Context(), poemID, callerID)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Poem retrieved", poem)
}

// PATCH /api/v1/poems/:id
func (h *Handler) UpdatePoem(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	poemID := c.Params("id")
	var req UpdatePoemRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	poem, err := h.service.Update(c.Context(), poemID, user.ID.Hex(), req)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Poem updated", poem)
}

// DELETE /api/v1/poems/:id
func (h *Handler) DeletePoem(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	poemID := c.Params("id")
	if err := h.service.Delete(c.Context(), poemID, user.ID.Hex()); err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "Poem deleted", nil)
}

// GET /api/v1/poems/me?limit=20&before=<id>
func (h *Handler) GetMyPoems(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	limit := c.QueryInt("limit", 20)
	before := c.Query("before")

	page, err := h.service.GetMyPoems(c.Context(), user.ID.Hex(), limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "My poems retrieved", page)
}

// GET /api/v1/poems/user/:userId?limit=20&before=<id>
func (h *Handler) GetUserPoems(c *fiber.Ctx) error {
	targetUserID := c.Params("userId")
	limit := c.QueryInt("limit", 20)
	before := c.Query("before")

	callerID := ""
	if user, ok := c.Locals("user").(*models.User); ok {
		callerID = user.ID.Hex()
	}

	page, err := h.service.GetUserPoems(c.Context(), targetUserID, callerID, limit, before)
	if err != nil {
		return response.BadRequest(c, err.Error())
	}

	return response.OK(c, "User poems retrieved", page)
}
