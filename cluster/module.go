package cluster

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

type ClusterConfig struct {
	Enabled      bool
	DataDir      string
	ClusterPort  int
	BindZeroTier bool
	StaticPeers  []string
}

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
	nodes := make([]*Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		nodes = append(nodes, n)
	}
	return nodes
}

func (r *NodeRegistry) GetOnline() []*Node {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nodes := make([]*Node, 0)
	now := time.Now().Unix()
	for _, n := range r.nodes {
		if now-n.LastSeen < 60 {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

func (r *NodeRegistry) AddPeer(id, addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers[id] = addr
}

type ClusterModule struct {
	Config   *ClusterConfig
	manager  *ClusterManager
	node     *Node
	registry *NodeRegistry
	mu       sync.Mutex
}

func generateUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func NewClusterModule(config *ClusterConfig) (*ClusterModule, error) {
	cm := &ClusterModule{
		Config:   config,
		registry: NewNodeRegistry(),
	}

	if config.Enabled {
		nodeID := generateUUID()
		cm.node = &Node{
			ID:         nodeID,
			Name:       "zerophone-" + nodeID[:8],
			ZeroTierIP: getZeroTierIP(config),
			PublicIP:   getPublicIP(),
			Port:       config.ClusterPort,
			Status:     "online",
			LastSeen:   time.Now().Unix(),
		}
		cm.registry.Add(cm.node)

		log.Println("Initializing cluster module...")
		log.Printf("Node ID: %s", cm.node.ID)
		log.Printf("ZeroTier IP: %s", cm.node.ZeroTierIP)

		cm.manager = NewClusterManager(config, cm.node, cm.registry)
	}

	return cm, nil
}

func getZeroTierIP(config *ClusterConfig) string {
	if config.BindZeroTier {
		if ztIP := os.Getenv("ZEROTIER_IP"); ztIP != "" {
			return ztIP
		}
		conn, err := net.Dial("udp", "1.1.1.1:53")
		if err == nil {
			defer conn.Close()
			if localAddr := conn.LocalAddr().(*net.UDPAddr); localAddr != nil {
				return localAddr.IP.String()
			}
		}
	}
	return ""
}

func getPublicIP() string {
	if pubIP := os.Getenv("PUBLIC_IP"); pubIP != "" {
		return pubIP
	}
	resp, err := http.Get("https://api.ipify.org")
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}
	return ""
}

func (cm *ClusterModule) GetLocalNode() *Node {
	return cm.node
}

func (cm *ClusterModule) GetNodeRegistry() *NodeRegistry {
	return cm.registry
}

func (cm *ClusterModule) Start(staticPeers []string) error {
	if cm.manager != nil {
		return cm.manager.Start(staticPeers)
	}
	return nil
}

func (cm *ClusterModule) RegisterRoutes(router *mux.Router) {
	router.HandleFunc("/cluster/status", cm.handleStatus)
	router.HandleFunc("/cluster/nodes", cm.handleNodes)
	router.HandleFunc("/cluster/peers", cm.handlePeers)
}

func (cm *ClusterModule) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	nodeID := ""
	if cm.node != nil {
		nodeID = cm.node.ID
	}

	status := map[string]interface{}{
		"enabled": cm.Config != nil && cm.Config.Enabled,
		"node_id": nodeID,
	}
	if cm.manager != nil {
		for k, v := range cm.manager.GetManagerStatus() {
			status[k] = v
		}
	}
	json.NewEncoder(w).Encode(status)
}

func (cm *ClusterModule) handleNodes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cm.registry.GetAll())
}

func (cm *ClusterModule) handlePeers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	peers := cm.Config.StaticPeers
	valid := []string{}
	if peers != nil {
		for _, p := range peers {
			if p != "" {
				valid = append(valid, p)
			}
		}
	}
	json.NewEncoder(w).Encode(valid)
}

func (cm *ClusterModule) BroadcastNodeUpdate(data []byte) {
	log.Printf("Broadcasting node update: %s", string(data))
}

func (cm *ClusterModule) ForwardSignal(toNodeID string, data []byte) error {
	return fmt.Errorf("not implemented")
}
