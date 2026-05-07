package home

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPIHome/internal/access"
	configaccess "github.com/router-for-me/CLIProxyAPIHome/internal/access/config_access"
	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/registry"
	"github.com/router-for-me/CLIProxyAPIHome/internal/util"
	"github.com/router-for-me/CLIProxyAPIHome/internal/watcher/synthesizer"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

type Runtime struct {
	cfgMu sync.RWMutex
	cfg   *config.Config

	authDir    string
	configPath string

	configSubsMu    sync.Mutex
	nextConfigSubID uint64
	configSubs      map[uint64]func(payload []byte) error

	accessManager *access.Manager
	coreManager   *coreauth.Manager

	cancel context.CancelFunc

	fileWatcher interface{ Stop() error }
}

func NewRuntime(cfg *config.Config) (*Runtime, error) {
	if cfg == nil {
		return nil, fmt.Errorf("home runtime: config is nil")
	}

	resolvedAuthDir, errResolveAuthDir := util.ResolveAuthDir(cfg.AuthDir)
	if errResolveAuthDir != nil {
		return nil, errResolveAuthDir
	}
	if strings.TrimSpace(resolvedAuthDir) != "" {
		cfg.AuthDir = resolvedAuthDir
	}

	store := coreauth.GetTokenStore()
	if dirSetter, ok := store.(interface{ SetBaseDir(string) }); ok {
		dirSetter.SetBaseDir(cfg.AuthDir)
	}

	selector := selectorFromConfig(cfg)
	coreManager := coreauth.NewManager(store, selector, nil)
	coreManager.SetRoundTripperProvider(newDefaultRoundTripperProvider())
	coreManager.SetConfig(cfg)
	coreManager.SetOAuthModelAlias(cfg.OAuthModelAlias)

	accessManager := access.NewManager()
	configaccess.Register(&cfg.SDKConfig)
	accessManager.SetProviders(access.RegisteredProviders())

	return &Runtime{
		cfg:           cfg,
		authDir:       cfg.AuthDir,
		accessManager: accessManager,
		coreManager:   coreManager,
	}, nil
}

func (r *Runtime) Start(ctx context.Context, configPath string) error {
	if r == nil {
		return fmt.Errorf("home runtime: runtime is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	configPath = strings.TrimSpace(configPath)
	if configPath != "" {
		configPath = filepath.Clean(configPath)
		if !filepath.IsAbs(configPath) {
			if abs, errAbs := filepath.Abs(configPath); errAbs == nil {
				configPath = abs
			}
		}
	}

	r.cfgMu.Lock()
	r.configPath = configPath
	r.cfgMu.Unlock()

	runCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel

	if strings.TrimSpace(r.authDir) != "" {
		if errEnsureAuthDir := os.MkdirAll(r.authDir, 0o755); errEnsureAuthDir != nil {
			return fmt.Errorf("home runtime: ensure auth dir: %w", errEnsureAuthDir)
		}
	}

	registry.StartModelsUpdater(runCtx)
	r.registerModelRefreshCallback()

	if errLoad := r.loadAuths(runCtx); errLoad != nil {
		return errLoad
	}

	if r.coreManager != nil {
		interval := 15 * time.Minute
		r.coreManager.StartAutoRefresh(context.Background(), interval)
		log.Infof("core auth auto-refresh started (interval=%s)", interval)
	}

	if errWatch := r.startFileWatcher(runCtx, configPath); errWatch != nil {
		return errWatch
	}

	return nil
}

func (r *Runtime) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.fileWatcher != nil {
		_ = r.fileWatcher.Stop()
		r.fileWatcher = nil
	}
	if r.coreManager != nil {
		r.coreManager.StopAutoRefresh()
	}
}

func (r *Runtime) Config() *config.Config {
	if r == nil {
		return nil
	}
	r.cfgMu.RLock()
	defer r.cfgMu.RUnlock()
	return r.cfg
}

func (r *Runtime) CoreManager() *coreauth.Manager {
	if r == nil {
		return nil
	}
	return r.coreManager
}

func (r *Runtime) AccessManager() *access.Manager {
	if r == nil {
		return nil
	}
	return r.accessManager
}

func (r *Runtime) Authenticate(ctx context.Context, headers http.Header) (*access.Result, *access.AuthError) {
	return r.authenticateRequest(ctx, headers)
}

func (r *Runtime) loadAuths(ctx context.Context) error {
	if r == nil || r.coreManager == nil {
		return nil
	}

	r.cfgMu.RLock()
	cfg := r.cfg
	authDir := r.authDir
	r.cfgMu.RUnlock()
	if cfg == nil {
		return fmt.Errorf("home runtime: config is nil")
	}

	now := time.Now()
	sctx := &synthesizer.SynthesisContext{
		Config:      cfg,
		AuthDir:     authDir,
		Now:         now,
		IDGenerator: synthesizer.NewStableIDGenerator(),
	}

	ctxSkipPersist := coreauth.WithSkipPersist(ctx)

	fileSynth := synthesizer.NewFileSynthesizer()
	fileAuths, errFile := fileSynth.Synthesize(sctx)
	if errFile != nil {
		return fmt.Errorf("home runtime: synthesize auth files: %w", errFile)
	}

	configSynth := synthesizer.NewConfigSynthesizer()
	configAuths, errCfg := configSynth.Synthesize(sctx)
	if errCfg != nil {
		return fmt.Errorf("home runtime: synthesize config auths: %w", errCfg)
	}

	desired := make(map[string]*coreauth.Auth, len(fileAuths)+len(configAuths))
	for _, a := range fileAuths {
		if a == nil || strings.TrimSpace(a.ID) == "" {
			continue
		}
		desired[a.ID] = a
		r.applyCoreAuthAddOrUpdate(ctxSkipPersist, a)
	}
	for _, a := range configAuths {
		if a == nil || strings.TrimSpace(a.ID) == "" {
			continue
		}
		desired[a.ID] = a
		r.applyCoreAuthAddOrUpdate(ctxSkipPersist, a)
	}

	removed := 0
	current := r.coreManager.List()
	for _, a := range current {
		if a == nil || strings.TrimSpace(a.ID) == "" {
			continue
		}
		if _, ok := desired[a.ID]; ok {
			continue
		}
		r.applyCoreAuthRemove(ctxSkipPersist, a.ID)
		removed++
	}

	log.Infof("loaded auths (files=%d config=%d removed=%d)", len(fileAuths), len(configAuths), removed)
	return nil
}

type DispatchResult struct {
	Model       string
	AccessToken string
	BaseURL     string
	APIKey      string

	AuthID   string
	Provider string

	Auth *coreauth.Auth
}

func (r *Runtime) Dispatch(ctx context.Context, reqModel string, headers http.Header) (*DispatchResult, error) {
	if r == nil || r.coreManager == nil {
		return nil, fmt.Errorf("home runtime: core manager is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	providers := r.availableProviderKeys()
	if len(providers) == 0 {
		return nil, fmt.Errorf("home runtime: no providers available")
	}

	if !r.supportsRequestedModel(reqModel) {
		trimmedModel := strings.TrimSpace(reqModel)
		if trimmedModel == "" {
			trimmedModel = "requested model"
		}
		return nil, &coreauth.Error{Code: "model_not_found", Message: fmt.Sprintf("model %s does not exist", trimmedModel)}
	}

	opts := coreauth.Options{}
	if headers != nil {
		opts.Headers = headers.Clone()
	}
	decision, errDispatch := r.coreManager.Dispatch(ctx, providers, reqModel, opts)
	if errDispatch != nil {
		return nil, errDispatch
	}
	if decision == nil || decision.Auth == nil {
		return nil, fmt.Errorf("home runtime: dispatch returned nil auth")
	}

	auth := decision.Auth
	upstreamModel := strings.TrimSpace(decision.UpstreamModel)
	if upstreamModel == "" {
		upstreamModel = strings.TrimSpace(reqModel)
	}

	accessToken := extractAccessToken(auth)
	baseURL := ""
	apiKey := ""
	if auth.Attributes != nil {
		baseURL = strings.TrimSpace(auth.Attributes["base_url"])
		apiKey = strings.TrimSpace(auth.Attributes["api_key"])
	}

	return &DispatchResult{
		Model:       upstreamModel,
		AccessToken: accessToken,
		BaseURL:     baseURL,
		APIKey:      apiKey,
		AuthID:      auth.ID,
		Provider:    decision.Provider,
		Auth:        auth.Clone(),
	}, nil
}

func (r *Runtime) supportsRequestedModel(model string) bool {
	if r == nil {
		return false
	}
	trimmedModel := strings.TrimSpace(model)
	if trimmedModel == "" {
		return false
	}
	modelKey := strings.TrimSpace(stripModelSuffix(trimmedModel))
	if modelKey == "" {
		modelKey = trimmedModel
	}
	return registry.LookupModelInfo(modelKey) != nil
}

func stripModelSuffix(model string) string {
	lastOpen := strings.LastIndex(model, "(")
	if lastOpen == -1 {
		return model
	}
	if !strings.HasSuffix(model, ")") {
		return model
	}
	return model[:lastOpen]
}

// AddToken stores a credential JSON blob into the auth directory and schedules it for use.
// It returns the created (or existing) auth file name under auth-dir.
func (r *Runtime) AddToken(ctx context.Context, rawJSON string) (string, error) {
	if r == nil {
		return "", fmt.Errorf("home runtime: runtime is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rawJSON = strings.TrimSpace(rawJSON)
	if rawJSON == "" {
		return "", fmt.Errorf("home runtime: empty token json")
	}
	if !gjson.Valid(rawJSON) {
		return "", fmt.Errorf("home runtime: invalid token json")
	}

	r.cfgMu.RLock()
	cfg := r.cfg
	authDir := r.authDir
	r.cfgMu.RUnlock()
	if cfg == nil {
		return "", fmt.Errorf("home runtime: config is nil")
	}
	if strings.TrimSpace(authDir) == "" {
		return "", fmt.Errorf("home runtime: auth-dir is empty")
	}

	tokenType := strings.TrimSpace(gjson.Get(rawJSON, "type").String())
	if tokenType == "" {
		return "", fmt.Errorf("home runtime: token json missing type")
	}

	sum := sha256.Sum256([]byte(rawJSON))
	token := hex.EncodeToString(sum[:8])
	baseName := strings.ToLower(tokenType) + "-" + token + ".json"
	fullPath := filepath.Join(authDir, baseName)

	if errMk := os.MkdirAll(authDir, 0o755); errMk != nil {
		return "", fmt.Errorf("home runtime: create auth dir: %w", errMk)
	}

	if _, errStat := os.Stat(fullPath); errStat == nil {
		r.applyAuthFile(ctx, fullPath, []byte(rawJSON))
		return baseName, nil
	} else if !os.IsNotExist(errStat) {
		return "", fmt.Errorf("home runtime: stat auth file: %w", errStat)
	}

	if errWrite := os.WriteFile(fullPath, []byte(rawJSON), 0o600); errWrite != nil {
		return "", fmt.Errorf("home runtime: write auth file: %w", errWrite)
	}

	r.applyAuthFile(ctx, fullPath, []byte(rawJSON))
	return baseName, nil
}

func (r *Runtime) applyAuthFile(ctx context.Context, fullPath string, data []byte) {
	if r == nil || r.coreManager == nil {
		return
	}
	r.cfgMu.RLock()
	cfg := r.cfg
	authDir := r.authDir
	r.cfgMu.RUnlock()
	if cfg == nil {
		return
	}

	sctx := &synthesizer.SynthesisContext{
		Config:      cfg,
		AuthDir:     authDir,
		Now:         time.Now(),
		IDGenerator: synthesizer.NewStableIDGenerator(),
	}

	auths := synthesizer.SynthesizeAuthFile(sctx, fullPath, data)
	if len(auths) == 0 {
		return
	}

	ctxSkipPersist := coreauth.WithSkipPersist(ctx)
	for _, a := range auths {
		r.applyCoreAuthAddOrUpdate(ctxSkipPersist, a)
	}
}

func (r *Runtime) authenticateRequest(ctx context.Context, headers http.Header) (*access.Result, *access.AuthError) {
	if r == nil || r.accessManager == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	req, errReq := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/", nil)
	if errReq != nil {
		return nil, access.NewNoCredentialsError()
	}
	if headers != nil {
		req.Header = headers.Clone()
	}

	return r.accessManager.Authenticate(ctx, req)
}
