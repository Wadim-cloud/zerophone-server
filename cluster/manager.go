package cluster

import (
	"fmt"
	"log"
	"sync"
	"time"
)

type ClusterManager struct {
	config       *ClusterConfig
	localNode    *Node
	registry     *NodeRegistry
	discovery    *DiscoveryService
	router       *RoutingService
	resilience   *FailureHandler
	zmqRouter    *ZMQNodeConnection
	zmqDiscovery *ZMQNodeConnection
	zmqRouting   *ZMQNodeConnection

	startTime time.Time
	stopCh    chan struct{}
	wg        sync.WaitGroup
	mu        sync.Mutex
	running   bool
}

func NewClusterManager(config *ClusterConfig, localNode *Node, registry *NodeRegistry) *ClusterManager {
	return &ClusterManager{
		config:    config,
		localNode: localNode,
		registry:  registry,
		stopCh:    make(chan struct{}),
		startTime: time.Now(),
	}
}

func (cm *ClusterManager) Start(staticPeers []string) error {
	cm.mu.Lock()
	if cm.running {
		cm.mu.Unlock()
		return nil
	}
	cm.running = true
	cm.mu.Unlock()

	log.Println("Starting cluster module with ZeroMQ integration...")

	if err := cm.initZeroMQConnections(); err != nil {
		log.Printf("ZMQ connections init warning: %v (continuing anyway)", err)
	}

	cm.discovery = NewDiscoveryService(cm.localNode, cm.registry)
	if err := cm.discovery.Start(); err != nil {
		log.Printf("Discovery service warning: %v", err)
	} else {
		log.Println("Discovery service started")
	}

	cm.router = NewRoutingService()
	if err := cm.router.Start(); err != nil {
		log.Printf("Routing service warning: %v", err)
	} else {
		log.Println("Routing service started")
	}

	cm.resilience = NewFailureHandler()
	if err := cm.resilience.Start(); err != nil {
		log.Printf("Resilience service warning: %v", err)
	} else {
		log.Println("Resilience service started")
	}

	if err := cm.connectToStaticPeers(staticPeers); err != nil {
		log.Printf("Static peers warning: %v", err)
	}

	go cm.registerNodeWithDiscovery()

	cm.wg.Add(1)
	go cm.monitoringLoop()

	log.Println("Cluster module with ZeroMQ integration started successfully")
	log.Printf("Node ID: %s", cm.localNode.ID)
	log.Printf("ZeroTier IP: %s", cm.localNode.ZeroTierIP)
	log.Printf("Services: Discovery, Routing, Resilience")

	return nil
}

func (cm *ClusterManager) initZeroMQConnections() error {
	log.Println("Attempting ZMQ connections...")
	routerConfig := NewZMQNodeConnectionConfig{
		NodeID:     cm.localNode.ID,
		RouterAddr: fmt.Sprintf("tcp://*:%d", ZMQRouterPort),
		PubAddr:    fmt.Sprintf("tcp://*:%d", ZMQPubPort),
		DealerAddr: fmt.Sprintf("tcp://*:%d", ZMQRouterPort+1),
		Bind:       true,
	}

	routerConn, err := NewZMQNodeConnection(routerConfig)
	if err != nil {
		return fmt.Errorf("failed to create router connection: %w", err)
	}
	cm.zmqRouter = routerConn

	discoveryConfig := NewZMQNodeConnectionConfig{
		NodeID:  cm.localNode.ID,
		PubAddr: fmt.Sprintf("tcp://*:%d", ZMQDiscoveryPubPort),
		Bind:    true,
	}

	discoveryConn, err := NewZMQNodeConnection(discoveryConfig)
	if err != nil {
		routerConn.Close()
		return fmt.Errorf("failed to create discovery connection: %w", err)
	}
	cm.zmqDiscovery = discoveryConn

	return nil
}

func (cm *ClusterManager) connectToStaticPeers(peers []string) error {
	for _, peer := range peers {
		peerAddr := peer
		cm.router.AddPeer(peer, peerAddr)
		cm.zmqDiscovery.AddPeer(peer, peerAddr)
		log.Printf("Connected to static peer: %s", peer)
	}
	return nil
}

func (cm *ClusterManager) registerNodeWithDiscovery() {
	time.Sleep(1 * time.Second)
	if cm.discovery != nil {
		cm.discovery.BroadcastAnnouncement("node_registered", map[string]interface{}{
			"node_id":     cm.localNode.ID,
			"zerotier_ip": cm.localNode.ZeroTierIP,
			"public_ip":   cm.localNode.PublicIP,
			"port":        cm.localNode.Port,
		})
	}
}

func (cm *ClusterManager) monitoringLoop() {
	defer cm.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-cm.stopCh:
			return
		case <-ticker.C:
			cm.performHealthCheck()
		}
	}
}

func (cm *ClusterManager) performHealthCheck() {
	log.Println("Performing cluster health check...")
	if cm.registry != nil {
		for _, peer := range cm.registry.GetOnline() {
			if peer.ID != cm.localNode.ID {
				log.Printf("Health check: %s is online", peer.ID)
			}
		}
	}
}

func (cm *ClusterManager) Stop() error {
	cm.mu.Lock()
	if !cm.running {
		cm.mu.Unlock()
		return nil
	}
	cm.running = false
	cm.mu.Unlock()

	log.Println("Stopping cluster manager...")

	close(cm.stopCh)
	cm.wg.Wait()

	if cm.resilience != nil {
		cm.resilience.Stop()
	}
	if cm.router != nil {
		cm.router.Stop()
	}
	if cm.discovery != nil {
		cm.discovery.Stop()
	}

	if cm.zmqRouter != nil {
		cm.zmqRouter.Close()
	}
	if cm.zmqDiscovery != nil {
		cm.zmqDiscovery.Close()
	}

	log.Println("Cluster manager stopped")
	return nil
}

func (cm *ClusterManager) GetManagerStatus() map[string]interface{} {
	cm.mu.Lock()
	running := cm.running
	cm.mu.Unlock()

	status := map[string]interface{}{
		"running":       running,
		"uptime":        time.Since(cm.startTime).String(),
		"node_id":       cm.localNode.ID,
		"zerotier_ip":   cm.localNode.ZeroTierIP,
		"discovery":     cm.discovery != nil,
		"routing":       cm.router != nil,
		"resilience":    cm.resilience != nil,
		"zmq_router":    cm.zmqRouter != nil,
		"zmq_discovery": cm.zmqDiscovery != nil,
	}

	return status
}
