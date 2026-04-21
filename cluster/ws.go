package cluster

import (
	"encoding/json"
	"fmt"
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
	MsgJoin       = "JOIN"
)

// Call states
const (
	CallStateIdle       = "IDLE"
	CallStateInviting   = "INVITING"
	CallStateRinging    = "RINGING"
	CallStateConnecting = "CONNECTING"
	CallStateActive     = "ACTIVE"
	CallStateTerminated = "TERMINATED"
)

// Valid state transitions
var validTransitions = map[string][]string{
	CallStateIdle:       {CallStateInviting},
	CallStateInviting:   {CallStateRinging, CallStateTerminated},
	CallStateRinging:    {CallStateConnecting, CallStateTerminated},
	CallStateConnecting: {CallStateActive, CallStateTerminated},
	CallStateActive:     {CallStateTerminated},
}

func isValidTransition(from, to string) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// Client names storage
var clientNames = make(map[string]string)

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

	// ICE buffering per session
	pendingICE = make(map[string][]string)
	iceMu      sync.RWMutex
)

type WSClient struct {
	ID   string
	Name string
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
			log.Printf("[WS] client connected: %s as '%s' (total: %d)", client.ID, client.Name, clientCount)
			// Broadcast updated user list
			h.broadcastUserList()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)
				close(client.Send)
			}
			h.mu.Unlock()
			log.Println("[WS] client disconnected:", client.ID)
			// Broadcast updated user list
			h.broadcastUserList()

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

func (h *WSHub) broadcastUserList() {
	users := h.GetAllClients()
	userList := make([]map[string]string, 0, len(users))
	for _, id := range users {
		name := id
		if n, ok := clientNames[id]; ok {
			name = n
		}
		userList = append(userList, map[string]string{"id": id, "name": name})
	}

	data, _ := json.Marshal(map[string]interface{}{
		"type":  MsgUsers,
		"users": userList,
	})

	h.mu.RLock()
	for _, c := range h.clients {
		c.Send <- data
	}
	h.mu.RUnlock()
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

func (h *WSHub) SendTo(nodeID string, data []byte) bool {
	h.mu.RLock()
	client, ok := h.clients[nodeID]
	h.mu.RUnlock()

	if !ok {
		return false
	}

	select {
	case client.Send <- data:
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

func UpdateCallState(callID, newState string) bool {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()

	s, ok := callSessions[callID]
	if !ok {
		return false
	}

	if !isValidTransition(s.State, newState) {
		log.Printf("[CALL] INVALID transition %s: %s -> %s", callID, s.State, newState)
		return false
	}

	log.Printf("[CALL] %s: %s -> %s", callID, s.State, newState)
	s.State = newState
	return true
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

// Flush buffered ICE candidates to peer
func flushICE(callID, to string) {
	candidates := GetBufferedICE(callID)
	if len(candidates) == 0 {
		return
	}

	log.Printf("[ICE] Flushing %d candidates to %s", len(candidates), to)

	for _, ice := range candidates {
		msg := map[string]interface{}{
			"type":      MsgICE,
			"call_id":   callID,
			"from":      "",
			"to":        to,
			"candidate": ice,
		}
		data, _ := json.Marshal(msg)
		wsHub.SendTo(to, data)
	}
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

// Validate session access
func isSessionParticipant(callID, nodeID string) bool {
	session := GetCallSession(callID)
	if session == nil {
		return false
	}
	return session.Caller == nodeID || session.Callee == nodeID
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
	Type      string          `json:"type"`
	CallID    string          `json:"call_id,omitempty"`
	From      string          `json:"from,omitempty"`
	To        string          `json:"to,omitempty"`
	SDP       string          `json:"sdp,omitempty"`
	Candidate string          `json:"candidate,omitempty"`
	ICE       string          `json:"ice,omitempty"`
	Code      int             `json:"code,omitempty"`
	Time      int64           `json:"time"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func HandleVoIPMessage(from string, msg VoIPMessage) {
	log.Printf("[WS] %s from=%s to=%s call=%s", msg.Type, from, msg.To, msg.CallID)

	// Handle JOIN - store client name
	if msg.Type == MsgJoin || msg.Type == "JOIN" {
		var joinName string
		if msg.Payload != nil {
			var joinData struct {
				Name string `json:"name"`
			}
			json.Unmarshal(msg.Payload, &joinData)
			joinName = joinData.Name
		}
		if joinName == "" {
			joinName = msg.From
		}
		if joinName != "" {
			clientNames[from] = joinName
			log.Printf("[WS] %s joined as '%s'", from, joinName)
		}
		// Broadcast updated user list on JOIN
		wsHub.broadcastUserList()
	}

	// Respond to GET_USERS
	if msg.Type == MsgGetUsers || msg.Type == "GET_USERS" {
		users := wsHub.GetAllClients()
		userList := make([]map[string]string, len(users))
		for i, userID := range users {
			name := userID
			if n, ok := clientNames[userID]; ok {
				name = n
			}
			userList[i] = map[string]string{"id": userID, "name": name}
		}

		resp := map[string]interface{}{
			"type":  MsgUsers,
			"users": userList,
		}
		data, _ := json.Marshal(resp)
		wsHub.SendTo(from, data)
		return
	}

	// Handle CALL_INVITE
	if msg.Type == MsgCallInvite {
		if msg.To == "" {
			log.Printf("[CALL] INVITE without destination")
			return
		}

		// Check if callee is online
		if _, ok := wsHub.GetClient(msg.To); !ok {
			log.Printf("[CALL] Callee %s not online", msg.To)
			return
		}

		// Create session
		callID := msg.CallID
		if callID == "" {
			callID = "call-" + fmt.Sprintf("%d", time.Now().UnixNano())
		}

		CreateCallSession(callID, from, msg.To)

		// Relay to callee
		msg.From = from
		msg.CallID = callID
		data, _ := json.Marshal(msg)
		if wsHub.SendTo(msg.To, data) {
			log.Printf("[CALL] INVITE relayed to %s (call_id: %s)", msg.To, callID)
		}
		return
	}

	// Handle CALL_ACCEPT
	if msg.Type == MsgCallAccept {
		if !UpdateCallState(msg.CallID, CallStateConnecting) {
			return
		}

		// Relay to caller
		msg.From = from
		data, _ := json.Marshal(msg)
		session := GetCallSession(msg.CallID)
		if session != nil {
			wsHub.SendTo(session.Caller, data)
			// Flush buffered ICE to caller
			go flushICE(msg.CallID, session.Caller)
		}
		return
	}

	// Handle CALL_REJECT
	if msg.Type == MsgCallReject {
		session := GetCallSession(msg.CallID)
		if session != nil {
			UpdateCallState(msg.CallID, CallStateTerminated)
			msg.From = from
			data, _ := json.Marshal(msg)
			wsHub.SendTo(session.Caller, data)
		}
		return
	}

	// Handle ICE candidate
	if msg.Type == MsgICE || msg.Type == "ICE_CANDIDATE" {
		candidate := msg.Candidate
		if candidate == "" {
			candidate = msg.ICE
		}

		if msg.To == "" {
			return
		}

		// Try immediate send
		msg.From = from
		data, _ := json.Marshal(msg)
		if !wsHub.SendTo(msg.To, data) {
			// Buffer if peer not ready
			if msg.CallID != "" {
				BufferICE(msg.CallID, candidate)
			}
		}
		return
	}

	// Handle CALL_END
	if msg.Type == MsgCallEnd {
		session := GetCallSession(msg.CallID)
		if session != nil {
			UpdateCallState(msg.CallID, CallStateTerminated)
			msg.From = from

			// Notify other participant
			if session.Caller != from {
				wsHub.SendTo(session.Caller, mustJSON(msg))
			}
			if session.Callee != from {
				wsHub.SendTo(session.Callee, mustJSON(msg))
			}
		}
		return
	}

	// Generic relay with session validation
	if msg.To != "" && msg.CallID != "" {
		// Validate sender is participant
		if !isSessionParticipant(msg.CallID, from) {
			log.Printf("[SECURITY] %s tried invalid access to %s", from, msg.CallID)
			return
		}

		msg.From = from
		data, _ := json.Marshal(msg)
		if wsHub.SendTo(msg.To, data) {
			log.Printf("[WS] relayed %s -> %s", msg.Type, msg.To)
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
		Name: userID,
		Conn: conn,
		Send: make(chan []byte, 256),
	}

	wsHub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}
