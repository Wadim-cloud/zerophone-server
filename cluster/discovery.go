package cluster

import (
	"encoding/json"
	"log"
	"sync"
	"time"
)

// =======================
// 🌐 CLUSTER EVENT MODEL
// =======================

type ClusterEvent struct {
	Type    string
	Payload []byte
}

// =======================
// 📡 DISCOVERY SERVICE
// =======================

type DiscoveryService struct {
	localNode *Node
	registry  *NodeRegistry

	zmqConn *ZMQNodeConnection

	signalResolver *SignalResolver

	stopCh chan struct{}
	wg     sync.WaitGroup

	mu sync.RWMutex

	// 🧠 EVENT BUS (UPSTREAM ONLY)
	EventBus chan ClusterEvent
}

// =======================
// INIT
// =======================

func NewDiscoveryService(localNode *Node, registry *NodeRegistry) *DiscoveryService {
	return &DiscoveryService{
		localNode: localNode,
		registry:  registry,
		stopCh:    make(chan struct{}),
	}
}

// =======================
// START
// =======================

func (d *DiscoveryService) Start() error {
	d.EventBus = make(chan ClusterEvent, 256)

	d.wg.Add(1)
	go d.receiveLoop()

	log.Println("[DISCOVERY] started")
	return nil
}

// =======================
// STOP
// =======================

func (d *DiscoveryService) Stop() {
	close(d.stopCh)
	d.wg.Wait()
}

// =======================
// ZMQ ATTACHMENT
// =======================

func (d *DiscoveryService) SetZMQ(conn *ZMQNodeConnection) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.zmqConn = conn
}

func (d *DiscoveryService) getZMQ() *ZMQNodeConnection {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.zmqConn
}

func (d *DiscoveryService) SetSignalResolver(r *SignalResolver) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.signalResolver = r
}

// =======================
// EVENT EMITTER
// =======================

func (d *DiscoveryService) emit(eventType string, payload []byte) {
	if d.EventBus == nil {
		return
	}

	select {
	case d.EventBus <- ClusterEvent{
		Type:    eventType,
		Payload: payload,
	}:
	default:
		// drop under load (cluster must never block)
	}
}

// =======================
// PUBLIC API
// =======================

// Cluster broadcast (pure transport)
func (d *DiscoveryService) BroadcastAnnouncement(eventType string, data map[string]interface{}) error {
	conn := d.getZMQ()
	if conn == nil {
		return nil
	}

	payload, _ := json.Marshal(data)
	return conn.Broadcast(eventType, payload)
}

// Peer list access (read-only)
func (d *DiscoveryService) GetPeers() []*Node {
	if d.registry == nil {
		return nil
	}
	return d.registry.GetAll()
}

// =======================
// MAIN RECEIVE LOOP
// =======================

func (d *DiscoveryService) receiveLoop() {
	defer d.wg.Done()

	for {
		select {
		case <-d.stopCh:
			return
		default:
		}

		conn := d.getZMQ()
		if conn == nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		msgs, err := conn.Receive()
		if err != nil || len(msgs) < 2 {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(msgs[1]), &payload); err != nil {
			continue
		}

		switch msgs[0] {

		// =======================
		// 👤 USER PRESENCE SYNC
		// =======================

		case "USER_ONLINE":
			id, _ := payload["user_id"].(string)
			name, _ := payload["name"].(string)
			node, _ := payload["node"].(string)

			if id != "" && node != "" && d.signalResolver != nil {
				d.signalResolver.UpsertFromRegistry(&Node{
					ID:         id,
					Name:       name,
					ZeroTierIP: node,
				})
			}

			// emit upward only
			raw, _ := json.Marshal(payload)
			d.emit("USER_ONLINE", raw)

		// =======================
		// 🧭 NODE STATUS SYNC
		// =======================

		case "NODE_ONLINE":
			log.Printf("[DISCOVERY] node online: %+v", payload)

			// 🧠 feed resolver from discovery
			if nodeID, ok := payload["node_id"].(string); ok && d.signalResolver != nil {
				ip, _ := payload["ip"].(string)
				d.signalResolver.UpsertFromRegistry(&Node{
					ID:         nodeID,
					ZeroTierIP: ip,
				})
			}

			raw, _ := json.Marshal(payload)
			d.emit("NODE_ONLINE", raw)

		// =======================
		// 📡 CALL SIGNAL (RAW EVENT ONLY)
		// =======================

		case "CALL_SIGNAL":
			raw, _ := json.Marshal(payload)
			d.emit("CALL_SIGNAL", raw)

		// =======================
		// 📡 ROUTE GOSSIP
		// =======================

		case "ROUTE_GOSSIP":
			if data, ok := payload["data"].(string); ok && d.signalResolver != nil {
				d.signalResolver.ApplyGossip([]byte(data))
			}
		}
	}
}
