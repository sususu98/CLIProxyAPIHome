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

func TestRuntimeConfigFromRootPreservesAdvancedPayloadModelMatchers(t *testing.T) {
	root := map[string]any{
		"payload": map[string]any{
			"default": []any{
				map[string]any{
					"models": []any{
						map[string]any{
							"name":          "gemini-*",
							"protocol":      "gemini",
							"from-protocol": "responses",
							"headers": map[string]any{
								"X-Client-Tier": "tenant-*",
							},
							"match": []any{
								map[string]any{"metadata.client": "codex"},
							},
							"not-match": []any{
								map[string]any{"metadata.mode": "dev"},
							},
							"exist":     []any{"tools.#(type==\"web_search\").type"},
							"not-exist": []any{"metadata.disable_payload"},
						},
					},
					"params": map[string]any{
						"generationConfig.thinkingConfig.thinkingBudget": 32768,
					},
				},
			},
		},
	}

	cfg, payload, errConfig := RuntimeConfigFromRoot(root)
	if errConfig != nil {
		t.Fatalf("RuntimeConfigFromRoot() error = %v", errConfig)
	}
	if len(cfg.Payload.Default) != 1 || len(cfg.Payload.Default[0].Models) != 1 {
		t.Fatalf("payload default models = %#v, want one advanced matcher", cfg.Payload.Default)
	}
	model := cfg.Payload.Default[0].Models[0]
	if model.FromProtocol != "responses" {
		t.Fatalf("FromProtocol = %q, want responses", model.FromProtocol)
	}
	if model.Headers["X-Client-Tier"] != "tenant-*" {
		t.Fatalf("Headers = %#v, want X-Client-Tier matcher", model.Headers)
	}
	if len(model.Match) != 1 || model.Match[0]["metadata.client"] != "codex" {
		t.Fatalf("Match = %#v, want metadata.client matcher", model.Match)
	}
	if len(model.NotMatch) != 1 || model.NotMatch[0]["metadata.mode"] != "dev" {
		t.Fatalf("NotMatch = %#v, want metadata.mode matcher", model.NotMatch)
	}
	if len(model.Exist) != 1 || model.Exist[0] != "tools.#(type==\"web_search\").type" {
		t.Fatalf("Exist = %#v, want web_search path", model.Exist)
	}
	if len(model.NotExist) != 1 || model.NotExist[0] != "metadata.disable_payload" {
		t.Fatalf("NotExist = %#v, want disable payload path", model.NotExist)
	}
	for _, want := range []string{"from-protocol: responses", "X-Client-Tier: tenant-*", "not-exist:"} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("runtime payload missing %q:\n%s", want, string(payload))
		}
	}
}
