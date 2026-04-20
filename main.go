package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/user"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"zerophone/cluster"
)

var (
	addr       = flag.String("addr", ":8080", "http listen address")
	serverURL  = flag.String("server", "", "server URL to connect to")
	configPort = flag.Int("port", 8081, "cluster port")

	clusterModule *cluster.ClusterModule
	peers         = make(map[string]*Peer)
	peersMu       sync.RWMutex
	started       time.Time
	nodeName      string
	networkID     string
	localIP       string
	serverAddr    string
	isClient      bool
	pendingCalls  = make(map[string]*PendingCall)
	callsMu       sync.Mutex
)

type PendingCall struct {
	CallID   string    `json:"call_id"`
	From     string    `json:"from"`
	FromName string    `json:"from_name"`
	To       string    `json:"to"`
	Status   string    `json:"status"` // pending, accepted, rejected
	Created  time.Time `json:"created"`
}

type Peer struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	NetworkID    string   `json:"network_id"`
	Status       string   `json:"status"`
	LastSeen     int64    `json:"last_seen"`
	IP           string   `json:"ip,omitempty"`
	Port         int      `json:"port,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type Signal struct {
	Type   string `json:"type"`
	From   string `json:"from"`
	To     string `json:"to,omitempty"`
	Sender string `json:"sender"`
	CallID string `json:"call_id,omitempty"`
	SDP    string `json:"sdp,omitempty"`
	ICE    string `json:"ice,omitempty"`
	Time   int64  `json:"time"`
}

func main() {
	flag.Parse()

	nodeName = getNodeName()
	localIP, networkID = detectZeroTier()

	if *serverURL != "" {
		serverAddr = *serverURL
		isClient = true
		log.Printf("Client mode: connecting to %s", serverAddr)
	} else {
		// Server mode
		if localIP != "" {
			log.Printf("ZeroTier: IP=%s Network=%s Node=%s", localIP, networkID, nodeName)
		}
	}

	// Initialize if server
	if !isClient && (localIP != "" || os.Getenv("ZEROPHONE_CLUSTER") == "1") {
		var err error
		clusterModule, err = cluster.NewClusterModule(&cluster.ClusterConfig{
			Enabled:      true,
			DataDir:      getDataDir(),
			ClusterPort:  *configPort,
			BindZeroTier: localIP != "",
			StaticPeers:  nil,
		})
		if err != nil {
			log.Printf("Cluster init failed: %v", err)
		} else {
			log.Printf("Cluster node: %s", clusterModule.GetLocalNode().ID)
		}
	}

	started = time.Now()

	router := mux.NewRouter()
	router.HandleFunc("/", handleIndex)
	router.HandleFunc("/call", handleCall)
	router.HandleFunc("/call/signal", handleCallSignal)
	router.HandleFunc("/call/respond", handleCallResponse)
	router.HandleFunc("/call.html", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/call.html")
	})
	router.HandleFunc("/register", handleRegister)
	router.HandleFunc("/heartbeat", handleHeartbeat)
	router.HandleFunc("/nodes", handleNodes)
	router.HandleFunc("/signal", handleSignal)
	router.HandleFunc("/status", handleStatus)
	router.HandleFunc("/ping", handlePing)
	router.HandleFunc("/presence", handlePresence)
	router.HandleFunc("/dial", handleDial)
	router.HandleFunc("/answer", handleAnswer)
	router.HandleFunc("/call/request", handleCallRequest)
	router.HandleFunc("/call/end", handleCallEnd)
	router.HandleFunc("/sdp/offer", handleSDPOffer)
	router.HandleFunc("/sdp/answer", handleSDPAnswer)
	router.HandleFunc("/ice/candidate", handleICECandidate)

	// Cluster routes
	if clusterModule != nil {
		clusterModule.RegisterRoutes(router)
		clusterModule.Start(nil)
		go broadcastPresenceLoop()
	}

	// Client: connect and register
	if isClient && serverAddr != "" {
		go clientLoop()
	}

	bindAddr := *addr
	if localIP != "" && *addr == ":8080" {
		bindAddr = net.JoinHostPort(localIP, "8080")
	}

	log.Printf("ZeroPhone v1.0 starting on %s", bindAddr)
	if isClient {
		log.Printf("Running as client - connect to %s", serverAddr)
	}
	log.Fatal(http.ListenAndServe(bindAddr, router))
}

func getNodeName() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return "node-" + h
	}
	if u, err := user.Current(); err == nil {
		return "phone-" + u.Username
	}
	b := make([]byte, 4)
	rand.Read(b)
	return "zerophone-" + hex.EncodeToString(b)
}

func detectZeroTier() (string, string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", ""
	}
	for _, iface := range ifaces {
		if !strings.HasPrefix(iface.Name, "zt") {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ipStr := strings.Split(addr.String(), "/")[0]
			if isZeroTierIP(ipStr) {
				return ipStr, strings.TrimPrefix(iface.Name, "zt")
			}
		}
	}
	if zt := os.Getenv("ZEROTIER_IP"); zt != "" {
		return zt, os.Getenv("ZEROTIER_NETWORK")
	}
	return "", ""
}

func isZeroTierIP(ip string) bool {
	return strings.HasPrefix(ip, "fd") || strings.HasPrefix(ip, "10.")
}

func getDataDir() string {
	if dir := os.Getenv("ZEROPHONE_DATA"); dir != "" {
		return dir
	}
	if u, err := user.Current(); err == nil {
		return "/home/" + u.Username + "/.zerophone"
	}
	return "/var/lib/zerophone"
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func clientLoop() {
	// Register with server
	for {
		if serverAddr == "" {
			time.Sleep(5 * time.Second)
			continue
		}

		req := struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			NetworkID string `json:"network_id"`
		}{
			ID:        generateID(),
			Name:      nodeName,
			NetworkID: networkID,
		}

		data, _ := json.Marshal(req)
		resp, err := http.Post(serverAddr+"/register", "application/json", strings.NewReader(string(data)))
		if err != nil {
			log.Printf("Register failed: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}
		resp.Body.Close()

		log.Printf("Registered with server")

		// Heartbeat loop
		for {
			http.Get(serverAddr + "/heartbeat?node_id=" + req.ID)
			time.Sleep(5 * time.Second)
		}
	}
}

func broadcastPresenceLoop() {
	knownPeers := []string{}
	if peersEnv := os.Getenv("ZEROPHONE_PEERS"); peersEnv != "" {
		knownPeers = strings.Split(peersEnv, ",")
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		// ZeroMQ broadcast
		if clusterModule != nil {
			p := map[string]string{
				"node_id":  clusterModule.GetLocalNode().ID,
				"name":     nodeName,
				"ip":       localIP,
				"networks": networkID,
			}
			data, _ := json.Marshal(p)
			clusterModule.BroadcastNodeUpdate(data)
		}

		// HTTP ping known peers
		for _, peer := range knownPeers {
			go func(p string) {
				http.Get("http://" + p + "/ping?node_id=" + nodeName)
			}(peer)
		}
	}
}

// Handlers

func handleIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "static/index.html")
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := map[string]interface{}{
		"version":   "1.0.0",
		"node_name": nodeName,
		"uptime":    time.Since(started).String(),
		"ip":        localIP,
		"network":   networkID,
		"cluster":   clusterModule != nil,
		"client":    isClient,
		"server":    serverAddr,
	}
	if clusterModule != nil {
		status["node_id"] = clusterModule.GetLocalNode().ID
	}
	peersMu.RLock()
	status["peers"] = len(peers)
	peersMu.RUnlock()
	json.NewEncoder(w).Encode(status)
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		NetworkID string `json:"network_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		req.ID = generateID()
	}
	if req.Name == "" {
		req.Name = nodeName
	}

	peer := &Peer{
		ID:        req.ID,
		Name:      req.Name,
		NetworkID: req.NetworkID,
		Status:    "online",
		LastSeen:  time.Now().Unix(),
		IP:        localIP,
	}

	peersMu.Lock()
	peers[req.ID] = peer
	peersMu.Unlock()

	log.Printf("[REGISTER] %s as %s", req.ID, req.Name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     req.ID,
		"name":   req.Name,
		"status": "registered",
	})
}

func handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if nodeID := r.URL.Query().Get("node_id"); nodeID != "" {
		peersMu.Lock()
		if p, ok := peers[nodeID]; ok {
			p.LastSeen = time.Now().Unix()
			p.Status = "online"
		}
		peersMu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleNodes(w http.ResponseWriter, r *http.Request) {
	networkID := r.URL.Query().Get("network_id")

	peersMu.RLock()
	var result []*Peer
	for _, p := range peers {
		if networkID == "" || p.NetworkID == networkID {
			if time.Now().Unix()-p.LastSeen > 60 {
				p.Status = "offline"
			}
			result = append(result, p)
		}
	}
	peersMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func handleSignal(w http.ResponseWriter, r *http.Request) {
	var sig Signal
	json.NewDecoder(r.Body).Decode(&sig)
	sig.Time = time.Now().Unix()
	if sig.Sender == "" && clusterModule != nil {
		sig.Sender = clusterModule.GetLocalNode().ID
	}
	log.Printf("[SIGNAL] %s from %s to %s", sig.Type, sig.Sender, sig.To)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "delivered"})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	peersMu.RLock()
	online := 0
	for _, p := range peers {
		if p.Status == "online" {
			online++
		}
	}
	peersMu.RUnlock()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"version":   "1.0.0",
		"node_name": nodeName,
		"uptime":    time.Since(started).String(),
		"ip":        localIP,
		"network":   networkID,
		"cluster":   clusterModule != nil,
		"client":    isClient,
		"peers":     len(peers),
		"online":    online,
	})
}

func handlePing(w http.ResponseWriter, r *http.Request) {
	var data map[string]interface{}
	json.NewDecoder(r.Body).Decode(&data)
	device, _ := data["device"].(string)
	if device != "" {
		peersMu.Lock()
		peers[device] = &Peer{ID: device, Name: device, Status: "online", LastSeen: time.Now().Unix()}
		peersMu.Unlock()
		log.Printf("[PRESENCE] %s", device)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handlePresence(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		peersMu.RLock()
		list := make([]*Peer, 0, len(peers))
		for _, p := range peers {
			list = append(list, p)
		}
		peersMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(list)
		return
	}
	var p struct {
		NodeID string `json:"node_id"`
		Name   string `json:"name"`
		IP     string `json:"ip"`
	}
	json.NewDecoder(r.Body).Decode(&p)
	if p.NodeID != "" {
		peersMu.Lock()
		peers[p.NodeID] = &Peer{ID: p.NodeID, Name: p.Name, Status: "online", LastSeen: time.Now().Unix(), IP: p.IP}
		peersMu.Unlock()
		log.Printf("[PRESENCE] %s from %s", p.Name, p.IP)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleDial(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To string `json:"to"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	callID := generateID()
	log.Printf("[CALL] to %s", req.To)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"call_id": callID})
}

func handleAnswer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleCallRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To     string `json:"to"`
		Caller string `json:"caller"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	callID := generateID()
	log.Printf("[CALL] from %s to %s", req.Caller, req.To)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"call_id": callID,
		"type":    "ringing",
	})
}

func handleCall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	nodeID := ""
	if clusterModule != nil {
		nodeID = clusterModule.GetLocalNode().ID
	}

	if r.Method == "POST" {
		var req struct {
			To   string `json:"to"`
			Type string `json:"type"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		// Create pending call
		if req.To != "" && nodeID != "" {
			callID := generateID()
			callsMu.Lock()
			pendingCalls[callID] = &PendingCall{
				CallID:   callID,
				From:     nodeID,
				FromName: nodeName,
				To:       req.To,
				Status:   "pending",
				Created:  time.Now(),
			}
			callsMu.Unlock()

			log.Printf("[CALL] %s -> %s (call_id: %s)", nodeName, req.To, callID)

			// Store for polling - also send via HTTP to peer if known
			peersMu.RLock()
			for _, p := range peers {
				if p.ID == req.To {
					// Peer known - send directly
					go http.Post("http://"+p.IP+"/signal", "application/json",
						strings.NewReader(`{"type":"CALL","call_id":"`+callID+`","from":"`+nodeID+`","from_name":"`+nodeName+`"}`))
				}
			}
			peersMu.RUnlock()

			json.NewEncoder(w).Encode(map[string]string{"call_id": callID})
			return
		}
	}

	// GET - check for pending incoming calls for this node
	if r.Method == "GET" || r.Method == "POST" {
		callsMu.Lock()
		var incoming []*PendingCall
		for _, c := range pendingCalls {
			if c.To == nodeID && c.Status == "pending" {
				incoming = append(incoming, c)
			}
		}
		callsMu.Unlock()
		json.NewEncoder(w).Encode(incoming)
		return
	}

	json.NewEncoder(w).Encode([]*PendingCall{})
}

func handleCallSignal(w http.ResponseWriter, r *http.Request) {
	// Receive call signal
	w.Header().Set("Content-Type", "application/json")
	var sig struct {
		CallID string `json:"call_id"`
		From   string `json:"from"`
		Type   string `json:"type"`
	}
	json.NewDecoder(r.Body).Decode(&sig)

	nodeID := ""
	if clusterModule != nil {
		nodeID = clusterModule.GetLocalNode().ID
	}

	if sig.Type == "CALL" && nodeID != "" {
		callsMu.Lock()
		pendingCalls[sig.CallID] = &PendingCall{
			CallID:  sig.CallID,
			From:    sig.From,
			To:      nodeID,
			Status:  "pending",
			Created: time.Now(),
		}
		callsMu.Unlock()
		log.Printf("[CALL INCOMING] %s -> %s", sig.From, nodeID)
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleCallResponse(w http.ResponseWriter, r *http.Request) {
	// Accept/reject call
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		CallID string `json:"call_id"`
		Status string `json:"status"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	callsMu.Lock()
	if c, ok := pendingCalls[req.CallID]; ok {
		c.Status = req.Status
		log.Printf("[CALL %s] %s", req.Status, c.CallID)
	}
	callsMu.Unlock()

	json.NewEncoder(w).Encode(map[string]string{"status": req.Status})
}

func handleCallEnd(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ended"})
}

func handleSDPOffer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "received"})
}

func handleSDPAnswer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleICECandidate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
