package home

import (
	"context"
	"fmt"
	"os"
	"strings"

	configaccess "github.com/router-for-me/CLIProxyAPIHome/internal/access/config_access"
	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/watcher"
	sdkaccess "github.com/router-for-me/CLIProxyAPIHome/sdk/access"
	sdkAuth "github.com/router-for-me/CLIProxyAPIHome/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPIHome/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

func (r *Runtime) startFileWatcher(ctx context.Context, configPath string) error {
	if r == nil {
		return nil
	}
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil
	}

	r.cfgMu.RLock()
	authDir := r.authDir
	r.cfgMu.RUnlock()

	w, errNew := watcher.NewWatcher(configPath, authDir, watcher.Callbacks{
		OnConfigYAMLChange: func(ctx context.Context, data []byte) {
			r.PublishConfigYAML(data)
		},
		OnConfigReload: func(ctx context.Context, cfg *config.Config) {
			if errApply := r.applyConfigAndReloadAuths(ctx, cfg); errApply != nil {
				log.Errorf("config reload apply failed: %v", errApply)
			}
		},
		OnAuthChange: func(ctx context.Context) {
			if errReload := r.loadAuths(ctx); errReload != nil {
				log.Errorf("auth reload failed: %v", errReload)
			}
		},
	})
	if errNew != nil {
		return fmt.Errorf("home runtime: init file watcher: %w", errNew)
	}

	if errStart := w.Start(ctx); errStart != nil {
		_ = w.Stop()
		return fmt.Errorf("home runtime: start file watcher: %w", errStart)
	}

	r.fileWatcher = w
	log.Infof("hot reload enabled (config=%s auth-dir=%s)", configPath, strings.TrimSpace(authDir))
	return nil
}

func (r *Runtime) applyConfigAndReloadAuths(ctx context.Context, cfg *config.Config) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	currentLevel := log.GetLevel()
	if cfg.Debug {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.InfoLevel)
	}
	if nextLevel := log.GetLevel(); nextLevel != currentLevel {
		log.Infof("log level changed from %s to %s (debug=%t)", currentLevel, nextLevel, cfg.Debug)
	}

	store := sdkAuth.GetTokenStore()
	if dirSetter, ok := store.(interface{ SetBaseDir(string) }); ok {
		dirSetter.SetBaseDir(cfg.AuthDir)
	}

	if r.coreManager != nil {
		r.coreManager.SetConfig(cfg)
		r.coreManager.SetOAuthModelAlias(cfg.OAuthModelAlias)
		r.coreManager.SetSelector(selectorFromConfig(cfg))
	}

	configaccess.Register(&cfg.SDKConfig)
	if r.accessManager != nil {
		r.accessManager.SetProviders(sdkaccess.RegisteredProviders())
	}

	if strings.TrimSpace(cfg.AuthDir) != "" {
		if errEnsure := os.MkdirAll(cfg.AuthDir, 0o755); errEnsure != nil {
			return fmt.Errorf("home runtime: ensure auth dir: %w", errEnsure)
		}
	}

	r.cfgMu.Lock()
	r.cfg = cfg
	r.authDir = cfg.AuthDir
	r.cfgMu.Unlock()

	if errReload := r.loadAuths(coreauth.WithSkipPersist(ctx)); errReload != nil {
		return errReload
	}
	return nil
}
