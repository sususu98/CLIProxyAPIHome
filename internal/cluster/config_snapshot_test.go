package cluster

import "testing"

func TestRuntimeConfigFromRootAppliesHomeModeScalarsAndPreservesRemoteManagement(t *testing.T) {
	root := map[string]any{
		"api-keys":                 []any{"local-key"},
		"usage-statistics-enabled": false,
		"disable-cooling":          false,
		"ws-auth":                  true,
		"remote-management": map[string]any{
			"allow-remote":          true,
			"disable-control-panel": false,
		},
	}

	cfg, _, errConfig := RuntimeConfigFromRoot(root)
	if errConfig != nil {
		t.Fatalf("RuntimeConfigFromRoot() error = %v", errConfig)
	}
	if len(cfg.APIKeys) != 0 {
		t.Fatalf("APIKeys = %#v, want nil/empty", cfg.APIKeys)
	}
	if !cfg.UsageStatisticsEnabled {
		t.Fatal("UsageStatisticsEnabled = false, want true")
	}
	if !cfg.DisableCooling {
		t.Fatal("DisableCooling = false, want true")
	}
	if cfg.WebsocketAuth {
		t.Fatal("WebsocketAuth = true, want false")
	}
	if !cfg.RemoteManagement.AllowRemote {
		t.Fatal("RemoteManagement.AllowRemote = false, want preserved true")
	}
	if cfg.RemoteManagement.DisableControlPanel {
		t.Fatal("RemoteManagement.DisableControlPanel = true, want preserved false")
	}
}
