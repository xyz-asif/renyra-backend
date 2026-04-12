// Package main Chat API
package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	firebase "firebase.google.com/go/v4"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"google.golang.org/api/option"
	"github.com/xyz-asif/renyra-backend/internal/config"
	"github.com/xyz-asif/renyra-backend/internal/database"
	"github.com/xyz-asif/renyra-backend/internal/features/auth"
	"github.com/xyz-asif/renyra-backend/internal/features/chat"
	"github.com/xyz-asif/renyra-backend/internal/features/connections"
	"github.com/xyz-asif/renyra-backend/internal/features/feed"
	"github.com/xyz-asif/renyra-backend/internal/features/follows"
	"github.com/xyz-asif/renyra-backend/internal/features/notifications"
	"github.com/xyz-asif/renyra-backend/internal/features/poems"
	"github.com/xyz-asif/renyra-backend/internal/features/profile"
	"github.com/xyz-asif/renyra-backend/internal/features/reports"
	"github.com/xyz-asif/renyra-backend/internal/features/social"
	"github.com/xyz-asif/renyra-backend/internal/features/users"
	"github.com/xyz-asif/renyra-backend/internal/middleware"
	"github.com/xyz-asif/renyra-backend/internal/routes"
	"github.com/xyz-asif/renyra-backend/pkg/response"
)

func main() {
	// Force IPv4 for all outbound connections.
	// The server has no working IPv6 route to Google (Firebase token verification
	// hangs for 60s on IPv6 before timing out, causing intermittent 401s).
	http.DefaultTransport.(*http.Transport).DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp4", addr)
	}

	// 1. Load Config
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// 2. Connect Database
	db, err := database.Connect(cfg.MongoDBURI, cfg.DatabaseName)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	// 3. Create MongoDB Indexes
	if err := database.CreateIndexes(context.Background(), db.Database); err != nil {
		log.Printf("Warning: Failed to create indexes: %v", err)
	}

	// Initialize Firebase App globally
	var opts []option.ClientOption
	if cfg.FirebaseCredsPath != "" {
		opts = append(opts, option.WithCredentialsFile(cfg.FirebaseCredsPath))
	}
	firebaseApp, err := firebase.NewApp(context.Background(), &firebase.Config{ProjectID: cfg.FirebaseProjectID}, opts...)
	if err != nil {
		log.Printf("Warning: Failed to initialize Firebase App: %v", err)
	}

	// 4. Setup Repositories
	userRepo := users.NewRepository(db.Database)
	connectionRepo := connections.NewRepository(db.Database)
	chatRepo := chat.NewRepository(db.Database)

	// Initialize WebSockets Hub
	chatHub := chat.NewHub()
	go chatHub.Run() // Run the hub in a background goroutine

	// Initialize notification system
	notifRepo := notifications.NewRepository(db.Database)
	fcmSender := notifications.NewFirebaseFCM(firebaseApp)
	notifService := notifications.NewService(notifRepo, userRepo, chatHub, fcmSender)
	notifHandler := notifications.NewHandler(notifService)

	// Initialize services
	userService := users.NewService(userRepo, chatHub, connectionRepo, chatRepo)
	connectionService := connections.NewService(connectionRepo, notifService, userRepo)
	chatService := chat.NewService(chatRepo, userRepo, connectionRepo, chatHub, notifService)

	// Wire up dependencies for real-time room creation
	connectionService.SetHub(chatHub)
	connectionService.SetChatService(chatService)

	// Initialize handlers
	userHandler := users.NewHandler(userService)
	connectionHandler := connections.NewHandler(connectionService)
	chatHandler := chat.NewHandler(chatService)

	// Initialize poetry feature
	profileRepo := profile.NewRepository(db.Database)
	profileService := profile.NewService(profileRepo)
	profileHandler := profile.NewHandler(profileService)

	// Initialize social feature (likes, comments, reposts) — before poems so we can enrich isLikedByMe
	socialRepo := social.NewRepository(db.Database)

	poemRepo := poems.NewRepository(db.Database)
	poemService := poems.NewService(poemRepo, userRepo, socialRepo, notifService)
	poemHandler := poems.NewHandler(poemService)

	followRepo := follows.NewRepository(db.Database)
	followService := follows.NewService(followRepo, userRepo, notifService, db.Client)
	followHandler := follows.NewHandler(followService)

	socialService := social.NewService(socialRepo, userRepo, notifService, db.Database)
	socialHandler := social.NewHandler(socialService)

	feedRepo := feed.NewRepository(db.Database)
	feedService := feed.NewService(feedRepo, followRepo, userRepo, socialRepo)
	feedHandler := feed.NewHandler(feedService)

	// Initialize Reports feature
	reportRepo := reports.NewRepository(db.Database)
	reportService := reports.NewService(reportRepo, notifService)
	reportHandler := reports.NewHandler(reportService)

	// 5. Setup Auth feature (JWT exchange / refresh / logout)
	authRepo := auth.NewRepository(db.Database)
	authService := auth.NewService(authRepo, userService, firebaseApp, cfg)
	authHandler := auth.NewHandler(authService)

	// 6. Setup Middleware (JWT local validation — no Firebase network call per request)
	authMiddleware, err := middleware.NewAuthMiddleware(firebaseApp, userService, cfg)
	if err != nil {
		log.Printf("Warning: Auth middleware not setup: %v", err)
	}

	// 7. Setup Fiber
	app := fiber.New(fiber.Config{
		AppName:      "Chat API v1.0",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	})
	app.Use(logger.New())
	app.Use(cors.New())

	// Root Route
	app.Get("/", func(c *fiber.Ctx) error {
		return response.OK(c, "Welcome to Chat API", fiber.Map{
			"version":     "v1",
			"healthCheck": "/health",
		})
	})

	// 8. Setup Routes
	routes.SetupRoutes(
		app,
		authMiddleware,
		authHandler,
		userHandler,
		connectionHandler,
		chatHandler,
		notifHandler,
		profileHandler,
		poemHandler,
		followHandler,
		feedHandler,
		socialHandler,
		reportHandler,
		db.Database,
	)
	// 8. Start Server (Graceful Shutdown)
	log.Printf("🚀 Starting Chat API on port %s", cfg.Port)
	
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := app.Listen(":" + cfg.Port); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server listen error: %v", err)
		}
	}()

	<-shutdownChan
	log.Println("Shutting down gracefully...")

	// 9. Cleanup
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		log.Printf("Fiber Shutdown Error: %v", err)
	}

	if err := db.Client.Disconnect(shutdownCtx); err != nil {
		log.Printf("MongoDB Disconnect Error: %v", err)
	}

	log.Println("Server stopped properly")
}
