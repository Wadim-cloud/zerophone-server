package cluster

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"zerophone/core"
)

// =======================
// 👤 REMOTE USER
// =======================

type RemoteUser struct {
	ID   string
	Name string
	Node string
}

// =======================
// 📡 WS HUB (TRANSPORT LAYER ONLY)
// =======================

type WSHub struct {
	clients    map[string]*WSClient
	register   chan *WSClient
	unregister chan *WSClient
	broadcast  chan []byte

	mu sync.RWMutex

	discovery *DiscoveryService
	zmq       *ZMQNodeConnection
	nodeID    string

	remoteUsers map[string]*RemoteUser

	// 🧠 CORE INTEGRATION
	router *core.SignalRouter
}

type WSClient struct {
	ID   string
	Name string
	Conn *websocket.Conn
	Send chan []byte
}

var wsHub *WSHub

// =======================
// 🚀 INIT
// =======================

func InitWSHub(discovery *DiscoveryService, zmq *ZMQNodeConnection, nodeID string) *WSHub {
	wsHub = &WSHub{
		clients:     make(map[string]*WSClient),
		register:    make(chan *WSClient),
		unregister:  make(chan *WSClient),
		broadcast:   make(chan []byte, 1024),
		discovery:   discovery,
		zmq:         zmq,
		nodeID:      nodeID,
		remoteUsers: make(map[string]*RemoteUser),
	}

	go wsHub.run()

	if discovery != nil {
		discovery.EventBus = make(chan ClusterEvent, 256)
		go wsHub.handleDiscoveryEvents()
	}

	return wsHub
}

// =======================
// 🔌 CONNECT SIGNAL ROUTER
// =======================

func (h *WSHub) AttachSignalRouter(r *core.SignalRouter) {
	h.router = r
	// Note: Transport is set at ClusterModule level, not here
}

// =======================
// 📡 TRANSPORT INTERFACE
// =======================

// REQUIRED by SignalRouter
func (h *WSHub) SendTo(id string, data []byte) bool {
	h.mu.RLock()
	client, ok := h.clients[id]
	h.mu.RUnlock()

	if ok {
		select {
		case client.Send <- data:
			return true
		default:
			return false
		}
	}

	// 🌍 cluster fallback (pure transport)
	if h.zmq != nil {
		_ = h.zmq.Broadcast("CALL_SIGNAL", data)
	}

	return false
}

// =======================
// 🔁 MAIN LOOP
// =======================

func (h *WSHub) run() {
	for {
		select {

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client
			h.mu.Unlock()

			log.Printf("[WS] connected %s", client.ID)

			if h.discovery != nil {
				_ = h.discovery.BroadcastAnnouncement("USER_ONLINE", map[string]interface{}{
					"user_id": client.ID,
					"name":    client.Name,
					"node":    h.nodeID,
				})
			}

			h.broadcastUserList()

		case client := <-h.unregister:
			h.mu.Lock()
			delete(h.clients, client.ID)
			h.mu.Unlock()

			close(client.Send)

			log.Printf("[WS] disconnected %s", client.ID)

			if h.discovery != nil {
				_ = h.discovery.BroadcastAnnouncement("USER_OFFLINE", map[string]interface{}{
					"user_id": client.ID,
					"node":    h.nodeID,
				})
			}

			h.broadcastUserList()

		case msg := <-h.broadcast:
			h.mu.RLock()
			for _, c := range h.clients {
				select {
				case c.Send <- msg:
				default:
				}
			}
			h.mu.RUnlock()
		}
	}
}

// =======================
// 🧠 SIGNAL ENTRY POINT
// =======================

func (h *WSHub) HandleIncoming(from string, msg []byte) {
	if h.router == nil {
		log.Println("[WS] no signal router attached")
		return
	}

	var s core.Signal
	if err := json.Unmarshal(msg, &s); err != nil {
		log.Println("[WS] invalid signal:", err)
		return
	}

	if s.From == "" {
		s.From = from
	}

	// 🧠 everything goes through SIP brain
	h.router.Dispatch(s)
}

// =======================
// 🌍 CLUSTER EVENTS
// =======================

func (h *WSHub) handleDiscoveryEvents() {
	if h.discovery == nil || h.discovery.EventBus == nil {
		return
	}

	for event := range h.discovery.EventBus {
		h.handleClusterEvent(event.Type, event.Payload)
	}
}

func (h *WSHub) handleClusterEvent(event string, payload []byte) {
	switch event {

	case "USER_ONLINE":
		var u map[string]string
		_ = json.Unmarshal(payload, &u)

		h.mu.Lock()
		h.remoteUsers[u["user_id"]] = &RemoteUser{
			ID:   u["user_id"],
			Name: u["name"],
			Node: u["node"],
		}
		h.mu.Unlock()

		h.broadcastUserList()

	case "USER_OFFLINE":
		var u map[string]string
		_ = json.Unmarshal(payload, &u)

		h.mu.Lock()
		delete(h.remoteUsers, u["user_id"])
		h.mu.Unlock()

		h.broadcastUserList()

		// 🚫 IMPORTANT CHANGE:
		// NO MORE CALL_SIGNAL HANDLING HERE
		// Signals now go → SignalRouter → Resolver → Transport
	}
}

// =======================
// 👤 USERS (INCLUDING REMOTE)
// =======================

func (h *WSHub) AddRemoteUser(id, name, node string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.remoteUsers[id] = &RemoteUser{
		ID:   id,
		Name: name,
		Node: node,
	}

	h.broadcastUserList()
}

// GetUsers returns list of all connected users (local + remote)
func (h *WSHub) GetUsers() []map[string]string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	users := []map[string]string{}

	for _, c := range h.clients {
		users = append(users, map[string]string{
			"id":   c.ID,
			"name": c.Name,
		})
	}

	for _, r := range h.remoteUsers {
		users = append(users, map[string]string{
			"id":   r.ID,
			"name": r.Name + " 🌍",
			"node": r.Node,
		})
	}

	return users
}

func (h *WSHub) broadcastUserList() {
	users := []map[string]string{}

	h.mu.RLock()

	for _, c := range h.clients {
		users = append(users, map[string]string{
			"id":   c.ID,
			"name": c.Name,
		})
	}

	for _, r := range h.remoteUsers {
		users = append(users, map[string]string{
			"id":   r.ID,
			"name": r.Name + " 🌍",
			"node": r.Node,
		})
	}

	h.mu.RUnlock()

	data, _ := json.Marshal(map[string]interface{}{
		"type":  "USERS",
		"users": users,
	})

	h.mu.RLock()
	for _, c := range h.clients {
		select {
		case c.Send <- data:
		default:
		}
	}
	h.mu.RUnlock()
}

// =======================
// 🌐 HANDLER
// =======================

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	userID := mux.Vars(r)["user_id"]

	log.Printf("[WS] New connection from user: %s", userID)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Upgrade failed: %v", err)
		return
	}

	client := &WSClient{
		ID:   userID,
		Name: userID,
		Conn: conn,
		Send: make(chan []byte, 256),
	}

	wsHub.register <- client

	go client.writePump()
	go client.readPump()
}

// =======================
// 🔁 CLIENT LOOP
// =======================

func (c *WSClient) readPump() {
	defer func() {
		wsHub.unregister <- c
		c.Conn.Close()
	}()

	for {
		_, msg, err := c.Conn.ReadMessage()
		if err != nil {
			return
		}

		wsHub.HandleIncoming(c.ID, msg)
	}
}

func (c *WSClient) writePump() {
	for {
		select {
		case msg, ok := <-c.Send:
			if !ok {
				return
			}
			_ = c.Conn.WriteMessage(websocket.TextMessage, msg)
		}
	}
}
