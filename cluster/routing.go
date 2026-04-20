package cluster

import (
	"sync"
	"time"
)

type RoutingService struct {
	peers        map[string]string
	routingTable map[string]string
	stopCh       chan struct{}
	wg           sync.WaitGroup
	mu           sync.RWMutex
}

func NewRoutingService() *RoutingService {
	return &RoutingService{
		peers:        make(map[string]string),
		routingTable: make(map[string]string),
		stopCh:       make(chan struct{}),
	}
}

func (r *RoutingService) Start() error {
	r.wg.Add(1)
	go r.routingLoop()
	return nil
}

func (r *RoutingService) Stop() {
	close(r.stopCh)
	r.wg.Wait()
}

func (r *RoutingService) AddPeer(peerID, addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.peers[peerID] = addr
	r.routingTable[peerID] = addr
}

func (r *RoutingService) GetPeer(peerID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	addr, ok := r.peers[peerID]
	return addr, ok
}

func (r *RoutingService) routingLoop() {
	defer r.wg.Done()
	for {
		select {
		case <-r.stopCh:
			return
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}
