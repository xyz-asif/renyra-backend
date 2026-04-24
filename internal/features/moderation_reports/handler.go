package moderation_reports

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

// CreateReport handles submitting a new moderation report for a user or post
func (h *Handler) CreateReport(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	var req CreateModerationReportRequest
	if err := c.BodyParser(&req); err != nil {
		return response.BadRequest(c, "Invalid request body")
	}

	report, err := h.service.CreateReport(c.Context(), user.ID.Hex(), user.DisplayName, user.Email, req)
	if err != nil {
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "must be") || strings.Contains(err.Error(), "invalid") {
			return response.ValidationFailed(c, err.Error())
		}
		if strings.Contains(err.Error(), "already reported") {
			return response.ValidationFailed(c, err.Error())
		}
		if strings.Contains(err.Error(), "not found") {
			return response.NotFound(c, err.Error())
		}
		return response.InternalError(c, err.Error())
	}

	return response.Created(c, "Report submitted successfully", report)
}

// GetAllReports handles fetching all moderation reports (admin use)
func (h *Handler) GetAllReports(c *fiber.Ctx) error {
	limit := c.QueryInt("limit", 20)
	offset := c.QueryInt("offset", 0)

	var targetType *models.ReportTargetType
	targetTypeStr := c.Query("targetType")
	if targetTypeStr == "user" {
		t := models.ReportTargetTypeUser
		targetType = &t
	} else if targetTypeStr == "post" {
		t := models.ReportTargetTypePost
		targetType = &t
	}

	var status *string
	statusStr := c.Query("status")
	if statusStr != "" {
		status = &statusStr
	}

	paginated, err := h.service.GetAllReports(c.Context(), targetType, status, limit, offset)
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Reports retrieved successfully", paginated)
}

// GetMyReports handles fetching the current user's submitted reports
func (h *Handler) GetMyReports(c *fiber.Ctx) error {
	user, ok := c.Locals("user").(*models.User)
	if !ok {
		return response.Unauthorized(c, "Unauthorized")
	}

	limit := c.QueryInt("limit", 20)
	offset := c.QueryInt("offset", 0)

	paginated, err := h.service.GetMyReports(c.Context(), user.ID.Hex(), limit, offset)
	if err != nil {
		return response.InternalError(c, err.Error())
	}

	return response.OK(c, "Your reports retrieved successfully", paginated)
}

// UpdateReport handles updating a report's status and admin notes (admin use)
func (h *Handler) UpdateReport(c *fiber.Ctx) error {
	reportID := c.Params("id")
	if reportID == "" {
		return response.BadRequest(c, "Report ID is required")
	}

	var req UpdateModerationReportRequest
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
