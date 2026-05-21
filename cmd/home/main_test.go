package main

import (
	"context"
	"strings"
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

func TestResolveSQLitePath_UsesFlagOverride(t *testing.T) {
	t.Parallel()

	got := resolveSQLitePath("flag.db", "config.db")
	if got != "flag.db" {
		t.Fatalf("resolveSQLitePath() = %q, want flag.db", got)
	}
}

func TestResolveSQLitePath_UsesConfigFallback(t *testing.T) {
	t.Parallel()

	got := resolveSQLitePath("", "config.db")
	if got != "config.db" {
		t.Fatalf("resolveSQLitePath() = %q, want config.db", got)
	}
}

func TestResolveSQLitePath_UsesDefault(t *testing.T) {
	t.Parallel()

	got := resolveSQLitePath("", "")
	if got != "home.db" {
		t.Fatalf("resolveSQLitePath() = %q, want home.db", got)
	}
}

func TestExportOptionsForDir_UsesDefaultAuthDirWithoutOutputDir(t *testing.T) {
	t.Parallel()

	opts := exportOptionsForDir("", nil)
	if opts.OutputDir != "" {
		t.Fatalf("OutputDir = %q, want empty", opts.OutputDir)
	}
	if opts.AuthDirName != "" {
		t.Fatalf("AuthDirName = %q, want empty for backend default", opts.AuthDirName)
	}
}

func TestExportOptionsForDir_UsesAuthsForExplicitOutputDir(t *testing.T) {
	t.Parallel()

	opts := exportOptionsForDir("out", nil)
	if opts.OutputDir != "out" {
		t.Fatalf("OutputDir = %q, want out", opts.OutputDir)
	}
	if opts.AuthDirName != "auths" {
		t.Fatalf("AuthDirName = %q, want auths", opts.AuthDirName)
	}
}

func TestResolveDatabaseNodeIP_RejectsClusterSQLiteWithoutExternalIP(t *testing.T) {
	t.Parallel()

	cfg := &cluster.Config{
		SQLite: cluster.SQLiteConfig{Path: "home.db"},
	}
	got, errNodeIP := resolveDatabaseNodeIP(context.Background(), nil, cluster.DatabaseBackendSQLite, cfg, true)
	if errNodeIP == nil {
		t.Fatalf("resolveDatabaseNodeIP() error = nil, want node.external-ip error")
	}
	if got != "" {
		t.Fatalf("resolveDatabaseNodeIP() = %q, want empty ip on error", got)
	}
	if !strings.Contains(errNodeIP.Error(), "node.external-ip is required when cluster uses sqlite backend") {
		t.Fatalf("resolveDatabaseNodeIP() error = %v, want node.external-ip sqlite error", errNodeIP)
	}
}

func TestResolveDatabaseNodeIP_UsesLoopbackForNonClusterSQLite(t *testing.T) {
	t.Parallel()

	got, errNodeIP := resolveDatabaseNodeIP(context.Background(), nil, cluster.DatabaseBackendSQLite, nil, false)
	if errNodeIP != nil {
		t.Fatalf("resolveDatabaseNodeIP() error = %v", errNodeIP)
	}
	if got != "127.0.0.1" {
		t.Fatalf("resolveDatabaseNodeIP() = %q, want 127.0.0.1", got)
	}
}
