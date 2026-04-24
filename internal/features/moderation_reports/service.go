package moderation_reports

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/xyz-asif/renyra-backend/internal/features/notifications"
	"github.com/xyz-asif/renyra-backend/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Service interface {
	CreateReport(ctx context.Context, reporterID, reporterName, reporterEmail string, req CreateModerationReportRequest) (*models.ModerationReport, error)
	GetAllReports(ctx context.Context, targetType *models.ReportTargetType, status *string, limit, offset int) (*PaginatedModerationReports, error)
	GetMyReports(ctx context.Context, reporterID string, limit, offset int) (*PaginatedModerationReports, error)
	UpdateReport(ctx context.Context, reportID string, req UpdateModerationReportRequest) (*models.ModerationReport, error)
}

type service struct {
	repo         Repository
	notifService notifications.Service
	userRepo     UserRepository
	poemRepo     PoemRepository
}

type UserRepository interface {
	GetUserByID(ctx context.Context, id bson.ObjectID) (*models.User, error)
}

type PoemRepository interface {
	GetByID(ctx context.Context, poemID bson.ObjectID) (*models.Poem, error)
}

type CreateModerationReportRequest struct {
	TargetType models.ReportTargetType `json:"targetType"`
	TargetID   string                  `json:"targetId"`
	Reason     models.ReportReason     `json:"reason"`
	Details    string                  `json:"details,omitempty"`
}

type UpdateModerationReportRequest struct {
	Status       string `json:"status,omitempty"`
	AdminNotes   string `json:"adminNotes,omitempty"`
	Resolution   string `json:"resolution,omitempty"`
}

type PaginatedModerationReports struct {
	Reports    []models.ModerationReport `json:"reports"`
	Pagination struct {
		Limit   int  `json:"limit"`
		Offset  int  `json:"offset"`
		HasMore bool `json:"hasMore"`
	} `json:"pagination"`
}

func NewService(repo Repository, notifService notifications.Service, userRepo UserRepository, poemRepo PoemRepository) Service {
	return &service{
		repo:         repo,
		notifService: notifService,
		userRepo:     userRepo,
		poemRepo:     poemRepo,
	}
}

func (s *service) CreateReport(ctx context.Context, reporterID, reporterName, reporterEmail string, req CreateModerationReportRequest) (*models.ModerationReport, error) {
	// Validate reporter ID
	reporterOID, err := bson.ObjectIDFromHex(reporterID)
	if err != nil {
		return nil, errors.New("invalid reporter ID")
	}

	// Validate target ID
	targetOID, err := bson.ObjectIDFromHex(req.TargetID)
	if err != nil {
		return nil, errors.New("invalid target ID")
	}

	// Validate target type
	if req.TargetType != models.ReportTargetTypeUser && req.TargetType != models.ReportTargetTypePost {
		return nil, errors.New("targetType must be 'user' or 'post'")
	}

	// Validate reason
	validReasons := map[models.ReportReason]bool{
		models.ReportReasonSpam:          true,
		models.ReportReasonHarassment:    true,
		models.ReportReasonInappropriate: true,
		models.ReportReasonImpersonation: true,
		models.ReportReasonOther:         true,
	}
	if !validReasons[req.Reason] {
		return nil, errors.New("invalid reason: must be one of spam_or_misleading, harassment_or_bullying, inappropriate_content, impersonation, or other")
	}

	// Validate details length (optional but max 2000 chars)
	details := strings.TrimSpace(req.Details)
	if len(details) > 2000 {
		return nil, errors.New("details must be at most 2000 characters")
	}

	// Check if user already reported this target
	existing, err := s.repo.GetByReporterAndTarget(ctx, reporterOID, targetOID, req.TargetType)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("you have already reported this " + string(req.TargetType))
	}

	// Get target name (user display name or post title)
	var targetName string
	if req.TargetType == models.ReportTargetTypeUser {
		user, err := s.userRepo.GetUserByID(ctx, targetOID)
		if err != nil {
			return nil, errors.New("target user not found")
		}
		if user == nil {
			return nil, errors.New("target user not found")
		}
		targetName = user.DisplayName
	} else {
		poem, err := s.poemRepo.GetByID(ctx, targetOID)
		if err != nil {
			return nil, errors.New("target post not found")
		}
		if poem == nil {
			return nil, errors.New("target post not found")
		}
		targetName = poem.Title
	}

	report := &models.ModerationReport{
		ReporterID:    reporterOID,
		ReporterName:  reporterName,
		ReporterEmail: reporterEmail,
		TargetType:    req.TargetType,
		TargetID:      targetOID,
		TargetName:    targetName,
		Reason:        req.Reason,
		Details:       details,
		Status:        "pending",
	}

	if err := s.repo.Create(ctx, report); err != nil {
		return nil, err
	}

	return report, nil
}

func (s *service) GetAllReports(ctx context.Context, targetType *models.ReportTargetType, status *string, limit, offset int) (*PaginatedModerationReports, error) {
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	reports, hasMore, err := s.repo.GetAll(ctx, targetType, status, limit, offset)
	if err != nil {
		return nil, err
	}

	if reports == nil {
		reports = make([]models.ModerationReport, 0)
	}

	res := &PaginatedModerationReports{
		Reports: reports,
	}
	res.Pagination.Limit = limit
	res.Pagination.Offset = offset
	res.Pagination.HasMore = hasMore

	return res, nil
}

func (s *service) GetMyReports(ctx context.Context, reporterID string, limit, offset int) (*PaginatedModerationReports, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	reporterOID, err := bson.ObjectIDFromHex(reporterID)
	if err != nil {
		return nil, errors.New("invalid reporter ID")
	}

	reports, hasMore, err := s.repo.GetByReporterID(ctx, reporterOID, limit, offset)
	if err != nil {
		return nil, err
	}

	if reports == nil {
		reports = make([]models.ModerationReport, 0)
	}

	res := &PaginatedModerationReports{
		Reports: reports,
	}
	res.Pagination.Limit = limit
	res.Pagination.Offset = offset
	res.Pagination.HasMore = hasMore

	return res, nil
}

func (s *service) UpdateReport(ctx context.Context, reportID string, req UpdateModerationReportRequest) (*models.ModerationReport, error) {
	oid, err := bson.ObjectIDFromHex(reportID)
	if err != nil {
		return nil, errors.New("invalid report ID")
	}

	existing, err := s.repo.GetByID(ctx, oid)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, errors.New("report not found")
	}

	updates := make(map[string]interface{})

	if req.Status != "" {
		validStatuses := map[string]bool{
			"pending":       true,
			"under_review":  true,
			"resolved":      true,
			"dismissed":     true,
		}
		if !validStatuses[req.Status] {
			return nil, errors.New("invalid status value")
		}
		updates["status"] = req.Status
	}

	if req.AdminNotes != "" {
		updates["adminNotes"] = strings.TrimSpace(req.AdminNotes)
	}

	if req.Resolution != "" {
		updates["resolution"] = strings.TrimSpace(req.Resolution)
	}

	if len(updates) == 0 {
		return existing, nil
	}

	// Track status change for notification
	statusChanged := req.Status != "" && req.Status != existing.Status

	if err := s.repo.Update(ctx, oid, updates); err != nil {
		return nil, err
	}

	// Fetch updated document
	updated, err := s.repo.GetByID(ctx, oid)
	if err != nil {
		return nil, err
	}

	// Send notification to reporter if status changed to resolved or dismissed
	if s.notifService != nil && existing.ReporterID != bson.NilObjectID && statusChanged {
		if req.Status == "resolved" || req.Status == "dismissed" {
			var title, body string
			if req.Status == "resolved" {
				title = "Your report has been resolved"
				body = "We have reviewed your report about " + existing.TargetName + " and taken appropriate action."
			} else {
				title = "Your report has been dismissed"
				body = "We have reviewed your report about " + existing.TargetName + " and found no violation of our guidelines."
			}

			resType := models.ResourceTypeReportUser
			if existing.TargetType == models.ReportTargetTypePost {
				resType = models.ResourceTypeReportPost
			}

			go func(recipientID bson.ObjectID, nTitle, nBody string, resType string, reportIDHex string) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := s.notifService.Send(bgCtx, models.SendNotificationRequest{
					RecipientID:  recipientID,
					ActorID:      bson.NilObjectID,
					Type:         models.NotifTypeReportStatusUpdated,
					ResourceType: resType,
					ResourceID:   reportIDHex,
					Title:        nTitle,
					Body:         nBody,
					GroupKey:     "moderation_report:" + reportIDHex,
				}); err != nil {
					// Log error but don't fail the update
				}
			}(existing.ReporterID, title, body, string(resType), oid.Hex())
		}
	}

	return updated, nil
}
