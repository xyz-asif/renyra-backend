package middleware

import (
	"errors"
	"log"
	"strings"

	firebase "firebase.google.com/go/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gofiber/fiber/v2"
	"github.com/xyz-asif/renyra-backend/internal/config"
	"github.com/xyz-asif/renyra-backend/internal/features/auth"
	"github.com/xyz-asif/renyra-backend/internal/features/users"
	"github.com/xyz-asif/renyra-backend/pkg/response"
)

type AuthMiddleware struct {
	App         *firebase.App
	userService users.Service
	cfg         *config.Config
}

func NewAuthMiddleware(app *firebase.App, userService users.Service, cfg *config.Config) (*AuthMiddleware, error) {
	if app == nil {
		return nil, fiber.NewError(500, "firebase app is required")
	}
	return &AuthMiddleware{App: app, userService: userService, cfg: cfg}, nil
}

// VerifyToken validates the JWT access token locally (no Firebase network call).
func (am *AuthMiddleware) VerifyToken(c *fiber.Ctx) error {
	tokenStr := extractToken(c)
	if tokenStr == "" {
		return response.Unauthorized(c, "Missing Authorization header")
	}

	claims, err := am.parseJWT(tokenStr)
	if err != nil {
		log.Printf("JWT verification failed: %v", err)
		return response.Unauthorized(c, "Invalid token")
	}

	user, err := am.userService.GetUserByID(c.Context(), claims.UserID)
	if err != nil || user == nil || user.ID.IsZero() {
		log.Printf("User not found for sub=%s: %v", claims.UserID, err)
		return response.Unauthorized(c, "User not found")
	}

	c.Locals("uid", user.FirebaseUID)
	c.Locals("email", user.Email)
	c.Locals("user", user)
	return c.Next()
}

// Protect is an alias for VerifyToken used by route setup.
func (am *AuthMiddleware) Protect() fiber.Handler {
	return am.VerifyToken
}

// OptionalAuth sets user in locals if a valid token is present; continues regardless.
func (am *AuthMiddleware) OptionalAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tokenStr := extractToken(c)
		if tokenStr != "" {
			if claims, err := am.parseJWT(tokenStr); err == nil {
				if user, err := am.userService.GetUserByID(c.Context(), claims.UserID); err == nil && user != nil && !user.ID.IsZero() {
					c.Locals("uid", user.FirebaseUID)
					c.Locals("email", user.Email)
					c.Locals("user", user)
				}
			}
		}
		return c.Next()
	}
}

// parseJWT validates the token signature and expiry, returning the claims.
func (am *AuthMiddleware) parseJWT(tokenStr string) (*auth.Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &auth.Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(am.cfg.JWTSecret), nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}
	claims, ok := token.Claims.(*auth.Claims)
	if !ok {
		return nil, errors.New("invalid token claims")
	}
	return claims, nil
}

// extractToken extracts the Bearer token from Authorization header or ?token= query param.
func extractToken(c *fiber.Ctx) string {
	authHeader := c.Get("Authorization")
	if authHeader != "" {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != authHeader {
			return token
		}
	}
	return c.Query("token")
}
