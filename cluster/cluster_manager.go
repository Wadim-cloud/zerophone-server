package cluster

import (
	"log"
	"sync"
	"time"

	"zerophone/core"
)

type ClusterManager struct {
	config    *ClusterConfig
	localNode *Node
	registry  *NodeRegistry

	discovery      *DiscoveryService
	signalResolver *SignalResolver
	resilience     *FailureHandler

	zmqRouter    *ZMQNodeConnection
	zmqDiscovery *ZMQNodeConnection

	wsHub *WSHub

	// 🧠 NEW: brain injection
	signalRouter *core.SignalRouter

	startTime time.Time
	stopCh    chan struct{}
	wg        sync.WaitGroup

	mu      sync.Mutex
	running bool
}

//
// =======================
// INIT
// =======================

func NewClusterManager(config *ClusterConfig, node *Node, registry *NodeRegistry) *ClusterManager {
	return &ClusterManager{
		config:    config,
		localNode: node,
		registry:  registry,
	}
}

//
// =======================
// 🚀 START
// =======================

func (cm *ClusterManager) Start(staticPeers []string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.running {
		return nil
	}

	cm.stopCh = make(chan struct{})
	cm.startTime = time.Now()
	cm.running = true

	if cm.discovery != nil {
		cm.discovery.SetSignalResolver(cm.signalResolver)
		_ = cm.discovery.Start()
	}

	go cm.gossipLoop()

	log.Println("[CLUSTER] started")
	return nil
}

//
// =======================
// 📡 GOSSIP LOOP
// =======================

func (cm *ClusterManager) gossipLoop() {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-cm.stopCh:
			return

		case <-t.C:
			if cm.signalResolver == nil || cm.discovery == nil || cm.localNode == nil {
				continue
			}

			data := cm.signalResolver.BuildGossip(cm.localNode.ID)

			_ = cm.discovery.BroadcastAnnouncement("ROUTE_GOSSIP", map[string]interface{}{
				"data": string(data),
			})
		}
	}
}
