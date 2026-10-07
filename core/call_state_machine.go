package core

import (
	"log"
	"sync"
	"time"
)

// =======================
// 📞 SIP-LIKE CALL STATES
// =======================

type CallState string

const (
	StateIdle    CallState = "IDLE"
	StateCalling CallState = "CALLING" // INVITE sent
	StateTrying  CallState = "TRYING"  // received, processing
	StateRinging CallState = "RINGING" // remote ringing
	StateActive  CallState = "ACTIVE"  // call established
	StateEnding  CallState = "ENDING"
	StateEnded   CallState = "ENDED"
	StateFailed  CallState = "FAILED"
)

// =======================
// 🧠 SIP TIMERS (simplified RFC-like behavior)
// =======================

const (
	InviteTimeout = 30 * time.Second // INVITE expires
	RetransmitT1  = 500 * time.Millisecond
	RetransmitMax = 4 * time.Second
)

// =======================
// 📦 CALL STATE MACHINE
// =======================

type CallStateMachine struct {
	mu sync.RWMutex

	calls map[string]*CallSession

	// hook for cluster or transport failure recovery
	OnFailure func(callID string, reason string)
}

// =======================
// 📞 CALL SESSION
// =======================

type CallSession struct {
	ID        string
	State     CallState
	LastEvent time.Time

	RetryCount int

	// INVITE lifecycle tracking
	InviteSentAt time.Time
	LastReTx     time.Time

	// timers
	timer *time.Timer
}

// =======================
// 🚀 INIT
// =======================

func NewCallStateMachine() *CallStateMachine {
	return &CallStateMachine{
		calls: make(map[string]*CallSession),
	}
}

// =======================
// 🔁 MAIN ENTRY
// =======================

func (sm *CallStateMachine) Handle(s Signal) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.calls[s.CallID]

	if !ok {
		session = &CallSession{
			ID:    s.CallID,
			State: StateIdle,
		}
		sm.calls[s.CallID] = session
	}

	prev := session.State
	next := sm.transition(session, s)

	if prev != next {
		log.Printf("[CALL FSM] %s → %s (%s)", prev, next, s.Type)
	}

	session.State = next
	session.LastEvent = time.Now()

	// =======================
	// INVITE TIMER ARMING
	// =======================

	if s.Type == SignalInvite && session.InviteSentAt.IsZero() {
		sm.armInviteTimeout(session)
	}
}

// =======================
// 🧭 STATE TRANSITIONS (SIP CORE)
// =======================

func (sm *CallStateMachine) transition(c *CallSession, s Signal) CallState {

	switch c.State {

	// ---------------- IDLE ----------------
	case StateIdle:
		switch s.Type {
		case SignalInvite:
			c.InviteSentAt = time.Now()
			return StateCalling
		}

	// ---------------- CALLING ----------------
	case StateCalling:
		switch s.Type {
		case SignalTrying:
			return StateTrying
		case SignalRinging:
			return StateRinging
		case SignalOK:
			return StateActive
		case SignalReject:
			return StateFailed
		}

	// ---------------- TRYING ----------------
	case StateTrying:
		switch s.Type {
		case SignalRinging:
			return StateRinging
		case SignalOK:
			return StateActive
		case SignalReject:
			return StateFailed
		}

	// ---------------- RINGING ----------------
	case StateRinging:
		switch s.Type {
		case SignalOK:
			return StateActive
		case SignalReject:
			return StateFailed
		}

	// ---------------- ACTIVE ----------------
	case StateActive:
		switch s.Type {
		case SignalAck:
			// ACK received, connection fully confirmed
			return StateActive
		case SignalBye:
			return StateEnding
		}

	// ---------------- ENDING ----------------
	case StateEnding:
		switch s.Type {
		case SignalBye:
			return StateEnded
		}
	}

	return c.State
}

// =======================
// 🔍 TRANSITION QUERIES
// =======================

func (sm *CallStateMachine) canTransition(from string, signal string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if call, ok := sm.calls[from]; ok {
		next := sm.transition(call, Signal{Type: SignalType(signal)})
		return next != call.State
	}
	return false
}

func (sm *CallStateMachine) nextState(from string, signal string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if call, ok := sm.calls[from]; ok {
		next := sm.transition(call, Signal{Type: SignalType(signal)})
		return string(next)
	}
	return from
}

// =======================
// ⏱ INVITE TIMEOUT (SIP STYLE RETRY FAILURE)
// =======================

func (sm *CallStateMachine) armInviteTimeout(c *CallSession) {
	if c.timer != nil {
		c.timer.Stop()
	}

	c.timer = time.AfterFunc(InviteTimeout, func() {
		sm.mu.Lock()
		defer sm.mu.Unlock()

		if session, ok := sm.calls[c.ID]; ok {
			if session.State != StateActive && session.State != StateEnded {
				log.Printf("[CALL TIMEOUT] INVITE expired %s", c.ID)

				session.State = StateFailed

				if sm.OnFailure != nil {
					go sm.OnFailure(c.ID, "invite_timeout")
				}
			}
		}
	})
}

// =======================
// 🔁 RETRANSMISSION LOGIC (LIGHT SIP BEHAVIOR)
// =======================

func (sm *CallStateMachine) ShouldRetransmit(callID string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	c, ok := sm.calls[callID]
	if !ok {
		return false
	}

	if c.State != StateCalling {
		return false
	}

	if time.Since(c.LastReTx) > RetransmitT1 {
		return true
	}

	return false
}

// =======================
// ❌ CLEANUP
// =======================

func (sm *CallStateMachine) EndCall(callID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if c, ok := sm.calls[callID]; ok {
		c.State = StateEnded
		if c.timer != nil {
			c.timer.Stop()
		}
	}
}

// =======================
// 📊 DEBUG
// =======================

func (sm *CallStateMachine) GetState(callID string) CallState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if c, ok := sm.calls[callID]; ok {
		return c.State
	}
	return StateIdle
}
