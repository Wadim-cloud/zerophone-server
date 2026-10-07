package core

import (
	"encoding/json"
	"log"
	"sync"
)

// =======================
// 🧬 SIGNAL TYPES (SIP-LIKE)
// =======================

type SignalType string

const (
	// SIP core
	SignalInvite  SignalType = "INVITE"
	SignalTrying  SignalType = "TRYING"
	SignalRinging SignalType = "RINGING"
	SignalOK      SignalType = "OK"
	SignalAck     SignalType = "ACK"
	SignalReject  SignalType = "REJECT"
	SignalBye     SignalType = "BYE"

	// WebRTC transport layer
	SignalSDP SignalType = "SDP"
	SignalICE SignalType = "ICE"

	// internal control
	SignalCancel SignalType = "CANCEL"
)

// =======================
// 📡 SIGNAL MODEL
// =======================

type Signal struct {
	CallID string          `json:"call_id"`
	From   string          `json:"from"`
	To     string          `json:"to"`
	Type   SignalType      `json:"type"`
	Data   json.RawMessage `json:"data,omitempty"`

	// WebRTC fields
	SDP       string `json:"sdp,omitempty"`
	Candidate string `json:"candidate,omitempty"`
}

// =======================
// 🔌 DEPENDENCIES
// =======================

// Call state brain (SIP FSM)
type CallStateHandler interface {
	Handle(Signal)
}

// Routing graph / cluster resolver
type SignalResolver interface {
	Resolve(peerID string) (addr string, ok bool)
	DecideICEStrategy(from, to string) ICEStrategyData
}

type ICEStrategyData struct {
	PreferredNetwork string `json:"preferred_network"`
	UseRelay         bool   `json:"use_relay"`
	RelayNode        string `json:"relay_node"`
	PriorityBoost    int    `json:"priority_boost"`
}

// Transport abstraction (WS / ZMQ / future QUIC / UDP)
type SignalTransport interface {
	SendTo(id string, data []byte) bool
}

// =======================
// 🧠 SIP ROUTER CORE
// =======================

type SignalHandler func(Signal)

type SignalRouter struct {
	mu sync.RWMutex

	handlers map[SignalType][]SignalHandler

	callRouter CallStateHandler
	resolver   SignalResolver
	transport  SignalTransport

	// 🧠 call fan-out state (forking INVITEs etc)
	activeForks map[string][]string
}

// =======================
// 🚀 INIT
// =======================

func NewSignalRouter(cr CallStateHandler) *SignalRouter {
	return &SignalRouter{
		handlers:    make(map[SignalType][]SignalHandler),
		callRouter:  cr,
		activeForks: make(map[string][]string),
	}
}

// =======================
// 🔌 PLUG DEPENDENCIES
// =======================

func (r *SignalRouter) SetResolver(res SignalResolver) {
	r.resolver = res
}

func (r *SignalRouter) SetTransport(t SignalTransport) {
	r.transport = t
}

// =======================
// 👂 EVENT SUBSCRIPTIONS
// =======================

func (r *SignalRouter) On(t SignalType, h SignalHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[t] = append(r.handlers[t], h)
}

// =======================
// 🧠 MAIN DISPATCH ENGINE (SIP CORE)
// =======================

func (r *SignalRouter) Dispatch(s Signal) {
	log.Printf("[SIGNAL] Dispatch: %s %s → %s", s.Type, s.From, s.To)

	// 0. ICE strategy injection for WebRTC signals
	if r.resolver != nil && (s.Type == SignalICE || s.Type == SignalSDP) && s.To != "" {
		strategy := r.resolver.DecideICEStrategy(s.From, s.To)

		var payload map[string]interface{}
		if err := json.Unmarshal(s.Data, &payload); err != nil {
			// If unmarshal fails, create empty payload
			payload = make(map[string]interface{})
		}

		payload["_ice"] = strategy
		newData, _ := json.Marshal(payload)
		s.Data = newData
	}

	// 1. ALWAYS update call state machine first
	if r.callRouter != nil {
		r.callRouter.Handle(s)
	}

	// 2. async observers (analytics / logs / UI)
	r.mu.RLock()
	hs := r.handlers[s.Type]
	r.mu.RUnlock()

	for _, h := range hs {
		go h(s)
	}

	// 3. routing logic
	r.route(s)
}

// =======================
// 🧭 SIP ROUTING ENGINE
// =======================

func (r *SignalRouter) route(s Signal) {
	log.Printf("[ROUTER] Routing signal %s to %s", s.Type, s.To)

	if r.transport == nil || s.To == "" || s.To == s.From {
		log.Printf("[ROUTER] Invalid routing: transport=%v, to=%s, from=%s", r.transport != nil, s.To, s.From)
		return
	}

	// Guard against self-calls
	if s.From == s.To {
		log.Printf("[ROUTER] Dropping self-call from %s", s.From)
		return
	}

	// SDP and Candidate are now proper fields in Signal struct

	// Marshal the signal
	payload, err := json.Marshal(s)
	if err != nil {
		log.Println("[ROUTER] marshal error:", err)
		return
	}

	// Try direct delivery
	if r.transport.SendTo(s.To, payload) {
		log.Printf("[ROUTER] Direct delivery successful to %s", s.To)
		return
	}

	log.Printf("[ROUTER] Direct delivery failed for %s", s.To)

	// =======================
	// 2. CLUSTER RESOLUTION
	// =======================

	if r.resolver != nil {
		if addr, ok := r.resolver.Resolve(s.To); ok {
			log.Printf("[ROUTER] resolved %s → %s", s.To, addr)

			// retry via logical ID (transport abstracts physical routing)
			if r.transport.SendTo(s.To, payload) {
				log.Printf("[ROUTER] Cluster delivery successful to %s", s.To)
				return
			}
		}
	}

	// =======================
	// 3. FALLBACK BEHAVIOR
	// =======================

	log.Printf("[ROUTER] no route for %s (dropping signal)", s.To)
	if err != nil {
		log.Println("[ROUTER] marshal error:", err)
		return
	}

	// Try direct delivery
	if r.transport.SendTo(s.To, payload) {
		log.Printf("[ROUTER] Direct delivery successful to %s", s.To)
		return
	}

	// =======================
	// 1. DIRECT DELIVERY
	// =======================

	if r.transport.SendTo(s.To, payload) {
		log.Printf("[ROUTER] Direct delivery successful to %s", s.To)
		return
	}

	log.Printf("[ROUTER] Direct delivery failed for %s", s.To)

	// =======================
	// 2. CLUSTER RESOLUTION
	// =======================

	if r.resolver != nil {
		if addr, ok := r.resolver.Resolve(s.To); ok {
			log.Printf("[ROUTER] resolved %s → %s", s.To, addr)

			// retry via logical ID (transport abstracts physical routing)
			if r.transport.SendTo(s.To, payload) {
				log.Printf("[ROUTER] Cluster delivery successful to %s", s.To)
				return
			}
		}
	}

	// =======================
	// 3. FALLBACK BEHAVIOR
	// =======================

	log.Printf("[ROUTER] no route for %s (dropping signal)", s.To)
}

// =======================
// 🧬 SIP EXTENSIONS
// =======================

// Fork an INVITE to multiple targets (parallel ringing)
func (r *SignalRouter) ForkInvite(callID string, from string, targets []string, data json.RawMessage) {
	r.mu.Lock()
	r.activeForks[callID] = targets
	r.mu.Unlock()

	for _, t := range targets {
		s := Signal{
			CallID: callID,
			From:   from,
			To:     t,
			Type:   SignalInvite,
			Data:   data,
		}

		go r.Dispatch(s)
	}
}

// Cancel forked calls (SIP CANCEL semantics)
func (r *SignalRouter) CancelFork(callID string) {
	r.mu.RLock()
	targets := r.activeForks[callID]
	r.mu.RUnlock()

	for _, t := range targets {
		s := Signal{
			CallID: callID,
			To:     t,
			Type:   SignalCancel,
		}

		go r.Dispatch(s)
	}

	r.mu.Lock()
	delete(r.activeForks, callID)
	r.mu.Unlock()
}

// =======================
// 📦 JSON HELPERS
// =======================

func (r *SignalRouter) FromJSON(b []byte) (Signal, error) {
	var s Signal
	err := json.Unmarshal(b, &s)
	return s, err
}
