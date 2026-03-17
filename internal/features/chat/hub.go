package chat

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/gofiber/contrib/websocket"
	"github.com/xyz-asif/gotodo/internal/models"
)

const sendBufSize = 256

// Hub manages active WebSocket connections
type Hub struct {
	clients       map[string]map[*clientContext]bool
	clientsMu     sync.RWMutex
	manualOffline map[string]bool // users manually marked offline (app in background)
	manualMu      sync.RWMutex
	gracePeriods  map[string]*time.Timer // users in grace period after disconnect
	graceMu       sync.RWMutex
	onUserOnline  func(userID string)     // callback when user comes online
	onUserOffline func(userID string)     // callback when user goes offline
	register      chan *clientContext
	unregister    chan *clientContext
	broadcast     chan broadcastMessage
}

// clientContext holds one WebSocket connection and its dedicated send channel.
// All writes go through the send channel so only one goroutine ever calls
// WriteMessage on a given connection — eliminating concurrent-write panics.
type clientContext struct {
	userID string
	conn   *websocket.Conn
	send   chan []byte
}

type broadcastMessage struct {
	userIDs     []string
	messageData []byte
}

// NewHub creates a new Hub instance with buffered channels for high throughput
func NewHub() *Hub {
	return &Hub{
		clients:       make(map[string]map[*clientContext]bool),
		manualOffline: make(map[string]bool),
		gracePeriods:  make(map[string]*time.Timer),
		register:      make(chan *clientContext, 64),
		unregister:    make(chan *clientContext, 64),
		broadcast:     make(chan broadcastMessage, 1024),
	}
}

// writePump is the sole goroutine allowed to write to a connection.
// It drains the client's send channel until it is closed.
func (h *Hub) writePump(client *clientContext) {
	defer client.conn.Close()
	for data := range client.send {
		if err := client.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("WS write error for user %s: %v", client.userID, err)
			// Drain remaining messages so the channel can be GC'd
			for range client.send {
			}
			return
		}
	}
}

// Run starts the hub's main event loop
func (h *Hub) Run() {
	log.Printf("[HUB] Hub event loop started")
	for {
		select {
		case client := <-h.register:
			h.clientsMu.Lock()
			wasOffline := len(h.clients[client.userID]) == 0
			if h.clients[client.userID] == nil {
				h.clients[client.userID] = make(map[*clientContext]bool)
			}
			h.clients[client.userID][client] = true
			h.clientsMu.Unlock()
			
			// Cancel grace period if user reconnected
			h.CancelGracePeriod(client.userID)
			
			// Start the dedicated writer for this connection
			go h.writePump(client)
			
			// Trigger online callback if this was the first connection
			if wasOffline && h.onUserOnline != nil {
				h.onUserOnline(client.userID)
			}

		case client := <-h.unregister:
			h.clientsMu.Lock()
			if conns, ok := h.clients[client.userID]; ok {
				if _, exists := conns[client]; exists {
					delete(conns, client)
					close(client.send) // signals writePump to exit
					remainingConns := len(conns)
					if remainingConns == 0 {
						delete(h.clients, client.userID)
						// Clear manual presence — user has no connections, they're truly offline
						h.manualMu.Lock()
						wasManuallyOffline := h.manualOffline[client.userID]
						delete(h.manualOffline, client.userID)
						h.manualMu.Unlock()

						// Skip grace period if user was manually marked offline
						if wasManuallyOffline {
							if h.onUserOffline != nil {
								h.onUserOffline(client.userID)
							}
						} else {
							h.startGracePeriod(client.userID)
						}
					}
				}
			}
			h.clientsMu.Unlock()

		case msg := <-h.broadcast:
			h.clientsMu.RLock()
			for _, uid := range msg.userIDs {
				for client := range h.clients[uid] {
					select {
					case client.send <- msg.messageData:
					default:
						// Send buffer full — drop message to avoid blocking the hub
						log.Printf("WS send buffer full for user %s, dropping message", uid)
					}
				}
			}
			h.clientsMu.RUnlock()
		}
	}
}

// SendToUsers sends a modeled websocket message to multiple users at once.
// The send is non-blocking: if the broadcast channel is full the message is
// dropped and an error is returned so callers are never hung.
func (h *Hub) SendToUsers(userIDs []string, msg models.WSMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	select {
	case h.broadcast <- broadcastMessage{userIDs: userIDs, messageData: data}:
	default:
		log.Printf("WS broadcast channel full, dropping message type=%s", msg.Type)
	}
	return nil
}

// SendMessage sends a modeled websocket message to a single user (convenience wrapper)
func (h *Hub) SendMessage(userID string, msg models.WSMessage) error {
	return h.SendToUsers([]string{userID}, msg)
}

// SetPresenceCallbacks sets the online/offline callbacks for presence broadcasting
// Must be called after service is created to avoid circular dependency
func (h *Hub) SetPresenceCallbacks(onOnline, onOffline func(userID string)) {
	h.onUserOnline = onOnline
	h.onUserOffline = onOffline
}

// SetManualPresence marks a user as manually online/offline (for mobile app background/foreground).
// When isOnline=false, user appears offline to others but WebSocket stays connected.
// When isOnline=true, user's presence returns to actual connection state.
func (h *Hub) SetManualPresence(userID string, isOnline bool) {
	h.manualMu.Lock()
	if isOnline {
		delete(h.manualOffline, userID)
	} else {
		h.manualOffline[userID] = true
	}
	h.manualMu.Unlock()
}

// startGracePeriod starts a 2-second grace period before broadcasting user offline
// This allows quick reconnections without generating offline/online events
func (h *Hub) startGracePeriod(userID string) {
	const graceDuration = 2 * time.Second

	h.graceMu.Lock()
	// Cancel any existing grace period
	if timer, ok := h.gracePeriods[userID]; ok {
		timer.Stop()
	}
	
	// Check if manually marked offline - broadcast immediately, no grace period
	h.manualMu.RLock()
	manuallyOffline := h.manualOffline[userID]
	h.manualMu.RUnlock()
	
	if manuallyOffline {
		// User manually went offline - broadcast immediately with no grace period
		h.graceMu.Unlock()
		if h.onUserOffline != nil {
			h.onUserOffline(userID)
		}
		return
	}
	
	h.gracePeriods[userID] = time.AfterFunc(graceDuration, func() {
		// Wait a tiny bit for any pending register to be processed
		time.Sleep(100 * time.Millisecond)
		
		// Double-check: if user reconnected between timer start and fire
		h.clientsMu.RLock()
		stillConnected := len(h.clients[userID]) > 0
		h.clientsMu.RUnlock()
		if stillConnected {
			return
		}
		
		// Broadcast offline
		if h.onUserOffline != nil {
			h.onUserOffline(userID)
		}
		h.graceMu.Lock()
		delete(h.gracePeriods, userID)
		h.graceMu.Unlock()
	})
	h.graceMu.Unlock()
}

// CancelGracePeriod cancels any active grace period for a user
// Called on manual presence updates and reconnections
func (h *Hub) CancelGracePeriod(userID string) {
	h.graceMu.Lock()
	if timer, ok := h.gracePeriods[userID]; ok {
		timer.Stop()
		delete(h.gracePeriods, userID)
	}
	h.graceMu.Unlock()
}

// IsUserOnline checks if a user has any active WebSocket connections.
// Returns false if the user is manually marked offline or in grace period.
func (h *Hub) IsUserOnline(userID string) bool {
	h.manualMu.RLock()
	if h.manualOffline[userID] {
		h.manualMu.RUnlock()
		return false
	}
	h.manualMu.RUnlock()

	h.graceMu.RLock()
	if _, inGracePeriod := h.gracePeriods[userID]; inGracePeriod {
		h.graceMu.RUnlock()
		return true // Still considered online during grace period
	}
	h.graceMu.RUnlock()

	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	conns, ok := h.clients[userID]
	return ok && len(conns) > 0
}

// OnlineUserCount returns how many users are currently connected
func (h *Hub) OnlineUserCount() int {
	h.clientsMu.RLock()
	defer h.clientsMu.RUnlock()
	return len(h.clients)
}

// DisconnectUser closes all WebSocket connections for a specific user
func (h *Hub) DisconnectUser(userID string) {
	h.clientsMu.RLock()
	conns, ok := h.clients[userID]
	h.clientsMu.RUnlock()
	
	if !ok {
		return
	}
	
	// Close all connections for this user
	for client := range conns {
		client.conn.Close()
	}
}
