package home

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	homeerrors "github.com/router-for-me/CLIProxyAPIHome/internal/errors"
	"github.com/router-for-me/CLIProxyAPIHome/internal/managementasset"
	"github.com/router-for-me/CLIProxyAPIHome/internal/registry"
	"github.com/router-for-me/CLIProxyAPIHome/internal/util"
	"github.com/router-for-me/CLIProxyAPIHome/internal/watcher/synthesizer"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type Runtime struct {
	cfgMu sync.RWMutex
	cfg   *config.Config

	authDir    string
	configPath string

	configSubsMu    sync.Mutex
	nextConfigSubID uint64
	configSubs      map[uint64]func(payload []byte) error

	accessManager  *access.Manager
	coreManager    *coreauth.Manager
	clusterAdapter ClusterAdapter
	clusterRefresh func(context.Context, string) ([]byte, error)
	originalStore  coreauth.Store

	clusterUsageQueueMu sync.Mutex
	clusterUsageQueue   *usagePayloadQueue

	cancel context.CancelFunc

	fileWatcher interface{ Stop() error }
}

type ClusterAdapter interface {
	Enabled() bool
	LoadAuthIndex(ctx context.Context) error
	ListMinimalAuths() []*coreauth.Auth
	GetFullAuth(ctx context.Context, uuid string) (*coreauth.Auth, error)
	LoadConfigYAML(ctx context.Context) ([]byte, error)
}

type clusterUsageStore interface {
	StoreUsagePayload(ctx context.Context, payload string) error
}

type appLogStore interface {
	StoreAppLogPayload(ctx context.Context, clientIP string, payload string) error
}

type channelScopedAuthStore interface {
	AllowedAuthIDsForAPIKey(ctx context.Context, apiKey string) ([]string, error)
}

type modelScopedAuthStore interface {
	AllowedModelIDsForAPIKey(ctx context.Context, apiKey string) ([]string, error)
}

type apiKeyScopedDispatchStore interface {
	AllowedDispatchIDsForAPIKey(ctx context.Context, apiKey string) ([]string, []string, error)
}

// NewRuntime creates a new runtime.
func NewRuntime(cfg *config.Config) (*Runtime, error) {
	// Keep validation before state changes so failures leave existing data intact.
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

	runtime := &Runtime{
		cfg:           cfg,
		authDir:       cfg.AuthDir,
		accessManager: accessManager,
		coreManager:   coreManager,
		originalStore: store,
	}
	runtime.refreshAccessProviders()
	return runtime, nil
}

// SetClusterAdapter sets a cluster adapter.
func (r *Runtime) SetClusterAdapter(adapter ClusterAdapter) {
	if r == nil {
		return
	}
	r.clusterAdapter = adapter
	r.refreshAccessProviders()
	if r.coreManager != nil {
		if adapter != nil && adapter.Enabled() {
			r.coreManager.SetFullAuthResolver(adapter)
			if store, ok := adapter.(coreauth.Store); ok {
				r.coreManager.SetStore(store)
			}
		} else {
			r.coreManager.SetFullAuthResolver(nil)
			r.coreManager.SetStore(r.originalStore)
		}
	}
}

// SetClusterRefreshHandler sets a cluster refresh handler.
func (r *Runtime) SetClusterRefreshHandler(handler func(context.Context, string) ([]byte, error)) {
	if r == nil {
		return
	}
	r.clusterRefresh = handler
}

// Start starts the process.
func (r *Runtime) Start(ctx context.Context, configPath string) error {
	// Keep validation before state changes so failures leave existing data intact.
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
	r.startClusterUsageWriter(runCtx)

	if !r.clusterAutoRefreshGated() && strings.TrimSpace(r.authDir) != "" {
		if errEnsureAuthDir := os.MkdirAll(r.authDir, 0o755); errEnsureAuthDir != nil {
			return fmt.Errorf("home runtime: ensure auth dir: %w", errEnsureAuthDir)
		}
	}

	registry.StartModelsUpdater(runCtx)
	r.registerModelRefreshCallback()
	managementasset.SetCurrentConfig(r.cfg)
	managementasset.StartAutoUpdater(context.Background(), configPath)

	if errLoad := r.loadAuths(runCtx); errLoad != nil {
		return errLoad
	}

	clusterMode := r.clusterAutoRefreshGated()
	if clusterMode {
		log.Infof("core auth auto-refresh waiting for cluster master")
	} else {
		r.StartAutoRefresh(context.Background())
	}

	if clusterMode {
		log.Infof("hot reload file watcher disabled in cluster mode")
	} else {
		if errWatch := r.startFileWatcher(runCtx, configPath); errWatch != nil {
			return errWatch
		}
	}

	return nil
}

// Stop stops the process.
func (r *Runtime) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.stopClusterUsageWriter()
	if r.fileWatcher != nil {
		_ = r.fileWatcher.Stop()
		r.fileWatcher = nil
	}
	r.StopAutoRefresh()
	if r.coreManager != nil {
		r.coreManager.Shutdown()
	}
}

// StartAutoRefresh starts an auto refresh.
func (r *Runtime) StartAutoRefresh(ctx context.Context) {
	if r == nil || r.coreManager == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	interval := 15 * time.Minute
	r.coreManager.StartAutoRefresh(ctx, interval)
	log.Infof("core auth auto-refresh started (interval=%s)", interval)
}

// StopAutoRefresh stops an auto refresh.
func (r *Runtime) StopAutoRefresh() {
	if r == nil || r.coreManager == nil {
		return
	}
	r.coreManager.StopAutoRefresh()
}

// clusterAutoRefreshGated handles a cluster auto refresh gated.
func (r *Runtime) clusterAutoRefreshGated() bool {
	return r != nil && r.clusterAdapter != nil && r.clusterAdapter.Enabled()
}

// Config handles a config.
func (r *Runtime) Config() *config.Config {
	if r == nil {
		return nil
	}
	r.cfgMu.RLock()
	defer r.cfgMu.RUnlock()
	return r.cfg
}

// CoreManager handles a core manager.
func (r *Runtime) CoreManager() *coreauth.Manager {
	if r == nil {
		return nil
	}
	return r.coreManager
}

// RefreshNow refreshes refresh now.
func (r *Runtime) RefreshNow(ctx context.Context, authIndex string) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("home runtime: runtime is nil")
	}
	if r.clusterRefresh != nil {
		return r.clusterRefresh(ctx, authIndex)
	}
	return r.RefreshNowLocal(ctx, authIndex)
}

// RefreshNowLocal refreshes refresh now local.
func (r *Runtime) RefreshNowLocal(ctx context.Context, authIndex string) ([]byte, error) {
	if r == nil || r.coreManager == nil {
		return nil, fmt.Errorf("home runtime: runtime not ready")
	}
	updated, errRefresh := r.coreManager.RefreshNow(ctx, authIndex)
	if errRefresh != nil {
		return nil, errRefresh
	}
	return BuildRefreshPayload(updated)
}

// UpdateAuthInMemory updates an auth in memory.
func (r *Runtime) UpdateAuthInMemory(ctx context.Context, auth *coreauth.Auth) (*coreauth.Auth, error) {
	if r == nil || r.coreManager == nil {
		return nil, fmt.Errorf("home runtime: runtime not ready")
	}
	return r.coreManager.Update(coreauth.WithSkipPersist(ctx), auth)
}

// RefreshClusterAuthIndex refreshes refresh cluster auth index.
func (r *Runtime) RefreshClusterAuthIndex(ctx context.Context, uuid string) error {
	if r == nil || r.clusterAdapter == nil {
		return nil
	}
	refresher, ok := r.clusterAdapter.(interface {
		RefreshAuthIndex(context.Context, string) error
	})
	if !ok || refresher == nil {
		return nil
	}
	return refresher.RefreshAuthIndex(ctx, uuid)
}

// PersistClusterUsagePayload stores persist cluster usage payload.
func (r *Runtime) PersistClusterUsagePayload(ctx context.Context, payload string) (bool, error) {
	if r == nil || r.clusterAdapter == nil || !r.clusterAdapter.Enabled() {
		return false, nil
	}
	queue := r.getClusterUsageQueue()
	if queue == nil {
		return true, nil
	}
	if ok := queue.Push(payload); !ok {
		log.Warnf("cluster usage queue is stopped; accepting usage without persistence")
	}
	return true, nil
}

// PersistAppLogPayload stores a CPA app log payload in the runtime database.
func (r *Runtime) PersistAppLogPayload(ctx context.Context, clientIP string, payload string) (bool, error) {
	if r == nil || r.clusterAdapter == nil || !r.clusterAdapter.Enabled() {
		return false, nil
	}
	store, ok := r.clusterAdapter.(appLogStore)
	if !ok || store == nil {
		return false, fmt.Errorf("app log store is unavailable")
	}
	return true, store.StoreAppLogPayload(ctx, clientIP, payload)
}

// BuildRefreshPayload builds a build refresh payload.
func BuildRefreshPayload(updated *coreauth.Auth) ([]byte, error) {
	// Resolve credential context before calling upstream OAuth services.
	if updated == nil {
		return nil, fmt.Errorf("auth manager: auth not found")
	}
	auth := SanitizeAuthForDownstream(updated)
	if auth == nil {
		return nil, fmt.Errorf("auth manager: auth not found")
	}
	authJSON, errMarshal := json.Marshal(auth)
	if errMarshal != nil {
		return nil, errMarshal
	}

	authIndex := strings.TrimSpace(auth.EnsureIndex())
	if authIndex == "" {
		return nil, fmt.Errorf("auth manager: auth not found")
	}
	authJSON, errSetAuthIndex := sjson.SetBytes(authJSON, "auth_index", authIndex)
	if errSetAuthIndex != nil {
		return nil, errSetAuthIndex
	}

	out := []byte("{}")
	out, _ = sjson.SetBytes(out, "auth_index", authIndex)
	out, _ = sjson.SetRawBytes(out, "auth", authJSON)
	return out, nil
}

// Authenticate validates request credentials and returns the access result.
func (r *Runtime) Authenticate(ctx context.Context, headers http.Header) (*access.Result, *access.AuthError) {
	return r.authenticateRequest(ctx, headers)
}

// AuthenticateHTTPRequest validates request credentials from a complete HTTP request.
func (r *Runtime) AuthenticateHTTPRequest(ctx context.Context, req *http.Request) (*access.Result, *access.AuthError) {
	return r.authenticateHTTPRequest(ctx, req)
}

// ReloadAuths handles a reload auths.
func (r *Runtime) ReloadAuths(ctx context.Context) error {
	return r.loadAuths(coreauth.WithSkipPersist(ctx))
}

// loadAuths loads an auths.
func (r *Runtime) loadAuths(ctx context.Context) error {
	// Normalize auth state before updating runtime indexes.
	if r == nil || r.coreManager == nil {
		return nil
	}
	if r.clusterAdapter != nil && r.clusterAdapter.Enabled() {
		return r.loadClusterAuths(ctx, r.clusterAdapter)
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

// DispatchForAPIKey processes dispatch with API-key channel restrictions.
func (r *Runtime) DispatchForAPIKey(ctx context.Context, reqModel string, headers http.Header, apiKey string) (*DispatchResult, error) {
	opts := coreauth.Options{}
	if headers != nil {
		opts.Headers = headers.Clone()
	}
	allowedAuthIDs, allowedModelIDs, errAllowed := r.allowedDispatchIDsForAPIKey(ctx, apiKey)
	if errAllowed != nil {
		return nil, errAllowed
	}
	metadata := make(map[string]any)
	if allowedAuthIDs != nil {
		metadata[coreauth.AllowedAuthIDsMetadataKey] = allowedAuthIDs
	}
	if allowedModelIDs != nil {
		metadata[coreauth.AllowedModelIDsMetadataKey] = allowedModelIDs
	}
	if len(metadata) > 0 {
		opts.Metadata = metadata
	}
	return r.dispatchWithOptions(ctx, reqModel, opts)
}

func (r *Runtime) dispatchWithOptions(ctx context.Context, reqModel string, opts coreauth.Options) (*DispatchResult, error) {
	// Build the candidate view before applying availability rules.
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
		return nil, &coreauth.Error{
			Code:    homeerrors.TypeModelNotFound,
			Message: fmt.Sprintf(homeerrors.MessageModelDoesNotExistFmt, trimmedModel),
		}
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

func (r *Runtime) allowedAuthIDsForAPIKey(ctx context.Context, apiKey string) ([]string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" || r == nil || r.clusterAdapter == nil {
		return nil, nil
	}
	store, ok := r.clusterAdapter.(channelScopedAuthStore)
	if !ok || store == nil {
		return nil, nil
	}
	return store.AllowedAuthIDsForAPIKey(ctx, apiKey)
}

func (r *Runtime) allowedDispatchIDsForAPIKey(ctx context.Context, apiKey string) ([]string, []string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" || r == nil || r.clusterAdapter == nil {
		return nil, nil, nil
	}
	if store, ok := r.clusterAdapter.(apiKeyScopedDispatchStore); ok && store != nil {
		return store.AllowedDispatchIDsForAPIKey(ctx, apiKey)
	}

	allowedAuthIDs, errAuthIDs := r.allowedAuthIDsForAPIKey(ctx, apiKey)
	if errAuthIDs != nil {
		return nil, nil, errAuthIDs
	}
	var allowedModelIDs []string
	if store, ok := r.clusterAdapter.(modelScopedAuthStore); ok && store != nil {
		modelIDs, errModelIDs := store.AllowedModelIDsForAPIKey(ctx, apiKey)
		if errModelIDs != nil {
			return nil, nil, errModelIDs
		}
		allowedModelIDs = modelIDs
	}
	return allowedAuthIDs, allowedModelIDs, nil
}

// supportsRequestedModel handles a supports requested model.
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

// stripModelSuffix handles a strip model suffix.
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
	// Resolve credential context before calling upstream OAuth services.
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

// applyAuthFile applies an auth file.
func (r *Runtime) applyAuthFile(ctx context.Context, fullPath string, data []byte) {
	// Normalize auth state before updating runtime indexes.
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

func (r *Runtime) refreshAccessProviders() {
	if r == nil || r.accessManager == nil {
		return
	}
	providers := access.RegisteredProviders()
	if provider := r.clusterAccessProvider(); provider != nil {
		providers = append(providers, provider)
	}
	r.accessManager.SetProviders(providers)
}

func (r *Runtime) clusterAccessProvider() access.Provider {
	if r == nil || r.clusterAdapter == nil || !r.clusterAdapter.Enabled() {
		return nil
	}
	validator, ok := r.clusterAdapter.(apiKeyValidator)
	if !ok || validator == nil {
		return nil
	}
	return newClusterAPIKeyAccessProvider(validator)
}

// authenticateRequest handles an authenticate request.
func (r *Runtime) authenticateRequest(ctx context.Context, headers http.Header) (*access.Result, *access.AuthError) {
	if r == nil || r.accessManager == nil {
		return nil, access.NewNoCredentialsError()
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

	return r.authenticateHTTPRequest(ctx, req)
}

func (r *Runtime) authenticateHTTPRequest(ctx context.Context, req *http.Request) (*access.Result, *access.AuthError) {
	if r == nil || r.accessManager == nil {
		return nil, access.NewNoCredentialsError()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if req == nil {
		return nil, access.NewNoCredentialsError()
	}
	return r.accessManager.Authenticate(ctx, req)
}
