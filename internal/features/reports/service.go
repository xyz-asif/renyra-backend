package reports

import (
	"context"
	"errors"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/xyz-asif/renyra-backend/internal/features/notifications"
	"github.com/xyz-asif/renyra-backend/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type Service interface {
	CreateReport(ctx context.Context, userID, userName, email string, req CreateReportRequest) (*models.Report, error)
	GetReports(ctx context.Context, isBug *bool, limit, offset int) (*PaginatedReports, error)
	GetMyReports(ctx context.Context, userID string, isBug *bool, limit, offset int) (*PaginatedReports, error)
	UpdateReport(ctx context.Context, reportID string, req UpdateReportRequest) (*models.Report, error)
}

type service struct {
	repo         Repository
	notifService notifications.Service
}

func NewService(repo Repository, notifService notifications.Service) Service {
	return &service{
		repo:         repo,
		notifService: notifService,
	}
}

type CreateReportRequest struct {
	IsBug       bool   `json:"isBug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	ImageURL    string `json:"imageURL,omitempty"`
	AppVersion  string `json:"appVersion,omitempty"`
	DeviceInfo  string `json:"deviceInfo,omitempty"`
	Platform    string `json:"platform,omitempty"`
}

type UpdateReportRequest struct {
	Status     string `json:"status,omitempty"`
	AdminReply string `json:"adminReply,omitempty"`
}

type PaginatedReports struct {
	Reports []models.Report `json:"reports"`
	Pagination struct {
		Limit   int  `json:"limit"`
		Offset  int  `json:"offset"`
		HasMore bool `json:"hasMore"`
	} `json:"pagination"`
}

func (s *service) CreateReport(ctx context.Context, userID, userName, email string, req CreateReportRequest) (*models.Report, error) {
	// Validate User ID
	oid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	// Validate title and description
	title := strings.TrimSpace(req.Title)
	if title == "" || len(title) > 200 {
		return nil, errors.New("title is required and must be between 1 and 200 characters")
	}

	description := strings.TrimSpace(req.Description)
	if description == "" || len(description) > 5000 {
		return nil, errors.New("description is required and must be between 1 and 5000 characters")
	}

	// Optional: validate imageURL
	if req.ImageURL != "" {
		if _, err := url.ParseRequestURI(req.ImageURL); err != nil {
			return nil, errors.New("invalid image URL format")
		}
	}

	// Validate Platform
	platform := strings.ToLower(req.Platform)
	if platform != "" && platform != "ios" && platform != "android" && platform != "web" {
		return nil, errors.New("platform must be ios, android, or web")
	}

	report := &models.Report{
		UserID:      oid,
		UserName:    userName,
		Email:       email,
		IsBug:       req.IsBug,
		Title:       title,
		Description: description,
		ImageURL:    req.ImageURL,
		Status:      "open",   // default status
		Priority:    "medium", // default priority
		AppVersion:  req.AppVersion,
		DeviceInfo:  req.DeviceInfo,
		Platform:    platform,
	}

	if err := s.repo.Create(ctx, report); err != nil {
		return nil, err
	}

	return report, nil
}

func (s *service) GetReports(ctx context.Context, isBug *bool, limit, offset int) (*PaginatedReports, error) {
	if limit <= 0 {
		limit = 20
	} else if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	reports, hasMore, err := s.repo.GetAll(ctx, isBug, limit, offset)
	if err != nil {
		return nil, err
	}

	// Ensure we return an empty array instead of null
	if reports == nil {
		reports = make([]models.Report, 0)
	}

	res := &PaginatedReports{
		Reports: reports,
	}
	res.Pagination.Limit = limit
	res.Pagination.Offset = offset
	res.Pagination.HasMore = hasMore

	return res, nil
}

func (s *service) GetMyReports(ctx context.Context, userID string, isBug *bool, limit, offset int) (*PaginatedReports, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	oid, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	reports, hasMore, err := s.repo.GetByUserID(ctx, oid, isBug, limit, offset)
	if err != nil {
		return nil, err
	}

	if reports == nil {
		reports = make([]models.Report, 0)
	}

	res := &PaginatedReports{
		Reports: reports,
	}
	res.Pagination.Limit = limit
	res.Pagination.Offset = offset
	res.Pagination.HasMore = hasMore

	return res, nil
}

func (s *service) UpdateReport(ctx context.Context, reportID string, req UpdateReportRequest) (*models.Report, error) {
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
		validStatuses := map[string]bool{"open": true, "in_progress": true, "resolved": true, "closed": true}
		if !validStatuses[req.Status] {
			return nil, errors.New("invalid status value")
		}
		updates["status"] = req.Status
	}

	if req.AdminReply != "" {
		updates["adminReply"] = strings.TrimSpace(req.AdminReply)
	}

	if len(updates) == 0 {
		return existing, nil
	}

	// Capture what actually changed before writing, for notification purposes.
	statusChanged := req.Status != "" && req.Status != existing.Status
	newAdminReply := strings.TrimSpace(req.AdminReply)
	replyChanged := newAdminReply != "" && newAdminReply != existing.AdminReply

	if err := s.repo.Update(ctx, oid, updates); err != nil {
		return nil, err
	}

	// Fetch updated document
	updated, err := s.repo.GetByID(ctx, oid)
	if err != nil {
		return nil, err
	}

	// Send at most ONE notification per update to avoid duplicate alerts when both
	// status and adminReply change in the same request.
	// Priority: admin reply > status-only change.
	if s.notifService != nil && existing.UserID != bson.NilObjectID && (statusChanged || replyChanged) {
		resType := models.ResourceTypeFeatureRequest
		if existing.IsBug {
			resType = models.ResourceTypeBugReport
		}

		var notifType, title, body, groupKey string
		if replyChanged {
			replyBody := newAdminReply
			if len(replyBody) > 300 {
				replyBody = replyBody[:300]
			}
			notifType = models.NotifTypeReportAdminReply
			title = "Admin replied to your report"
			body = replyBody
			groupKey = "report_reply:" + oid.Hex()
		} else {
			notifType = models.NotifTypeReportStatusUpdated
			title = "Your report status was updated"
			body = "Status: " + req.Status
			groupKey = "report_status:" + oid.Hex()
		}

		go func(recipientID bson.ObjectID, rt, nType, nTitle, nBody, nGroupKey, reportIDHex string) {
			bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.notifService.Send(bgCtx, models.SendNotificationRequest{
				RecipientID:  recipientID,
				ActorID:      bson.NilObjectID,
				Type:         nType,
				ResourceType: rt,
				ResourceID:   reportIDHex,
				Title:        nTitle,
				Body:         nBody,
				GroupKey:     nGroupKey,
			}); err != nil {
				log.Printf("[reports] notification failed: %v", err)
			}
		}(existing.UserID, resType, notifType, title, body, groupKey, oid.Hex())
	}

	return updated, nil
}
