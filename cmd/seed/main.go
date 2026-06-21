package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/xyz-asif/renyra-backend/internal/config"
	"github.com/xyz-asif/renyra-backend/internal/database"
	"github.com/xyz-asif/renyra-backend/internal/features/poems"
	"github.com/xyz-asif/renyra-backend/internal/features/social"
	"github.com/xyz-asif/renyra-backend/internal/features/users"
	"github.com/xyz-asif/renyra-backend/internal/models"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// noopNotifService satisfies notifications.Service with no side effects.
type noopNotifService struct{}

func (noopNotifService) Send(_ context.Context, _ models.SendNotificationRequest) error {
	return nil
}
func (noopNotifService) GetNotifications(_ context.Context, _ string, _ int, _ string) ([]models.NotificationResponse, bool, error) {
	return nil, false, nil
}
func (noopNotifService) GetUnreadCount(_ context.Context, _ string) (int64, error) { return 0, nil }
func (noopNotifService) MarkAsRead(_ context.Context, _, _ string) error            { return nil }
func (noopNotifService) MarkAllAsRead(_ context.Context, _ string) error            { return nil }

// ── JSON schema ──────────────────────────────────────────────────────────────

type personaPoem struct {
	Title       string   `json:"title"`
	PlainText   string   `json:"plainText"`
	Hashtags    []string `json:"hashtags"`
	Mood        string   `json:"mood"`
	Description *string  `json:"description"`
	TextAlign   string   `json:"textAlign"`
	CoverColor  string   `json:"coverColor"`
}

type persona struct {
	Email string        `json:"email"`
	Poems []personaPoem `json:"poems"`
}

// ── helpers ──────────────────────────────────────────────────────────────────

// buildContentJSON produces a Quill Delta array: [{"insert":"..."}]
// Flutter Quill expects a raw JSON array, not the {"ops":[...]} wrapper.
func buildContentJSON(plainText string) string {
	text := strings.ReplaceAll(plainText, "\r", "")
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	encoded, _ := json.Marshal(text)
	return fmt.Sprintf(`[{"insert":%s}]`, string(encoded))
}

func findUserByEmail(ctx context.Context, db *mongo.Database, email string) (*models.User, error) {
	var user models.User
	err := db.Collection("users").FindOne(ctx, bson.M{"email": email}).Decode(&user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func pickOthers(ids []string, exclude string, n int, rng *rand.Rand) []string {
	pool := make([]string, 0, len(ids)-1)
	for _, id := range ids {
		if id != exclude {
			pool = append(pool, id)
		}
	}
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	if n > len(pool) {
		n = len(pool)
	}
	return pool[:n]
}

// fixContentJSON patches existing seeded poems that have the wrong {"ops":[...]}
// format. Reads plainText from each doc and regenerates contentJson.
func fixContentJSON(ctx context.Context, db *mongo.Database, authorOIDs []bson.ObjectID) {
	type poemDoc struct {
		ID        bson.ObjectID `bson:"_id"`
		PlainText string        `bson:"plainText"`
	}

	cursor, err := db.Collection("poems").Find(ctx, bson.M{
		"authorId":  bson.M{"$in": authorOIDs},
		"isDeleted": false,
	})
	if err != nil {
		log.Printf("fix: query error: %v", err)
		return
	}
	defer cursor.Close(ctx)

	fixed := 0
	for cursor.Next(ctx) {
		var doc poemDoc
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		_, err := db.Collection("poems").UpdateOne(ctx,
			bson.M{"_id": doc.ID},
			bson.M{"$set": bson.M{"contentJson": buildContentJSON(doc.PlainText)}},
		)
		if err != nil {
			log.Printf("fix: update %s: %v", doc.ID.Hex(), err)
			continue
		}
		fixed++
	}
	log.Printf("  Fixed contentJson on %d poems", fixed)
}

// ── main ─────────────────────────────────────────────────────────────────────

func main() {
	fixOnly := flag.Bool("fix-only", false, "fix contentJson on existing seeded poems, skip creating new ones")
	personasFile := flag.String("file", "personas.json", "path to personas JSON file")
	flag.Parse()

	log.Println("Renyra seed script starting...")

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := database.Connect(cfg.MongoDBURI, cfg.DatabaseName)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer db.Client.Disconnect(context.Background())

	notif := noopNotifService{}
	userRepo := users.NewRepository(db.Database)
	socialRepo := social.NewRepository(db.Database)
	poemRepo := poems.NewRepository(db.Database)
	poemService := poems.NewService(poemRepo, userRepo, socialRepo, notif)
	socialService := social.NewService(socialRepo, userRepo, notif, db.Database)

	data, err := os.ReadFile(*personasFile)
	if err != nil {
		log.Fatalf("read personas.json: %v — run from the project root", err)
	}
	var personas []persona
	if err := json.Unmarshal(data, &personas); err != nil {
		log.Fatalf("parse personas.json: %v", err)
	}

	ctx := context.Background()

	// Collect user IDs for all personas (needed by both modes)
	userIDs := make([]string, 0, len(personas))
	for _, p := range personas {
		user, err := findUserByEmail(ctx, db.Database, p.Email)
		if err != nil {
			log.Printf("  SKIP %s — not found in DB: %v", p.Email, err)
			continue
		}
		userIDs = append(userIDs, user.ID.Hex())
	}

	// Build ObjectID slice for fix phase
	authorOIDs := make([]bson.ObjectID, 0, len(userIDs))
	for _, id := range userIDs {
		if oid, err := bson.ObjectIDFromHex(id); err == nil {
			authorOIDs = append(authorOIDs, oid)
		}
	}

	if *fixOnly {
		fmt.Println()
		log.Printf("Fix mode: patching contentJson on existing poems for %d accounts...", len(userIDs))
		fixContentJSON(ctx, db.Database, authorOIDs)
		log.Println("Done.")
		return
	}

	log.Println("WARNING: Run this script only once. Likes are toggled — running again will unlike.")

	// ── Phase 1: create poems ─────────────────────────────────────────────────
	type seededPoem struct {
		id       string
		authorID string
	}
	var allPoems []seededPoem
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	fmt.Println()
	log.Printf("Phase 1: creating poems for %d accounts...", len(personas))

	for _, p := range personas {
		user, err := findUserByEmail(ctx, db.Database, p.Email)
		if err != nil {
			log.Printf("  SKIP %s — not found in DB (have they logged in?): %v", p.Email, err)
			continue
		}
		authorID := user.ID.Hex()
		log.Printf("  account: %s", p.Email)

		created := 0
		for _, poem := range p.Poems {
			desc := ""
			if poem.Description != nil {
				desc = *poem.Description
			}
			req := poems.CreatePoemRequest{
				Title:       poem.Title,
				ContentJSON: buildContentJSON(poem.PlainText),
				PlainText:   poem.PlainText,
				Hashtags:    poem.Hashtags,
				Mood:        poem.Mood,
				IsOriginal:  true,
				Visibility:  "public",
				CoverColor:  poem.CoverColor,
				Description: desc,
				TextAlign:   poem.TextAlign,
			}
			resp, err := poemService.Create(ctx, authorID, req)
			if err != nil {
				log.Printf("    ERR %q: %v", poem.Title, err)
				continue
			}
			allPoems = append(allPoems, seededPoem{id: resp.ID, authorID: authorID})
			created++
		}
		log.Printf("    %d/%d poems created", created, len(p.Poems))
	}

	// ── Phase 2: cross-likes ─────────────────────────────────────────────────
	fmt.Println()
	log.Printf("Phase 2: adding likes to %d poems...", len(allPoems))

	likeCount := 0
	for _, poem := range allPoems {
		for _, likerID := range pickOthers(userIDs, poem.authorID, 3, rng) {
			if _, _, err := socialService.TogglePoemLike(ctx, likerID, poem.id); err != nil {
				log.Printf("  like ERR poem %s: %v", poem.id, err)
				continue
			}
			likeCount++
		}
	}

	// ── Phase 3: fix contentJson on newly created poems ───────────────────────
	fmt.Println()
	log.Printf("Phase 3: verifying contentJson format...")
	fixContentJSON(ctx, db.Database, authorOIDs)

	fmt.Println()
	log.Printf("Done.")
	log.Printf("  Poems created  : %d", len(allPoems))
	log.Printf("  Likes added    : %d", likeCount)
	log.Printf("  Accounts seeded: %d/%d", len(userIDs), len(personas))
}
