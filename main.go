package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
)

var (
	addr   = flag.String("addr", ":3478", "http service address")
	dbPath = flag.String("db", "zerophone.db", "path to SQLite database file")
)

func main() {
	flag.Parse()

	// Initialize database
	db, err := NewDatabase(*dbPath)
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}
	if err := db.Migrate(); err != nil {
		log.Fatal("Failed to migrate database:", err)
	}
	defer db.Close()

	log.Println("Database initialized at", *dbPath)

	// Initialize store with database
	store := NewStore()
	store.SetDB(db)

	hub := NewWSHub()
	go hub.Run()

	// Start call timeout monitor
	go monitorCallTimeouts(store)

	router := NewRouter(store, hub)

	// Serve static files
	router.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))

	go func() {
		if err := http.ListenAndServe(*addr, router); err != nil {
			log.Fatal("ListenAndServe:", err)
		}
	}()

	log.Println("ZeroPhone server started on", *addr)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("shutting down...")
}

func NewRouter(store *Store, hub *WSHub) *mux.Router {
	r := mux.NewRouter()

	r.HandleFunc("/register", RegisterHandler(store)).Methods("POST")
	r.HandleFunc("/heartbeat", HeartbeatHandler(store)).Methods("POST")
	r.HandleFunc("/nodes", NodesHandler(store)).Methods("GET")
	r.HandleFunc("/signal", SignalHandler(store, hub)).Methods("POST")
	r.HandleFunc("/poll/{node_id}", PollHandler(store)).Methods("GET")
	r.HandleFunc("/ws/{node_id}", WSHandler(hub)).Methods("GET")

	return r
}

func RegisterHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RegisterRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.ID == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		if req.NetworkID == "" {
			http.Error(w, "network_id required", http.StatusBadRequest)
			return
		}
		node := store.RegisterNode(req.ID, req.Name, req.NetworkID, req.Capabilities)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(node)
	}
}

func HeartbeatHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.URL.Query().Get("node_id")
		if nodeID == "" {
			http.Error(w, "node_id required", http.StatusBadRequest)
			return
		}
		if !store.Heartbeat(nodeID) {
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}
		w.Write([]byte("ok"))
	}
}

func NodesHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		networkID := r.URL.Query().Get("network_id")
		if networkID != "" {
			json.NewEncoder(w).Encode(store.GetNodesByNetwork(networkID))
		} else {
			json.NewEncoder(w).Encode(store.GetNodes())
		}
	}
}

func SignalHandler(store *Store, hub *WSHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req SignalRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.FromID == "" || req.ToID == "" {
			http.Error(w, "from_id and to_id required", http.StatusBadRequest)
			return
		}

		msg := Message{
			Type:    req.Type,
			FromID:  req.FromID,
			ToID:    req.ToID,
			CallID:  req.CallID,
			SDP:     req.SDP,
			Payload: req.Payload,
		}

		switch req.Type {
		case MsgCallRequest:
			if req.CallID != "" {
				store.CreateCall(req.CallID, req.FromID, req.ToID)
			}
		case MsgCallAccept:
			if req.CallID != "" {
				store.UpdateCallState(req.CallID, CallStateActive)
			}
		case MsgCallEnd:
			if req.CallID != "" {
				store.UpdateCallState(req.CallID, CallStateEnded)
			}
		case MsgSDPOffer, MsgSDPAnswer, MsgICECandidate:
			// WebRTC signaling - just pass through
		}

		store.QueueMessage(msg)

		if hub != nil {
			hub.SendTo(req.ToID, msg)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "queued"})
	}
}

func PollHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeID := mux.Vars(r)["node_id"]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(store.GetMessages(nodeID))
	}
}

// monitorCallTimeouts checks for calls ringing > 60s and auto-rejects them
func monitorCallTimeouts(store *Store) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now().Unix()
		calls, err := store.db.GetAllCalls()
		if err != nil {
			continue
		}
		for _, call := range calls {
			if call.State == CallStateRinging {
				// Check if call is older than 60 seconds
				if now-call.CreatedAt > 60 {
					store.UpdateCallState(call.CallID, CallStateEnded)
					log.Println("Call", call.CallID, "timed out after 60s ringing")
				}
			}
		}
	}
}
