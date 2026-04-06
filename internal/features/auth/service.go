package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	firebase "firebase.google.com/go/v4"
	"github.com/golang-jwt/jwt/v5"
	"github.com/xyz-asif/gotodo/internal/config"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	accessTokenTTL  = 24 * time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour
)

// Claims are the JWT payload fields.
type Claims struct {
	UserID      string `json:"sub"`
	FirebaseUID string `json:"uid"`
	Email       string `json:"email"`
	jwt.RegisteredClaims
}

type Service interface {
	Exchange(ctx context.Context, firebaseToken string) (accessToken, refreshToken string, err error)
	Refresh(ctx context.Context, refreshToken string) (newAccessToken, newRefreshToken string, err error)
	Logout(ctx context.Context, refreshToken string) error
}

type service struct {
	repo        Repository
	userService users.Service
	firebaseApp *firebase.App
	cfg         *config.Config
}

func NewService(repo Repository, userService users.Service, firebaseApp *firebase.App, cfg *config.Config) Service {
	return &service{
		repo:        repo,
		userService: userService,
		firebaseApp: firebaseApp,
		cfg:         cfg,
	}
}

// Exchange verifies a Firebase ID token (once) and issues a JWT access + refresh token pair.
func (s *service) Exchange(ctx context.Context, firebaseToken string) (string, string, error) {
	// 1. Verify Firebase token — the only time Firebase is contacted per session
	client, err := s.firebaseApp.Auth(ctx)
	if err != nil {
		return "", "", errors.New("firebase auth unavailable")
	}
	decoded, err := client.VerifyIDToken(ctx, firebaseToken)
	if err != nil {
		return "", "", errors.New("invalid firebase token")
	}

	uid := decoded.UID
	email, _ := decoded.Claims["email"].(string)
	picture, _ := decoded.Claims["picture"].(string)
	name, _ := decoded.Claims["name"].(string)

	// 2. Get or create user in MongoDB
	user, err := s.userService.GetOrCreateUser(ctx, uid, email, name, picture)
	if err != nil {
		return "", "", err
	}

	// 3. Issue JWT access token
	accessToken, err := s.signAccessToken(user.ID.Hex(), uid, email)
	if err != nil {
		return "", "", err
	}

	// 4. Generate and store refresh token
	refreshToken, err := s.createRefreshToken(ctx, user.ID)
	if err != nil {
		return "", "", err
	}

	return accessToken, refreshToken, nil
}

// Refresh validates a refresh token, rotates it, and issues a new access + refresh token pair.
func (s *service) Refresh(ctx context.Context, refreshToken string) (string, string, error) {
	hash := hashToken(refreshToken)

	// 1. Look up hashed token in DB
	rt, err := s.repo.FindRefreshToken(ctx, hash)
	if err != nil {
		return "", "", errors.New("invalid or expired refresh token")
	}

	// 2. Check expiry (belt-and-suspenders; MongoDB TTL index also cleans up)
	if time.Now().After(rt.ExpiresAt) {
		_ = s.repo.DeleteRefreshToken(ctx, hash)
		return "", "", errors.New("refresh token expired")
	}

	// 3. Delete old record immediately (token rotation — prevents replay)
	if err := s.repo.DeleteRefreshToken(ctx, hash); err != nil {
		return "", "", err
	}

	// 4. Load user for claims
	user, err := s.userService.GetUserByID(ctx, rt.UserID.Hex())
	if err != nil {
		return "", "", err
	}

	// 5. Issue new access token
	newAccess, err := s.signAccessToken(user.ID.Hex(), user.FirebaseUID, user.Email)
	if err != nil {
		return "", "", err
	}

	// 6. Issue new refresh token
	newRefresh, err := s.createRefreshToken(ctx, user.ID)
	if err != nil {
		return "", "", err
	}

	return newAccess, newRefresh, nil
}

// Logout revokes the refresh token server-side.
func (s *service) Logout(ctx context.Context, refreshToken string) error {
	hash := hashToken(refreshToken)
	return s.repo.DeleteRefreshToken(ctx, hash)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func (s *service) signAccessToken(userID, firebaseUID, email string) (string, error) {
	claims := Claims{
		UserID:      userID,
		FirebaseUID: firebaseUID,
		Email:       email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(accessTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.cfg.JWTSecret))
}

func (s *service) createRefreshToken(ctx context.Context, userID bson.ObjectID) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	plain := hex.EncodeToString(raw)
	hash := hashToken(plain)

	if err := s.repo.SaveRefreshToken(ctx, userID, hash, time.Now().Add(refreshTokenTTL)); err != nil {
		return "", err
	}
	return plain, nil
}

func hashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}
