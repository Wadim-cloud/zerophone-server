package cluster

import (
	"testing"
)

func TestNewNodeRegistry(t *testing.T) {
	registry := NewNodeRegistry()
	if registry == nil {
		t.Error("NewNodeRegistry should not return nil")
	}
	if len(registry.nodes) != 0 {
		t.Error("New registry should be empty")
	}
}

func TestNodeRegistryAddGet(t *testing.T) {
	registry := NewNodeRegistry()

	node := &Node{
		ID:         "test-node-1",
		Name:       "Test Node",
		ZeroTierIP: "10.0.0.1",
		Status:     "online",
	}

	registry.Add(node)

	retrieved, ok := registry.Get("test-node-1")
	if !ok {
		t.Error("Node should be found after add")
	}
	if retrieved.Name != "Test Node" {
		t.Errorf("Expected name 'Test Node', got '%s'", retrieved.Name)
	}
}

func TestNodeRegistryGetAll(t *testing.T) {
	registry := NewNodeRegistry()

	registry.Add(&Node{ID: "node-1", Name: "Node 1"})
	registry.Add(&Node{ID: "node-2", Name: "Node 2"})

	all := registry.GetAll()
	if len(all) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(all))
	}
}

func TestGenerateUUID(t *testing.T) {
	uuid1 := generateUUID()
	uuid2 := generateUUID()

	if uuid1 == "" {
		t.Error("UUID should not be empty")
	}
	if len(uuid1) != 32 {
		t.Errorf("Expected 32 hex chars, got %d", len(uuid1))
	}
	if uuid1 == uuid2 {
		t.Error("Generated UUIDs should be unique")
	}
}
