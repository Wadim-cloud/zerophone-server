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
			h.mu.Unlock()
			log.Println("[WS] client connected:", client.ID)

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
	// Parse payload if SDP not at root level
	if msg.SDP == "" && msg.Payload != nil {
		var p struct {
			SDP interface{} `json:"sdp"`
		}
		json.Unmarshal(msg.Payload, &p)
		if sdp, ok := p.SDP.(string); ok {
			msg.SDP = sdp
		} else if sdpMap, ok := p.SDP.(map[string]interface{}); ok {
			// Convert SDP object to JSON string
			sdpJSON, _ := json.Marshal(sdpMap)
			msg.SDP = string(sdpJSON)
		}
	}

	switch msg.Type {
	case MsgCallInvite:
		if _, ok := wsHub.GetClient(msg.To); ok {
			msg.Type = MsgCallInvite
			msg.Time = time.Now().Unix()
			msg.From = from
			data, _ := json.Marshal(msg)
			wsHub.SendTo(msg.To, data)
		}

	case MsgCallRing:
		if _, ok := wsHub.GetClient(msg.To); ok {
			msg.From = from
			data, _ := json.Marshal(msg)
			wsHub.SendTo(msg.To, data)
		}

	case MsgCallAccept:
		if _, ok := wsHub.GetClient(msg.To); ok {
			msg.From = from
			data, _ := json.Marshal(msg)
			wsHub.SendTo(msg.To, data)
		}

	case MsgCallReject:
		if _, ok := wsHub.GetClient(msg.To); ok {
			msg.From = from
			data, _ := json.Marshal(msg)
			wsHub.SendTo(msg.To, data)
		}

	case MsgICE:
		if _, ok := wsHub.GetClient(msg.To); ok {
			msg.From = from
			data, _ := json.Marshal(msg)
			wsHub.SendTo(msg.To, data)
		}

	case MsgCallEnd:
		if _, ok := wsHub.GetClient(msg.To); ok {
			msg.From = from
			data, _ := json.Marshal(msg)
			wsHub.SendTo(msg.To, data)
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
