package core

import (
	"log"
	"sync"
	"time"
)

// =======================
// 🧬 DISTRIBUTED CALL NODE STATE
// =======================

type CallNodeState struct {
	CallID       string            `json:"call_id"`
	State        string            `json:"state"`
	Participants map[string]string `json:"participants"` // userID → nodeID

	LastUpdate time.Time `json:"last_update"`
	Version    int       `json:"version"`
}

// =======================
// 📡 CALL GRAPH ENGINE (DIJKSTRA & CALL STATE TOPOLOGY)
// =======================

type CallGraph struct {
	mu sync.RWMutex

	calls map[string]*CallNodeState

	// cluster propagation hook
	broadcast func(callID string, state CallNodeState)
}

func NewCallGraph() *CallGraph {
	return &CallGraph{
		calls: make(map[string]*CallNodeState),
	}
}

// =======================
// 🔌 CONNECT CLUSTER PROPAGATION
// =======================

func (g *CallGraph) SetBroadcaster(fn func(callID string, state CallNodeState)) {
	g.broadcast = fn
}

// =======================
// 📞 CREATE CALL
// =======================

func (g *CallGraph) CreateCall(callID string, from string, to string) *CallNodeState {
	g.mu.Lock()
	defer g.mu.Unlock()

	state := &CallNodeState{
		CallID: callID,
		State:  "DIALING",
		Participants: map[string]string{
			from: "",
			to:   "",
		},
		LastUpdate: time.Now(),
		Version:    1,
	}

	g.calls[callID] = state

	g.propagate(state)

	log.Printf("[CALLGRAPH] created call %s (%s → %s)", callID, from, to)

	return state
}

// =======================
// 🔁 UPDATE CALL STATE
// =======================

func (g *CallGraph) UpdateState(callID string, newState string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	call, ok := g.calls[callID]
	if !ok {
		return
	}

	call.State = newState
	call.LastUpdate = time.Now()
	call.Version++

	g.propagate(call)

	log.Printf("[CALLGRAPH] %s → %s", callID, newState)
}

// =======================
// 👥 ADD PARTICIPANT
// =======================

func (g *CallGraph) AddParticipant(callID, userID, nodeID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	call, ok := g.calls[callID]
	if !ok {
		return
	}

	call.Participants[userID] = nodeID
	call.LastUpdate = time.Now()
	call.Version++

	g.propagate(call)
}

// =======================
// 💔 REMOVE PARTICIPANT
// =======================

func (g *CallGraph) RemoveParticipant(callID, userID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	call, ok := g.calls[callID]
	if !ok {
		return
	}

	delete(call.Participants, userID)
	call.Version++

	g.propagate(call)
}

// =======================
// 📡 PROPAGATION ENGINE
// =======================

func (g *CallGraph) propagate(state *CallNodeState) {
	if g.broadcast == nil {
		return
	}

	copy := *state

	g.broadcast(state.CallID, copy)
}

// =======================
// 🔍 GET CALL
// =======================

func (g *CallGraph) Get(callID string) (*CallNodeState, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	c, ok := g.calls[callID]
	return c, ok
}

// =======================
// 🧹 CLEANUP EXPIRED
// =======================

func (g *CallGraph) CleanupExpired(maxAge time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()

	now := time.Now()

	for id, c := range g.calls {
		if now.Sub(c.LastUpdate) > maxAge {
			delete(g.calls, id)
			log.Printf("[CALLGRAPH] cleaned expired call %s", id)
		}
	}
}
