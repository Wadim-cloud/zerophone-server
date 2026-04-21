package main

import (
	"bytes"
	"crypto/rand"
	"crypto/tls"
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

// CORS middleware for handling preflight and headers
func corsMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "DNT,User-Agent,X-Requested-With,If-Modified-Since,Cache-Control,Content-Type,Range,Authorization")
		w.Header().Set("Access-Control-Expose-Headers", "Content-Length,Content-Range")
		w.Header().Set("Access-Control-Max-Age", "1728000")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		h.ServeHTTP(w, r)
	})
}

// Call states - proper VoIP states
const (
	CallStateNull       = ""
	CallStateIdle       = "IDLE"
	CallStateInviting   = "INVITING"
	CallStateRinging    = "RINGING"
	CallStateConnecting = "CONNECTING"
	CallStateActive     = "ACTIVE"
	CallStateTerminated = "TERMINATED"
	CallStateAccepted   = "ACCEPTED"
	CallStateReject     = "REJECTED"
	CallStateBusy       = "BUSY"
	CallStateEnded      = "ENDED"
)

// SIP-like messages
const (
	SIPInvite  = "INVITE"
	SIPTrying  = "100 TRYING"
	SIPRinging = "180 RINGING"
	SIPOK      = "200 OK"
	SIPACK     = "ACK"
	SIPBYE     = "BYE"
	SIPBusy    = "486 BUSY"
)

// CallStateMachine manages call states
type CallStateMachine struct {
	mu          sync.RWMutex
	transitions map[string]map[string]string
}

func NewCallStateMachine() *CallStateMachine {
	csm := &CallStateMachine{
		transitions: make(map[string]map[string]string),
	}
	csm.transitions[CallStateNull] = map[string]string{
		"INVITE": CallStateInviting,
	}
	csm.transitions[CallStateIdle] = map[string]string{
		"INVITE": CallStateInviting,
	}
	csm.transitions[CallStateInviting] = map[string]string{
		"100 TRYING":  CallStateInviting,
		"180 RINGING": CallStateRinging,
		"486 BUSY":    CallStateBusy,
		"REJECT":      CallStateReject,
		"200 OK":      CallStateConnecting,
		"BYE":         CallStateTerminated,
	}
	csm.transitions[CallStateRinging] = map[string]string{
		"200 OK": CallStateConnecting,
		"REJECT": CallStateReject,
		"BYE":    CallStateTerminated,
	}
	csm.transitions[CallStateConnecting] = map[string]string{
		"CONNECT": CallStateActive,
		"REJECT":  CallStateReject,
		"BYE":     CallStateTerminated,
		"TIMEOUT": CallStateTerminated,
	}
	csm.transitions[CallStateActive] = map[string]string{
		"BYE": CallStateTerminated,
	}
	csm.transitions[CallStateTerminated] = map[string]string{
		"INVITE": CallStateInviting,
	}
	return csm
}

func (csm *CallStateMachine) canTransition(from, msg string) bool {
	csm.mu.RLock()
	defer csm.mu.RUnlock()
	if transitions, ok := csm.transitions[from]; ok {
		_, allowed := transitions[msg]
		return allowed
	}
	return false
}

func (csm *CallStateMachine) nextState(from, msg string) string {
	csm.mu.RLock()
	defer csm.mu.RUnlock()
	if transitions, ok := csm.transitions[from]; ok {
		if next, allowed := transitions[msg]; allowed {
			return next
		}
	}
	return from
}

// SDP with codec negotiation
type SDPExchange struct {
	Codecs     []string `json:"codecs"`
	Bitrate    int      `json:"bitrate"`
	SampleRate int      `json:"sample_rate"`
	Channels   int      `json:"channels"`
	ptime      int      `json:"ptime"`
	MaxPtime   int      `json:"max_ptime"`
}

var supportedCodecs = []string{"opus", "PCMU", "PCMA"}

func negotiateCodecs(local, remote []string) string {
	for _, rc := range remote {
		for _, lc := range local {
			if strings.EqualFold(rc, lc) {
				return strings.ToLower(rc)
			}
		}
	}
	return "opus"
}

// ICE gathering timeout (10 seconds)
const ICEGatheringTimeout = 10 * time.Second

// TURN fallback
var turnServers = []string{
	"turn:turn.l.google.com:3478",
	"turn:turn1.l.google.com:3478",
	"turn:turn2.l.google.com:3478",
}

// ICEConfig for ICE servers
type ICEConfig struct {
	URLs       string `json:"urls"`
	Username   string `json:"username,omitempty"`
	Credential string `json:"credential,omitempty"`
}

func getICEServers() []ICEConfig {
	return []ICEConfig{
		{URLs: "stun:stun.l.google.com:19302"},
		{URLs: "stun:stun1.l.google.com:19302"},
		{URLs: "stun:stun2.l.google.com:19302"},
		{URLs: "turn:turn.l.google.com:3478", Username: "guest", Credential: "somerealm"},
		{URLs: "turn:turn1.l.google.com:3478", Username: "guest", Credential: "somerealm"},
		{URLs: "turn:turn2.l.google.com:3478", Username: "guest", Credential: "somerealm"},
	}
}

var (
	addr       = flag.String("addr", ":8080", "http listen address")
	serverURL  = flag.String("server", "", "server URL to connect to")
	configPort = flag.Int("port", 8081, "cluster port")
	certFile   = flag.String("cert", "", "TLS certificate file")
	keyFile    = flag.String("key", "", "TLS key file")

	clusterModule    *cluster.ClusterModule
	wsHub            *cluster.WSHub
	peers            = make(map[string]*Peer)
	peersMu          sync.RWMutex
	started          time.Time
	nodeName         string
	networkID        string
	localIP          string
	serverAddr       string
	isClient         bool
	pendingCalls     = make(map[string]*PendingCall)
	callsMu          sync.RWMutex
	callStateMachine *CallStateMachine

	useTLS bool
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

func scheme() string {
	if useTLS {
		return "https://"
	}
	return "http://"
}

func getHTTPClient() *http.Client {
	if useTLS {
		return &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}
	}
	return &http.Client{Timeout: 5 * time.Second}
}

func main() {
	flag.Parse()

	callStateMachine = NewCallStateMachine()

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

	// Wrap with CORS middleware
	handler := corsMiddleware(router)

	wsHub = cluster.InitWSHub()

	router.HandleFunc("/", handleIndex)
	router.HandleFunc("/ws/{user_id}", cluster.HandleWebSocket)
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
	router.HandleFunc("/sdp/receive", handleSDPReceive)
	router.HandleFunc("/ice/candidate", handleICECandidate)
	router.HandleFunc("/ice/receive", handleICEReceive)
	router.HandleFunc("/ice/servers", handleICEServers)
	router.HandleFunc("/debug", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "static/debug.html")
	})
	router.HandleFunc("/stats", handleStats)
	router.HandleFunc("/secure", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"secure":   useTLS,
			"tls":      useTLS,
			"client":   isClient,
			"tls_cert": *certFile != "",
			"tls_key":  *keyFile != "",
		})
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

	// Use flag values only - let nginx handle SSL
	certPath := *certFile
	keyPath := *keyFile
	useTLS = (certPath != "" && keyPath != "")

	log.Printf("ZeroPhone v1.0 starting on %s", bindAddr)

	// Start server
	if useTLS {
		server := &http.Server{
			Addr:    bindAddr,
			Handler: handler,
			TLSConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		}
		log.Printf("HTTPS enabled with %s", certPath)
		log.Fatal(server.ListenAndServeTLS(certPath, keyPath))
	} else {
		log.Printf("HTTP mode (no certificates found)")
		log.Fatal(http.ListenAndServe(bindAddr, handler))
	}
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
		resp, err := getHTTPClient().Post(scheme()+serverAddr+"/register", "application/json", strings.NewReader(string(data)))
		if err != nil {
			log.Printf("Register failed: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}
		resp.Body.Close()

		log.Printf("Registered with server")

		// Heartbeat loop
		for {
			getHTTPClient().Get(scheme() + serverAddr + "/heartbeat?node_id=" + req.ID)
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
						req, _ := http.NewRequest("GET", scheme()+addr+"/ping?node_id="+nodeName+"&ip="+localIP+"&network="+networkID, nil)
						getHTTPClient().Do(req)
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
						req, _ := http.NewRequest("GET", scheme()+ip+":8080/presence", nil)
						resp, err := getHTTPClient().Do(req)
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
					client := getHTTPClient()
					// Notify peer of our name
					data := map[string]interface{}{
						"node_id": req.ID,
						"name":    req.Name,
						"ip":      localIP,
						"network": req.NetworkID,
					}
					payload, _ := json.Marshal(data)
					client.Post(scheme()+target+":8080/presence", "application/json", bytes.NewReader(payload))
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
					getHTTPClient().Post(scheme()+targetIP+":8080/call/signal", "application/json", strings.NewReader(string(data)))
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
						getHTTPClient().Post(scheme()+callerIP+":8080/call/signal", "application/json", strings.NewReader(string(data)))
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

	log.Printf("[SDP] OFFER from %s | call_id: %s | sdp_len: %d", req.To, req.CallID, len(req.SDP))

	var sdp struct {
		Type string `json:"type"`
		PT   []struct {
			MimeType  string `json:"mimeType"`
			ClockRate int    `json:"clockRate"`
		} `json:"media"`
	}
	json.Unmarshal([]byte(req.SDP), &sdp)
	for _, m := range sdp.PT {
		log.Printf("[CODEC] %s @ %dHz", m.MimeType, m.ClockRate)
	}

	if req.To != "" {
		targetIP := ""
		peersMu.RLock()
		if p, ok := peers[req.To]; ok {
			targetIP = p.IP
		}
		peersMu.RUnlock()

		if targetIP != "" {
			log.Printf("[SEND] Forwarding offer to %s (%s)", req.To, targetIP)
			go getHTTPClient().Post(scheme()+targetIP+":8080/sdp/receive", "application/json",
				strings.NewReader(`{"call_id":"`+req.CallID+`","sdp":"`+req.SDP+`","type":"offer"}`))
		}
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
		CallID    string `json:"call_id"`
		Candidate string `json:"candidate"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	log.Printf("[ICE]Candidate for %s: %s", req.CallID, req.Candidate[:min(50, len(req.Candidate))])

	callsMu.RLock()
	var targetID string
	if c, ok := pendingCalls[req.CallID]; ok {
		if c.From != "" {
			if clusterModule != nil && clusterModule.GetLocalNode().ID == c.From {
				targetID = c.To
			} else {
				targetID = c.From
			}
		}
	}
	callsMu.RUnlock()

	if targetID != "" {
		targetIP := ""
		peersMu.RLock()
		if p, ok := peers[targetID]; ok {
			targetIP = p.IP
		}
		peersMu.RUnlock()

		if targetIP != "" {
			go getHTTPClient().Post(scheme()+targetIP+":8080/ice/receive", "application/json",
				strings.NewReader(`{"call_id":"`+req.CallID+`","candidate":"`+req.Candidate+`"}`))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleSDPReceive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CallID string `json:"call_id"`
		SDP    string `json:"sdp"`
		Type   string `json:"type"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	log.Printf("[SDP RECEIVE] type: %s, call_id: %s", req.Type, req.CallID)

	callsMu.Lock()
	if c, ok := pendingCalls[req.CallID]; ok {
		log.Printf("[SDP] Stored for call: %s, from: %s, to: %s", req.CallID, c.From, c.To)
	}
	callsMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleICEReceive(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CallID    string `json:"call_id"`
		Candidate string `json:"candidate"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	log.Printf("[ICE RECEIVE] candidate for call: %s", req.CallID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleICEServers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	servers := getICEServers()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ice_servers":  servers,
		"timeout_secs": 10,
		"codecs":       supportedCodecs,
	})
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	callsMu.RLock()
	activeCalls := len(pendingCalls)
	callsMu.RUnlock()

	peersMu.RLock()
	peerCount := len(peers)
	peersMu.RUnlock()

	stats := map[string]interface{}{
		"active_calls": activeCalls,
		"peer_count":   peerCount,
		"local_ip":     localIP,
		"network":      networkID,
		"uptime":       time.Since(started).String(),
	}

	json.NewEncoder(w).Encode(stats)
}
