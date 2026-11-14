package crm

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	wsWriteWait  = 10 * time.Second
	wsPongWait   = 60 * time.Second
	wsPingPeriod = 25 * time.Second
	maxOffline   = 32
)

type wsClient struct {
	userID int64
	conn   *websocket.Conn
	send   chan []byte
}

// WSHub управляет WebSocket-подключениями сотрудников и буфером оффлайн-уведомлений (без внешней зависимости от Node.js).
type WSHub struct {
	logger   *slog.Logger
	mu       sync.RWMutex
	clients  map[int64]map[*wsClient]struct{}
	offline  map[int64][][]byte
	upgrader websocket.Upgrader
}

func NewWSHub(logger *slog.Logger) *WSHub {
	if logger == nil {
		logger = slog.Default()
	}
	return &WSHub{
		logger:  logger,
		clients: make(map[int64]map[*wsClient]struct{}),
		offline: make(map[int64][][]byte),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
}

// IsUserOnline сообщает диспетчеру пула задач, есть ли у сотрудника открытая WS-сессия.
func (h *WSHub) IsUserOnline(userID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[userID]) > 0
}

// OnlineUserIDs возвращает снимок ID всех онлайн-сотрудников.
func (h *WSHub) OnlineUserIDs() []int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]int64, 0, len(h.clients))
	for uid, conns := range h.clients {
		if len(conns) > 0 {
			ids = append(ids, uid)
		}
	}
	return ids
}

// BroadcastToUsers отправляет событие конкретным пользователям или буферизует его до следующего подключения.
func (h *WSHub) BroadcastToUsers(userIDs []int64, event WSEvent) {
	if len(userIDs) == 0 {
		return
	}
	raw, err := json.Marshal(event)
	if err != nil {
		h.logger.Error("ws marshal failed", "error", err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for _, uid := range userIDs {
		conns := h.clients[uid]
		if len(conns) == 0 {
			buf := append(h.offline[uid], raw)
			if len(buf) > maxOffline {
				buf = buf[len(buf)-maxOffline:]
			}
			h.offline[uid] = buf
			continue
		}
		for c := range conns {
			select {
			case c.send <- raw:
			default:
				// Переполненный канал закрываем, чтобы медленный клиент не тормозил остальных.
				h.removeLocked(c)
			}
		}
	}
}

// BroadcastAll отправляет событие всем подключённым сотрудникам (например, обновление календаря).
func (h *WSHub) BroadcastAll(event WSEvent) {
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, conns := range h.clients {
		for c := range conns {
			select {
			case c.send <- raw:
			default:
				h.removeLocked(c)
			}
		}
	}
}

// ServeWS обслуживает авторизованное WebSocket-соединение пользователя.
func (h *WSHub) ServeWS(w http.ResponseWriter, r *http.Request, userID int64) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Debug("ws upgrade failed", "error", err)
		return
	}

	client := &wsClient{
		userID: userID,
		conn:   conn,
		send:   make(chan []byte, 64),
	}

	h.mu.Lock()
	if h.clients[userID] == nil {
		h.clients[userID] = make(map[*wsClient]struct{})
	}
	h.clients[userID][client] = struct{}{}
	queued := h.offline[userID]
	delete(h.offline, userID)
	h.mu.Unlock()

	for _, msg := range queued {
		select {
		case client.send <- msg:
		default:
		}
	}

	go h.writePump(client)
	h.readPump(client)
}

func (h *WSHub) removeLocked(c *wsClient) {
	conns := h.clients[c.userID]
	if conns == nil {
		return
	}
	if _, ok := conns[c]; !ok {
		return
	}
	delete(conns, c)
	if len(conns) == 0 {
		delete(h.clients, c.userID)
	}
	close(c.send)
}

func (h *WSHub) unregister(c *wsClient) {
	h.mu.Lock()
	h.removeLocked(c)
	h.mu.Unlock()
}

func (h *WSHub) readPump(c *wsClient) {
	defer func() {
		h.unregister(c)
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(64 * 1024)
	_ = c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (h *WSHub) writePump(c *wsClient) {
	ticker := time.NewTicker(wsPingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
