package middleware

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"sdsm/app/backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = 50 * time.Second
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Configure properly for production
	},
}

type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []byte
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mutex      sync.RWMutex
	logger     *utils.Logger
}

func NewHub(logger *utils.Logger) *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		logger:     logger,
	}
}

func (h *Hub) Run() {
	pingTicker := time.NewTicker(pingPeriod)
	defer pingTicker.Stop()

	for {
		select {
		case conn := <-h.register:
			h.mutex.Lock()
			h.clients[conn] = true
			h.mutex.Unlock()
			h.logf("WebSocket client connected")

		case conn := <-h.unregister:
			h.mutex.Lock()
			if _, ok := h.clients[conn]; ok {
				delete(h.clients, conn)
				conn.Close()
			}
			h.mutex.Unlock()
			h.logf("WebSocket client disconnected")

		case message := <-h.broadcast:
			h.writeToClients(websocket.TextMessage, message)

		case <-pingTicker.C:
			h.writePingToClients()
		}
	}
}

func (h *Hub) writeToClients(messageType int, payload []byte) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	for conn := range h.clients {
		if err := conn.SetWriteDeadline(time.Now().Add(writeWait)); err != nil {
			h.logf("WebSocket set write deadline error: %v", err)
		}
		if err := conn.WriteMessage(messageType, payload); err != nil {
			h.logf("WebSocket write error: %v", err)
			conn.Close()
			delete(h.clients, conn)
		}
	}
}

func (h *Hub) writePingToClients() {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	for conn := range h.clients {
		deadline := time.Now().Add(writeWait)
		if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
			h.logf("WebSocket ping error: %v", err)
			conn.Close()
			delete(h.clients, conn)
		}
	}
}

func (h *Hub) Broadcast(message []byte) {
	h.broadcast <- message
}

func (h *Hub) GetClientCount() int {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	return len(h.clients)
}

func (h *Hub) HandleWebSocket() gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			h.logf("WebSocket upgrade error: %v", err)
			return
		}

		conn.SetReadLimit(1024)
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			return conn.SetReadDeadline(time.Now().Add(pongWait))
		})

		h.register <- conn

		defer func() {
			h.unregister <- conn
		}()

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNoStatusReceived) {
					h.logf("WebSocket error: %v", err)
				}
				break
			}
		}
	}
}

func (h *Hub) logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if h.logger != nil {
		h.logger.Write(msg)
		return
	}
	// Fallback: write to default SDSM log instead of stdout
	utils.NewLogger("").Write(msg)
}
