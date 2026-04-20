package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type WSClient struct {
	id   string
	conn *websocket.Conn
	send chan []byte
}

type WSHub struct {
	clients    map[string]*WSClient
	register   chan *WSClient
	unregister chan *WSClient
	broadcast  chan []byte
	mu         sync.RWMutex
}

func NewWSHub() *WSHub {
	return &WSHub{
		clients:    make(map[string]*WSClient),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
		broadcast:  make(chan []byte),
	}
}

func (h *WSHub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.id] = client
			h.mu.Unlock()
			log.Println("client connected:", client.id)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.id]; ok {
				delete(h.clients, client.id)
				close(client.send)
			}
			h.mu.Unlock()
			log.Println("client disconnected:", client.id)

		case message := <-h.broadcast:
			h.mu.RLock()
			for _, client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client.id)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *WSHub) SendTo(nodeID string, msg Message) bool {
	data, err := json.Marshal(msg)
	if err != nil {
		return false
	}

	h.mu.RLock()
	client, ok := h.clients[nodeID]
	h.mu.RUnlock()

	if !ok {
		return false
	}

	select {
	case client.send <- data:
		return true
	default:
		close(client.send)
		go func() {
			h.mu.Lock()
			delete(h.clients, nodeID)
			h.mu.Unlock()
		}()
		return false
	}
}

func WSHandler(hub *WSHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		nodeID := vars["node_id"]
		if nodeID == "" {
			http.Error(w, "node_id required", http.StatusBadRequest)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("upgrade error:", err)
			return
		}

		client := &WSClient{
			id:   nodeID,
			conn: conn,
			send: make(chan []byte, 256),
		}

		hub.register <- client

		go func() {
			defer func() {
				hub.unregister <- client
				conn.Close()
			}()

			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					break
				}
			}
		}()

		go func() {
			for {
				msg, ok := <-client.send
				if !ok {
					conn.WriteMessage(websocket.CloseMessage, []byte{})
					return
				}

				w := strings.Builder{}
				w.WriteString(" ")
				w.Write(msg)

				if err := conn.WriteMessage(websocket.TextMessage, []byte(w.String()[1:])); err != nil {
					break
				}
			}
		}()
	}
}
