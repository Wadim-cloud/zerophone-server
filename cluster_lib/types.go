package main

import "time"

// Node represents a cluster node
type Node struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	NetworkID    string   `json:"network_id"`
	ServerID     string   `json:"server_id"`
	ZeroTierIP   string   `json:"zerotier_ip"`
	Status       string   `json:"status"`
	LastSeen     int64    `json:"last_seen"`
	Capabilities []string `json:"capabilities"`
}

// Message represents a signaling message
type Message struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
	Time      int64  `json:"time"`
	Delivered bool   `json:"delivered"`
}

// Call represents a voice call
type Call struct {
	CallID string `json:"call_id"`
	A      string `json:"a"`     // Caller
	B      string `json:"b"`     // Callee
	State  string `json:"state"` // ringing, active, ended
}

// Database interface for data persistence
type Database interface {
	// Node operations
	CreateNode(id, name, networkID string, caps []string, serverID string) error
	UpsertNode(node *Node) error
	GetNode(id string) (*Node, error)
	GetAllNodes() ([]*Node, error)
	GetNodesByNetwork(networkID string) ([]*Node, error)
	UpdateNodeLastSeen(id string) error
	DeleteNode(id string) error

	// Message operations
	QueueMessage(msg Message) error
	GetAndMarkMessages(nodeID string) ([]Message, error)

	// Call operations
	CreateCall(callID, a, b string) error
	GetCall(callID string) (*Call, error)
	UpdateCallState(callID, state string) error
}

// InMemoryDB is a simple in-memory implementation of Database
type InMemoryDB struct {
	nodes    map[string]*Node
	messages []Message
	calls    map[string]*Call
}

func NewInMemoryDB() *InMemoryDB {
	return &InMemoryDB{
		nodes:    make(map[string]*Node),
		messages: make([]Message, 0),
		calls:    make(map[string]*Call),
	}
}

func (db *InMemoryDB) CreateNode(id, name, networkID string, caps []string, serverID string) error {
	db.nodes[id] = &Node{
		ID:           id,
		Name:         name,
		NetworkID:    networkID,
		ServerID:     serverID,
		LastSeen:     time.Now().Unix(),
		Status:       "online",
		Capabilities: caps,
	}
	return nil
}

func (db *InMemoryDB) UpsertNode(node *Node) error {
	node.LastSeen = time.Now().Unix()
	db.nodes[node.ID] = node
	return nil
}

func (db *InMemoryDB) GetNode(id string) (*Node, error) {
	return db.nodes[id], nil
}

func (db *InMemoryDB) GetAllNodes() ([]*Node, error) {
	nodes := make([]*Node, 0, len(db.nodes))
	for _, n := range db.nodes {
		nodes = append(nodes, n)
	}
	return nodes, nil
}

func (db *InMemoryDB) GetNodesByNetwork(networkID string) ([]*Node, error) {
	nodes := make([]*Node, 0)
	for _, n := range db.nodes {
		if n.NetworkID == networkID {
			nodes = append(nodes, n)
		}
	}
	return nodes, nil
}

func (db *InMemoryDB) UpdateNodeLastSeen(id string) error {
	if n, ok := db.nodes[id]; ok {
		n.LastSeen = time.Now().Unix()
	}
	return nil
}

func (db *InMemoryDB) DeleteNode(id string) error {
	delete(db.nodes, id)
	return nil
}

func (db *InMemoryDB) QueueMessage(msg Message) error {
	db.messages = append(db.messages, msg)
	return nil
}

func (db *InMemoryDB) GetAndMarkMessages(nodeID string) ([]Message, error) {
	var result []Message
	var remaining []Message
	for _, m := range db.messages {
		if m.To == nodeID && !m.Delivered {
			m.Delivered = true
			result = append(result, m)
		} else {
			remaining = append(remaining, m)
		}
	}
	db.messages = remaining
	return result, nil
}

func (db *InMemoryDB) CreateCall(callID, a, b string) error {
	db.calls[callID] = &Call{
		CallID: callID,
		A:      a,
		B:      b,
		State:  "ringing",
	}
	return nil
}

func (db *InMemoryDB) GetCall(callID string) (*Call, error) {
	return db.calls[callID], nil
}

func (db *InMemoryDB) UpdateCallState(callID, state string) error {
	if c, ok := db.calls[callID]; ok {
		c.State = state
	}
	return nil
}
