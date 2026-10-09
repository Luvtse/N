package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"nidaw-backend/internal/shared/auth"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type WebSocketHandler struct {
	upgrader       websocket.Upgrader
	authService    *auth.Service
	allowedOrigins []string // Phase B/B6: env-driven origin allowlist (CORS_ORIGINS)
	clients        map[*Client]bool
	broadcast      chan Message
	register       chan *Client
	unregister     chan *Client
	mu             sync.RWMutex
}

// isAllowedOrigin implements the Phase B/B6 CheckOrigin policy.
// Rules mirror the HTTP CORS middleware: empty allowlist => reject all
// cross-origin WS handshakes; explicit "*" => allow; otherwise exact match.
func (h *WebSocketHandler) isAllowedOrigin(origin string) bool {
	if origin == "" {
		// Non-browser clients (mobile apps, services) send no Origin header.
		return true
	}
	for _, o := range h.allowedOrigins {
		if o == "*" || strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}

type Client struct {
	hub    *WebSocketHandler
	conn   *websocket.Conn
	send   chan []byte
	userID uuid.UUID
	rideID *uuid.UUID
	topics map[string]bool
	// closed guards double-close of c.send. It is only ever flipped by the
	// hub goroutine (run loop), which serializes all close operations, so
	// plain field access is safe within that single-goroutine context.
	closed bool
}

// safeCloseSend closes client.send exactly once. MUST only be called from the
// hub run() goroutine so there is no race between broadcast eviction and
// unregister handling.
func (c *Client) safeCloseSend() {
	if !c.closed {
		c.closed = true
		close(c.send)
	}
}

type Message struct {
	Type      string                 `json:"type"`
	Payload   map[string]interface{} `json:"payload"`
	Timestamp int64                  `json:"timestamp"`
}

func NewWebSocketHandler(authService *auth.Service, allowedOrigins []string) *WebSocketHandler {
	hub := &WebSocketHandler{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin:     nil, // set below via hub receiver once allowedOrigins exist
		},
		authService:    authService,
		allowedOrigins: allowedOrigins,
		clients:        make(map[*Client]bool),
		broadcast:      make(chan Message),
		register:       make(chan *Client),
		unregister:     make(chan *Client),
	}
	// Phase B/B6: real origin allowlist replaces the old "return true" stub.
	hub.upgrader.CheckOrigin = func(r *http.Request) bool {
		return hub.isAllowedOrigin(r.Header.Get("Origin"))
	}

	go hub.run()
	return hub
}

func (h *WebSocketHandler) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			// Hub owns lifecycle: only this goroutine ever closes client.send.
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.safeCloseSend()
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			data := mustJSON(message)
			topic, _ := message.Payload["topic"].(string)
			h.mu.Lock()
			for client := range h.clients {
				subscribed := client.topics[topic] || client.topics["*"]
				if !subscribed {
					continue
				}
				select {
				case client.send <- data:
				default:
					// Slow consumer: evict + close once, from the hub only.
					delete(h.clients, client)
					client.safeCloseSend()
				}
			}
			h.mu.Unlock()
		}
	}
}

// Phase B/B6: token extraction order —
//  1. Sec-WebSocket-Protocol subprotocol ("nidaw-auth.<jwt>") preferred, keeps
//     tokens out of URLs/access logs/history.
//  2. Authorization header (mobile clients that can set headers).
//  3. ?token= query param — accepted ONLY as a deprecated fallback; it is
//     logged with a warning so we can drive clients off it.
func extractWSToken(r *http.Request) (string, bool) {
	// Subprotocol form: nidaw-auth.<token>
	for _, p := range websocket.Subprotocols(r) {
		if strings.HasPrefix(p, "nidaw-auth.") {
			return strings.TrimPrefix(p, "nidaw-auth."), true
		}
	}
	// Standard bearer header.
	if h := r.Header.Get("Authorization"); h != "" {
		parts := strings.Fields(h)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			return parts[1], true
		}
	}
	// Deprecated query-param fallback.
	if t := r.URL.Query().Get("token"); t != "" {
		log.Printf("WARN: WebSocket auth via query param is deprecated (path=%s); migrate client to Sec-WebSocket-Protocol", r.URL.Path)
		return t, true
	}
	return "", false
}

func (h *WebSocketHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	token, ok := extractWSToken(r)
	if !ok || token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	claims, err := h.authService.ValidateAccessToken(token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// Defense-in-depth (audit follow-up): the Kong gateway enforces access-token
	// type upstream, but a direct/pod-network connection must not accept any
	// other credential class. Claims.Type is already pinned to AccessToken by
	// ValidateAccessToken; assert it again here so a future refactor of the
	// auth service cannot silently widen this entry point (e.g., accepting
	// refresh tokens for long-lived streams).
	if !claims.IsAccessToken() {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// Echo back the negotiated subprotocol when the client used one, per RFC 6455
	// (gorilla/websocket responds with the first offered protocol automatically
	// only if we don't filter; keep "nidaw-auth.*" out of the response list).
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	client := &Client{
		hub:    h,
		conn:   conn,
		send:   make(chan []byte, 256),
		userID: claims.UserID,
		topics: make(map[string]bool),
	}

	h.register <- client

	go client.writePump()
	go client.readPump()
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		var msg Message
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		c.handleMessage(msg)
	}
}

func (c *Client) handleMessage(msg Message) {
	switch msg.Type {
	case "subscribe":
		if topic, ok := msg.Payload["topic"].(string); ok {
			c.topics[topic] = true
			c.trySend(Message{
				Type: "subscribed",
				Payload: map[string]interface{}{
					"topic": topic,
				},
				Timestamp: time.Now().Unix(),
			})
		}

	case "unsubscribe":
		if topic, ok := msg.Payload["topic"].(string); ok {
			delete(c.topics, topic)
			c.trySend(Message{
				Type: "unsubscribed",
				Payload: map[string]interface{}{
					"topic": topic,
				},
				Timestamp: time.Now().Unix(),
			})
		}

	case "ping":
		c.trySend(Message{
			Type:      "pong",
			Payload:   map[string]interface{}{},
			Timestamp: time.Now().Unix(),
		})
	}
}

// trySend performs a non-blocking send on c.send. If the channel is closed
// (hub evicted this client) or full (slow consumer), the message is dropped
// instead of panicking or blocking readPump. This removes the race where
// readPump wrote to a channel the hub had already closed.
func (c *Client) trySend(msg Message) {
	data := mustJSON(msg)
	defer func() {
		// Recover from "send on closed channel" if the hub raced us during
		// eviction; dropping the reply is safe here.
		_ = recover()
	}()
	select {
	case c.send <- data:
	default:
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// BroadcastToRide sends a message to all clients subscribed to a specific ride
func (h *WebSocketHandler) BroadcastToRide(rideID uuid.UUID, message Message) {
	topic := "ride:" + rideID.String()
	message.Payload["topic"] = topic
	message.Timestamp = time.Now().Unix()
	h.broadcast <- message
}

// BroadcastToDriver sends a message to a specific driver
func (h *WebSocketHandler) BroadcastToDriver(driverID uuid.UUID, message Message) {
	topic := "driver:" + driverID.String()
	message.Payload["topic"] = topic
	message.Timestamp = time.Now().Unix()
	h.broadcast <- message
}

func mustJSON(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}
