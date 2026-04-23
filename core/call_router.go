package core

import (
	"encoding/json"
	"log"
	"sync"
)

// =======================
// 📞 CALL ROUTER CORE
// =======================

type CallLoadTracker interface {
	MarkCallStart(nodeID string)
	MarkCallEnd(nodeID string)
}

type CallRouter struct {
	stateMachine *CallStateMachine

	// transport hooks (pluggable)
	wsSend      func(to string, data []byte) bool
	clusterSend func(to string, data []byte) bool

	// load tracking hook
	loadTracker CallLoadTracker

	mu sync.RWMutex

	calls map[string]string // callID → state
}

// =======================
// INIT
// =======================

func NewCallRouter(sm *CallStateMachine) *CallRouter {
	return &CallRouter{
		stateMachine: sm,
		calls:        make(map[string]string),
	}
}

// =======================
// TRANSPORT PLUGINS
// =======================

func (c *CallRouter) SetWS(fn func(to string, data []byte) bool) {
	c.wsSend = fn
}

func (c *CallRouter) SetCluster(fn func(to string, data []byte) bool) {
	c.clusterSend = fn
}

func (c *CallRouter) SetLoadTracker(tracker CallLoadTracker) {
	c.loadTracker = tracker
}

// =======================
// MAIN ENTRY
// =======================

func (c *CallRouter) Handle(s Signal) {
	if s.CallID == "" {
		log.Println("[CALL] missing callID, ignored")
		return
	}

	// =======================
	// 1. STATE TRANSITION
	// =======================

	c.mu.Lock()

	current := c.calls[s.CallID]
	if current == "" {
		current = "IDLE"
	}

	if !c.stateMachine.canTransition(current, string(s.Type)) {
		log.Printf("[CALL] invalid transition %s → %s", current, s.Type)
		c.mu.Unlock()
		return
	}

	next := c.stateMachine.nextState(current, string(s.Type))
	c.calls[s.CallID] = next

	c.mu.Unlock()

	log.Printf("[CALL STATE] %s → %s (%s)", current, next, s.CallID)

	// 🧠 track load on call start/end
	if s.Type == SignalInvite && c.loadTracker != nil {
		c.loadTracker.MarkCallStart(s.To)
	}
	if (s.Type == SignalBye || s.Type == SignalReject) && c.loadTracker != nil {
		c.loadTracker.MarkCallEnd(s.To)
	}

	// =======================
	// 2. ROUTE OUTSIDE LOCK
	// =======================

	c.route(s)
}

// =======================
// ROUTING LAYER
// =======================

func (c *CallRouter) route(s Signal) {
	if s.To == "" {
		return
	}

	payload, err := json.Marshal(s)
	if err != nil {
		return
	}

	// =======================
	// 1. WS PATH (FAST PATH)
	// =======================

	if c.wsSend != nil {
		ok := c.wsSend(s.To, payload)
		if ok {
			return
		}
	}

	// =======================
	// 2. CLUSTER PATH (FALLBACK)
	// =======================

	if c.clusterSend != nil {
		c.clusterSend(s.To, payload)
	}
}

// =======================
// INTROSPECTION
// =======================

func (c *CallRouter) GetState(callID string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.calls[callID]
}
