package reports

import (
	"strings"

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

// CreateReport handles submitting a new bug report or feature request
func (h *Handler) CreateReport(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	var req CreateReportRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	report, err := h.service.CreateReport(c.Context(), user.ID.Hex(), user.DisplayName, user.Email, req)
	if err != nil {
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "must be") {
			return response.ValidationFailed(c, err.Error())
		}
		return response.InternalError(c, err.Error())
	}

	return response.Created(c, "Report submitted successfully", report)
}

// GetReports handles fetching all paginated reports (public)
func (h *Handler) GetReports(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 20)
	offset := c.QueryInt("offset", 0)
	
	var isBug *bool
	isBugStr := c.Query("isBug")
	if isBugStr == "true" {
		t := true
		isBug = &t
	} else if isBugStr == "false" {
		f := false
		isBug = &f
	}

	paginated, err := h.service.GetReports(c.Context(), isBug, limit, offset)
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Reports retrieved successfully", paginated)
}

// GetMyReports handles fetching the currently authenticated user's reports
func (h *Handler) GetMyReports(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	limit := c.QueryInt("limit", 20)
	offset := c.QueryInt("offset", 0)

	var isBug *bool
	isBugStr := c.Query("isBug")
	if isBugStr == "true" {
		t := true
		isBug = &t
	} else if isBugStr == "false" {
		f := false
		isBug = &f
	}

	paginated, err := h.service.GetMyReports(c.Context(), user.ID.Hex(), isBug, limit, offset)
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "User reports retrieved successfully", paginated)
}

// UpdateReport handles patching status and adminReply (public, admin use)
func (h *Handler) UpdateReport(c *fiber.Ctx) error {
	reportID := c.Params("id")
	if reportID == "" {
		return response.BadRequest(c, "Report ID is required")
	}

	var req UpdateReportRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	updated, err := h.service.UpdateReport(c.Context(), reportID, req)
	if err != nil {
		if err.Error() == "report not found" {
			return response.NotFound(c, err.Error())
		}
		if strings.Contains(err.Error(), "invalid") {
			return response.ValidationFailed(c, err.Error())
		}
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Report updated successfully", updated)
}
