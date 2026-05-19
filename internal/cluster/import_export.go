package cluster

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	appconfig "github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/watcher/synthesizer"
	"gopkg.in/yaml.v3"
)

type ImportOptions struct {
	ConfigPath string
	AuthDir    string
	Repository *Repository
	Now        time.Time
}

type ImportStats struct {
	Created     int
	Updated     int
	Unchanged   int
	Restored    int
	Overwritten int
	Skipped     int
}

type ExportOptions struct {
	OutputDir   string
	Repository  *Repository
	ConfigName  string
	AuthDirName string
}

type ExportStats struct {
	ConfigBytes int
	AuthFiles   int
}

var defaultImportTime = time.Unix(1, 0).UTC()

// ImportLocalState imports config.yaml and auth-dir credentials into the repository.
func ImportLocalState(ctx context.Context, opts ImportOptions) (ImportStats, error) {
	// Normalize source data before building the derived payload.
	stats := ImportStats{}
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Repository == nil {
		return stats, fmt.Errorf("import repository is required")
	}
	now := opts.Now
	if now.IsZero() {
		// Keep default imports stable so identical source files remain idempotent.
		now = defaultImportTime
	}

	configPath := strings.TrimSpace(opts.ConfigPath)
	if configPath == "" {
		configPath = "config.yaml"
	}
	info, errStat := os.Stat(configPath)
	if errStat != nil {
		if os.IsNotExist(errStat) {
			return stats, fmt.Errorf("config path %q does not exist", configPath)
		}
		return stats, fmt.Errorf("stat config path %q: %w", configPath, errStat)
	}
	if info.IsDir() {
		return stats, fmt.Errorf("config path %q is a directory", configPath)
	}

	rawConfig, errReadConfig := os.ReadFile(configPath)
	if errReadConfig != nil {
		return stats, fmt.Errorf("read config path %q: %w", configPath, errReadConfig)
	}
	root := map[string]any{}
	if len(rawConfig) != 0 {
		if errUnmarshalConfig := yaml.Unmarshal(rawConfig, &root); errUnmarshalConfig != nil {
			return stats, fmt.Errorf("parse config path %q: %w", configPath, errUnmarshalConfig)
		}
		if root == nil {
			root = map[string]any{}
		}
	}
	cfg, _, errRuntimeConfig := RuntimeConfigFromRoot(root)
	if errRuntimeConfig != nil {
		return stats, errRuntimeConfig
	}
	authDir := resolveImportAuthDir(configPath, cfg, opts.AuthDir)

	if errImportConfig := importConfigRoot(ctx, opts.Repository, root, &stats); errImportConfig != nil {
		return stats, errImportConfig
	}

	pendingAuths := make(map[string]*coreauth.Auth)
	authOrder := make([]string, 0, 16)
	if errConfigAuths := collectImportConfigAuths(cfg, authDir, now, pendingAuths, &authOrder, &stats); errConfigAuths != nil {
		return stats, errConfigAuths
	}
	if errFileAuths := collectImportAuthFiles(cfg, authDir, now, pendingAuths, &authOrder, &stats); errFileAuths != nil {
		return stats, errFileAuths
	}

	for _, uuid := range authOrder {
		auth := pendingAuths[uuid]
		if auth == nil {
			stats.Skipped++
			continue
		}
		_, result, errUpsertAuth := opts.Repository.UpsertAuthWithResult(ctx, auth, "upsert")
		if errUpsertAuth != nil {
			return stats, errUpsertAuth
		}
		addImportResult(&stats, result)
	}
	return stats, nil
}

// ExportLocalState exports repository config and auth files into a local directory.
func ExportLocalState(ctx context.Context, opts ExportOptions) (ExportStats, error) {
	stats := ExportStats{}
	ctx = contextOrBackground(ctx)
	if opts.Repository == nil {
		return stats, fmt.Errorf("export repository is required")
	}

	outputDir := strings.TrimSpace(opts.OutputDir)
	if outputDir == "" {
		outputDir = "."
	}
	configName := strings.TrimSpace(opts.ConfigName)
	if configName == "" {
		configName = "config.yaml"
	}
	authDirName := strings.TrimSpace(opts.AuthDirName)
	if authDirName == "" {
		authDirName = "auth"
	}
	var errValidatePath error
	configName, errValidatePath = validateExportRelativePath("ConfigName", configName)
	if errValidatePath != nil {
		return stats, errValidatePath
	}
	authDirName, errValidatePath = validateExportRelativePath("AuthDirName", authDirName)
	if errValidatePath != nil {
		return stats, errValidatePath
	}

	configPath := filepath.Join(outputDir, configName)
	authDir := filepath.Join(outputDir, authDirName)
	if errEnsureTargets := ensureExportTargetsAvailable(configPath, configName, authDir); errEnsureTargets != nil {
		return stats, errEnsureTargets
	}

	snapshot, errSnapshot := opts.Repository.LoadConfigSnapshot(ctx)
	if errSnapshot != nil {
		return stats, errSnapshot
	}
	root, errRoot := ConfigRootFromSnapshot(snapshot)
	if errRoot != nil {
		return stats, errRoot
	}
	if root == nil {
		root = make(map[string]any)
	}

	auths, errAuths := opts.Repository.ListAuths(ctx)
	if errAuths != nil {
		return stats, errAuths
	}
	sort.Slice(auths, func(i, j int) bool {
		left := ""
		right := ""
		if auths[i] != nil {
			left = auths[i].ID
		}
		if auths[j] != nil {
			right = auths[j].ID
		}
		return left < right
	})

	root["auth-dir"] = authDirName
	ApplyCredentialConfigToRoot(root, auths)
	if _, errNormalizeSecret := normalizeConfigRootSecrets(root); errNormalizeSecret != nil {
		return stats, errNormalizeSecret
	}

	data, errMarshal := yaml.Marshal(root)
	if errMarshal != nil {
		return stats, errMarshal
	}
	if len(data) == 0 {
		return stats, fmt.Errorf("exported config is empty")
	}
	if errWriteConfig := writeExportFileExclusive(configPath, configName, data, 0o600); errWriteConfig != nil {
		return stats, errWriteConfig
	}
	stats.ConfigBytes = len(data)

	authFiles, errWriteAuthFiles := writeDisbandAuthFilesExclusive(auths, authDir)
	if errWriteAuthFiles != nil {
		return stats, errWriteAuthFiles
	}
	stats.AuthFiles = authFiles
	return stats, nil
}

func validateExportRelativePath(field string, value string) (string, error) {
	value = strings.TrimSpace(value)
	cleanValue := filepath.Clean(value)
	if value == "" || value == "." || filepath.IsAbs(value) || cleanValue != value || cleanValue == ".." || strings.HasPrefix(cleanValue, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid export %s %q: path must be a clean relative path within OutputDir", field, value)
	}
	return cleanValue, nil
}

func ensureExportTargetsAvailable(configPath string, configName string, authDir string) error {
	if _, errStat := os.Stat(configPath); errStat == nil {
		return fmt.Errorf("%s already exists", exportTargetName(configName, configPath))
	} else if !os.IsNotExist(errStat) {
		return fmt.Errorf("stat export config path %q: %w", configPath, errStat)
	}

	info, errStatAuth := os.Stat(authDir)
	if errStatAuth != nil {
		if os.IsNotExist(errStatAuth) {
			return nil
		}
		return fmt.Errorf("stat export auth dir %q: %w", authDir, errStatAuth)
	}
	if !info.IsDir() {
		return fmt.Errorf("auth dir %q already exists and is not a directory", authDir)
	}
	entries, errReadDir := os.ReadDir(authDir)
	if errReadDir != nil {
		return fmt.Errorf("read export auth dir %q: %w", authDir, errReadDir)
	}
	if len(entries) > 0 {
		return fmt.Errorf("auth dir %q already exists and is not empty", authDir)
	}
	return nil
}

func writeExportFileExclusive(path string, name string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if errMkdir := os.MkdirAll(dir, 0o700); errMkdir != nil {
			return errMkdir
		}
	}
	file, errOpenFile := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if errOpenFile != nil {
		if os.IsExist(errOpenFile) {
			return fmt.Errorf("%s already exists", exportTargetName(name, path))
		}
		return errOpenFile
	}
	if _, errWriteFile := file.Write(data); errWriteFile != nil {
		if errCloseFile := file.Close(); errCloseFile != nil {
			return fmt.Errorf("write export file %q: %w; close error: %v", path, errWriteFile, errCloseFile)
		}
		return errWriteFile
	}
	if errCloseFile := file.Close(); errCloseFile != nil {
		return errCloseFile
	}
	return nil
}

func exportTargetName(name string, path string) string {
	name = strings.TrimSpace(name)
	if name != "" {
		return name
	}
	return filepath.Base(path)
}

func importConfigRoot(ctx context.Context, repo *Repository, root map[string]any, stats *ImportStats) error {
	for key, value := range root {
		normalizedKey := strings.TrimSpace(key)
		if normalizedKey == "" {
			stats.Skipped++
			continue
		}
		if normalizedKey == configAPIKeysRootKey {
			apiKeyStats, errAPIKeys := repo.UpsertAPIKeys(ctx, normalizeAPIKeysFromAny(value))
			if errAPIKeys != nil {
				return errAPIKeys
			}
			addImportAPIKeyStats(stats, apiKeyStats)
			continue
		}
		if isClusterCredentialConfigKey(normalizedKey) {
			stats.Skipped++
			continue
		}
		result, errUpsertConfigValue := repo.UpsertConfigValueWithResult(ctx, normalizedKey, value)
		if errUpsertConfigValue != nil {
			return errUpsertConfigValue
		}
		addImportResult(stats, result)
	}
	return nil
}

func collectImportConfigAuths(cfg *appconfig.Config, authDir string, now time.Time, pending map[string]*coreauth.Auth, order *[]string, stats *ImportStats) error {
	sctx := &synthesizer.SynthesisContext{
		Config:      cfg,
		AuthDir:     authDir,
		Now:         now,
		IDGenerator: synthesizer.NewStableIDGenerator(),
		ClusterMode: true,
		UUIDForAuth: func(auth *coreauth.Auth) string {
			if auth == nil || auth.Attributes == nil {
				return ""
			}
			return DeterministicAPIKeyUUID(
				auth.Provider,
				auth.Attributes["base_url"],
				APIKeyHash(auth.Attributes["api_key"]),
				auth.Attributes["compat_name"],
				auth.Attributes["provider_key"],
			)
		},
	}
	auths, errSynthesize := synthesizer.NewConfigSynthesizer().Synthesize(sctx)
	if errSynthesize != nil {
		return errSynthesize
	}
	for _, auth := range auths {
		addImportAuth(pending, order, stats, auth)
	}
	return nil
}

func collectImportAuthFiles(cfg *appconfig.Config, authDir string, now time.Time, pending map[string]*coreauth.Auth, order *[]string, stats *ImportStats) error {
	if strings.TrimSpace(authDir) == "" {
		return nil
	}
	entries, errReadDir := os.ReadDir(authDir)
	if errReadDir != nil {
		if os.IsNotExist(errReadDir) {
			return nil
		}
		return errReadDir
	}

	legacyUUIDs := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		fullPath := filepath.Join(authDir, entry.Name())
		rawPayload, errReadFile := os.ReadFile(fullPath)
		if errReadFile != nil {
			return errReadFile
		}
		updatedPayload, fileUUID, changed, errUUID := EnsureOAuthPayloadUUID(rawPayload)
		if errUUID != nil {
			return errUUID
		}
		if changed {
			if errWriteFile := os.WriteFile(fullPath, updatedPayload, 0o600); errWriteFile != nil {
				return errWriteFile
			}
		}

		currentFileUUID := fileUUID
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
			legacyUUIDs[legacyID] = currentFileUUID
			return currentFileUUID
		}

		auths := synthesizer.SynthesizeAuthFile(sctx, fullPath, updatedPayload)
		if len(auths) == 0 {
			stats.Skipped++
			continue
		}
		ApplyOriginalAuthFileName(auths, entry.Name())
		for _, auth := range auths {
			addImportAuth(pending, order, stats, auth)
		}
	}
	return nil
}

func addImportAuth(pending map[string]*coreauth.Auth, order *[]string, stats *ImportStats, auth *coreauth.Auth) {
	if auth == nil {
		stats.Skipped++
		return
	}
	uuid := strings.TrimSpace(auth.ID)
	if uuid == "" {
		stats.Skipped++
		return
	}
	if _, exists := pending[uuid]; exists {
		stats.Overwritten++
	} else {
		*order = append(*order, uuid)
	}
	pending[uuid] = auth
}

func addImportResult(stats *ImportStats, result UpsertResult) {
	switch result {
	case UpsertResultCreated:
		stats.Created++
	case UpsertResultUpdated:
		stats.Updated++
	case UpsertResultUnchanged:
		stats.Unchanged++
	case UpsertResultRestored:
		stats.Restored++
	default:
		stats.Skipped++
	}
}

func addImportAPIKeyStats(stats *ImportStats, apiKeyStats APIKeyUpsertStats) {
	stats.Created += apiKeyStats.Created
	stats.Unchanged += apiKeyStats.Unchanged
	stats.Restored += apiKeyStats.Restored
	stats.Updated += apiKeyStats.Removed
}

func resolveImportAuthDir(configPath string, cfg *appconfig.Config, explicitAuthDir string) string {
	if authDir := strings.TrimSpace(explicitAuthDir); authDir != "" {
		return authDir
	}
	authDir := ""
	if cfg != nil {
		authDir = strings.TrimSpace(cfg.AuthDir)
	}
	if authDir == "" || filepath.IsAbs(authDir) {
		return authDir
	}
	configDir := filepath.Dir(configPath)
	if configDir == "" || configDir == "." {
		return authDir
	}
	return filepath.Join(configDir, authDir)
}
