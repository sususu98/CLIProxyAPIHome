package home

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginstore"
	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/util"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sys/cpu"
	"gopkg.in/yaml.v3"
)

type PluginPlatform struct {
	GOOS    string `json:"goos"`
	GOARCH  string `json:"goarch"`
	Variant string `json:"variant,omitempty"`
}

type pluginStoreSyncState struct {
	Key  string
	Path string
	Hash string
	Size int64
}

type pluginStoreManifestSyncKey struct {
	ID         string         `json:"id"`
	Version    string         `json:"version"`
	ReleaseTag string         `json:"release_tag"`
	Repository string         `json:"repository"`
	SourceID   string         `json:"source_id,omitempty"`
	SourceURL  string         `json:"source_url,omitempty"`
	PluginsDir string         `json:"plugins_dir"`
	Platform   PluginPlatform `json:"platform"`
}

type pluginFileDigest struct {
	Hash string
	Size int64
}

type pluginArtifactRuntime interface {
	PluginBusy(id string) bool
	UnloadPlugin(id string) bool
}

// CurrentPluginPlatform reports the platform used by Home plugin discovery.
func CurrentPluginPlatform() PluginPlatform {
	return PluginPlatform{
		GOOS:    goruntime.GOOS,
		GOARCH:  goruntime.GOARCH,
		Variant: currentPluginCPUVariant(),
	}
}

func (r *Runtime) syncPluginStoreManifests(ctx context.Context, cfg *config.Config) error {
	if r == nil || cfg == nil || !cfg.Plugins.Enabled {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	platform := CurrentPluginPlatform()
	client := newPluginStoreClient(cfg)
	root := strings.TrimSpace(cfg.Plugins.Dir)
	if root == "" {
		root = "plugins"
	}

	r.pluginSyncMu.Lock()
	defer r.pluginSyncMu.Unlock()

	ids := make([]string, 0, len(cfg.Plugins.Configs))
	for id := range cfg.Plugins.Configs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		item := cfg.Plugins.Configs[id]
		if !pluginConfigEnabled(item) || !pluginInstanceLoadsInHome(id, item) {
			continue
		}
		manifest, okManifest, errManifest := storeManifestFromPluginConfig(id, item)
		if errManifest != nil {
			return errManifest
		}
		if !okManifest {
			continue
		}
		syncKey, errKey := pluginStoreManifestKey(root, platform, manifest)
		if errKey != nil {
			return errKey
		}
		if r.pluginStoreArtifactCurrent(root, manifest.ID, syncKey) {
			continue
		}
		result, errInstall := r.installPluginStoreManifest(ctx, client, manifest, root, platform)
		if errInstall != nil {
			return errInstall
		}
		if errMark := r.markPluginStoreSynced(root, manifest, syncKey, result); errMark != nil {
			return errMark
		}
	}
	return nil
}

func (r *Runtime) installPluginStoreManifest(ctx context.Context, client pluginstore.Client, manifest pluginstore.Manifest, root string, platform PluginPlatform) (pluginstore.InstallResult, error) {
	id := strings.TrimSpace(manifest.ID)
	if id == "" {
		return pluginstore.InstallResult{}, fmt.Errorf("home plugins: manifest plugin id is empty")
	}
	result, errInstall := client.InstallManifest(ctx, manifest, pluginStoreInstallOptions(root, platform, r.pluginRuntimeForArtifacts(), id))
	if errInstall != nil {
		return pluginstore.InstallResult{}, fmt.Errorf("home plugins: install %s: %w", id, errInstall)
	}
	return result, nil
}

func pluginRootFromConfigDir(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return "plugins"
	}
	root = filepath.Clean(root)
	return root
}

func (r *Runtime) deleteRemovedPluginArtifacts(ctx context.Context, oldCfg *config.Config, newCfg *config.Config) {
	_ = ctx
	if r == nil || oldCfg == nil {
		return
	}
	removed := removedHomeLoadedStorePluginIDs(oldCfg, newCfg)
	if len(removed) == 0 {
		return
	}
	root := strings.TrimSpace(oldCfg.Plugins.Dir)
	if root == "" {
		root = "plugins"
	}
	for _, id := range removed {
		path, deleted, errDelete := r.deletePluginArtifact(root, id)
		if errDelete != nil {
			log.Warnf("failed to delete removed home plugin %s: %v", id, errDelete)
			continue
		}
		r.clearPluginStoreSyncState(id)
		if deleted {
			log.Infof("deleted removed home plugin %s (%s)", id, path)
		}
	}
}

func (r *Runtime) deletePluginArtifact(root string, id string) (string, bool, error) {
	id = strings.TrimSpace(id)
	if !validPluginFileID(id) {
		return "", false, fmt.Errorf("invalid plugin id %q", id)
	}
	path, errPath := currentPluginFilePath(root, id)
	if errPath != nil {
		return "", false, errPath
	}
	if path == "" {
		return "", false, nil
	}
	if errPrepare := preparePluginArtifactMutation(r.pluginRuntimeForArtifacts(), id); errPrepare != nil {
		return path, false, errPrepare
	}
	if errRemove := os.Remove(path); errRemove != nil {
		if errors.Is(errRemove, os.ErrNotExist) {
			return path, false, nil
		}
		return path, false, errRemove
	}
	return path, true, nil
}

func (r *Runtime) pluginRuntimeForArtifacts() pluginArtifactRuntime {
	if r == nil || r.pluginHost == nil {
		return nil
	}
	return r.pluginHost
}

func pluginStoreInstallOptions(root string, platform PluginPlatform, runtime pluginArtifactRuntime, id string) pluginstore.InstallOptions {
	return pluginstore.InstallOptions{
		PluginsDir: root,
		GOOS:       platform.GOOS,
		GOARCH:     platform.GOARCH,
		PluginLoaded: func() bool {
			return pluginRuntimeBusy(runtime, id)
		},
		BeforeWrite: func() error {
			return preparePluginArtifactMutation(runtime, id)
		},
	}
}

func preparePluginArtifactMutation(runtime pluginArtifactRuntime, id string) error {
	if !pluginRuntimeBusy(runtime, id) {
		return nil
	}
	if runtime == nil || (!runtime.UnloadPlugin(id) && pluginRuntimeBusy(runtime, id)) {
		return pluginstore.ErrLoadedPluginLocked
	}
	return nil
}

func pluginRuntimeBusy(runtime pluginArtifactRuntime, id string) bool {
	return runtime != nil && runtime.PluginBusy(id)
}

func removedHomeLoadedStorePluginIDs(oldCfg *config.Config, newCfg *config.Config) []string {
	oldIDs := homeLoadedStorePluginIDs(oldCfg)
	if len(oldIDs) == 0 {
		return nil
	}
	newIDs := homeLoadedStorePluginIDs(newCfg)
	removed := make([]string, 0)
	for id := range oldIDs {
		if _, ok := newIDs[id]; ok {
			continue
		}
		removed = append(removed, id)
	}
	sort.Strings(removed)
	return removed
}

func homeLoadedStorePluginIDs(cfg *config.Config) map[string]struct{} {
	if cfg == nil || !cfg.Plugins.Enabled {
		return nil
	}
	out := make(map[string]struct{})
	for id, item := range cfg.Plugins.Configs {
		id = strings.TrimSpace(id)
		if id == "" || !pluginConfigEnabled(item) || !pluginInstanceLoadsInHome(id, item) {
			continue
		}
		if _, okManifest, _ := storeManifestFromPluginConfig(id, item); okManifest {
			out[id] = struct{}{}
		}
	}
	return out
}

func pluginStoreManifestKey(root string, platform PluginPlatform, manifest pluginstore.Manifest) (string, error) {
	key := pluginStoreManifestSyncKey{
		ID:         strings.TrimSpace(manifest.ID),
		Version:    strings.TrimSpace(manifest.Version),
		ReleaseTag: strings.TrimSpace(manifest.ReleaseTag),
		Repository: strings.TrimSpace(manifest.Repository),
		SourceID:   strings.TrimSpace(manifest.SourceID),
		SourceURL:  strings.TrimSpace(manifest.SourceURL),
		PluginsDir: filepath.Clean(strings.TrimSpace(root)),
		Platform: PluginPlatform{
			GOOS:    strings.TrimSpace(platform.GOOS),
			GOARCH:  strings.TrimSpace(platform.GOARCH),
			Variant: strings.TrimSpace(platform.Variant),
		},
	}
	if key.PluginsDir == "." {
		key.PluginsDir = ""
	}
	data, errMarshal := json.Marshal(key)
	if errMarshal != nil {
		return "", fmt.Errorf("home plugins: marshal sync key for %s: %w", key.ID, errMarshal)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (r *Runtime) pluginStoreArtifactCurrent(root string, id string, key string) bool {
	if r == nil || r.pluginStoreSync == nil {
		return false
	}
	id = strings.TrimSpace(id)
	state, ok := r.pluginStoreSync[id]
	if !ok || state.Key != key {
		return false
	}
	path, errPath := currentPluginFilePath(root, id)
	if errPath != nil || strings.TrimSpace(path) == "" {
		return false
	}
	if filepath.Clean(path) != filepath.Clean(state.Path) {
		return false
	}
	digest, errDigest := pluginFileDigestForPath(path)
	if errDigest != nil {
		return false
	}
	return digest.Hash == state.Hash && digest.Size == state.Size
}

func (r *Runtime) markPluginStoreSynced(root string, manifest pluginstore.Manifest, key string, result pluginstore.InstallResult) error {
	if r == nil {
		return nil
	}
	id := strings.TrimSpace(manifest.ID)
	if id == "" {
		return fmt.Errorf("home plugins: sync state plugin id is empty")
	}
	path := strings.TrimSpace(result.Path)
	if path == "" {
		var errPath error
		path, errPath = currentPluginFilePath(root, id)
		if errPath != nil {
			return fmt.Errorf("home plugins: locate synced artifact %s: %w", id, errPath)
		}
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("home plugins: synced artifact %s path is empty", id)
	}
	digest, errDigest := pluginFileDigestForPath(path)
	if errDigest != nil {
		return fmt.Errorf("home plugins: hash synced artifact %s: %w", id, errDigest)
	}
	if r.pluginStoreSync == nil {
		r.pluginStoreSync = make(map[string]pluginStoreSyncState)
	}
	r.pluginStoreSync[id] = pluginStoreSyncState{
		Key:  key,
		Path: filepath.Clean(path),
		Hash: digest.Hash,
		Size: digest.Size,
	}
	return nil
}

func (r *Runtime) clearPluginStoreSyncState(id string) {
	if r == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	r.pluginSyncMu.Lock()
	defer r.pluginSyncMu.Unlock()
	if r.pluginStoreSync != nil {
		delete(r.pluginStoreSync, id)
	}
}

func pluginFileDigestForPath(path string) (pluginFileDigest, error) {
	file, errOpen := os.Open(path)
	if errOpen != nil {
		return pluginFileDigest{}, errOpen
	}
	defer func() {
		if errClose := file.Close(); errClose != nil {
			log.WithError(errClose).Debugf("home plugins: close digest file %s", path)
		}
	}()
	hasher := sha256.New()
	size, errCopy := io.Copy(hasher, file)
	if errCopy != nil {
		return pluginFileDigest{}, errCopy
	}
	return pluginFileDigest{
		Hash: hex.EncodeToString(hasher.Sum(nil)),
		Size: size,
	}, nil
}

func currentPluginFilePath(root string, id string) (string, error) {
	paths, errPaths := pluginFilePaths(root, id)
	if errPaths != nil {
		return "", errPaths
	}
	if len(paths) == 0 {
		return "", nil
	}
	return paths[0], nil
}

func pluginFilePaths(root string, id string) ([]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "plugins"
	}
	extension := pluginExtension(goruntime.GOOS)
	out := make([]string, 0)
	for _, dir := range pluginCandidateDirs(root, goruntime.GOOS, goruntime.GOARCH, currentPluginCPUVariant()) {
		entries, errReadDir := os.ReadDir(dir)
		if errReadDir != nil {
			if errors.Is(errReadDir, os.ErrNotExist) {
				continue
			}
			return nil, errReadDir
		}
		files := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry == nil || !entry.Type().IsRegular() {
				continue
			}
			if strings.HasSuffix(strings.ToLower(entry.Name()), extension) {
				files = append(files, filepath.Join(dir, entry.Name()))
			}
		}
		sort.Strings(files)
		for _, filePath := range files {
			fileID := pluginIDFromPath(filePath)
			if fileID == id {
				out = append(out, filePath)
			}
		}
	}
	return out, nil
}

func pluginCandidateDirs(root string, goos string, goarch string, variant string) []string {
	dirs := make([]string, 0, 3)
	if variant != "" {
		dirs = append(dirs, filepath.Join(root, goos, goarch+"-"+variant))
	}
	dirs = append(dirs, filepath.Join(root, goos, goarch))
	dirs = append(dirs, root)
	return dirs
}

func pluginIDFromPath(path string) string {
	base := filepath.Base(path)
	lowerBase := strings.ToLower(base)
	for _, extension := range []string{".so", ".dylib", ".dll"} {
		if strings.HasSuffix(lowerBase, extension) {
			return base[:len(base)-len(extension)]
		}
	}
	return base
}

func validPluginFileID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return false
	}
	for _, char := range id {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '-', char == '_', char == '.':
		default:
			return false
		}
	}
	return true
}

func storeManifestFromPluginConfig(id string, item config.PluginInstanceConfig) (pluginstore.Manifest, bool, error) {
	if item.Raw.Kind == 0 {
		return pluginstore.Manifest{}, false, nil
	}
	storeNode := yamlMappingValue(&item.Raw, "store")
	if storeNode == nil || storeNode.Kind == 0 {
		return pluginstore.Manifest{}, false, nil
	}
	var manifest pluginstore.Manifest
	if errDecode := storeNode.Decode(&manifest); errDecode != nil {
		return pluginstore.Manifest{}, false, fmt.Errorf("home plugins: decode store manifest for %s: %w", id, errDecode)
	}
	if strings.TrimSpace(manifest.ID) == "" {
		manifest.ID = strings.TrimSpace(id)
	}
	if errValidate := manifest.Validate(); errValidate != nil {
		return pluginstore.Manifest{}, false, fmt.Errorf("home plugins: invalid store manifest for %s: %w", id, errValidate)
	}
	return manifest, true, nil
}

func pluginConfigEnabled(item config.PluginInstanceConfig) bool {
	return item.Enabled != nil && *item.Enabled
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		if keyNode == nil || keyNode.Value != key {
			continue
		}
		return node.Content[i+1]
	}
	return nil
}

var newPluginStoreClient = func(cfg *config.Config) pluginstore.Client {
	client := &http.Client{}
	if cfg != nil && strings.TrimSpace(cfg.ProxyURL) != "" {
		util.SetProxy(&config.SDKConfig{ProxyURL: strings.TrimSpace(cfg.ProxyURL)}, client)
	}
	return pluginstore.NewClient(client, "")
}

func currentPluginCPUVariant() string {
	if goruntime.GOARCH != "amd64" {
		return ""
	}
	if cpu.X86.HasAVX512F && cpu.X86.HasAVX512BW && cpu.X86.HasAVX512CD && cpu.X86.HasAVX512DQ && cpu.X86.HasAVX512VL {
		return "v4"
	}
	if cpu.X86.HasAVX && cpu.X86.HasAVX2 && cpu.X86.HasBMI1 && cpu.X86.HasBMI2 && cpu.X86.HasFMA {
		return "v3"
	}
	if cpu.X86.HasSSE3 && cpu.X86.HasSSSE3 && cpu.X86.HasSSE41 && cpu.X86.HasSSE42 && cpu.X86.HasPOPCNT {
		return "v2"
	}
	return "v1"
}

func pluginExtension(goos string) string {
	switch strings.ToLower(strings.TrimSpace(goos)) {
	case "darwin", "mac", "macos", "osx":
		return ".dylib"
	case "windows":
		return ".dll"
	default:
		return ".so"
	}
}
