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
}

// ApplyDownstreamHomeModeScalars only applies scalar Home-mode overrides.
// It does not remove sensitive or Home-local roots such as api-keys,
// remote-management, auth-dir, tls, or credential roots. Callers that build
// downstream CPA YAML must filter those roots separately first.
func ApplyDownstreamHomeModeScalars(root map[string]any) {
	if len(root) == 0 {
		return
	}
	root["usage-statistics-enabled"] = true
	root["disable-cooling"] = true
	root["ws-auth"] = false
	root["enable-gemini-cli-endpoint"] = false
}
