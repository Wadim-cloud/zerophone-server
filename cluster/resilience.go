package cluster

import (
	"sync"
	"time"
)

type FailureHandler struct {
	failures map[string]int
	stopCh   chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
}

func NewFailureHandler() *FailureHandler {
	return &FailureHandler{
		failures: make(map[string]int),
		stopCh:   make(chan struct{}),
	}
}

func (f *FailureHandler) Start() error {
	f.wg.Add(1)
	go f.healthCheckLoop()
	return nil
}

func (f *FailureHandler) Stop() {
	close(f.stopCh)
	f.wg.Wait()
}

func (f *FailureHandler) RecordFailure(nodeID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures[nodeID]++
}

func (f *FailureHandler) GetFailureCount(nodeID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failures[nodeID]
}

func (f *FailureHandler) ResetFailure(nodeID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.failures, nodeID)
}

func (f *FailureHandler) healthCheckLoop() {
	defer f.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-f.stopCh:
			return
		case <-ticker.C:
		}
	}
}
