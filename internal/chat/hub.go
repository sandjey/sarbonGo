package chat

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	maxMsgSize = 32 << 10
)

// Client is a single WebSocket connection bound to a user.
type Client struct {
	UserID uuid.UUID
	Conn   *websocket.Conn
	Send   chan []byte
	Hub    *Hub
	logger *zap.Logger
}

// ReadPump reads messages from the connection (blocking). Call in a goroutine or after WritePump.
func (c *Client) ReadPump() {
	defer func() {
		c.Hub.Unregister(c)
		_ = c.Conn.Close()
	}()
	c.Conn.SetReadLimit(maxMsgSize)
	_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
	c.Conn.SetPongHandler(func(string) error {
		_ = c.Conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, raw, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.logger.Debug("chat ws read error", zap.Error(err))
			}
			break
		}
		var envelope struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}
		// Keep Redis "online" fresh for long-lived WebSocket (presence TTL 65s).
		c.Hub.TouchPresence(c.UserID)
		switch envelope.Type {
		case "typing":
			var body struct {
				ConversationID string `json:"conversation_id"`
			}
			if json.Unmarshal(envelope.Data, &body) == nil && body.ConversationID != "" {
				convID, err := uuid.Parse(body.ConversationID)
				if err == nil && convID != uuid.Nil {
					c.Hub.BroadcastTyping(convID, c.UserID)
				}
			}
		case "typing_stop":
			var body struct {
				ConversationID string `json:"conversation_id"`
			}
			if json.Unmarshal(envelope.Data, &body) == nil && body.ConversationID != "" {
				convID, err := uuid.Parse(body.ConversationID)
				if err == nil && convID != uuid.Nil {
					c.Hub.BroadcastTypingStop(convID, c.UserID)
				}
			}
		case "message":
			// Persistence via REST; WS only receives broadcasts.
		default:
			// WebRTC/call signaling: forward to peer (validated by Hub callback).
			if isCallSignalType(envelope.Type) {
				c.Hub.ForwardCallSignal(c.UserID, envelope.Type, envelope.Data)
			}
		}
	}
}

// WritePump writes messages to the connection. Run in a goroutine.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.Conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.Send:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// OnTypingPeer resolves peer user ID for a conversation; used to send typing to the right user.
type OnTypingPeer func(conversationID, fromUserID uuid.UUID) (peerID uuid.UUID, ok bool)

// OnCallSignal validates and routes signaling payload to the peer.
// It should return the peer user id and the payload to send (already wrapped as {"type","data"} or raw), or ok=false to drop.
type OnCallSignal func(fromUserID uuid.UUID, msgType string, data json.RawMessage) (peerUserID uuid.UUID, payload []byte, ok bool)

// Hub holds all connected clients by user ID and broadcasts to conversations.
type Hub struct {
	mu              sync.RWMutex
	clients         map[uuid.UUID][]*Client
	presence        *PresenceStore
	onTyping        OnTypingPeer
	onCall          OnCallSignal
	onUserConnected func(userID uuid.UUID)
	onSendToUser    func(userID uuid.UUID, payload []byte)
	logger          *zap.Logger
}

func NewHub(presence *PresenceStore, logger *zap.Logger) *Hub {
	return &Hub{
		clients:  make(map[uuid.UUID][]*Client),
		presence: presence,
		logger:   logger,
	}
}

// SetOnTyping sets callback to resolve conversation peer for typing events.
func (h *Hub) SetOnTyping(f OnTypingPeer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onTyping = f
}

func (h *Hub) SetOnCallSignal(f OnCallSignal) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onCall = f
}

// SetOnUserConnected sets callback called when a user opens a new WS connection.
func (h *Hub) SetOnUserConnected(f func(userID uuid.UUID)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onUserConnected = f
}

func (h *Hub) SetOnSendToUser(f func(userID uuid.UUID, payload []byte)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onSendToUser = f
}

func (h *Hub) Register(userID uuid.UUID, conn *websocket.Conn) *Client {
	c := &Client{
		UserID: userID,
		Conn:   conn,
		Send:   make(chan []byte, 256),
		Hub:    h,
		logger: h.logger,
	}
	h.mu.Lock()
	h.clients[userID] = append(h.clients[userID], c)
	h.mu.Unlock()
	if h.presence != nil {
		_ = h.presence.SetOnline(context.Background(), userID)
	}
	h.mu.RLock()
	cb := h.onUserConnected
	h.mu.RUnlock()
	if cb != nil {
		go cb(userID)
	}
	return c
}

// Unregister removes client and sets offline if no more connections for user.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	list := h.clients[c.UserID]
	for i, cl := range list {
		if cl == c {
			list = append(list[:i], list[i+1:]...)
			break
		}
	}
	if len(list) == 0 {
		delete(h.clients, c.UserID)
		if h.presence != nil {
			_ = h.presence.SetOffline(context.Background(), c.UserID)
		}
	} else {
		h.clients[c.UserID] = list
	}
	h.mu.Unlock()
	close(c.Send)
}

// SendToUser sends payload to all connections of the user.
func (h *Hub) SendToUser(userID uuid.UUID, payload []byte) {
	h.mu.RLock()
	list := h.clients[userID]
	cb := h.onSendToUser
	h.mu.RUnlock()
	for _, c := range list {
		select {
		case c.Send <- payload:
		default:
			// skip if buffer full
		}
	}
	if cb != nil {
		cb(userID, payload)
	}
}

// IsOnline returns true if user has at least one active WS client.
func (h *Hub) IsOnline(userID uuid.UUID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[userID]) > 0
}

// BroadcastToConversation sends payload to both participants (userA, userB).
func (h *Hub) BroadcastToConversation(userAID, userBID uuid.UUID, payload []byte) {
	h.SendToUser(userAID, payload)
	h.SendToUser(userBID, payload)
}

// BroadcastTyping notifies the other participant in the conversation (uses OnTypingPeer to resolve peer).
func (h *Hub) BroadcastTyping(conversationID, fromUserID uuid.UUID) {
	if h.presence != nil {
		_ = h.presence.SetTyping(context.Background(), conversationID, fromUserID)
	}
	h.mu.RLock()
	f := h.onTyping
	h.mu.RUnlock()
	if f == nil {
		return
	}
	peerID, ok := f(conversationID, fromUserID)
	if !ok {
		return
	}
	out := map[string]interface{}{
		"type": "typing",
		"data": map[string]string{
			"conversation_id": conversationID.String(),
			"user_id":         fromUserID.String(),
		},
	}
	raw, _ := json.Marshal(out)
	h.SendToUser(peerID, raw)
}

// TouchPresence refreshes Redis online TTL while the WebSocket stays connected.
func (h *Hub) TouchPresence(userID uuid.UUID) {
	if h.presence == nil || userID == uuid.Nil {
		return
	}
	_ = h.presence.Heartbeat(context.Background(), userID)
}

// BroadcastTypingStop clears typing in Redis and notifies the peer immediately.
func (h *Hub) BroadcastTypingStop(conversationID, fromUserID uuid.UUID) {
	if h.presence != nil {
		_ = h.presence.ClearTyping(context.Background(), conversationID, fromUserID)
	}
	h.mu.RLock()
	f := h.onTyping
	h.mu.RUnlock()
	if f == nil {
		return
	}
	peerID, ok := f(conversationID, fromUserID)
	if !ok {
		return
	}
	out := map[string]interface{}{
		"type": "typing_stop",
		"data": map[string]string{
			"conversation_id": conversationID.String(),
			"user_id":         fromUserID.String(),
		},
	}
	raw, _ := json.Marshal(out)
	h.SendToUser(peerID, raw)
}

// SendConversationReadEvent notifies the peer that reader_id advanced the read cursor (REST mark read / open chat).
func (h *Hub) SendConversationReadEvent(peerID uuid.UUID, conversationID, readerID uuid.UUID) {
	if peerID == uuid.Nil {
		return
	}
	out := map[string]interface{}{
		"type": "conversation_read",
		"data": map[string]string{
			"conversation_id": conversationID.String(),
			"reader_id":         readerID.String(),
		},
	}
	raw, _ := json.Marshal(out)
	h.SendToUser(peerID, raw)
}

// BroadcastMessageUpdated notifies both participants (multi-device / edit sync).
func (h *Hub) BroadcastMessageUpdated(userAID, userBID uuid.UUID, msg *Message) {
	payload, _ := json.Marshal(map[string]interface{}{
		"type": "message_updated",
		"data": msg,
	})
	h.BroadcastToConversation(userAID, userBID, payload)
}

// BroadcastMessageDeleted notifies both participants after a soft delete.
func (h *Hub) BroadcastMessageDeleted(userAID, userBID uuid.UUID, conversationID, messageID uuid.UUID) {
	payload, _ := json.Marshal(map[string]interface{}{
		"type": "message_deleted",
		"data": map[string]string{
			"conversation_id": conversationID.String(),
			"message_id":      messageID.String(),
		},
	})
	h.BroadcastToConversation(userAID, userBID, payload)
}

// ForwardCallSignal routes signaling payload (webrtc.* / call.*) to the other party if allowed.
func (h *Hub) ForwardCallSignal(fromUserID uuid.UUID, msgType string, data json.RawMessage) {
	h.mu.RLock()
	f := h.onCall
	h.mu.RUnlock()
	if f == nil {
		return
	}
	peerID, payload, ok := f(fromUserID, msgType, data)
	if !ok || peerID == uuid.Nil || len(payload) == 0 {
		return
	}
	h.SendToUser(peerID, payload)
}

func isCallSignalType(t string) bool {
	return strings.HasPrefix(t, "webrtc.") || strings.HasPrefix(t, "call.")
}

// BroadcastMessage sends a message event to both participants. Conversation is (userAID, userBID).
func (h *Hub) BroadcastMessage(userAID, userBID uuid.UUID, msg *Message) {
	payload, _ := json.Marshal(map[string]interface{}{
		"type": "message",
		"data": msg,
	})
	h.BroadcastToConversation(userAID, userBID, payload)
}
