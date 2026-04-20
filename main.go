package main

import (
	"bytes"
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
	callsMu       sync.RWMutex
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
	router.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/debug.html")
	})

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
			if strings.HasPrefix(ipStr, "fd") || strings.HasPrefix(ipStr, "10.") {
				networkID := strings.TrimPrefix(iface.Name, "zt")
				log.Printf("[ZEROTIER] Detected: %s on %s", ipStr, iface.Name)
				return ipStr, networkID
			}
		}
	}
	if zt := os.Getenv("ZEROTIER_IP"); zt != "" {
		return zt, os.Getenv("ZEROTIER_NETWORK")
	}
	return "", ""
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
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		// Broadcast own presence
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

		// Also broadcast via HTTP to discovery addresses on ZeroTier
		if localIP != "" {
			// Try common ZeroTier broadcast addresses
			ips := []string{
				"10.121.15.223:8080", // Wadim's ZeroTier IP
				"10.121.15.208:8080", // Common base
			}
			for _, ip := range ips {
				if ip != localIP+":8080" {
					go func(addr string) {
						req, _ := http.NewRequest("GET", "http://"+addr+"/ping?node_id="+nodeName+"&ip="+localIP+"&network="+networkID, nil)
						client := &http.Client{Timeout: 2 * time.Second}
						client.Do(req)
					}(ip)
				}
			}
		}

		// Also check /nodes endpoint from self to register peers
		if clusterModule != nil {
			allNodes := clusterModule.GetNodeRegistry().GetAll()
			for _, n := range allNodes {
				if n.ZeroTierIP != "" && n.ZeroTierIP != localIP {
					go func(ip string) {
						req, _ := http.NewRequest("GET", "http://"+ip+":8080/presence", nil)
						client := &http.Client{Timeout: 2 * time.Second}
						resp, err := client.Do(req)
						if err == nil {
							var nodes []*Peer
							json.NewDecoder(resp.Body).Decode(&nodes)
							resp.Body.Close()
							peersMu.Lock()
							for _, p := range nodes {
								if p.ID != clusterModule.GetLocalNode().ID {
									peers[p.ID] = p
								}
							}
							peersMu.Unlock()
						}
					}(n.ZeroTierIP)
				}
			}
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

	// Update local node name
	nodeName = req.Name
	networkID = req.NetworkID

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

	// Update cluster node name too
	if clusterModule != nil {
		clusterModule.GetLocalNode().Name = req.Name
	}
	peersMu.Unlock()

	log.Printf("[REGISTER] %s as %s", req.ID, req.Name)

	// Broadcast name update to ZeroTier peers
	if localIP != "" {
		targetIPs := []string{}
		if strings.HasPrefix(localIP, "10.121.15.") {
			targetIPs = []string{"10.121.15.208", "10.121.15.223"}
		}
		for _, ip := range targetIPs {
			if ip != localIP {
				go func(target string) {
					client := &http.Client{Timeout: 3 * time.Second}
					// Notify peer of our name
					data := map[string]interface{}{
						"node_id": req.ID,
						"name":    req.Name,
						"ip":      localIP,
						"network": req.NetworkID,
					}
					payload, _ := json.Marshal(data)
					client.Post("http://"+target+":8080/presence", "application/json", bytes.NewReader(payload))
				}(ip)
			}
		}
	}

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

	var result []*Peer

	// First, get cluster nodes from ZeroMQ cluster
	if clusterModule != nil {
		clusterNodes := clusterModule.GetNodeRegistry().GetAll()
		for _, n := range clusterNodes {
			if networkID == "" || n.NetworkID == networkID {
				peer := &Peer{
					ID:        n.ID,
					Name:      n.Name,
					NetworkID: n.NetworkID,
					Status:    n.Status,
					LastSeen:  n.LastSeen,
					IP:        n.ZeroTierIP,
				}
				if peer.Status == "" {
					if time.Now().Unix()-peer.LastSeen > 60 {
						peer.Status = "offline"
					} else {
						peer.Status = "online"
					}
				}
				result = append(result, peer)
			}
		}
	}

	// Also include HTTP-registered peers
	peersMu.RLock()
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

	ztInterface := ""
	if localIP != "" {
		ifaces, _ := net.Interfaces()
		for _, iface := range ifaces {
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				if strings.Split(addr.String(), "/")[0] == localIP {
					ztInterface = iface.Name
					break
				}
			}
		}
	}

	status := map[string]interface{}{
		"version":       "1.0.0",
		"node_name":     nodeName,
		"node_id":       "",
		"uptime":        time.Since(started).String(),
		"ip":            localIP,
		"network":       networkID,
		"zt_interface":  ztInterface,
		"cluster":       clusterModule != nil,
		"client":        isClient,
		"peers":         len(peers),
		"online":        online,
		"cluster_nodes": 0,
	}

	if clusterModule != nil {
		status["node_id"] = clusterModule.GetLocalNode().ID
		status["cluster_nodes"] = len(clusterModule.GetNodeRegistry().GetAll())
	}

	json.NewEncoder(w).Encode(status)
}

func handlePing(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("node_id")
	nodeIP := r.URL.Query().Get("ip")
	networkID := r.URL.Query().Get("network")

	if nodeID != "" {
		peersMu.Lock()
		if _, ok := peers[nodeID]; !ok {
			peers[nodeID] = &Peer{
				ID:        nodeID,
				Name:      nodeID,
				Status:    "online",
				LastSeen:  time.Now().Unix(),
				IP:        nodeIP,
				NetworkID: networkID,
			}
		} else {
			peers[nodeID].Status = "online"
			peers[nodeID].LastSeen = time.Now().Unix()
			if nodeIP != "" {
				peers[nodeID].IP = nodeIP
			}
		}
		peersMu.Unlock()
		log.Printf("[PRESENCE] %s from %s (network: %s)", nodeID, nodeIP, networkID)
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

	// POST - initiate or respond to call
	if r.Method == "POST" {
		var req struct {
			To     string `json:"to"`
			Type   string `json:"type"`
			CallID string `json:"call_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		// Initiate call
		if req.To != "" {
			callID := req.CallID
			if callID == "" {
				callID = generateID()
			}

			// Find target peer's IP
			targetIP := ""
			targetName := req.To

			// Look in cluster registry
			if clusterModule != nil {
				if n, ok := clusterModule.GetNodeRegistry().Get(req.To); ok {
					targetIP = n.ZeroTierIP
					targetName = n.Name
				}
			}
			// Look in local peers
			peersMu.RLock()
			if p, ok := peers[req.To]; ok {
				targetIP = p.IP
				targetName = p.Name
			}
			peersMu.RUnlock()

			// Store call locally
			callsMu.Lock()
			pendingCalls[callID] = &PendingCall{
				CallID:   callID,
				From:     nodeID,
				FromName: nodeName,
				To:       req.To,
				Status:   "calling",
				Created:  time.Now(),
			}
			callsMu.Unlock()

			log.Printf("[CALL] %s calling %s (call_id: %s, target_ip: %s)", nodeName, targetName, callID, targetIP)

			// Send call signal to peer if we have their IP (and it's not ourselves)
			if targetIP != "" && targetIP != localIP {
				go func() {
					payload := map[string]interface{}{
						"type":      "CALL",
						"call_id":   callID,
						"from":      nodeID,
						"from_name": nodeName,
					}
					data, _ := json.Marshal(payload)
					http.Post("http://"+targetIP+":8080/call/signal", "application/json", strings.NewReader(string(data)))
				}()
			}

			json.NewEncoder(w).Encode(map[string]string{"call_id": callID})
			return
		}

		// Respond to call (accept/reject)
		if req.CallID != "" && req.Type != "" {
			callsMu.Lock()
			if c, ok := pendingCalls[req.CallID]; ok {
				c.Status = req.Type // "accepted" or "rejected"
				log.Printf("[CALL %s] %s", req.Type, req.CallID)

				// Notify the other party
				go func() {
					// Find caller's IP
					callerIP := ""
					peersMu.RLock()
					if p, ok := peers[c.From]; ok {
						callerIP = p.IP
					}
					peersMu.RUnlock()

					if callerIP != "" && callerIP != localIP {
						payload := map[string]interface{}{
							"type":    req.Type,
							"call_id": req.CallID,
						}
						data, _ := json.Marshal(payload)
						http.Post("http://"+callerIP+":8080/call/signal", "application/json", strings.NewReader(string(data)))
					}
				}()
			}
			callsMu.Unlock()
			json.NewEncoder(w).Encode(map[string]string{"status": req.Type})
			return
		}
	}

	// GET - check for pending incoming calls
	callsMu.Lock()
	var result []*PendingCall
	now := time.Now()
	for _, c := range pendingCalls {
		// Show calls for this node that are pending or calling
		if c.To == nodeID && (c.Status == "pending" || c.Status == "calling") {
			// Auto-timeout after 60 seconds
			if now.Sub(c.Created) < 60*time.Second {
				result = append(result, c)
			}
		}
		// Also show outgoing calls that haven't been answered
		if c.From == nodeID && c.Status == "calling" {
			if now.Sub(c.Created) < 60*time.Second {
				result = append(result, c)
			}
		}
	}
	callsMu.Unlock()

	json.NewEncoder(w).Encode(result)
}

func handleCallSignal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var sig struct {
		CallID   string `json:"call_id"`
		From     string `json:"from"`
		FromName string `json:"from_name"`
		Type     string `json:"type"`
	}
	json.NewDecoder(r.Body).Decode(&sig)

	nodeID := ""
	if clusterModule != nil {
		nodeID = clusterModule.GetLocalNode().ID
	}

	// Handle incoming call
	if sig.Type == "CALL" && sig.CallID != "" {
		callsMu.Lock()
		if _, exists := pendingCalls[sig.CallID]; !exists {
			pendingCalls[sig.CallID] = &PendingCall{
				CallID:   sig.CallID,
				From:     sig.From,
				FromName: sig.FromName,
				To:       nodeID,
				Status:   "pending",
				Created:  time.Now(),
			}
			log.Printf("[CALL INCOMING] %s is calling (call_id: %s)", sig.FromName, sig.CallID)
		}
		callsMu.Unlock()
	}

	// Handle call response (accepted/rejected)
	if (sig.Type == "accepted" || sig.Type == "rejected") && sig.CallID != "" {
		callsMu.Lock()
		if c, ok := pendingCalls[sig.CallID]; ok {
			c.Status = sig.Type
			log.Printf("[CALL %s] %s", sig.Type, sig.CallID)
		}
		callsMu.Unlock()
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
	var req struct {
		CallID string `json:"call_id"`
		SDP    string `json:"sdp"`
		To     string `json:"to"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	log.Printf("[SDP OFFER] call_id: %s", req.CallID)

	// Forward to target peer if known
	if req.To != "" {
		peersMu.RLock()
		if p, ok := peers[req.To]; ok {
			go http.Post("http://"+p.IP+":8080/sdp/receive", "application/json",
				strings.NewReader(`{"call_id":"`+req.CallID+`","sdp":"`+req.SDP+`","type":"offer"}`))
		}
		peersMu.RUnlock()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleSDPAnswer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CallID string `json:"call_id"`
		SDP    string `json:"sdp"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	log.Printf("[SDP ANSWER] call_id: %s", req.CallID)

	// Store answer for polling
	callsMu.Lock()
	if c, ok := pendingCalls[req.CallID]; ok {
		c.Status = "accepted"
	}
	callsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleICECandidate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CallID        string `json:"call_id"`
		Candidate     string `json:"candidate"`
		SDPMID        string `json:"sdpMid"`
		SDPMLineIndex int    `json:"sdpMLineIndex"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	// Forward candidate if we know the peer
	callsMu.RLock()
	if c, ok := pendingCalls[req.CallID]; ok {
		targetID := c.To
		if c.From == clusterModule.GetLocalNode().ID {
			targetID = c.To
		} else {
			targetID = c.From
		}
		callsMu.RUnlock()

		peersMu.RLock()
		if p, ok := peers[targetID]; ok {
			go http.Post("http://"+p.IP+":8080/ice/receive", "application/json",
				strings.NewReader(`{"call_id":"`+req.CallID+`","candidate":"`+req.Candidate+`"}`))
		}
		peersMu.RUnlock()
	} else {
		callsMu.RUnlock()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
