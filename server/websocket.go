package main

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	conn    *websocket.Conn
	account Account
	send    chan []byte
	done    chan struct{}
	once    sync.Once
}

type Hub struct {
	mu      sync.Mutex
	clients map[*Client]bool
	closing bool
}

func newHub() *Hub {
	return &Hub{clients: make(map[*Client]bool)}
}

func sharesChannel(a, b Account) bool {
	for _, channel := range a.Channels {
		if b.canAccess(channel.ID) {
			return true
		}
	}

	return false
}

type PresenceUser struct {
	User
	ChannelIDs []string `json:"channelIds"`
}

// Only expose memberships in channels the recipient can access.
func presenceUser(account, recipient Account) PresenceUser {
	channelIDs := make([]string, 0)
	for _, channel := range account.Channels {
		if recipient.canAccess(channel.ID) {
			channelIDs = append(channelIDs, channel.ID)
		}
	}
	return PresenceUser{User: account.User, ChannelIDs: channelIDs}
}

func (c *Client) stop() {
	c.once.Do(func() {
		close(c.done)
		_ = c.conn.Close()
	})
}

// Called with the hub lock held. Disconnect slow clients rather than lose events.
func (c *Client) enqueue(event Event) {
	payload, err := json.Marshal(event)
	if err != nil {
		c.stop()
		return
	}

	select {
	case c.send <- payload:
	default:
		c.stop()
	}
}

func (h *Hub) add(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closing {
		c.stop()
		return
	}

	users := make([]PresenceUser, 0)
	seen := make(map[string]bool)
	alreadyOnline := false

	for other := range h.clients {
		if other.account.User.ID == c.account.User.ID {
			alreadyOnline = true
		}
		if !seen[other.account.User.ID] &&
			(other.account.User.ID == c.account.User.ID || sharesChannel(c.account, other.account)) {
			users = append(users, presenceUser(other.account, c.account))
			seen[other.account.User.ID] = true
		}
	}

	if !seen[c.account.User.ID] {
		users = append(users, presenceUser(c.account, c.account))
	}

	c.enqueue(Event{Type: "presence.snapshot", Data: map[string]any{"users": users}})

	if !alreadyOnline {
		for other := range h.clients {
			if sharesChannel(c.account, other.account) {
				other.enqueue(Event{
					Type: "presence.changed",
					Data: map[string]any{"user": presenceUser(c.account, other.account), "online": true},
				})
			}
		}
	}

	h.clients[c] = true
}

func (h *Hub) remove(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.clients[c] {
		return
	}
	delete(h.clients, c)

	for other := range h.clients {
		if other.account.User.ID == c.account.User.ID {
			return
		}
	}

	for other := range h.clients {
		if sharesChannel(c.account, other.account) {
			other.enqueue(Event{
				Type: "presence.changed",
				Data: map[string]any{"userId": c.account.User.ID, "online": false},
			})
		}
	}
}

func (h *Hub) message(channel string, event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for c := range h.clients {
		if c.account.canAccess(channel) {
			c.enqueue(event)
		}
	}
}

func (h *Hub) read(userID string, event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for c := range h.clients {
		if c.account.User.ID == userID {
			c.enqueue(event)
		}
	}
}

func (h *Hub) closeStaleUser(account Account) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for c := range h.clients {
		if c.account.User.ID == account.User.ID && !reflect.DeepEqual(c.account, account) {
			c.stop()
		}
	}
}

func (h *Hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.closing = true
	for c := range h.clients {
		c.stop()
	}
}

func (s *Server) socketSessionValid(c *Client, token string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	userID, err := s.store.db.Get(ctx, sessionKey(token)).Result()
	if err != nil || userID != c.account.User.ID {
		return false
	}

	account, err := s.store.account(ctx, userID)
	return err == nil && reflect.DeepEqual(account, c.account)
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request, account Account, token string) {
	ttl, err := s.store.db.TTL(r.Context(), sessionKey(token)).Result()
	if err != nil {
		s.internalError(w, err)
		return
	}
	if ttl <= 0 {
		fail(w, http.StatusUnauthorized, "UNAUTHENTICATED", "Session expired. Sign in again.")
		return
	}

	upgrader := websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second,
		CheckOrigin:      s.originAllowed,
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	c := &Client{
		conn:    conn,
		account: account,
		send:    make(chan []byte, 128),
		done:    make(chan struct{}),
	}

	conn.SetReadLimit(32768)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	})

	s.hub.add(c)
	defer s.hub.remove(c)
	defer c.stop()

	// Recheck after registration, covering a grant update during the handshake.
	if !s.socketSessionValid(c, token) {
		return
	}

	go s.writeSocket(c, token, ttl)

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (s *Server) writeSocket(c *Client, token string, ttl time.Duration) {
	defer c.stop()

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	expiry := time.NewTimer(ttl)
	defer expiry.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-expiry.C:
			return
		case payload := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ping.C:
			if !s.socketSessionValid(c, token) {
				return
			}
			if err := c.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
				return
			}
		}
	}
}
