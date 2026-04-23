package cluster

import (
	"sync"
	"time"
)

type ReachabilityType string

const (
	ReachDirect  ReachabilityType = "DIRECT"
	ReachRelay   ReachabilityType = "RELAY"
	ReachUnknown ReachabilityType = "UNKNOWN"
)

type ReachabilityState struct {
	From string
	To   string

	Type      ReachabilityType
	LastCheck time.Time
	LatencyMs float64
	Success   bool
}

type ReachabilityMap struct {
	mu sync.RWMutex

	// key: from → to
	matrix map[string]map[string]*ReachabilityState
}

func NewReachabilityMap() *ReachabilityMap {
	return &ReachabilityMap{
		matrix: make(map[string]map[string]*ReachabilityState),
	}
}

// ---------------- UPDATE ----------------

func (r *ReachabilityMap) Set(from, to string, state *ReachabilityState) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.matrix[from]; !ok {
		r.matrix[from] = make(map[string]*ReachabilityState)
	}

	r.matrix[from][to] = state
}

// ---------------- GET ----------------

func (r *ReachabilityMap) Get(from, to string) (*ReachabilityState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if row, ok := r.matrix[from]; ok {
		s, ok := row[to]
		return s, ok
	}

	return nil, false
}