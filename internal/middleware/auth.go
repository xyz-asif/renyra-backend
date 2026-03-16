package middleware

import (
	"context"
	"log"
	"strings"

	firebase "firebase.google.com/go/v4"
	"github.com/gofiber/fiber/v2"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"github.com/xyz-asif/gotodo/internal/models"
	"github.com/xyz-asif/gotodo/pkg/response"
	"google.golang.org/api/option"
)

type AuthMiddleware struct {
	App         *firebase.App
	userService users.Service
}

func NewAuthMiddleware(credPath, projectID string, userService users.Service) (*AuthMiddleware, error) {
	ctx := context.Background()
	opts := []option.ClientOption{}

	if credPath != "" {
		opts = append(opts, option.WithCredentialsFile(credPath))
	}

	config := &firebase.Config{ProjectID: projectID}

	app, err := firebase.NewApp(ctx, config, opts...)
	if err != nil {
		return nil, err
	}

	return &AuthMiddleware{
		App:         app,
		userService: userService,
	}, nil
}

func (am *AuthMiddleware) VerifyToken(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	token := ""

	if authHeader != "" {
		token = strings.TrimPrefix(authHeader, "Bearer ")
		if token == authHeader {
			return response.Unauthorized(c, "Invalid Authorization header format")
		}
	} else {
		// Check for token in query parameter (for WebSocket connections)
		token = c.Query("token")
		if token == "" {
			return response.Unauthorized(c, "Missing Authorization header")
		}
	}

	client, err := am.App.Auth(c.Context())
	if err != nil {
		log.Printf("Error getting auth client: %v", err)
		return response.InternalError(c, "Internal Server Error")
	}

	decodedToken, err := client.VerifyIDToken(c.Context(), token)
	if err != nil {
		log.Printf("Error verifying token: %v", err)
		return response.Unauthorized(c, "Invalid token")
	}

	uid := decodedToken.UID
	email, _ := decodedToken.Claims["email"].(string)
	picture, _ := decodedToken.Claims["picture"].(string)
	name, _ := decodedToken.Claims["name"].(string)

	// critical: Get or Create User
	user, err := am.userService.GetOrCreateUser(c.Context(), uid, email, name, picture)
	if err != nil {
		log.Printf("Error hydrating user: %v", err)
		return response.InternalError(c, "Failed to load user profile")
	}

	// Defensive check: ensure user and user.ID are valid
	if user == nil {
		log.Printf("GetOrCreateUser returned nil user for uid: %s", uid)
		return response.InternalError(c, "Failed to load user profile")
	}

	if user.ID.IsZero() {
		log.Printf("User ID is zero for uid: %s, email: %s", uid, email)
		return response.InternalError(c, "User profile is incomplete")
	}

	// Store user info in context
	c.Locals("uid", uid)
	c.Locals("email", email)
	c.Locals("user", user)

	return c.Next()
}

// Protect returns the VerifyToken middleware for routes that require authentication
func (am *AuthMiddleware) Protect() fiber.Handler {
	return am.VerifyToken
}

// extractToken extracts the Bearer token from Authorization header or query param
func extractToken(c *fiber.Ctx) string {
	authHeader := c.Get("Authorization")
	if authHeader != "" {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != authHeader {
			return token
		}
	}
	// Check for token in query parameter (for WebSocket connections)
	return c.Query("token")
}

// verifyAndGetUser verifies the token and returns the user
func (am *AuthMiddleware) verifyAndGetUser(ctx context.Context, token string) (*models.User, error) {
	client, err := am.App.Auth(ctx)
	if err != nil {
		return nil, err
	}

	decodedToken, err := client.VerifyIDToken(ctx, token)
	if err != nil {
		return nil, err
	}

	uid := decodedToken.UID
	email, _ := decodedToken.Claims["email"].(string)
	picture, _ := decodedToken.Claims["picture"].(string)
	name, _ := decodedToken.Claims["name"].(string)

	return am.userService.GetOrCreateUser(ctx, uid, email, name, picture)
}

// OptionalAuth — sets user in locals if token present, continues regardless
func (am *AuthMiddleware) OptionalAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := extractToken(c)
		if token != "" {
			if user, err := am.verifyAndGetUser(c.Context(), token); err == nil && user != nil && !user.ID.IsZero() {
				c.Locals("uid", user.FirebaseUID)
				c.Locals("email", user.Email)
				c.Locals("user", user)
			}
		}
		return c.Next()
	}
}
