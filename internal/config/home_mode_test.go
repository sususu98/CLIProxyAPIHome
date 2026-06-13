package config

import "testing"

func TestForceDownstreamHomeModeConfig(t *testing.T) {
	cfg := &Config{}
	cfg.APIKeys = []string{"local-key"}
	cfg.UsageStatisticsEnabled = false
	cfg.DisableCooling = false
	cfg.WebsocketAuth = true
	cfg.EnableGeminiCLIEndpoint = true
	cfg.RemoteManagement.AllowRemote = true
	cfg.RemoteManagement.DisableControlPanel = false

	ForceDownstreamHomeModeConfig(cfg)

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
	if cfg.EnableGeminiCLIEndpoint {
		t.Fatal("EnableGeminiCLIEndpoint = true, want false")
	}
	if cfg.RemoteManagement.AllowRemote {
		t.Fatal("RemoteManagement.AllowRemote = true, want false")
	}
	if !cfg.RemoteManagement.DisableControlPanel {
		t.Fatal("RemoteManagement.DisableControlPanel = false, want true")
	}
}

func TestApplyDownstreamHomeModeRoot(t *testing.T) {
	root := map[string]any{
		"api-keys":                   []any{"local-key"},
		"usage-statistics-enabled":   false,
		"disable-cooling":            false,
		"enable-gemini-cli-endpoint": true,
		"remote-management": map[string]any{
			"allow-remote":          true,
			"disable-control-panel": false,
		},
	}

	ApplyDownstreamHomeModeRoot(root)

	if _, ok := root["api-keys"]; ok {
		t.Fatal("api-keys should be removed from downstream config root")
	}
	if root["usage-statistics-enabled"] != true {
		t.Fatalf("usage-statistics-enabled = %v, want true", root["usage-statistics-enabled"])
	}
	if root["disable-cooling"] != true {
		t.Fatalf("disable-cooling = %v, want true", root["disable-cooling"])
	}
	if root["ws-auth"] != false {
		t.Fatalf("ws-auth = %v, want false", root["ws-auth"])
	}
	if root["enable-gemini-cli-endpoint"] != false {
		t.Fatalf("enable-gemini-cli-endpoint = %v, want false", root["enable-gemini-cli-endpoint"])
	}
	remoteManagement, ok := root["remote-management"].(map[string]any)
	if !ok {
		t.Fatalf("remote-management = %#v, want map", root["remote-management"])
	}
	if remoteManagement["allow-remote"] != false {
		t.Fatalf("allow-remote = %v, want false", remoteManagement["allow-remote"])
	}
	if remoteManagement["disable-control-panel"] != true {
		t.Fatalf("disable-control-panel = %v, want true", remoteManagement["disable-control-panel"])
	}
}

func TestApplyDownstreamHomeModeYAMLScalars(t *testing.T) {
	root := map[string]any{
		"api-keys":                 []any{"local-key"},
		"usage-statistics-enabled": false,
	}
	ApplyDownstreamHomeModeYAMLScalars(root)
	if _, ok := root["api-keys"]; ok {
		t.Fatal("api-keys should be removed from downstream yaml root")
	}
	if root["usage-statistics-enabled"] != true {
		t.Fatalf("usage-statistics-enabled = %v, want true", root["usage-statistics-enabled"])
	}
	if _, ok := root["remote-management"]; ok {
		t.Fatal("remote-management should not be injected into downstream yaml root")
	}
}
