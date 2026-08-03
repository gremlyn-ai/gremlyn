package api

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/gremlyn-ai/gremlyn/pkg/models"
	"github.com/rs/zerolog"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// Hub manages WebSocket connections grouped by session ID.
// It implements session.EventNotifier so the recorder can push events.
type Hub struct {
	logger zerolog.Logger

	mu    sync.RWMutex
	conns map[string]map[*websocket.Conn]struct{} // sessionID → connections
}

// NewHub creates a WebSocket Hub.
func NewHub(logger zerolog.Logger) *Hub {
	return &Hub{
		logger: logger,
		conns:  make(map[string]map[*websocket.Conn]struct{}),
	}
}

// Subscribe adds a connection to a session's broadcast group.
func (h *Hub) Subscribe(sessionID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[sessionID] == nil {
		h.conns[sessionID] = make(map[*websocket.Conn]struct{})
	}
	h.conns[sessionID][conn] = struct{}{}
}

// Unsubscribe removes a connection from a session's broadcast group.
func (h *Hub) Unsubscribe(sessionID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.conns[sessionID]; ok {
		delete(s, conn)
		if len(s) == 0 {
			delete(h.conns, sessionID)
		}
	}
}

// Notify broadcasts an arena event to all connections watching that session.
// This implements session.EventNotifier.
func (h *Hub) Notify(event models.ArenaEvent) {
	msg := WSMessage{
		Type: "arena_event",
		Data: eventToResponse(event),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Warn().Err(err).Msg("failed to marshal ws event")
		return
	}

	h.mu.RLock()
	conns := h.conns[event.SessionID]
	h.mu.RUnlock()

	for conn := range conns {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			h.logger.Debug().Err(err).Msg("ws write failed, removing connection")
			h.Unsubscribe(event.SessionID, conn)
			_ = conn.Close()
		}
	}
}

// HandleWS upgrades an HTTP connection to WebSocket and subscribes to a session's events.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request, sessionID string) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Warn().Err(err).Msg("ws upgrade failed")
		return
	}

	h.Subscribe(sessionID, conn)
	h.logger.Debug().
		Str("session_id", sessionID).
		Msg("ws client connected")

	// Send initial connected message.
	welcome := WSMessage{Type: "connected", Data: map[string]string{"session_id": sessionID}}
	welcomeData, _ := json.Marshal(welcome)
	_ = conn.WriteMessage(websocket.TextMessage, welcomeData)

	// Read loop — keeps the connection alive and handles client disconnects.
	defer func() {
		h.Unsubscribe(sessionID, conn)
		_ = conn.Close()
		h.logger.Debug().Str("session_id", sessionID).Msg("ws client disconnected")
	}()

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}
