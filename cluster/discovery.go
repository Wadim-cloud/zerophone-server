	package cluster

	import (
		"encoding/json"
		"sync"
		"time"
	)

	type DiscoveryService struct {
		localNode *Node
		registry  *NodeRegistry
		zmqConn   *ZMQNodeConnection
		stopCh    chan struct{}
		wg        sync.WaitGroup
	}

	func NewDiscoveryService(localNode *Node, registry *NodeRegistry) *DiscoveryService {
		return &DiscoveryService{
			localNode: localNode,
			registry:  registry,
			stopCh:    make(chan struct{}),
		}
	}

	func (d *DiscoveryService) Start() error {
		d.wg.Add(1)
		go d.receiveLoop()
		return nil
	}

	func (d *DiscoveryService) Stop() {
		close(d.stopCh)
		d.wg.Wait()
	}

	func (d *DiscoveryService) BroadcastAnnouncement(eventType string, data map[string]interface{}) error {
		if d.zmqConn == nil {
			return nil
		}
		payload, _ := json.Marshal(data)
		return d.zmqConn.Broadcast(eventType, payload)
	}

	func (d *DiscoveryService) receiveLoop() {
		defer d.wg.Done()
		for {
			select {
			case <-d.stopCh:
				return
			default:
				time.Sleep(100 * time.Millisecond)
			}
		}
	}

	func (d *DiscoveryService) GetPeers() []*Node {
		if d.registry == nil {
			return nil
		}
		return d.registry.GetAll()
	}
