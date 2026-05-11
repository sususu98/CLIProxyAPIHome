package cluster

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	appconfig "github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/watcher/synthesizer"
	"gopkg.in/yaml.v3"
)

type BootstrapOptions struct {
	ConfigPath string
	AuthDir    string
	Config     *appconfig.Config
	Repository *Repository
	Now        time.Time
}

func Bootstrap(ctx context.Context, opts BootstrapOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Repository == nil {
		return fmt.Errorf("cluster bootstrap repository is required")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	configPath := strings.TrimSpace(opts.ConfigPath)
	if configPath == "" {
		configPath = "config.yaml"
	}
	configExists := fileExists(configPath)
	cfg := opts.Config
	if cfg == nil && configExists {
		loadedConfig, errLoadConfig := appconfig.LoadConfigOptional(configPath, false)
		if errLoadConfig != nil {
			return errLoadConfig
		}
		cfg = loadedConfig
	}

	authDir := strings.TrimSpace(opts.AuthDir)
	if authDir == "" && cfg != nil {
		authDir = strings.TrimSpace(cfg.AuthDir)
	}

	configImported, errImportConfig := bootstrapConfig(ctx, opts.Repository, configPath, configExists)
	if errImportConfig != nil {
		return errImportConfig
	}
	if cfg != nil {
		if errImportConfigAuths := bootstrapConfigAuths(ctx, opts.Repository, cfg, authDir, now); errImportConfigAuths != nil {
			return errImportConfigAuths
		}
	}
	authFilesToDelete, errImportFiles := bootstrapAuthFiles(ctx, opts.Repository, cfg, authDir, now)
	if errImportFiles != nil {
		return errImportFiles
	}

	for _, authFile := range authFilesToDelete {
		if errRemove := os.Remove(authFile); errRemove != nil && !os.IsNotExist(errRemove) {
			return errRemove
		}
	}
	if configExists || configImported {
		if errRemove := os.Remove(configPath); errRemove != nil && !os.IsNotExist(errRemove) {
			return errRemove
		}
	}
	return nil
}

func bootstrapConfig(ctx context.Context, repo *Repository, configPath string, configExists bool) (bool, error) {
	snapshot, errSnapshot := repo.LoadConfigSnapshot(ctx)
	if errSnapshot != nil {
		return false, errSnapshot
	}
	if len(snapshot) != 0 || !configExists {
		return false, nil
	}

	rawConfig, errRead := os.ReadFile(configPath)
	if errRead != nil {
		return false, errRead
	}
	var root map[string]any
	if errUnmarshal := yaml.Unmarshal(rawConfig, &root); errUnmarshal != nil {
		return false, errUnmarshal
	}
	imported := false
	for key, value := range root {
		if isBootstrapCredentialConfigKey(key) {
			continue
		}
		if errUpsertConfigValue := repo.UpsertConfigValue(ctx, key, value); errUpsertConfigValue != nil {
			return false, errUpsertConfigValue
		}
		imported = true
	}
	return imported, nil
}

func bootstrapConfigAuths(ctx context.Context, repo *Repository, cfg *appconfig.Config, authDir string, now time.Time) error {
	sctx := &synthesizer.SynthesisContext{
		Config:      cfg,
		AuthDir:     authDir,
		Now:         now,
		IDGenerator: synthesizer.NewStableIDGenerator(),
		ClusterMode: true,
		UUIDForAuth: func(auth *coreauth.Auth) string {
			if auth == nil {
				return ""
			}
			attrs := auth.Attributes
			if attrs == nil {
				return ""
			}
			return DeterministicAPIKeyUUID(
				auth.Provider,
				attrs["base_url"],
				APIKeyHash(attrs["api_key"]),
				attrs["compat_name"],
				attrs["provider_key"],
			)
		},
	}
	auths, errSynthesize := synthesizer.NewConfigSynthesizer().Synthesize(sctx)
	if errSynthesize != nil {
		return errSynthesize
	}
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		if _, errUpsertAuth := repo.UpsertAuth(ctx, auth, "create"); errUpsertAuth != nil {
			return errUpsertAuth
		}
	}
	return nil
}

func bootstrapAuthFiles(ctx context.Context, repo *Repository, cfg *appconfig.Config, authDir string, now time.Time) ([]string, error) {
	if strings.TrimSpace(authDir) == "" {
		return nil, nil
	}
	entries, errReadDir := os.ReadDir(authDir)
	if errReadDir != nil {
		if os.IsNotExist(errReadDir) {
			return nil, nil
		}
		return nil, errReadDir
	}

	authFilesToDelete := make([]string, 0, len(entries))
	legacyUUIDs := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		fullPath := filepath.Join(authDir, entry.Name())
		rawPayload, errReadFile := os.ReadFile(fullPath)
		if errReadFile != nil {
			return nil, errReadFile
		}
		updatedPayload, fileUUID, changed, errUUID := EnsureOAuthPayloadUUID(rawPayload)
		if errUUID != nil {
			return nil, errUUID
		}
		if changed {
			if errWriteFile := os.WriteFile(fullPath, updatedPayload, 0o600); errWriteFile != nil {
				return nil, errWriteFile
			}
		}

		sctx := &synthesizer.SynthesisContext{
			Config:      cfg,
			AuthDir:     authDir,
			Now:         now,
			IDGenerator: synthesizer.NewStableIDGenerator(),
			ClusterMode: true,
		}
		sctx.UUIDForAuth = func(auth *coreauth.Auth) string {
			if auth == nil {
				return ""
			}
			legacyID := auth.ID
			if auth.Attributes != nil {
				if parentID := strings.TrimSpace(auth.Attributes["gemini_virtual_parent"]); parentID != "" {
					parentUUID := strings.TrimSpace(legacyUUIDs[parentID])
					if parentUUID == "" {
						parentUUID = parentID
					}
					auth.Attributes["gemini_virtual_parent"] = parentUUID
					if auth.Metadata != nil {
						auth.Metadata["virtual_parent_id"] = parentUUID
					}
					return DeterministicVirtualUUID(parentUUID, auth.Attributes["gemini_virtual_project"])
				}
			}
			legacyUUIDs[legacyID] = fileUUID
			return fileUUID
		}

		auths := synthesizer.SynthesizeAuthFile(sctx, fullPath, updatedPayload)
		for _, auth := range auths {
			if auth == nil {
				continue
			}
			if _, errUpsertAuth := repo.UpsertAuth(ctx, auth, "create"); errUpsertAuth != nil {
				return nil, errUpsertAuth
			}
		}
		authFilesToDelete = append(authFilesToDelete, fullPath)
	}
	return authFilesToDelete, nil
}

func isBootstrapCredentialConfigKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "auth-dir", "gemini-api-key", "vertex-api-key", "codex-api-key", "claude-api-key", "openai-compatibility":
		return true
	default:
		return false
	}
}

func fileExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, errStat := os.Stat(path)
	return errStat == nil && !info.IsDir()
}
