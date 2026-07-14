package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigOptionalParsesAndSanitizesXAIKeys(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	payload := []byte(`xai-api-key:
  - api-key: " xai-key "
    priority: 9
    prefix: "/grok/"
    base-url: " https://api.x.ai/v1 "
    websockets: true
    proxy-url: "socks5://proxy.example:1080"
    headers:
      X-Test: " value "
    models:
      - name: "grok-4.5"
        alias: "grok-latest"
        display-name: "Grok Latest"
        force-mapping: true
    excluded-models:
      - " GROK-3-* "
    disable-cooling: true
  - api-key: "dropped"
    base-url: " "
`)
	if errWrite := os.WriteFile(configPath, payload, 0o600); errWrite != nil {
		t.Fatalf("WriteFile() error = %v", errWrite)
	}

	cfg, errLoad := LoadConfigOptional(configPath, false)
	if errLoad != nil {
		t.Fatalf("LoadConfigOptional() error = %v", errLoad)
	}
	if len(cfg.XAIKey) != 1 {
		t.Fatalf("XAIKey count = %d, want 1", len(cfg.XAIKey))
	}
	entry := cfg.XAIKey[0]
	if entry.APIKey != " xai-key " || entry.BaseURL != "https://api.x.ai/v1" {
		t.Fatalf("xAI key/base URL = %q/%q", entry.APIKey, entry.BaseURL)
	}
	if entry.Prefix != "grok" || entry.Priority != 9 || !entry.Websockets || !entry.DisableCooling {
		t.Fatalf("xAI routing fields = %+v", entry)
	}
	if entry.Headers["X-Test"] != "value" || len(entry.ExcludedModels) != 1 || entry.ExcludedModels[0] != "grok-3-*" {
		t.Fatalf("xAI normalized fields = headers:%v excluded:%v", entry.Headers, entry.ExcludedModels)
	}
	if len(entry.Models) != 1 {
		t.Fatalf("xAI model count = %d, want 1", len(entry.Models))
	}
	model := entry.Models[0]
	if model.Name != "grok-4.5" || model.Alias != "grok-latest" || model.DisplayName != "Grok Latest" || !model.ForceMapping {
		t.Fatalf("xAI model = %+v", model)
	}
}
