package cluster

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"

	"zerophone/core"
)

//
// ─────────────────────────────────────────────
// CONFIG
// ─────────────────────────────────────────────
//

type ClusterConfig struct {
	Enabled      bool
	DataDir      string
	ClusterPort  int
	BindZeroTier bool
	StaticPeers  []string
}

//
// ─────────────────────────────────────────────
// NODE MODEL
// ─────────────────────────────────────────────
//

type Node struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	ZeroTierIP   string   `json:"zerotier_ip"`
	PublicIP     string   `json:"public_ip"`
	Port         int      `json:"port"`
	Status       string   `json:"status"`
	LastSeen     int64    `json:"last_seen"`
	NetworkID    string   `json:"network_id"`
	Capabilities []string `json:"capabilities"`
	ServerID     string   `json:"server_id"`
}

//
// ─────────────────────────────────────────────
// REGISTRY
// ─────────────────────────────────────────────
//

type NodeRegistry struct {
	mu    sync.RWMutex
	nodes map[string]*Node
	peers map[string]string
}

func NewNodeRegistry() *NodeRegistry {
	return &NodeRegistry{
		nodes: make(map[string]*Node),
		peers: make(map[string]string),
	}
}

func (r *NodeRegistry) Add(node *Node) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[node.ID] = node
}

func (r *NodeRegistry) Get(id string) (*Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.nodes[id]
	return n, ok
}

func (r *NodeRegistry) GetAll() []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		out = append(out, n)
	}
	return out
}

//
// ─────────────────────────────────────────────
// MODULE CORE
// ─────────────────────────────────────────────
//

type ClusterModule struct {
	Config   *ClusterConfig
	manager  *ClusterManager
	node     *Node
	registry *NodeRegistry

	wsHub *WSHub // WebSocket transport

	mu sync.Mutex

	// 🧠 NEW: signal brain injection
	signalRouter *core.SignalRouter
}

func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func NewClusterModule(config *ClusterConfig) (*ClusterModule, error) {
	m := &ClusterModule{
		Config:   config,
		registry: NewNodeRegistry(),
	}

	if !config.Enabled {
		return m, nil
	}

	nodeID := generateUUID()

	m.node = &Node{
		ID:         nodeID,
		Name:       "zerophone-" + nodeID[:8],
		ZeroTierIP: getZeroTierIP(config),
		PublicIP:   getPublicIP(),
		Port:       config.ClusterPort,
		Status:     "online",
		LastSeen:   time.Now().Unix(),
	}

	m.registry.Add(m.node)

	log.Println("[CLUSTER] booting node", m.node.ID)

	m.manager = NewClusterManager(config, m.node, m.registry)

	m.manager.signalResolver = NewSignalResolver(m.registry, nil, m.node.ID)

	return m, nil
}

//
// ─────────────────────────────────────────────
// SIGNAL ROUTER ATTACHMENT
// ─────────────────────────────────────────────
//

func (m *ClusterModule) AttachSignalRouter(sr *core.SignalRouter) {
	m.signalRouter = sr

	// 🔌 wire resolver
	sr.SetResolver(m)

	// 🔌 wire transport
	sr.SetTransport(m)
}

func (m *ClusterModule) SetWSHub(ws *WSHub) {
	m.wsHub = ws
}

//
// ─────────────────────────────────────────────
// SIGNAL RESOLVER IMPLEMENTATION
// ─────────────────────────────────────────────

func (m *ClusterModule) Resolve(peerID string) (string, bool) {
	if m.manager != nil && m.manager.signalResolver != nil {
		return m.manager.signalResolver.Resolve(peerID)
	}

	n, ok := m.registry.Get(peerID)
	if !ok {
		return "", false
	}

	if n.ZeroTierIP != "" {
		return n.ZeroTierIP, true
	}

	return n.PublicIP, true
}

func (m *ClusterModule) DecideICEStrategy(from, to string) core.ICEStrategyData {
	if m.manager == nil || m.manager.signalResolver == nil {
		return core.ICEStrategyData{}
	}

	strategy := m.manager.signalResolver.DecideICEStrategy(from, to)
	return core.ICEStrategyData{
		PreferredNetwork: string(strategy.PreferredNetwork),
		UseRelay:         strategy.UseRelay,
		RelayNode:        strategy.RelayNode,
		PriorityBoost:    strategy.PriorityBoost,
	}
}

//
// ─────────────────────────────────────────────
// CALL LOAD TRACKER IMPLEMENTATION
// ─────────────────────────────────────────────

func (m *ClusterModule) MarkCallStart(nodeID string) {
	if m.manager != nil && m.manager.signalResolver != nil {
		m.manager.signalResolver.MarkCallStart(nodeID)
	}
}

func (m *ClusterModule) MarkCallEnd(nodeID string) {
	if m.manager != nil && m.manager.signalResolver != nil {
		m.manager.signalResolver.MarkCallEnd(nodeID)
	}
}

//
// ─────────────────────────────────────────────
// SIGNAL TRANSPORT IMPLEMENTATION
// ─────────────────────────────────────────────
//

func (m *ClusterModule) SendTo(id string, data []byte) bool {
	log.Printf("[CLUSTER] SendTo called for %s", id)

	// cluster does NOT interpret signals anymore
	// only delivers bytes

	// try WebSocket first (local clients)
	if m.wsHub != nil {
		log.Printf("[CLUSTER] Trying WebSocket for %s", id)
		if m.wsHub.SendTo(id, data) {
			log.Printf("[CLUSTER] WebSocket delivery successful for %s", id)
			return true
		}
		log.Printf("[CLUSTER] WebSocket delivery failed for %s", id)
	} else {
		log.Printf("[CLUSTER] No WSHub attached")
	}

	if m.manager == nil {
		log.Printf("[CLUSTER] No manager, failing")
		return false
	}

	// try ZMQ route (remote nodes)
	if m.manager.zmqRouter != nil {
		log.Printf("[CLUSTER] Trying ZMQ for %s", id)
		err := m.manager.zmqRouter.SendMessage(id, &ZMQMessage{
			Type:     "SIGNAL",
			FromNode: m.node.ID,
			ToNode:   id,
			Payload: map[string]interface{}{
				"data": string(data),
			},
		})

		if err == nil {
			log.Printf("[CLUSTER] ZMQ delivery successful for %s", id)
			return true
		}
		log.Printf("[CLUSTER] ZMQ delivery failed: %v", err)
	}

	log.Printf("[CLUSTER] All delivery methods failed for %s", id)
	return false
}

//
// ─────────────────────────────────────────────
// PUBLIC API
// ─────────────────────────────────────────────
//

func (m *ClusterModule) GetLocalNode() *Node {
	return m.node
}

func (m *ClusterModule) GetNodeRegistry() *NodeRegistry {
	return m.registry
}

func (m *ClusterModule) GetDiscoveryService() *DiscoveryService {
	if m.manager != nil {
		return m.manager.discovery
	}
	return nil
}

func (m *ClusterModule) Start(staticPeers []string) error {
	if m.manager == nil {
		return nil
	}
	return m.manager.Start(staticPeers)
}

//
// ─────────────────────────────────────────────
// NETWORK HELPERS (DETERMINISTIC)
// ─────────────────────────────────────────────

func getZeroTierIP(config *ClusterConfig) string {
	if !config.BindZeroTier {
		return ""
	}

	if zt := os.Getenv("ZEROTIER_IP"); zt != "" {
		return zt
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range ifaces {
		if !strings.HasPrefix(iface.Name, "zt") {
			continue
		}

		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				return ipnet.IP.String()
			}
		}
	}

	return ""
}

func getPublicIP() string {
	if ip := os.Getenv("PUBLIC_IP"); ip != "" {
		return ip
	}

	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				ip := ipnet.IP.String()
				if !strings.HasPrefix(ip, "10.") &&
					!strings.HasPrefix(ip, "192.168.") &&
					!strings.HasPrefix(ip, "172.") &&
					ip != "127.0.0.1" {
					return ip
				}
			}
		}
	}

	return ""
}

//

func (m *ClusterModule) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/cluster/status", m.handleStatus)
	router.HandleFunc("/cluster/nodes", m.handleNodes)
	router.HandleFunc("/cluster/peers", m.handlePeers)
}

func (m *ClusterModule) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	out := map[string]interface{}{
		"enabled": m.Config.Enabled,
	}

	if m.node != nil {
		out["node_id"] = m.node.ID
	}

	_ = json.NewEncoder(w).Encode(out)
}

func (m *ClusterModule) handleNodes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(m.registry.GetAll())
}

func (m *ClusterModule) handlePeers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(m.Config.StaticPeers)
}

//
// ─────────────────────────────────────────────
// SIGNAL FORWARDING (NOW DEPRECATED LOGICALLY)
// ─────────────────────────────────────────────
//

func (m *ClusterModule) ForwardSignal(toNodeID string, data []byte) error {
	if m.signalRouter == nil {
		return fmt.Errorf("signal router not attached")
	}

	// 🚨 IMPORTANT:
	// cluster no longer decides anything
	// just forwards raw signal into brain

	var s core.Signal
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	m.signalRouter.Dispatch(s)
	return nil
}
