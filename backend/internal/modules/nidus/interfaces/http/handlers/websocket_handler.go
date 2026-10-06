package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"nidaw-backend/internal/shared/auth"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type WebSocketHandler struct {
	upgrader websocket.Upgrader
	authService *auth.Service
	clients  map[*Client]bool
	broadcast chan Message
	register  chan *Client
	unregister chan *Client
	mu        sync.RWMutex
}

type Client struct {
	hub      *WebSocketHandler
	conn     *websocket.Conn
	send     chan []byte
	userID   uuid.UUID
	rideID   *uuid.UUID
	topics   map[string]bool
}

type Message struct {
	Type      string                 `json:"type"`
	Payload   map[string]interface{} `json:"payload"`
	Timestamp int64                  `json:"timestamp"`
}

func NewWebSocketHandler(authService *auth.Service) *WebSocketHandler {
	hub := &WebSocketHandler{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // TODO: Configure in production
			},
		},
		authService: authService,
		clients:    make(map[*Client]bool),
		broadcast:  make(chan Message),
		register:   make(chan *Client),
		unregister: make(chan *Client),
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
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				// Check if client is subscribed to this topic
				topic := message.Payload["topic"].(string)
				if client.topics[topic] || client.topics["*"] {
					select {
					case client.send <- mustJSON(message):
					default:
						close(client.send)
						delete(h.clients, client)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *WebSocketHandler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Validate token from query parameter
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	// Validate JWT via the shared auth service (Phase B will move this to a
	// Sec-WebSocket-Protocol header to avoid token leakage in URLs).
	claims, err := h.authService.ValidateAccessToken(token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// Upgrade connection
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
			c.send <- mustJSON(Message{
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
			c.send <- mustJSON(Message{
				Type: "unsubscribed",
				Payload: map[string]interface{}{
					"topic": topic,
				},
				Timestamp: time.Now().Unix(),
			})
		}

	case "ping":
		c.send <- mustJSON(Message{
			Type:      "pong",
			Payload:   map[string]interface{}{},
			Timestamp: time.Now().Unix(),
		})
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