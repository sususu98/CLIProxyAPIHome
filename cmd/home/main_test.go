package main

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
)

func TestResolveClusterAdvertisedPortUsesExternalPort(t *testing.T) {
	t.Parallel()

	cfg := &cluster.Config{
		Node: cluster.NodeConfig{
			ExternalPort: 443,
			Port:         8327,
		},
	}

	port, errPort := resolveClusterAdvertisedPort(cfg, 8327)
	if errPort != nil {
		t.Fatalf("resolveClusterAdvertisedPort failed: %v", errPort)
	}
	if port != 443 {
		t.Fatalf("advertised port = %d, want 443", port)
	}
}

func TestResolveClusterAdvertisedPortFallsBackToListenPort(t *testing.T) {
	t.Parallel()

	cfg := &cluster.Config{
		Node: cluster.NodeConfig{
			Port: 8327,
		},
	}

	port, errPort := resolveClusterAdvertisedPort(cfg, 18327)
	if errPort != nil {
		t.Fatalf("resolveClusterAdvertisedPort failed: %v", errPort)
	}
	if port != 18327 {
		t.Fatalf("advertised port = %d, want 18327", port)
	}
}
