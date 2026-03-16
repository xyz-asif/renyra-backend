package routes

import (
	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"github.com/xyz-asif/gotodo/internal/features/chat"
	"github.com/xyz-asif/gotodo/internal/features/connections"
	"github.com/xyz-asif/gotodo/internal/features/feed"
	"github.com/xyz-asif/gotodo/internal/features/follows"
	"github.com/xyz-asif/gotodo/internal/features/notifications"
	"github.com/xyz-asif/gotodo/internal/features/poems"
	"github.com/xyz-asif/gotodo/internal/features/profile"
	"github.com/xyz-asif/gotodo/internal/features/social"
	"github.com/xyz-asif/gotodo/internal/features/users"
	"github.com/xyz-asif/gotodo/internal/middleware"
)

func SetupRoutes(
	app *fiber.App,
	authMiddleware *middleware.AuthMiddleware,
	userHandler *users.Handler,
	connectionHandler *connections.Handler,
	chatHandler *chat.Handler,
	notifHandler *notifications.Handler,
	profileHandler *profile.Handler,
	poemHandler *poems.Handler,
	followHandler *follows.Handler,
	feedHandler *feed.Handler,
	socialHandler *social.Handler,
) {
	api := app.Group("/api/v1")

	// Health check
	api.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// ── User Routes ──
	usersGroup := api.Group("/users")
	usersGroup.Get("/search", authMiddleware.VerifyToken, userHandler.Search)
	usersGroup.Get("/search-with-status", authMiddleware.VerifyToken, userHandler.SearchWithConnectionStatus)
	usersGroup.Get("/me", authMiddleware.VerifyToken, userHandler.GetMe)
	usersGroup.Patch("/me", authMiddleware.VerifyToken, userHandler.UpdateProfile)
	usersGroup.Post("/me/fcm-token", authMiddleware.VerifyToken, userHandler.RegisterFCMToken)

	// ── Connection / Friend Request Routes ──
	connGroup := api.Group("/connections")
	connGroup.Post("/request", authMiddleware.VerifyToken, connectionHandler.SendRequest)
	connGroup.Post("/:id/accept", authMiddleware.VerifyToken, connectionHandler.AcceptRequest)
	connGroup.Post("/:id/reject", authMiddleware.VerifyToken, connectionHandler.RejectRequest)
	connGroup.Post("/:id/cancel", authMiddleware.VerifyToken, connectionHandler.CancelRequest)
	connGroup.Delete("/:id", authMiddleware.VerifyToken, connectionHandler.RemoveConnection)
	connGroup.Get("/pending", authMiddleware.VerifyToken, connectionHandler.GetPendingRequests)
	connGroup.Get("/friends", authMiddleware.VerifyToken, connectionHandler.GetFriendsList)

	// ── Chat & Messaging Routes ──
	chatGroup := api.Group("/chat")

	// Rooms
	chatGroup.Get("/rooms", authMiddleware.VerifyToken, chatHandler.GetUserRooms)
	chatGroup.Post("/rooms/direct/:id", authMiddleware.VerifyToken, chatHandler.GetOrCreateDirectRoom)
	chatGroup.Get("/rooms/:roomId/messages", authMiddleware.VerifyToken, chatHandler.GetRoomMessages)
	chatGroup.Post("/rooms/:roomId/messages", authMiddleware.VerifyToken, chatHandler.SendMessage)
	chatGroup.Post("/rooms/:roomId/read", authMiddleware.VerifyToken, chatHandler.MarkRoomAsRead)
	chatGroup.Delete("/rooms/:roomId", authMiddleware.VerifyToken, chatHandler.DeleteChat)

	// Messages
	chatGroup.Patch("/messages/:messageId/status", authMiddleware.VerifyToken, chatHandler.UpdateMessageStatus)
	chatGroup.Put("/messages/:messageId/reactions", authMiddleware.VerifyToken, chatHandler.UpdateMessageReaction)
	chatGroup.Patch("/messages/:messageId", authMiddleware.VerifyToken, chatHandler.EditMessage)
	chatGroup.Delete("/messages/:messageId", authMiddleware.VerifyToken, chatHandler.DeleteMessage)

	// Presence
	chatGroup.Get("/users/:id/presence", authMiddleware.VerifyToken, chatHandler.GetUserPresence)
	chatGroup.Post("/disconnect", authMiddleware.VerifyToken, chatHandler.Disconnect)

	// WebSocket
	chatGroup.Get("/ws", authMiddleware.VerifyToken, chatHandler.WsUpgrade, websocket.New(chatHandler.WebSocketHandle))

	// ── Notification Routes ──
	notifGroup := api.Group("/notifications")
	notifGroup.Get("/", authMiddleware.VerifyToken, notifHandler.GetNotifications)
	notifGroup.Get("/unread-count", authMiddleware.VerifyToken, notifHandler.GetUnreadCount)
	notifGroup.Post("/:id/read", authMiddleware.VerifyToken, notifHandler.MarkAsRead)
	notifGroup.Post("/read-all", authMiddleware.VerifyToken, notifHandler.MarkAllAsRead)

	// ── Profile Setup ──
	api.Post("/users/setup", authMiddleware.Protect(), profileHandler.SetupProfile)
	api.Get("/users/username/check", profileHandler.CheckUsername) // no auth — public
	api.Post("/users/username", authMiddleware.Protect(), profileHandler.SetUsername)

	// ── Poems ──
	poemRoutes := api.Group("/poems", authMiddleware.OptionalAuth()) // OptionalAuth: read works without token
	poemRoutes.Post("/", authMiddleware.Protect(), poemHandler.CreatePoem)
	poemRoutes.Get("/me", authMiddleware.Protect(), poemHandler.GetMyPoems)
	poemRoutes.Get("/user/:userId", poemHandler.GetUserPoems)
	poemRoutes.Get("/:id", poemHandler.GetPoem)
	poemRoutes.Patch("/:id", authMiddleware.Protect(), poemHandler.UpdatePoem)
	poemRoutes.Delete("/:id", authMiddleware.Protect(), poemHandler.DeletePoem)

	// ── Follow / Profile ──
	api.Post("/users/:id/follow", authMiddleware.Protect(), followHandler.ToggleFollow)
	api.Get("/users/:id/profile", authMiddleware.OptionalAuth(), followHandler.GetPublicProfile)
	api.Get("/users/:id/followers", authMiddleware.OptionalAuth(), followHandler.GetFollowers)
	api.Get("/users/:id/following", authMiddleware.OptionalAuth(), followHandler.GetFollowing)

	// ── Social: Poem Likes ──
	api.Post("/poems/:id/like", authMiddleware.Protect(), socialHandler.TogglePoemLike)
	api.Get("/poems/:id/likes", authMiddleware.OptionalAuth(), socialHandler.GetPoemLikers)

	// ── Social: Comments ──
	api.Post("/poems/:id/comments", authMiddleware.Protect(), socialHandler.AddComment)
	api.Get("/poems/:id/comments", authMiddleware.OptionalAuth(), socialHandler.GetComments)
	api.Delete("/comments/:id", authMiddleware.Protect(), socialHandler.DeleteComment)
	api.Post("/comments/:id/like", authMiddleware.Protect(), socialHandler.ToggleCommentLike)

	// ── Social: Reposts ──
	api.Post("/poems/:id/repost", authMiddleware.Protect(), socialHandler.ToggleRepost)
	api.Get("/users/:id/reposts", authMiddleware.OptionalAuth(), socialHandler.GetUserReposts)

	// ── Feed ──
	api.Get("/feed", authMiddleware.Protect(), feedHandler.GetHomeFeed)
	api.Get("/feed/explore", authMiddleware.OptionalAuth(), feedHandler.GetExploreFeed)
	api.Get("/feed/audio", authMiddleware.OptionalAuth(), feedHandler.GetAudioFeed)

	// ── Search ──
	api.Get("/search/poems", authMiddleware.OptionalAuth(), feedHandler.SearchPoems)
	api.Get("/search/users", authMiddleware.OptionalAuth(), feedHandler.SearchUsers)
}
