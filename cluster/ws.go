package cluster

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

const (
	MsgCallInvite = "CALL_INVITE"
	MsgCallRing   = "CALL_RINGING"
	MsgCallAccept = "CALL_ACCEPT"
	MsgCallReject = "CALL_REJECT"
	MsgICE        = "ICE_CANDIDATE"
	MsgCallEnd    = "CALL_END"
	MsgGetUsers   = "GET_USERS"
	MsgUsers      = "USERS"
)

const (
	CallStateIdle       = "IDLE"
	CallStateInviting   = "INVITING"
	CallStateRinging    = "RINGING"
	CallStateConnecting = "CONNECTING"
	CallStateActive     = "ACTIVE"
	CallStateTerminated = "TERMINATED"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// Call Session Manager
type CallSession struct {
	CallID    string
	Caller    string
	Callee    string
	State     string
	SDP       string
	CreatedAt time.Time
}

var (
	callSessions = make(map[string]*CallSession)
	sessionsMu   sync.RWMutex

	// ICE buffering
	pendingICE = make(map[string][]string)
	iceMu      sync.RWMutex
)

type WSClient struct {
	ID   string
	Conn *websocket.Conn
	Send chan []byte
}

type WSHub struct {
	clients    map[string]*WSClient
	register   chan *WSClient
	unregister chan *WSClient
	broadcast  chan []byte
	mu         sync.RWMutex
}

var wsHub *WSHub

func InitWSHub() *WSHub {
	wsHub = NewWSHub()
	go wsHub.Run()
	return wsHub
}

func NewWSHub() *WSHub {
	return &WSHub{
		clients:    make(map[string]*WSClient),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
		broadcast:  make(chan []byte, 1024),
	}
}

func (h *WSHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			clientCount := len(h.clients)
			h.mu.Unlock()
			log.Printf("[WS] client connected: %s (total: %d)", client.ID, clientCount)
			// Broadcast new user list to all clients
			go func() {
				users := h.getUserList()
				h.mu.RLock()
				for _, c := range h.clients {
					c.Send <- []byte(users)
				}
				h.mu.RUnlock()
			}()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)
				close(client.Send)
			}
			h.mu.Unlock()
			log.Println("[WS] client disconnected:", client.ID)

		case message := <-h.broadcast:
			h.mu.RLock()
			for _, client := range h.clients {
				select {
				case client.Send <- message:
				default:
					close(client.Send)
					delete(h.clients, client.ID)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *WSHub) Register(client *WSClient) {
	h.register <- client
}

func (h *WSHub) Unregister(client *WSClient) {
	h.unregister <- client
}

func (h *WSHub) GetClient(id string) (*WSClient, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	client, ok := h.clients[id]
	return client, ok
}

func (h *WSHub) GetAllClients() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]string, 0, len(h.clients))
	for id := range h.clients {
		ids = append(ids, id)
	}
	return ids
}

func (h *WSHub) getUserList() string {
	h.mu.RLock()
	clients := make([]map[string]string, 0, len(h.clients))
	for id := range h.clients {
		clients = append(clients, map[string]string{"id": id, "name": id})
	}
	h.mu.RUnlock()
	data, _ := json.Marshal(map[string]interface{}{
		"type":  MsgUsers,
		"users": clients,
	})
	return string(data)
}

// Call Session Management
func CreateCallSession(callID, caller, callee string) *CallSession {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	session := &CallSession{
		CallID:    callID,
		Caller:    caller,
		Callee:    callee,
		State:     CallStateInviting,
		CreatedAt: time.Now(),
	}
	callSessions[callID] = session
	log.Printf("[CALL] Created session %s: %s -> %s", callID, caller, callee)
	return session
}

func GetCallSession(callID string) *CallSession {
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	return callSessions[callID]
}

func UpdateCallState(callID, state string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if s, ok := callSessions[callID]; ok {
		log.Printf("[CALL] Session %s: %s -> %s", callID, s.State, state)
		s.State = state
	}
}

func EndCallSession(callID string) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if _, ok := callSessions[callID]; ok {
		log.Printf("[CALL] Ended session %s", callID)
		delete(callSessions, callID)
	}

	iceMu.Lock()
	defer iceMu.Unlock()
	delete(pendingICE, callID)
}

// ICE Buffering
func BufferICE(callID, candidate string) {
	iceMu.Lock()
	defer iceMu.Unlock()
	pendingICE[callID] = append(pendingICE[callID], candidate)
	log.Printf("[ICE] Buffered candidate for %s", callID)
}

func GetBufferedICE(callID string) []string {
	iceMu.Lock()
	defer iceMu.Unlock()
	candidates := pendingICE[callID]
	delete(pendingICE, callID)
	return candidates
}

func (h *WSHub) SendTo(nodeID string, msg []byte) bool {
	h.mu.RLock()
	client, ok := h.clients[nodeID]
	h.mu.RUnlock()

	if !ok {
		return false
	}

	select {
	case client.Send <- msg:
		return true
	default:
		h.mu.Lock()
		delete(h.clients, nodeID)
		h.mu.Unlock()
		return false
	}
}

func (h *WSHub) Broadcast(msg []byte) {
	h.broadcast <- msg
}

func (c *WSClient) ReadPump() {
	defer func() {
		wsHub.Unregister(c)
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(512 * 1024)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		messageType, message, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WS] read error: %v", err)
			}
			break
		}

		if messageType == websocket.TextMessage {
			var msg VoIPMessage
			if err := json.Unmarshal(message, &msg); err != nil {
				log.Printf("[WS] parse error: %v", err)
				continue
			}

			log.Printf("[WS] received %s from %s", msg.Type, c.ID)
			HandleVoIPMessage(c.ID, msg)
		}
	}
}

func (c *WSClient) WritePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.Conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

type VoIPMessage struct {
	Type    string          `json:"type"`
	CallID  string          `json:"call_id,omitempty"`
	From    string          `json:"from"`
	To      string          `json:"to,omitempty"`
	SDP     string          `json:"sdp,omitempty"`
	ICE     string          `json:"ice,omitempty"`
	Code    int             `json:"code,omitempty"`
	Time    int64           `json:"time"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

func HandleVoIPMessage(from string, msg VoIPMessage) {
	log.Printf("[WS] received: %s from=%s to=%s", msg.Type, from, msg.To)

	// ALWAYS respond to GET_USERS with list of all clients
	if msg.Type == MsgGetUsers || msg.Type == "GET_USERS" {
		users := wsHub.GetAllClients()
		log.Printf("[WS] GET_USERS: sending %d users to %s", len(users), from)

		// Broadcast to ALL clients (including sender)
		for _, userID := range users {
			resp := map[string]interface{}{
				"type":  "USERS",
				"users": users,
			}
			data, _ := json.Marshal(resp)
			wsHub.SendTo(userID, data)
		}
		return
	}

	// For all other messages, relay to 'to' if specified
	if msg.To != "" {
		msg.From = from // set sender
		data, _ := json.Marshal(msg)
		if wsHub.SendTo(msg.To, data) {
			log.Printf("[WS] relayed %s to %s", msg.Type, msg.To)
		} else {
			log.Printf("[WS] failed to send to %s", msg.To)
		}
	}
}

func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	userID := vars["user_id"]

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] upgrade error: %v", err)
		return
	}

	client := &WSClient{
		ID:   userID,
		Conn: conn,
		Send: make(chan []byte, 256),
	}

	wsHub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}
