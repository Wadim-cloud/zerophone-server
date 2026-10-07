package cluster

import (
	"encoding/json"
	"log"
	"math"
	"strings"
	"sync"
	"time"
)

//
// =======================
// 🌐 NETWORK TYPES
// =======================
//

type NetworkType string

const (
	NetworkZeroTier NetworkType = "zerotier"
	NetworkPublic   NetworkType = "public"
	NetworkUnknown  NetworkType = "unknown"
)

//
// =======================
// 🧭 NODE ROUTING STATE
// =======================
//

type RouteNode struct {
	NodeID string

	ZeroTierIP string
	PublicIP   string
	Addr       string

	Network NetworkType

	// 🧠 dynamic metrics
	LatencyMs   float64
	SuccessRate float64

	LastSeen  time.Time
	FailCount int
	Healthy   bool

	// 🧠 load awareness
	ActiveCalls int
	Capacity    int
	LoadFactor  float64

	// 🌍 NAT/TURN awareness
	HasPublicNAT bool
	STUNCapable  bool
	TURNCapable  bool
	TURNAddr     string
}

//
// =======================
// 📡 GOSSIP MESSAGE MODEL
// =======================

type RouteGossip struct {
	FromNode string                 `json:"from_node"`
	Routes   map[string]GossipRoute `json:"routes"`
}

type GossipRoute struct {
	LatencyMs   float64 `json:"latency_ms"`
	SuccessRate float64 `json:"success_rate"`
	FailCount   int     `json:"fail_count"`
	ActiveCalls int     `json:"active_calls"`
}

//
// =======================
// 🎯 ICE STRATEGY MODEL
// =======================

type ICEStrategy struct {
	PreferredNetwork NetworkType `json:"preferred_network"`
	UseRelay         bool        `json:"use_relay"`
	RelayNode        string      `json:"relay_node"`
	PriorityBoost    int         `json:"priority_boost"`
}

//
// =======================
// 🧠 SIGNAL RESOLVER V3
// =======================
//

type SignalResolver struct {
	mu sync.RWMutex

	nodes map[string]*RouteNode

	registry   *NodeRegistry
	resilience *FailureHandler

	localNodeID string

	// 🧠 graph edges: from → to → cost
	edges map[string]map[string]float64

	// hooks
	OnRouteChosen func(from, to, via string)
}

//
// =======================
// INIT
// =======================
//

func NewSignalResolver(reg *NodeRegistry, res *FailureHandler, localID string) *SignalResolver {
	return &SignalResolver{
		nodes:       make(map[string]*RouteNode),
		registry:    reg,
		resilience:  res,
		localNodeID: localID,
		edges:       make(map[string]map[string]float64),
	}
}

//
// =======================
// 📥 NODE SYNC (NOW SMART)
// =======================
//

func (r *SignalResolver) UpsertFromRegistry(node *Node) {
	r.mu.Lock()
	defer r.mu.Unlock()

	n, ok := r.nodes[node.ID]
	if !ok {
		n = &RouteNode{
			NodeID:      node.ID,
			LatencyMs:   50,
			SuccessRate: 1.0,
			Healthy:     true,
		}
		r.nodes[node.ID] = n
	}

	n.ZeroTierIP = node.ZeroTierIP
	n.PublicIP = node.PublicIP
	n.LastSeen = time.Now()

	// 🧠 classify network
	n.Network = r.detectNetwork(node)

	// 🧠 detect NAT (private IP ranges)
	if strings.HasPrefix(node.PublicIP, "192.") ||
		strings.HasPrefix(node.PublicIP, "10.") ||
		strings.HasPrefix(node.PublicIP, "172.") {
		n.HasPublicNAT = true
	}

	// 🌍 ZeroTier implies STUN capable
	if node.ZeroTierIP != "" {
		n.STUNCapable = true
	}

	// 🧭 choose preferred address
	n.Addr = r.selectBestAddr(n)
}

//
// =======================
// 🌐 NETWORK DETECTION
// =======================
//

func (r *SignalResolver) detectNetwork(node *Node) NetworkType {
	if node.ZeroTierIP != "" {
		return NetworkZeroTier
	}

	if node.PublicIP != "" {
		return NetworkPublic
	}

	return NetworkUnknown
}

func (r *SignalResolver) selectBestAddr(n *RouteNode) string {
	switch n.Network {
	case NetworkZeroTier:
		return n.ZeroTierIP
	case NetworkPublic:
		return n.PublicIP
	default:
		return ""
	}
}

//
// =======================
// ⚡ RESOLVE ENGINE
// =======================
//

func (r *SignalResolver) Resolve(peerID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	target, ok := r.nodes[peerID]
	if !ok {
		return r.registryFallback(peerID)
	}

	if r.isHealthy(target) {
		return target.Addr, true
	}

	// 🧭 Dijkstra shortest path algorithm lookup across cluster edges
	if path, cost := r.shortestPath(r.localNodeID, peerID); len(path) > 1 && cost < math.MaxFloat64 {
		nextHopID := path[1]
		if nextHopNode, ok := r.nodes[nextHopID]; ok && r.isHealthy(nextHopNode) {
			log.Printf("[DIJKSTRA RESOLVER] shortest path %s → %s via %s (cost: %.2f)",
				r.localNodeID, peerID, nextHopID, cost)
			if r.OnRouteChosen != nil {
				go r.OnRouteChosen(r.localNodeID, peerID, nextHopID)
			}
			return nextHopNode.Addr, true
		}
	}

	// 🔁 fallback to relay
	relay := r.findBestRelay(target)
	if relay != nil {
		log.Printf("[RESOLVER] relay %s → %s via %s",
			r.localNodeID, peerID, relay.NodeID)

		if r.OnRouteChosen != nil {
			go r.OnRouteChosen(r.localNodeID, peerID, relay.NodeID)
		}

		return relay.Addr, true
	}

	return "", false
}

//
// =======================
// 🌐 REGISTRY FALLBACK
// =======================
//

func (r *SignalResolver) registryFallback(peerID string) (string, bool) {
	if r.registry == nil {
		return "", false
	}

	node, ok := r.registry.Get(peerID)
	if !ok {
		return "", false
	}

	if node.ZeroTierIP != "" {
		return node.ZeroTierIP, true
	}

	if node.PublicIP != "" {
		return node.PublicIP, true
	}

	return "", false
}

//
// =======================
// 🌉 RELAY SELECTION (SMART)
// =======================
//

func (r *SignalResolver) findBestRelay(target *RouteNode) *RouteNode {
	var best *RouteNode
	bestScore := math.MaxFloat64

	for _, n := range r.nodes {

		if n.NodeID == target.NodeID {
			continue
		}

		if !r.isHealthy(n) {
			continue
		}

		// 🧠 network compatibility bonus
		networkPenalty := r.networkPenalty(target, n)

		// 🧠 scoring: latency * reliability * fail_risk + load_penalty
		score := (n.LatencyMs *
			(1.0 / (n.SuccessRate + 0.01)) *
			float64(1+n.FailCount)) + (n.LoadFactor * 100) + networkPenalty

		if score < bestScore {
			bestScore = score
			best = n
		}
	}

	return best
}

//
// =======================
// 🌐 NETWORK PENALTY
// =======================
//

func (r *SignalResolver) networkPenalty(a, b *RouteNode) float64 {
	// prefer same network
	if a.Network == b.Network {
		return 0
	}

	// prefer zerotier mesh routes
	if a.Network == NetworkZeroTier || b.Network == NetworkZeroTier {
		return 10
	}

	// public NAT paths are worst
	return 50
}

//
// =======================
// ❤️ HEALTH MODEL
// =======================
//

func (r *SignalResolver) isHealthy(n *RouteNode) bool {
	if r.resilience != nil && !r.resilience.IsNodeHealthy(n.NodeID) {
		return false
	}

	if n.FailCount > 3 {
		return false
	}

	if time.Since(n.LastSeen) > 90*time.Second {
		return false
	}

	return n.Healthy
}

//
// =======================
// 📊 FEEDBACK LOOPS
// =======================
//

func (r *SignalResolver) MarkSuccess(nodeID string, latency float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if n, ok := r.nodes[nodeID]; ok {
		n.SuccessRate = (n.SuccessRate*0.8 + 0.2)
		n.LatencyMs = (n.LatencyMs*0.8 + 0.2*latency)
		n.FailCount = 0
		n.Healthy = true
	}
}

func (r *SignalResolver) MarkFailure(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if n, ok := r.nodes[nodeID]; ok {
		n.FailCount++
		n.SuccessRate *= 0.9

		if n.FailCount > 3 {
			n.Healthy = false
		}
	}
}

//
// =======================
// 📞 LOAD AWARENESS
// =======================

func (r *SignalResolver) updateLoad(n *RouteNode) {
	if n.Capacity == 0 {
		n.Capacity = 100
	}
	n.LoadFactor = float64(n.ActiveCalls) / float64(n.Capacity)
}

func (r *SignalResolver) MarkCallStart(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if n, ok := r.nodes[nodeID]; ok {
		n.ActiveCalls++
		r.updateLoad(n)
	}
}

func (r *SignalResolver) MarkCallEnd(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if n, ok := r.nodes[nodeID]; ok {
		if n.ActiveCalls > 0 {
			n.ActiveCalls--
		}
		r.updateLoad(n)
	}
}

//
// =======================
// 📡 GOSSIP PROTOCOL
// =======================

func (r *SignalResolver) BuildGossip(localNode string) []byte {
	r.mu.RLock()
	defer r.mu.RUnlock()

	routes := make(map[string]GossipRoute)

	for id, n := range r.nodes {
		routes[id] = GossipRoute{
			LatencyMs:   n.LatencyMs,
			SuccessRate: n.SuccessRate,
			FailCount:   n.FailCount,
			ActiveCalls: n.ActiveCalls,
		}
	}

	msg := RouteGossip{
		FromNode: localNode,
		Routes:   routes,
	}

	data, _ := json.Marshal(msg)
	return data
}

func (r *SignalResolver) ApplyGossip(data []byte) {
	var g RouteGossip
	if err := json.Unmarshal(data, &g); err != nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for nodeID, gr := range g.Routes {
		n, ok := r.nodes[nodeID]
		if !ok {
			continue
		}

		// 🧠 blend external knowledge (not overwrite!)
		n.LatencyMs = (n.LatencyMs*0.7 + gr.LatencyMs*0.3)
		n.SuccessRate = (n.SuccessRate*0.7 + gr.SuccessRate*0.3)

		// blend active calls
		n.ActiveCalls = int(float64(n.ActiveCalls)*0.7 + float64(gr.ActiveCalls)*0.3)
		r.updateLoad(n)

		// failures propagate cautiously (never go down)
		if gr.FailCount > n.FailCount {
			n.FailCount = gr.FailCount
		}
	}

	log.Printf("[GOSSIP] updated from %s", g.FromNode)
}

//
// =======================
// 🎯 ICE STRATEGY DECIDER
// =======================

func (r *SignalResolver) DecideICEStrategy(from, to string) ICEStrategy {
	r.mu.RLock()
	defer r.mu.RUnlock()

	src, ok1 := r.nodes[from]
	dst, ok2 := r.nodes[to]

	strategy := ICEStrategy{
		PreferredNetwork: NetworkUnknown,
		UseRelay:         false,
		PriorityBoost:    0,
	}

	if !ok1 || !ok2 {
		return strategy
	}

	// 🌐 same network → direct boost
	if src.Network == dst.Network {
		strategy.PreferredNetwork = src.Network
		strategy.PriorityBoost = 100
		return strategy
	}

	// 🌍 ZeroTier preferred mesh
	if src.Network == NetworkZeroTier || dst.Network == NetworkZeroTier {
		strategy.PreferredNetwork = NetworkZeroTier
		strategy.PriorityBoost = 80
		return strategy
	}

	// 🚧 NAT fallback → use relay
	relay := r.findBestRelay(dst)
	if relay != nil {
		strategy.UseRelay = true
		strategy.RelayNode = relay.NodeID
		strategy.PriorityBoost = 50
	}

	// 🌍 TURN fallback for NAT
	if r.needsRelay(src, dst) {
		relay := r.findBestRelay(dst)
		if relay != nil && relay.TURNCapable {
			strategy.UseRelay = true
			strategy.RelayNode = relay.NodeID
			strategy.PreferredNetwork = NetworkPublic
			strategy.PriorityBoost = 30
			log.Printf("[ICE] TURN fallback via %s", relay.NodeID)
		}
	}

	return strategy
}

//
// =======================
// 🔗 GRAPH-BASED ROUTING
// =======================

func (r *SignalResolver) UpdateLink(a, b string, latency float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.edges[a] == nil {
		r.edges[a] = make(map[string]float64)
	}
	if r.edges[b] == nil {
		r.edges[b] = make(map[string]float64)
	}

	r.edges[a][b] = latency
	r.edges[b][a] = latency
}

func (r *SignalResolver) shortestPath(from, to string) ([]string, float64) {
	dist := map[string]float64{}
	prev := map[string]string{}
	visited := map[string]bool{}

	for node := range r.nodes {
		dist[node] = math.MaxFloat64
	}
	dist[from] = 0

	for {
		var current string
		minDist := math.MaxFloat64

		for n, d := range dist {
			if !visited[n] && d < minDist {
				minDist = d
				current = n
			}
		}

		if current == "" || current == to {
			break
		}

		visited[current] = true

		for neighbor, cost := range r.edges[current] {
			if visited[neighbor] {
				continue
			}
			alt := dist[current] + cost
			if alt < dist[neighbor] {
				dist[neighbor] = alt
				prev[neighbor] = current
			}
		}
	}

	path := []string{}
	cur := to

	for cur != "" {
		path = append([]string{cur}, path...)
		cur = prev[cur]
	}

	return path, dist[to]
}

//
// =======================
// 🌍 NAT AWARENESS
// =======================

func (r *SignalResolver) needsRelay(a, b *RouteNode) bool {
	if a.HasPublicNAT && b.HasPublicNAT {
		return true
	}
	if a.Network != b.Network {
		return true
	}
	return false
}

//
// =======================
// 🔍 DEBUG
// =======================
//

func (r *SignalResolver) DumpRoutes() map[string]RouteNode {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make(map[string]RouteNode)
	for id, n := range r.nodes {
		out[id] = *n
	}
	return out
}
