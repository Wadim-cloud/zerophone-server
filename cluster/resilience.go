package cluster

import (
	"log"
	"sync"
	"time"
)

const (
	FailureThreshold   = 3               // failures before marking unhealthy
	RecoveryTimeout    = 60 * time.Second // time before retrying node
	HealthCheckTick    = 10 * time.Second
)

type NodeHealth struct {
	Failures     int
	LastFailure  time.Time
	Unhealthy    bool
}

type FailureHandler struct {
	failures map[string]*NodeHealth

	registry *NodeRegistry

	stopCh chan struct{}
	wg     sync.WaitGroup
	mu     sync.RWMutex
}

func NewFailureHandler(registry *NodeRegistry) *FailureHandler {
	return &FailureHandler{
		failures: make(map[string]*NodeHealth),
		registry: registry,
		stopCh:   make(chan struct{}),
	}
}

func (f *FailureHandler) Start() error {
	f.wg.Add(1)
	go f.healthCheckLoop()
	log.Println("[RESILIENCE] started")
	return nil
}

func (f *FailureHandler) Stop() {
	close(f.stopCh)
	f.wg.Wait()
	log.Println("[RESILIENCE] stopped")
}

//
// 🚨 Record failure
//
func (f *FailureHandler) RecordFailure(nodeID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	h, ok := f.failures[nodeID]
	if !ok {
		h = &NodeHealth{}
		f.failures[nodeID] = h
	}

	h.Failures++
	h.LastFailure = time.Now()

	log.Printf("[RESILIENCE] failure %s count=%d", nodeID, h.Failures)

	if h.Failures >= FailureThreshold && !h.Unhealthy {
		h.Unhealthy = true
		f.markNodeUnhealthy(nodeID)
	}
}

//
// ✅ Reset on success
//
func (f *FailureHandler) ResetFailure(nodeID string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if h, ok := f.failures[nodeID]; ok {
		h.Failures = 0
		h.Unhealthy = false
		log.Printf("[RESILIENCE] node recovered %s", nodeID)
	}

	f.markNodeHealthy(nodeID)
}

//
// 📊 Get failure count
//
func (f *FailureHandler) GetFailureCount(nodeID string) int {
	f.mu.RLock()
	defer f.mu.RUnlock()

	if h, ok := f.failures[nodeID]; ok {
		return h.Failures
	}
	return 0
}

//
// ❌ Check if node is usable
//
func (f *FailureHandler) IsNodeHealthy(nodeID string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	h, ok := f.failures[nodeID]
	if !ok {
		return true
	}
	return !h.Unhealthy
}

//
// 🔁 Periodic recovery + cleanup
//
func (f *FailureHandler) healthCheckLoop() {
	defer f.wg.Done()

	ticker := time.NewTicker(HealthCheckTick)
	defer ticker.Stop()

	for {
		select {
		case <-f.stopCh:
			return

		case <-ticker.C:
			f.runHealthSweep()
		}
	}
}

func (f *FailureHandler) runHealthSweep() {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()

	for nodeID, h := range f.failures {
		if h.Unhealthy {
			// try recovery after timeout
			if now.Sub(h.LastFailure) > RecoveryTimeout {
				log.Printf("[RESILIENCE] retrying node %s", nodeID)

				h.Failures = 0
				h.Unhealthy = false

				f.markNodeHealthy(nodeID)
			}
		}
	}
}

//
// 🧩 Registry integration
//
func (f *FailureHandler) markNodeUnhealthy(nodeID string) {
	if f.registry == nil {
		return
	}

	node, ok := f.registry.Get(nodeID)
	if ok {
		node.Status = "unhealthy"
		node.LastSeen = time.Now().Unix()
		log.Printf("[RESILIENCE] node marked UNHEALTHY: %s", nodeID)
	}
}

func (f *FailureHandler) markNodeHealthy(nodeID string) {
	if f.registry == nil {
		return
	}

	node, ok := f.registry.Get(nodeID)
	if ok {
		node.Status = "online"
		node.LastSeen = time.Now().Unix()
		log.Printf("[RESILIENCE] node marked HEALTHY: %s", nodeID)
	}
}