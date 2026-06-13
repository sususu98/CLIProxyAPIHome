package config

// ForceDownstreamHomeModeConfig applies runtime overrides that downstream CPA
// nodes enforce when operating in Home mode.
func ForceDownstreamHomeModeConfig(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.APIKeys = nil
	cfg.UsageStatisticsEnabled = true
	cfg.DisableCooling = true
	cfg.WebsocketAuth = false
	cfg.EnableGeminiCLIEndpoint = false
	cfg.RemoteManagement.AllowRemote = false
	cfg.RemoteManagement.DisableControlPanel = true
}

// ApplyDownstreamHomeModeRoot applies Home-mode overrides to a persisted config
// snapshot root before it is stored or served through Management API.
func ApplyDownstreamHomeModeRoot(root map[string]any) {
	applyDownstreamHomeModeScalars(root)
	remoteManagement, ok := root["remote-management"].(map[string]any)
	if !ok || remoteManagement == nil {
		remoteManagement = make(map[string]any)
		root["remote-management"] = remoteManagement
	}
	remoteManagement["allow-remote"] = false
	remoteManagement["disable-control-panel"] = true
}

// ApplyDownstreamHomeModeYAMLScalars applies scalar Home-mode overrides to a
// config root distributed to downstream CPA nodes over RESP.
func ApplyDownstreamHomeModeYAMLScalars(root map[string]any) {
	applyDownstreamHomeModeScalars(root)
}

func applyDownstreamHomeModeScalars(root map[string]any) {
	if len(root) == 0 {
		return
	}
	delete(root, "api-keys")
	root["usage-statistics-enabled"] = true
	root["disable-cooling"] = true
	root["ws-auth"] = false
	root["enable-gemini-cli-endpoint"] = false
}
