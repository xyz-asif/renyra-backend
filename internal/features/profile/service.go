package profile

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/xyz-asif/renyra-backend/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Reserved usernames that cannot be registered
var reservedUsernames = map[string]bool{
	"admin": true, "support": true, "editor": true, "chatbee": true,
	"poetry": true, "official": true, "moderator": true, "help": true,
	"me": true, "settings": true, "explore": true, "feed": true,
	"search": true, "notifications": true, "profile": true,
}

// username format: lowercase, alphanumeric + underscore, 3–30 chars
var usernameRegex = regexp.MustCompile(`^[a-z0-9_]{3,30}$`)

type Service interface {
	SetupProfile(ctx context.Context, userID string, req ProfileSetupRequest) (*models.User, error)
	CheckUsername(ctx context.Context, username string) (CheckUsernameResult, error)
	SetUsername(ctx context.Context, userID string, username string) (*models.User, error)
}

type ProfileSetupRequest struct {
	DisplayName   string `json:"displayName"`
	Bio           string `json:"bio"`
	ExternalLink  string `json:"externalLink"`
	PhotoURL      string `json:"photoURL"`
	CoverImageURL string `json:"coverImageURL"`
}

type CheckUsernameResult struct {
	Username  string `json:"username"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"` // "taken" | "invalid_format" | "reserved" | ""
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) SetupProfile(ctx context.Context, userIDStr string, req ProfileSetupRequest) (*models.User, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}

	// Validate display name
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if req.DisplayName == "" {
		return nil, errors.New("display name is required")
	}
	if len(req.DisplayName) > 50 {
		return nil, errors.New("display name must be 50 characters or less")
	}

	// Validate bio
	if len(req.Bio) > 200 {
		return nil, errors.New("bio must be 200 characters or less")
	}

	// Validate external link (basic check)
	if req.ExternalLink != "" {
		if !strings.HasPrefix(req.ExternalLink, "http://") && !strings.HasPrefix(req.ExternalLink, "https://") {
			return nil, errors.New("external link must start with http:// or https://")
		}
	}

	return s.repo.UpdateProfile(ctx, userID, ProfileUpdateRequest{
		DisplayName:   req.DisplayName,
		Bio:           req.Bio,
		ExternalLink:  req.ExternalLink,
		PhotoURL:      req.PhotoURL,
		CoverImageURL: req.CoverImageURL,
	})
}

func (s *service) CheckUsername(ctx context.Context, username string) (CheckUsernameResult, error) {
	username = strings.ToLower(strings.TrimSpace(username))

	result := CheckUsernameResult{Username: username}

	// Format validation
	if !usernameRegex.MatchString(username) {
		result.Available = false
		result.Reason = "invalid_format"
		return result, nil
	}

	// Reserved check
	if reservedUsernames[username] {
		result.Available = false
		result.Reason = "reserved"
		return result, nil
	}

	// DB availability check
	taken, err := s.repo.IsUsernameTaken(ctx, username)
	if err != nil {
		return result, err
	}

	if taken {
		result.Available = false
		result.Reason = "taken"
	} else {
		result.Available = true
	}

	return result, nil
}

func (s *service) SetUsername(ctx context.Context, userIDStr string, username string) (*models.User, error) {
	userID, err := bson.ObjectIDFromHex(userIDStr)
	if err != nil {
		return nil, errors.New("invalid user id")
	}

	username = strings.ToLower(strings.TrimSpace(username))

	// Re-validate everything before writing
	if !usernameRegex.MatchString(username) {
		return nil, errors.New("invalid username format: use 3-30 lowercase letters, numbers, or underscores")
	}
	if reservedUsernames[username] {
		return nil, errors.New("this username is reserved")
	}

	return s.repo.SetUsername(ctx, userID, username)
}
