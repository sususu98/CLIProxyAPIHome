package cluster

import (
	"context"
	"encoding/json"
	"strings"

	appconfig "github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"gopkg.in/yaml.v3"
)

// LoadConfigAsRuntimeConfig loads a config as runtime config.
func (r *Repository) LoadConfigAsRuntimeConfig(ctx context.Context) (*appconfig.Config, []byte, error) {
	// Normalize source data before building the derived payload.
	snapshot, errSnapshot := r.LoadConfigSnapshot(ctx)
	if errSnapshot != nil {
		return nil, nil, errSnapshot
	}
	root, errRoot := ConfigRootFromSnapshot(snapshot)
	if errRoot != nil {
		return nil, nil, errRoot
	}
	secretChanged, errSecret := normalizeConfigRootSecrets(root)
	if errSecret != nil {
		return nil, nil, errSecret
	}
	cfg, payload, errConfig := RuntimeConfigFromRoot(root)
	if errConfig != nil {
		return nil, nil, errConfig
	}
	if secretChanged {
		if errUpsert := r.UpsertConfigValue(ctx, "remote-management", root["remote-management"]); errUpsert != nil {
			return nil, nil, errUpsert
		}
	}
	return cfg, payload, nil
}

// ConfigRootFromSnapshot derives config root from snapshot.
func ConfigRootFromSnapshot(snapshot map[string]json.RawMessage) (map[string]any, error) {
	root := make(map[string]any, len(snapshot))
	for key, raw := range snapshot {
		if isClusterCredentialConfigKey(key) {
			continue
		}
		var value any
		if len(raw) > 0 {
			if errUnmarshal := json.Unmarshal(raw, &value); errUnmarshal != nil {
				return nil, errUnmarshal
			}
		}
		root[key] = value
	}
	return root, nil
}

// RuntimeConfigFromRoot derives runtime config from root.
func RuntimeConfigFromRoot(root map[string]any) (*appconfig.Config, []byte, error) {
	// Normalize source data before building the derived payload.
	if _, errSecret := normalizeConfigRootSecrets(root); errSecret != nil {
		return nil, nil, errSecret
	}
	data, errMarshal := yaml.Marshal(root)
	if errMarshal != nil {
		return nil, nil, errMarshal
	}
	cfg := &appconfig.Config{}
	cfg.Pprof.Addr = appconfig.DefaultPprofAddr
	cfg.RemoteManagement.PanelGitHubRepository = appconfig.DefaultPanelGitHubRepository
	cfg.ErrorLogsMaxFiles = 10
	cfg.RedisUsageQueueRetentionSeconds = 60
	if errUnmarshal := yaml.Unmarshal(data, cfg); errUnmarshal != nil {
		return nil, nil, errUnmarshal
	}
	cfg.SanitizeGeminiKeys()
	cfg.SanitizeVertexCompatKeys()
	cfg.SanitizeCodexKeys()
	cfg.SanitizeCodexHeaderDefaults()
	cfg.SanitizeClaudeHeaderDefaults()
	cfg.SanitizeClaudeKeys()
	cfg.SanitizeOpenAICompatibility()
	cfg.SanitizeOAuthModelAlias()
	cfg.SanitizePayloadRules()
	if cfg.Pprof.Addr == "" {
		cfg.Pprof.Addr = appconfig.DefaultPprofAddr
	}
	if cfg.RemoteManagement.PanelGitHubRepository == "" {
		cfg.RemoteManagement.PanelGitHubRepository = appconfig.DefaultPanelGitHubRepository
	}
	appconfig.ForceDownstreamHomeModeConfig(cfg)
	return cfg, data, nil
}

// normalizeConfigRootSecrets normalizes a config root secrets.
func normalizeConfigRootSecrets(root map[string]any) (bool, error) {
	// Normalize source data before building the derived payload.
	if len(root) == 0 {
		return false, nil
	}
	rawRemoteManagement, ok := root["remote-management"]
	if !ok || rawRemoteManagement == nil {
		return false, nil
	}
	remoteManagement, ok := rawRemoteManagement.(map[string]any)
	if !ok {
		return false, nil
	}
	rawSecret, ok := remoteManagement["secret-key"]
	if !ok || rawSecret == nil {
		return false, nil
	}
	secret, ok := rawSecret.(string)
	if !ok {
		return false, nil
	}
	normalizedSecret, changed, errNormalizeSecret := appconfig.NormalizeRemoteManagementSecret(secret)
	if errNormalizeSecret != nil {
		return false, errNormalizeSecret
	}
	if !changed {
		return false, nil
	}
	remoteManagement["secret-key"] = normalizedSecret
	root["remote-management"] = remoteManagement
	return true, nil
}

// isClusterCredentialConfigKey reports whether cluster credential config key.
func isClusterCredentialConfigKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "auth-dir", "gemini-api-key", "vertex-api-key", "codex-api-key", "claude-api-key", "openai-compatibility":
		return true
	default:
		return false
	}
}
