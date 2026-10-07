package main

import (
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"zerophone/bridge"
	"zerophone/cluster"
	"zerophone/core"
)

var (
	addr        = flag.String("addr", ":9443", "http listen")
	serverURL   = flag.String("server", "", "server url to connect")
	clusterPort = flag.Int("port", 9443, "cluster port")

	clusterModule *cluster.ClusterModule
	wsHub         *cluster.WSHub
	signalRouter  *core.SignalRouter
	callState     *core.CallStateMachine
	sipBridge     *bridge.SIPBridge

	nodeID  string
	localIP string
	started time.Time
)

func main() {
	flag.Parse()

	localIP, _ = detectZeroTier()

	started = time.Now()

	router := mux.NewRouter()
	handler := corsMiddleware(router)

	// Initialize state machine from core/
	callState = core.NewCallStateMachine()

	// Initialize cluster if ZEROPHONE_CLUSTER=1 or ZeroTier detected
	if localIP != "" || os.Getenv("ZEROPHONE_CLUSTER") == "1" {
		clusterModule = initCluster()
		if clusterModule != nil {
			log.Printf("[MAIN] cluster node: %s", clusterModule.GetLocalNode().ID)
		}
	}

	// Initialize WebSocket hub first
	var discovery *cluster.DiscoveryService
	if clusterModule != nil {
		discovery = clusterModule.GetDiscoveryService()
	}
	wsHub = cluster.InitWSHub(discovery, nil, nodeID)

	// Initialize signal router from core/
	signalRouter = core.NewSignalRouter(callState)
	if clusterModule != nil {
		clusterModule.AttachSignalRouter(signalRouter)
		// Connect WSHub to ClusterModule for signal routing
		clusterModule.SetWSHub(wsHub)
	} else {
		// Standalone mode: route signals directly over WebSocket transport.
		signalRouter.SetTransport(wsHub)
	}

	// Attach SignalRouter to WSHub for incoming message dispatch
	wsHub.AttachSignalRouter(signalRouter)

	// Initialize SIP bridge endpoints (for go-b2bua -> ZeroPhone integration)
	sipBridge = bridge.NewSIPBridge(signalRouter)

	// Register routes
	router.HandleFunc("/", handleIndex)
	router.HandleFunc("/call.html", handleCallHTML)
	router.HandleFunc("/debug.html", handleDebugHTML)
	router.HandleFunc("/monitor.html", handleMonitorHTML)
	router.HandleFunc("/monitor", handleMonitorHTML)
	router.HandleFunc("/partner.html", handlePartnerHTML)
	router.HandleFunc("/partner", handlePartnerHTML)
	router.HandleFunc("/ws/{user_id}", cluster.HandleWebSocket)
	router.HandleFunc("/zerophone/ws/{user_id}", cluster.HandleWebSocket)
	router.HandleFunc("/call", handleCall)
	router.HandleFunc("/call/signal", handleCallSignal)
	router.HandleFunc("/call/respond", handleCallResponse)
	router.HandleFunc("/call/end", handleCallEnd)
	router.HandleFunc("/sip/map", sipBridge.HandleMap)
	router.HandleFunc("/sip/register", sipBridge.HandleRegister)
	router.HandleFunc("/sip/unregister", sipBridge.HandleUnregister)
	router.HandleFunc("/sip/sessions", sipBridge.HandleSessions)
	router.HandleFunc("/sip/media/capabilities", sipBridge.HandleMediaCapabilities)
	router.HandleFunc("/sip/media/sessions", sipBridge.HandleMediaSessions)
	router.HandleFunc("/sip/media/workers", sipBridge.HandleMediaWorkers)
	router.HandleFunc("/sip/invite", sipBridge.HandleInvite)
	router.HandleFunc("/sip/bye", sipBridge.HandleBYE)
	router.HandleFunc("/status", handleStatus)
	router.HandleFunc("/nodes", handleNodes)
	router.HandleFunc("/presence", handlePresence)
	router.HandleFunc("/ice/servers", handleICEServers)
	router.HandleFunc("/debug", handleDebug)

	// Serve static files (CSS, JS, images, etc.)
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("./static/"))))

	if clusterModule != nil {
		clusterModule.RegisterRoutes(router)
		clusterModule.Start(nil)
	}

	log.Printf("ZeroPhone v2.0 starting on %s (cluster=%v)", *addr, clusterModule != nil)

	log.Fatal(http.ListenAndServe(*addr, handler))
}

// =======================
// CLUSTER INITIALIZATION
// =======================

func initCluster() *cluster.ClusterModule {
	mod, err := cluster.NewClusterModule(&cluster.ClusterConfig{
		Enabled:      true,
		DataDir:      getDataDir(),
		ClusterPort:  *clusterPort,
		BindZeroTier: localIP != "",
		StaticPeers:  nil,
	})
	if err != nil {
		log.Printf("[CLUSTER] init failed: %v", err)
		return nil
	}
	nodeID = mod.GetLocalNode().ID
	return mod
}

// =======================
// NETWORK HELPERS
// =======================

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
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				return ipnet.IP.String(), strings.TrimPrefix(iface.Name, "zt")
			}
		}
	}
	return "", ""
}

func getDataDir() string {
	if dir := os.Getenv("ZEROPHONE_DATA"); dir != "" {
		return dir
	}
	return "/var/lib/zerophone"
}

// =======================
// CORS MIDDLEWARE
// =======================

func corsMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH, HEAD")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// =======================
// HTTP HANDLERS
// =======================

func handleIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./static/index.html")
}

func handleCallHTML(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./static/call.html")
}

func handleDebugHTML(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./static/debug.html")
}

func handleMonitorHTML(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./static/monitor.html")
}

func handlePartnerHTML(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "./static/partner.html")
}

func handleCall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	// TODO: Implement call handling via state machine
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleCallSignal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var sig core.Signal
	if err := json.NewDecoder(r.Body).Decode(&sig); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	if signalRouter != nil {
		signalRouter.Dispatch(sig)
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "delivered"})
}

func handleCallResponse(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleCallEnd(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ended"})
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := map[string]interface{}{
		"version":    "2.0.0",
		"uptime":     time.Since(started).String(),
		"local_ip":   localIP,
		"cluster":    clusterModule != nil,
		"signal_dir": signalRouter != nil,
	}
	if clusterModule != nil {
		status["node_id"] = clusterModule.GetLocalNode().ID
	}
	json.NewEncoder(w).Encode(status)
}

func handleNodes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if clusterModule != nil {
		json.NewEncoder(w).Encode(clusterModule.GetNodeRegistry().GetAll())
	} else {
		json.NewEncoder(w).Encode([]any{})
	}
}

func handlePresence(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get users from WSHub if available
	if wsHub != nil {
		users := wsHub.GetUsers()
		json.NewEncoder(w).Encode(users)
	} else {
		json.NewEncoder(w).Encode([]map[string]string{})
	}
}

func handleICEServers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ice_servers": []map[string]string{
			{"urls": "stun:stun.l.google.com:19302"},
		},
	})
}

func handleDebug(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	debug := map[string]interface{}{}
	if clusterModule != nil {
		debug["nodes"] = clusterModule.GetNodeRegistry().GetAll()
	}
	json.NewEncoder(w).Encode(debug)
}
