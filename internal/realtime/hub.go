package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

type Event struct {
	ID        int64           `json:"id"`
	ProjectID string          `json:"project_id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	At        time.Time       `json:"at"`
}

type Hub struct {
	rdb        *redis.Client
	upgrader   websocket.Upgrader
	ping       time.Duration
	pongWait   time.Duration
	mu         sync.Mutex
	presence   map[string]map[string]map[*websocket.Conn]struct{}
	subscribed map[string]context.CancelFunc
}

func NewHub(rdb *redis.Client, ping, pongWait time.Duration) *Hub {
	return &Hub{
		rdb:      rdb,
		ping:     ping,
		pongWait: pongWait,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		presence:   map[string]map[string]map[*websocket.Conn]struct{}{},
		subscribed: map[string]context.CancelFunc{},
	}
}

func (h *Hub) HandleWS(c *gin.Context) {
	projectID := c.Param("projectID")
	userID := c.Query("user_id")
	if userID == "" {
		userID = "anonymous"
	}
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	h.addPresence(projectID, userID, conn)
	defer h.removePresence(projectID, userID, conn)

	h.ensureProjectSubscriber(projectID)
	_ = conn.WriteJSON(gin.H{"type": "presence", "users": h.PresenceList(projectID)})

	since := c.Query("since")
	if since != "" {
		h.replayEvents(c, conn, projectID, since)
	}

	conn.SetReadDeadline(time.Now().Add(h.pongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(h.pongWait))
		return nil
	})

	ticker := time.NewTicker(h.ping)
	defer ticker.Stop()
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			if _, _, err := conn.NextReader(); err != nil {
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, []byte("ping")); err != nil {
				return
			}
		}
	}
}

func (h *Hub) replayEvents(c *gin.Context, conn *websocket.Conn, projectID, since string) {
	ctx := c.Request.Context()
	key := "events:" + projectID
	events, err := h.rdb.LRange(ctx, key, 0, 200).Result()
	if err != nil {
		return
	}
	for i := len(events) - 1; i >= 0; i-- {
		var evt Event
		if json.Unmarshal([]byte(events[i]), &evt) != nil {
			continue
		}
		if since != "" && evt.ID <= toInt64(since) {
			continue
		}
		_ = conn.WriteJSON(evt)
	}
}

func toInt64(s string) int64 {
	var v int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		v = v*10 + int64(r-'0')
	}
	return v
}

func (h *Hub) Publish(ctx context.Context, projectID, eventType string, payload any) error {
	payloadBytes, _ := json.Marshal(payload)
	id, err := h.rdb.Incr(ctx, "events:seq:"+projectID).Result()
	if err != nil {
		return err
	}
	evt := Event{ID: id, ProjectID: projectID, Type: eventType, Payload: payloadBytes, At: time.Now().UTC()}
	b, _ := json.Marshal(evt)
	pipe := h.rdb.TxPipeline()
	pipe.LPush(ctx, "events:"+projectID, b)
	pipe.LTrim(ctx, "events:"+projectID, 0, 999)
	pipe.Publish(ctx, "board_events:"+projectID, b)
	_, err = pipe.Exec(ctx)
	return err
}

func (h *Hub) ensureProjectSubscriber(projectID string) {
	h.mu.Lock()
	if _, ok := h.subscribed[projectID]; ok {
		h.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.subscribed[projectID] = cancel
	h.mu.Unlock()

	go func() {
		pubsub := h.rdb.Subscribe(ctx, "board_events:"+projectID)
		ch := pubsub.Channel()
		for msg := range ch {
			h.broadcast(projectID, []byte(msg.Payload))
		}
		_ = pubsub.Close()
	}()
}

func (h *Hub) addPresence(projectID, userID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.presence[projectID]; !ok {
		h.presence[projectID] = map[string]map[*websocket.Conn]struct{}{}
	}
	if _, ok := h.presence[projectID][userID]; !ok {
		h.presence[projectID][userID] = map[*websocket.Conn]struct{}{}
	}
	h.presence[projectID][userID][conn] = struct{}{}
}

func (h *Hub) removePresence(projectID, userID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.presence[projectID]; !ok {
		return
	}
	if _, ok := h.presence[projectID][userID]; !ok {
		return
	}
	delete(h.presence[projectID][userID], conn)
	if len(h.presence[projectID][userID]) == 0 {
		delete(h.presence[projectID], userID)
	}
}

func (h *Hub) PresenceList(projectID string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	users := make([]string, 0)
	for user := range h.presence[projectID] {
		users = append(users, user)
	}
	return users
}

func (h *Hub) broadcast(projectID string, payload []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, conns := range h.presence[projectID] {
		for conn := range conns {
			_ = conn.WriteMessage(websocket.TextMessage, payload)
		}
	}
}
