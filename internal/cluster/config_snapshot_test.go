package cluster

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

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

func TestRuntimeConfigFromRootPreservesPluginConfig(t *testing.T) {
	root := map[string]any{
		"plugins": map[string]any{
			"enabled": true,
			"dir":     "plugins",
			"configs": map[string]any{
				"sample": map[string]any{
					"enabled":  true,
					"priority": 7,
					"mode":     "fast",
					"nested": map[string]any{
						"value": "keep",
					},
				},
			},
		},
	}

	cfg, payload, errConfig := RuntimeConfigFromRoot(root)
	if errConfig != nil {
		t.Fatalf("RuntimeConfigFromRoot() error = %v", errConfig)
	}
	if !cfg.Plugins.Enabled {
		t.Fatal("Plugins.Enabled = false, want true")
	}
	plugin := cfg.Plugins.Configs["sample"]
	if plugin.Enabled == nil || !*plugin.Enabled {
		t.Fatalf("plugin enabled = %#v, want true", plugin.Enabled)
	}
	if plugin.Priority != 7 {
		t.Fatalf("plugin priority = %d, want 7", plugin.Priority)
	}
	raw, errMarshal := yaml.Marshal(&plugin.Raw)
	if errMarshal != nil {
		t.Fatalf("marshal plugin raw: %v", errMarshal)
	}
	if !strings.Contains(string(raw), "mode: fast") || !strings.Contains(string(raw), "value: keep") {
		t.Fatalf("plugin raw config lost custom fields:\n%s", string(raw))
	}
	if !strings.Contains(string(payload), "plugins:") || !strings.Contains(string(payload), "mode: fast") {
		t.Fatalf("runtime payload lost plugin config:\n%s", string(payload))
	}
}
