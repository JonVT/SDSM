package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
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

type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan []byte
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mutex      sync.RWMutex
	logger     *utils.Logger
	allowAnyOrigin bool
	allowedOrigins map[string]struct{}
}


func NewHub(logger *utils.Logger, allowedOrigins []string) *Hub {
	allowAny := false
	allowlist := make(map[string]struct{})
	for _, raw := range allowedOrigins {
		origin := strings.TrimSpace(raw)
		if origin == "" {
			continue
		}
		if origin == "*" {
			allowAny = true
			continue
		}
		if normalized, ok := normalizeOrigin(origin); ok {
			allowlist[normalized] = struct{}{}
			continue
		}
		if logger != nil {
			logger.Write(fmt.Sprintf("Ignoring invalid ws_allowed_origins entry: %q", origin))
		}
	}

	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
		logger:     logger,
		allowAnyOrigin: allowAny,
		allowedOrigins: allowlist,
	}
}

func normalizeOrigin(origin string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || u == nil {
		return "", false
	}
	scheme := strings.ToLower(strings.TrimSpace(u.Scheme))
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	if strings.TrimSpace(u.Host) == "" {
		return "", false
	}
	hostPort, ok := normalizeHostPort(u.Host, scheme)
	if !ok {
		return "", false
	}
	return scheme + "://" + hostPort, true
}

func normalizeHostPort(host, scheme string) (string, bool) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", false
	}
	u, err := url.Parse(scheme + "://" + host)
	if err != nil || u == nil {
		return "", false
	}
	hn := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if hn == "" {
		return "", false
	}
	port := strings.TrimSpace(u.Port())
	if port == "" {
		if strings.EqualFold(scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}
	if _, err := net.LookupPort("tcp", port); err != nil {
		return "", false
	}
	return hn + ":" + port, true
}

func requestScheme(r *http.Request) string {
	if r == nil {
		return "http"
	}
	if r.TLS != nil {
		return "https"
	}
	xfp := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if xfp != "" {
		parts := strings.Split(xfp, ",")
		if len(parts) > 0 {
			proto := strings.ToLower(strings.TrimSpace(parts[0]))
			if proto == "https" {
				return "https"
			}
		}
	}
	return "http"
}

func isSameOrigin(r *http.Request, origin string) bool {
	normOrigin, ok := normalizeOrigin(origin)
	if !ok {
		return false
	}
	reqScheme := requestScheme(r)
	reqHostPort, ok := normalizeHostPort(r.Host, reqScheme)
	if !ok {
		return false
	}
	return strings.EqualFold(normOrigin, reqScheme+"://"+reqHostPort)
}

func (h *Hub) checkOrigin(r *http.Request) bool {
	if h == nil {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// Non-browser clients may omit Origin.
		return true
	}
	if h.allowAnyOrigin {
		return true
	}
	if isSameOrigin(r, origin) {
		return true
	}
	normOrigin, ok := normalizeOrigin(origin)
	if !ok {
		return false
	}
	_, allowed := h.allowedOrigins[normOrigin]
	return allowed
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

func (h *Hub) HandleWebSocket() gin.HandlerFunc {
	return func(c *gin.Context) {
		upgrader := websocket.Upgrader{
			CheckOrigin: h.checkOrigin,
		}
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
