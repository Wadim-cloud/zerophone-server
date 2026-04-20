package main

import (
	"sync"
	"time"
)

type Store struct {
	mu         sync.RWMutex
	db         Database
	announceFn func(*Node)
}

func NewStore() *Store {
	return &Store{
		db: nil, // will be initialized in main
	}
}

func (s *Store) SetAnnounceFn(fn func(*Node)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.announceFn = fn
}

func (s *Store) Announce(node *Node) {
	if s.announceFn != nil {
		s.announceFn(node)
	}
}

func (s *Store) SetDB(db Database) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
}

func (s *Store) RegisterNode(id, name, networkID string, caps []string, serverID string) *Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	node := &Node{
		ID:           id,
		Name:         name,
		NetworkID:    networkID,
		ServerID:     serverID,
		LastSeen:     time.Now().Unix(),
		Status:       "offline",
		Capabilities: caps,
	}

	_ = s.db.CreateNode(id, name, networkID, caps, serverID)
	s.Announce(node)
	return node
}

func (s *Store) UpsertNode(node *Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.UpsertNode(node)
}

func (s *Store) Heartbeat(nodeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.db.UpdateNodeLastSeen(nodeID)
	if err != nil {
		return false
	}

	// Update status based on recent activity
	now := time.Now().Unix()
	node, err := s.db.GetNode(nodeID)
	if err != nil || now-node.LastSeen > 60 {
		return false
	}
	node.Status = "online"
	return true
}

func (s *Store) GetNodes() []*Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	nodes, err := s.db.GetAllNodes()
	if err != nil {
		return nil
	}

	now := time.Now().Unix()
	for _, node := range nodes {
		if now-node.LastSeen < 60 {
			node.Status = "online"
		} else {
			node.Status = "offline"
		}
	}

	return nodes
}

func (s *Store) GetNodesByNetwork(networkID string) []*Node {
	s.mu.Lock()
	defer s.mu.Unlock()

	nodes, err := s.db.GetNodesByNetwork(networkID)
	if err != nil {
		return nil
	}

	now := time.Now().Unix()
	for _, node := range nodes {
		if now-node.LastSeen < 60 {
			node.Status = "online"
		} else {
			node.Status = "offline"
		}
	}

	return nodes
}

func (s *Store) GetNode(id string) (*Node, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	node, err := s.db.GetNode(id)
	if err != nil {
		return nil, false
	}

	now := time.Now().Unix()
	if now-node.LastSeen < 60 {
		node.Status = "online"
	} else {
		node.Status = "offline"
	}

	return node, true
}

func (s *Store) QueueMessage(msg Message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg.Time = time.Now().Unix()
	_ = s.db.QueueMessage(msg)
}

func (s *Store) DeleteNode(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.DeleteNode(id)
}

func (s *Store) GetMessages(nodeID string) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs, err := s.db.GetAndMarkMessages(nodeID)
	if err != nil {
		return nil
	}
	return msgs
}

func (s *Store) CreateCall(callID, a, b string) *Call {
	s.mu.Lock()
	defer s.mu.Unlock()

	call := &Call{
		CallID: callID,
		A:      a,
		B:      b,
		State:  "ringing",
	}

	_ = s.db.CreateCall(callID, a, b)
	return call
}

func (s *Store) UpdateCallState(callID, state string) *Call {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = s.db.UpdateCallState(callID, state)
	call, _ := s.db.GetCall(callID)
	return call
}

func (s *Store) GetCall(callID string) (*Call, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	call, err := s.db.GetCall(callID)
	if err != nil {
		return nil, false
	}
	return call, true
}
