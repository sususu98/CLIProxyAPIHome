package managementasset

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/util"
	log "github.com/sirupsen/logrus"
)

const (
	assetsDirName = "assets"
	indexAssetName = "index.html"
	managementAssetName = "management.html"
	userAssetName = "user.html"
)

// AssetsDirName is the directory that stores hashed static assets.
const AssetsDirName = assetsDirName

// IndexFileName exposes the embedded entry asset filename.
const IndexFileName = indexAssetName

// ManagementFileName exposes the administrator control panel asset filename.
const ManagementFileName = managementAssetName

// UserFileName exposes the user workspace asset filename.
const UserFileName = userAssetName

// PanelFileNames lists all embedded control panel assets.
var PanelFileNames = []string{IndexFileName, ManagementFileName, UserFileName}

// SetCurrentConfig is retained for compatibility with runtime wiring.
func SetCurrentConfig(cfg *config.Config) {
	_ = cfg
}

// StartAutoUpdater is retained for compatibility but does not perform remote updates.
func StartAutoUpdater(ctx context.Context, configFilePath string) {
	_ = ctx
	_ = configFilePath
	log.Debug("management asset auto-updater disabled: embedded assets are bundled at build time")
}

// StaticDir resolves the directory that stores the control panel assets.
func StaticDir(configFilePath string) string {
	if override := strings.TrimSpace(os.Getenv("MANAGEMENT_STATIC_PATH")); override != "" {
		return filepath.Clean(override)
	}

	if writable := util.WritablePath(); writable != "" {
		return filepath.Join(writable, "static")
	}

	configFilePath = strings.TrimSpace(configFilePath)
	if configFilePath == "" {
		return ""
	}

	base := filepath.Dir(configFilePath)
	if fileInfo, err := os.Stat(configFilePath); err == nil && fileInfo.IsDir() {
		base = configFilePath
	}

	return filepath.Join(base, "static")
}

// FilePath resolves the absolute path to the management control panel asset.
func FilePath(configFilePath string) string {
	return FilePathFor(configFilePath, ManagementFileName)
}

// FilePathFor resolves the absolute path to a specific embedded control panel asset.
func FilePathFor(configFilePath string, assetName string) string {
	assetName = normalizePanelAssetName(assetName)
	if assetName == "" {
		return ""
	}

	dir := StaticDir(configFilePath)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, assetName)
}

// AssetPathFor resolves a path under the embedded control panel assets directory.
func AssetPathFor(configFilePath string, assetPath string) string {
	assetPath = strings.TrimSpace(assetPath)
	if assetPath == "" {
		return ""
	}

	assetPath = strings.TrimPrefix(assetPath, "/")
	cleaned := filepath.Clean(filepath.FromSlash(assetPath))
	if cleaned == "." || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return ""
	}

	dir := StaticDir(configFilePath)
	if dir == "" {
		return ""
	}

	assetsRoot := filepath.Join(dir, AssetsDirName)
	resolved := filepath.Join(assetsRoot, cleaned)
	if resolved != assetsRoot && !strings.HasPrefix(resolved, assetsRoot+string(filepath.Separator)) {
		return ""
	}
	return resolved
}

func normalizePanelAssetName(assetName string) string {
	assetName = strings.TrimSpace(assetName)
	if assetName == "" {
		return ""
	}

	base := filepath.Base(filepath.Clean(assetName))
	for _, panelAssetName := range PanelFileNames {
		if strings.EqualFold(base, panelAssetName) {
			return panelAssetName
		}
	}
	return ""
}
