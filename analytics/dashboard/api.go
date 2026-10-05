package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// WebSocket hub for real-time dashboard updates
type DashboardHub struct {
	clients    map[*DashboardClient]bool
	broadcast  chan DashboardUpdate
	register   chan *DashboardClient
	unregister chan *DashboardClient
	analytics  *PredictiveBI
}

type DashboardClient struct {
	hub        *DashboardHub
	conn       *websocket.Conn
	send       chan []byte
	userID     string
	dashboard  string
	city       string
}

type DashboardUpdate struct {
	Dashboard string      `json:"dashboard"`
	City      string      `json:"city"`
	Metrics   interface{} `json:"metrics"`
	Timestamp time.Time   `json:"timestamp"`
}

func NewDashboardHub(analytics *PredictiveBI) *DashboardHub {
	return &DashboardHub{
		clients:    make(map[*DashboardClient]bool),
		broadcast:  make(chan DashboardUpdate),
		register:   make(chan *DashboardClient),
		unregister: make(chan *DashboardClient),
		analytics:  analytics,
	}
}

func (h *DashboardHub) Run() {
	// Start metrics collection goroutine
	go h.collectMetrics()
	
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
			
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			
		case update := <-h.broadcast:
			for client := range h.clients {
				if client.dashboard == update.Dashboard && 
				   (client.city == update.City || client.city == "all") {
					select {
					case client.send <- mustJSON(update):
					default:
						close(client.send)
						delete(h.clients, client)
					}
				}
			}
		}
	}
}

func (h *DashboardHub) collectMetrics() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	cities := []string{"new_york", "london", "tokyo", "paris", "singapore"}
	
	for range ticker.C {
		for _, city := range cities {
			metrics, err := h.analytics.GetRealTimeMetrics(context.Background(), city)
			if err != nil {
				continue
			}
			
			h.broadcast <- DashboardUpdate{
				Dashboard: "operations",
				City:      city,
				Metrics:   metrics,
				Timestamp: time.Now(),
			}
		}
		
		// Executive insights every minute
		insights, err := h.analytics.GetExecutiveInsights(context.Background())
		if err == nil {
			h.broadcast <- DashboardUpdate{
				Dashboard: "executive",
				City:      "all",
				Metrics:   insights,
				Timestamp: time.Now(),
			}
		}
	}
}

func (h *DashboardHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	
	client := &DashboardClient{
		hub:       h,
		conn:      conn,
		send:      make(chan []byte, 256),
		userID:    r.URL.Query().Get("user_id"),
		dashboard: r.URL.Query().Get("dashboard"),
		city:      r.URL.Query().Get("city"),
	}
	
	client.hub.register <- client
	
	go client.writePump()
	go client.readPump()
}

func (c *DashboardClient) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	
	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (c *DashboardClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
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

func mustJSON(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}