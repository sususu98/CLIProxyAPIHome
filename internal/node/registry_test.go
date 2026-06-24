package node

import (
	"testing"
	"time"
)

func TestRegistryListDoesNotUseNodeKeyAsIP(t *testing.T) {
	registry := NewRegistry()
	connectedAt := time.Now().UTC()

	registry.AddWithNodeID("", "node-1", connectedAt)

	nodes := registry.List()
	if len(nodes) != 1 {
		t.Fatalf("nodes len = %d, want 1", len(nodes))
	}
	if nodes[0].NodeID != "node-1" {
		t.Fatalf("node_id = %q, want node-1", nodes[0].NodeID)
	}
	if nodes[0].IP != "" {
		t.Fatalf("ip = %q, want empty when no client IP was recorded", nodes[0].IP)
	}
	if nodes[0].ClientCount != 1 {
		t.Fatalf("client_count = %d, want 1", nodes[0].ClientCount)
	}
}
