package managementhttp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	cpasdkapi "github.com/router-for-me/CLIProxyAPI/v7/sdk/api"
	cpasdkauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	cpacoreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cpaconfig "github.com/router-for-me/CLIProxyAPI/v7/sdk/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	clustermanagement "github.com/router-for-me/CLIProxyAPIHome/internal/cluster/management"
	appconfig "github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"github.com/router-for-me/CLIProxyAPIHome/internal/managementasset"
	mgmthandlers "github.com/router-for-me/CLIProxyAPIHome/internal/managementhttp/hanlders"
	"github.com/router-for-me/CLIProxyAPIHome/internal/util"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type RouteKey struct {
	Method string
	Path   string
}

type RouteRegistry struct {
	routes            map[RouteKey]gin.HandlerFunc
	clusterManagement *ClusterManagementOption
}

// newRouteRegistry creates a route registry.
func newRouteRegistry() *RouteRegistry {
	return &RouteRegistry{
		routes: make(map[RouteKey]gin.HandlerFunc),
	}
}

// Set updates set.
func (r *RouteRegistry) Set(method, path string, handler gin.HandlerFunc) {
	if r == nil || handler == nil {
		return
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if method == "" || path == "" {
		return
	}
	r.routes[RouteKey{Method: method, Path: path}] = handler
}

// Delete handles delete.
func (r *RouteRegistry) Delete(method, path string) {
	if r == nil {
		return
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if method == "" || path == "" {
		return
	}
	delete(r.routes, RouteKey{Method: method, Path: path})
}

// Register wires package handlers into the provided registry.
func (r *RouteRegistry) Register(group *gin.RouterGroup) {
	if r == nil || group == nil || len(r.routes) == 0 {
		return
	}
	keys := make([]RouteKey, 0, len(r.routes))
	for k := range r.routes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Path == keys[j].Path {
			return keys[i].Method < keys[j].Method
		}
		return keys[i].Path < keys[j].Path
	})
	for _, k := range keys {
		h := r.routes[k]
		if h == nil {
			continue
		}
		group.Handle(k.Method, k.Path, h)
	}
}

type RouteOption func(*RouteRegistry)

type ClusterManagementOption struct {
	Enabled    bool
	Repository *cluster.Repository
	Runtime    *home.Runtime
	NodeIP     string
	NodePort   int
}

type DatabaseManagementOption = ClusterManagementOption

// WithRoute applies the route option.
func WithRoute(method, path string, handler gin.HandlerFunc) RouteOption {
	return func(r *RouteRegistry) {
		if r == nil {
			return
		}
		r.Set(method, path, handler)
	}
}

// WithoutRoute removes a route from the registry.
func WithoutRoute(method, path string) RouteOption {
	return func(r *RouteRegistry) {
		if r == nil {
			return
		}
		r.Delete(method, path)
	}
}

// WithDatabaseManagement applies the database-backed management option.
func WithDatabaseManagement(opt DatabaseManagementOption) RouteOption {
	return func(r *RouteRegistry) {
		if r == nil || !opt.Enabled || opt.Repository == nil || opt.Runtime == nil {
			return
		}
		optCopy := opt
		r.clusterManagement = &optCopy
		registerClusterManagementRoutes(r, clustermanagement.NewHandler(opt.Repository, opt.Runtime, opt.NodeIP, opt.NodePort))
	}
}

// WithClusterManagement applies the cluster management option.
func WithClusterManagement(opt ClusterManagementOption) RouteOption {
	return WithDatabaseManagement(opt)
}

// registerClusterManagementRoutes handles a register cluster management routes.
func registerClusterManagementRoutes(r *RouteRegistry, handler *clustermanagement.Handler) {
	// Validate request inputs before mutating persisted state.
	if r == nil || handler == nil {
		return
	}

	r.Set(http.MethodGet, "/nodes", handler.ListNodes)
	r.Set(http.MethodGet, "/latest-version", handler.GetLatestVersion)
	r.Set(http.MethodPost, "/certificates/clients", handler.CreateClientCertificate)
	r.Set(http.MethodGet, "/config", handler.GetConfig)
	r.Set(http.MethodGet, "/config.yaml", handler.GetConfigYAML)
	r.Set(http.MethodPut, "/config.yaml", handler.PutConfigYAML)

	r.Set(http.MethodGet, "/debug", handler.GetDebug)
	r.Set(http.MethodPut, "/debug", handler.PutDebug)
	r.Set(http.MethodPatch, "/debug", handler.PutDebug)
	r.Set(http.MethodGet, "/logging-to-file", handler.GetLoggingToFile)
	r.Set(http.MethodPut, "/logging-to-file", handler.PutLoggingToFile)
	r.Set(http.MethodPatch, "/logging-to-file", handler.PutLoggingToFile)
	r.Set(http.MethodGet, "/logs-max-total-size-mb", handler.GetLogsMaxTotalSizeMB)
	r.Set(http.MethodPut, "/logs-max-total-size-mb", handler.PutLogsMaxTotalSizeMB)
	r.Set(http.MethodPatch, "/logs-max-total-size-mb", handler.PutLogsMaxTotalSizeMB)
	r.Set(http.MethodGet, "/error-logs-max-files", handler.GetErrorLogsMaxFiles)
	r.Set(http.MethodPut, "/error-logs-max-files", handler.PutErrorLogsMaxFiles)
	r.Set(http.MethodPatch, "/error-logs-max-files", handler.PutErrorLogsMaxFiles)
	r.Set(http.MethodGet, "/usage-statistics-enabled", handler.GetUsageStatisticsEnabled)
	r.Set(http.MethodPut, "/usage-statistics-enabled", handler.PutUsageStatisticsEnabled)
	r.Set(http.MethodPatch, "/usage-statistics-enabled", handler.PutUsageStatisticsEnabled)
	r.Set(http.MethodGet, "/proxy-url", handler.GetProxyURL)
	r.Set(http.MethodPut, "/proxy-url", handler.PutProxyURL)
	r.Set(http.MethodPatch, "/proxy-url", handler.PutProxyURL)
	r.Set(http.MethodDelete, "/proxy-url", handler.DeleteProxyURL)

	r.Set(http.MethodGet, "/quota-exceeded/switch-project", handler.GetSwitchProject)
	r.Set(http.MethodPut, "/quota-exceeded/switch-project", handler.PutSwitchProject)
	r.Set(http.MethodPatch, "/quota-exceeded/switch-project", handler.PutSwitchProject)
	r.Set(http.MethodGet, "/quota-exceeded/switch-preview-model", handler.GetSwitchPreviewModel)
	r.Set(http.MethodPut, "/quota-exceeded/switch-preview-model", handler.PutSwitchPreviewModel)
	r.Set(http.MethodPatch, "/quota-exceeded/switch-preview-model", handler.PutSwitchPreviewModel)

	r.Set(http.MethodGet, "/api-keys", handler.GetAPIKeys)
	r.Set(http.MethodPut, "/api-keys", handler.PutAPIKeys)
	r.Set(http.MethodPatch, "/api-keys", handler.PatchAPIKeys)
	r.Set(http.MethodDelete, "/api-keys", handler.DeleteAPIKeys)

	r.Set(http.MethodGet, "/ampcode", handler.GetAmpCode)
	r.Set(http.MethodGet, "/ampcode/upstream-url", handler.GetAmpUpstreamURL)
	r.Set(http.MethodPut, "/ampcode/upstream-url", handler.PutAmpUpstreamURL)
	r.Set(http.MethodPatch, "/ampcode/upstream-url", handler.PutAmpUpstreamURL)
	r.Set(http.MethodDelete, "/ampcode/upstream-url", handler.DeleteAmpUpstreamURL)
	r.Set(http.MethodGet, "/ampcode/upstream-api-key", handler.GetAmpUpstreamAPIKey)
	r.Set(http.MethodPut, "/ampcode/upstream-api-key", handler.PutAmpUpstreamAPIKey)
	r.Set(http.MethodPatch, "/ampcode/upstream-api-key", handler.PutAmpUpstreamAPIKey)
	r.Set(http.MethodDelete, "/ampcode/upstream-api-key", handler.DeleteAmpUpstreamAPIKey)
	r.Set(http.MethodGet, "/ampcode/restrict-management-to-localhost", handler.GetAmpRestrictManagementToLocalhost)
	r.Set(http.MethodPut, "/ampcode/restrict-management-to-localhost", handler.PutAmpRestrictManagementToLocalhost)
	r.Set(http.MethodPatch, "/ampcode/restrict-management-to-localhost", handler.PutAmpRestrictManagementToLocalhost)
	r.Set(http.MethodGet, "/ampcode/model-mappings", handler.GetAmpModelMappings)
	r.Set(http.MethodPut, "/ampcode/model-mappings", handler.PutAmpModelMappings)
	r.Set(http.MethodPatch, "/ampcode/model-mappings", handler.PatchAmpModelMappings)
	r.Set(http.MethodDelete, "/ampcode/model-mappings", handler.DeleteAmpModelMappings)
	r.Set(http.MethodGet, "/ampcode/force-model-mappings", handler.GetAmpForceModelMappings)
	r.Set(http.MethodPut, "/ampcode/force-model-mappings", handler.PutAmpForceModelMappings)
	r.Set(http.MethodPatch, "/ampcode/force-model-mappings", handler.PutAmpForceModelMappings)
	r.Set(http.MethodGet, "/ampcode/upstream-api-keys", handler.GetAmpUpstreamAPIKeys)
	r.Set(http.MethodPut, "/ampcode/upstream-api-keys", handler.PutAmpUpstreamAPIKeys)
	r.Set(http.MethodPatch, "/ampcode/upstream-api-keys", handler.PatchAmpUpstreamAPIKeys)
	r.Set(http.MethodDelete, "/ampcode/upstream-api-keys", handler.DeleteAmpUpstreamAPIKeys)

	r.Set(http.MethodGet, "/request-log", handler.GetRequestLog)
	r.Set(http.MethodPut, "/request-log", handler.PutRequestLog)
	r.Set(http.MethodPatch, "/request-log", handler.PutRequestLog)
	r.Set(http.MethodGet, "/ws-auth", handler.GetWebsocketAuth)
	r.Set(http.MethodPut, "/ws-auth", handler.PutWebsocketAuth)
	r.Set(http.MethodPatch, "/ws-auth", handler.PutWebsocketAuth)
	r.Set(http.MethodGet, "/request-retry", handler.GetRequestRetry)
	r.Set(http.MethodPut, "/request-retry", handler.PutRequestRetry)
	r.Set(http.MethodPatch, "/request-retry", handler.PutRequestRetry)
	r.Set(http.MethodGet, "/max-retry-interval", handler.GetMaxRetryInterval)
	r.Set(http.MethodPut, "/max-retry-interval", handler.PutMaxRetryInterval)
	r.Set(http.MethodPatch, "/max-retry-interval", handler.PutMaxRetryInterval)
	r.Set(http.MethodGet, "/force-model-prefix", handler.GetForceModelPrefix)
	r.Set(http.MethodPut, "/force-model-prefix", handler.PutForceModelPrefix)
	r.Set(http.MethodPatch, "/force-model-prefix", handler.PutForceModelPrefix)
	r.Set(http.MethodGet, "/routing/strategy", handler.GetRoutingStrategy)
	r.Set(http.MethodPut, "/routing/strategy", handler.PutRoutingStrategy)
	r.Set(http.MethodPatch, "/routing/strategy", handler.PutRoutingStrategy)

	r.Set(http.MethodGet, "/gemini-api-key", handler.GetGeminiKeys)
	r.Set(http.MethodPut, "/gemini-api-key", handler.PutGeminiKeys)
	r.Set(http.MethodPatch, "/gemini-api-key", handler.PatchGeminiKey)
	r.Set(http.MethodDelete, "/gemini-api-key", handler.DeleteGeminiKey)
	r.Set(http.MethodGet, "/vertex-api-key", handler.GetVertexCompatKeys)
	r.Set(http.MethodPut, "/vertex-api-key", handler.PutVertexCompatKeys)
	r.Set(http.MethodPatch, "/vertex-api-key", handler.PatchVertexCompatKey)
	r.Set(http.MethodDelete, "/vertex-api-key", handler.DeleteVertexCompatKey)
	r.Set(http.MethodGet, "/codex-api-key", handler.GetCodexKeys)
	r.Set(http.MethodPut, "/codex-api-key", handler.PutCodexKeys)
	r.Set(http.MethodPatch, "/codex-api-key", handler.PatchCodexKey)
	r.Set(http.MethodDelete, "/codex-api-key", handler.DeleteCodexKey)
	r.Set(http.MethodGet, "/claude-api-key", handler.GetClaudeKeys)
	r.Set(http.MethodPut, "/claude-api-key", handler.PutClaudeKeys)
	r.Set(http.MethodPatch, "/claude-api-key", handler.PatchClaudeKey)
	r.Set(http.MethodDelete, "/claude-api-key", handler.DeleteClaudeKey)
	r.Set(http.MethodGet, "/openai-compatibility", handler.GetOpenAICompat)
	r.Set(http.MethodPut, "/openai-compatibility", handler.PutOpenAICompat)
	r.Set(http.MethodPatch, "/openai-compatibility", handler.PatchOpenAICompat)
	r.Set(http.MethodDelete, "/openai-compatibility", handler.DeleteOpenAICompat)

	r.Set(http.MethodGet, "/oauth-excluded-models", handler.GetOAuthExcludedModels)
	r.Set(http.MethodPut, "/oauth-excluded-models", handler.PutOAuthExcludedModels)
	r.Set(http.MethodPatch, "/oauth-excluded-models", handler.PatchOAuthExcludedModels)
	r.Set(http.MethodDelete, "/oauth-excluded-models", handler.DeleteOAuthExcludedModels)
	r.Set(http.MethodGet, "/oauth-model-alias", handler.GetOAuthModelAlias)
	r.Set(http.MethodPut, "/oauth-model-alias", handler.PutOAuthModelAlias)
	r.Set(http.MethodPatch, "/oauth-model-alias", handler.PatchOAuthModelAlias)
	r.Set(http.MethodDelete, "/oauth-model-alias", handler.DeleteOAuthModelAlias)
	r.Set(http.MethodGet, "/payload", handler.GetConfigRoot("/payload"))
	r.Set(http.MethodPut, "/payload", handler.PutConfigRoot("/payload"))
	r.Set(http.MethodPatch, "/payload", handler.PutConfigRoot("/payload"))
	r.Set(http.MethodDelete, "/payload", handler.DeleteConfigRoot("/payload"))

	r.Set(http.MethodGet, "/auth-files", handler.ListAuthFiles)
	r.Set(http.MethodGet, "/auth-files/models", handler.GetAuthFileModels)
	r.Set(http.MethodGet, "/auth-files/download", handler.DownloadAuthFile)
	r.Set(http.MethodPost, "/auth-files", handler.UploadAuthFile)
	r.Set(http.MethodDelete, "/auth-files", handler.DeleteAuthFile)
	r.Set(http.MethodPatch, "/auth-files/status", handler.PatchAuthFileStatus)
	r.Set(http.MethodPatch, "/auth-files/fields", handler.PatchAuthFileFields)
	r.Set(http.MethodGet, "/model-definitions/:channel", handler.GetStaticModelDefinitions)
	r.Set(http.MethodGet, "/anthropic-auth-url", handler.RequestAnthropicToken)
	r.Set(http.MethodGet, "/antigravity-auth-url", handler.RequestAntigravityToken)
	r.Set(http.MethodGet, "/codex-auth-url", handler.RequestCodexToken)
	r.Set(http.MethodGet, "/gemini-cli-auth-url", handler.RequestGeminiCLIToken)
	r.Set(http.MethodGet, "/kimi-auth-url", handler.RequestKimiToken)
	r.Set(http.MethodGet, "/xai-auth-url", handler.RequestXAIToken)
	r.Set(http.MethodGet, "/get-auth-status", handler.GetAuthStatus)
	r.Set(http.MethodPost, "/vertex/import", handler.ImportVertexCredential)
	r.Set(http.MethodPost, "/api-call", handler.APICall)
	r.Set(http.MethodPost, "/oauth-callback", handler.PostOAuthCallback)
}

type BuildResult struct {
	Engine      *gin.Engine
	Handler     *cpasdkapi.Handler
	AuthManager *cpacoreauth.Manager
}

// serveManagementControlPanel serves a management control panel.
func serveManagementControlPanel(cfg *cpaconfig.Config, configFilePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c == nil {
			return
		}
		if cfg == nil || cfg.RemoteManagement.DisableControlPanel {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		filePath := managementasset.FilePath(configFilePath)
		if strings.TrimSpace(filePath) == "" {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		if _, err := os.Stat(filePath); err != nil {
			if os.IsNotExist(err) {
				// Control panel bootstrap should not be canceled by client disconnects.
				if !managementasset.EnsureLatestManagementHTML(context.Background(), managementasset.StaticDir(configFilePath), cfg.ProxyURL, cfg.RemoteManagement.PanelGitHubRepository) {
					c.AbortWithStatus(http.StatusNotFound)
					return
				}
			} else {
				log.WithError(err).Error("failed to stat management control panel asset")
				c.AbortWithStatus(http.StatusInternalServerError)
				return
			}
		}

		c.File(filePath)
	}
}

// serveManagementControlPanelFromRuntime derives serve management control panel from runtime.
func serveManagementControlPanelFromRuntime(opt *ClusterManagementOption, configFilePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if opt == nil || !opt.Enabled || opt.Runtime == nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		handler := serveManagementControlPanel(cpaConfigFromHomeConfig(opt.Runtime.Config()), configFilePath)
		handler(c)
	}
}

// Build builds a build.
func Build(configFilePath string, opts ...RouteOption) (*BuildResult, error) {
	// Validate request inputs before mutating persisted state.
	configFilePath = strings.TrimSpace(configFilePath)
	if configFilePath == "" {
		return nil, fmt.Errorf("management http: config file path is empty")
	}

	preReg := newRouteRegistry()
	for i := range opts {
		if opts[i] == nil {
			continue
		}
		opts[i](preReg)
	}
	clusterOpt := preReg.clusterManagement
	clusterEnabled := clusterOpt != nil && clusterOpt.Enabled && clusterOpt.Repository != nil && clusterOpt.Runtime != nil

	var cfg *cpaconfig.Config
	if clusterEnabled {
		cfg = cpaConfigFromHomeConfig(clusterOpt.Runtime.Config())
	} else {
		loadedConfig, errLoad := cpaconfig.LoadConfigOptional(configFilePath, false)
		if errLoad != nil {
			return nil, fmt.Errorf("management http: load config: %w", errLoad)
		}
		cfg = loadedConfig
	}
	if cfg == nil {
		cfg = &cpaconfig.Config{}
	}

	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	resolvedAuthDir, errResolveAuthDir := util.ResolveAuthDir(cfg.AuthDir)
	if errResolveAuthDir != nil {
		return nil, fmt.Errorf("management http: resolve auth dir: %w", errResolveAuthDir)
	}
	if strings.TrimSpace(resolvedAuthDir) != "" {
		cfg.AuthDir = resolvedAuthDir
	}

	tokenStore := newMetadataProxyStore(cpasdkauth.GetTokenStore())
	if dirSetter, ok := tokenStore.(interface{ SetBaseDir(string) }); ok && strings.TrimSpace(cfg.AuthDir) != "" {
		dirSetter.SetBaseDir(cfg.AuthDir)
	}

	authManager := cpacoreauth.NewManager(tokenStore, nil, nil)
	authManager.SetConfig(cfg)
	authManager.SetOAuthModelAlias(cfg.OAuthModelAlias)
	if !clusterEnabled {
		if errAuthLoad := authManager.Load(context.Background()); errAuthLoad != nil {
			return nil, fmt.Errorf("management http: load auths: %w", errAuthLoad)
		}
	}

	handler := cpasdkapi.NewHandler(cfg, configFilePath, authManager)
	handler.SetLogDirectory("logs")

	engine := gin.New()
	_ = engine.SetTrustedProxies(nil)
	engine.Use(gin.Recovery())
	engine.Use(corsMiddleware())

	if clusterEnabled {
		engine.GET("/management.html", serveManagementControlPanelFromRuntime(clusterOpt, configFilePath))
	} else {
		engine.GET("/management.html", serveManagementControlPanel(cfg, configFilePath))
	}

	mgmt := engine.Group("/v0/management")

	reg := defaultRoutes(handler)
	for i := range opts {
		if opts[i] == nil {
			continue
		}
		opts[i](reg)
	}
	if clusterEnabled {
		mgmt.Use(
			withBuildInfoHeaders(),
			clusterAvailabilityMiddleware(clusterOpt, handler),
			handler.Middleware(),
		)
	} else {
		mgmt.Use(
			withBuildInfoHeaders(),
			refreshAndAvailabilityMiddleware(configFilePath, handler, authManager, tokenStore),
			handler.Middleware(),
		)
	}
	reg.Register(mgmt)

	return &BuildResult{
		Engine:      engine,
		Handler:     handler,
		AuthManager: authManager,
	}, nil
}

// corsMiddleware returns a Gin middleware handler that adds CORS headers.
func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "*")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// withBuildInfoHeaders applies the build info headers option.
func withBuildInfoHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c == nil {
			return
		}
		c.Writer.Header().Set("x-cpa-home-version", buildinfo.Version)
		c.Writer.Header().Set("x-cpa-home-commit", buildinfo.Commit)
		c.Writer.Header().Set("x-cpa-home-build-date", buildinfo.BuildDate)
		c.Next()
	}
}

// refreshAndAvailabilityMiddleware refreshes an and availability middleware.
func refreshAndAvailabilityMiddleware(configFilePath string, handler *cpasdkapi.Handler, authManager *cpacoreauth.Manager, tokenStore any) gin.HandlerFunc {
	// Resolve credential context before calling upstream OAuth services.
	envSecret, envSecretSet := os.LookupEnv("MANAGEMENT_PASSWORD")
	envSecret = strings.TrimSpace(envSecret)
	envManagementSecret := envSecretSet && envSecret != ""

	return func(c *gin.Context) {
		if c == nil {
			return
		}

		cfg, errLoad := cpaconfig.LoadConfigOptional(configFilePath, false)
		if errLoad != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "config_reload_failed", "message": errLoad.Error()})
			return
		}
		if cfg == nil {
			cfg = &cpaconfig.Config{}
		}

		resolvedAuthDir, errResolveAuthDir := util.ResolveAuthDir(cfg.AuthDir)
		if errResolveAuthDir != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "resolve_auth_dir_failed", "message": errResolveAuthDir.Error()})
			return
		}
		if strings.TrimSpace(resolvedAuthDir) != "" {
			cfg.AuthDir = resolvedAuthDir
		}

		hasSecret := strings.TrimSpace(cfg.RemoteManagement.SecretKey) != "" || envManagementSecret
		if !hasSecret {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		if handler != nil {
			handler.SetConfig(cfg)
		}
		if authManager != nil {
			authManager.SetConfig(cfg)
			authManager.SetOAuthModelAlias(cfg.OAuthModelAlias)
		}
		if tokenStore != nil {
			if dirSetter, ok := tokenStore.(interface{ SetBaseDir(string) }); ok && strings.TrimSpace(cfg.AuthDir) != "" {
				dirSetter.SetBaseDir(cfg.AuthDir)
			}
		}
		if authManager != nil {
			ctx := c.Request.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if errAuthLoad := authManager.Load(ctx); errAuthLoad != nil {
				log.WithError(errAuthLoad).Warn("management http: auth manager reload failed")
			}
		}

		c.Next()
	}
}

// clusterAvailabilityMiddleware handles a cluster availability middleware.
func clusterAvailabilityMiddleware(opt *ClusterManagementOption, handler *cpasdkapi.Handler) gin.HandlerFunc {
	// Validate request inputs before mutating persisted state.
	envSecret, envSecretSet := os.LookupEnv("MANAGEMENT_PASSWORD")
	envSecret = strings.TrimSpace(envSecret)
	envManagementSecret := envSecretSet && envSecret != ""

	return func(c *gin.Context) {
		if c == nil {
			return
		}
		if opt == nil || !opt.Enabled || opt.Repository == nil || opt.Runtime == nil {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		cfg := cpaConfigFromHomeConfig(opt.Runtime.Config())
		if cfg == nil {
			cfg = &cpaconfig.Config{}
		}
		hasSecret := strings.TrimSpace(cfg.RemoteManagement.SecretKey) != "" || envManagementSecret
		if !hasSecret {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if handler != nil {
			handler.SetConfig(cfg)
		}
		c.Next()
	}
}

// cpaConfigFromHomeConfig derives cpa config from home config.
func cpaConfigFromHomeConfig(homeCfg *appconfig.Config) *cpaconfig.Config {
	if homeCfg == nil {
		return nil
	}
	data, errMarshal := yaml.Marshal(homeCfg)
	if errMarshal != nil {
		return &cpaconfig.Config{}
	}
	cfg := &cpaconfig.Config{}
	if errUnmarshal := yaml.Unmarshal(data, cfg); errUnmarshal != nil {
		return &cpaconfig.Config{}
	}
	return cfg
}

// defaultRoutes handles a default routes.
func defaultRoutes(handler *cpasdkapi.Handler) *RouteRegistry {
	// Validate request inputs before mutating persisted state.
	r := newRouteRegistry()
	if handler == nil {
		return r
	}

	r.Set(http.MethodGet, "/nodes", mgmthandlers.ListNodes)
	r.Set(http.MethodGet, "/config", handler.GetConfig)
	r.Set(http.MethodGet, "/config.yaml", handler.GetConfigYAML)
	r.Set(http.MethodPut, "/config.yaml", handler.PutConfigYAML)
	r.Set(http.MethodGet, "/latest-version", handler.GetLatestVersion)

	r.Set(http.MethodGet, "/debug", handler.GetDebug)
	r.Set(http.MethodPut, "/debug", handler.PutDebug)
	r.Set(http.MethodPatch, "/debug", handler.PutDebug)

	r.Set(http.MethodGet, "/logging-to-file", handler.GetLoggingToFile)
	r.Set(http.MethodPut, "/logging-to-file", handler.PutLoggingToFile)
	r.Set(http.MethodPatch, "/logging-to-file", handler.PutLoggingToFile)

	r.Set(http.MethodGet, "/logs-max-total-size-mb", handler.GetLogsMaxTotalSizeMB)
	r.Set(http.MethodPut, "/logs-max-total-size-mb", handler.PutLogsMaxTotalSizeMB)
	r.Set(http.MethodPatch, "/logs-max-total-size-mb", handler.PutLogsMaxTotalSizeMB)

	r.Set(http.MethodGet, "/error-logs-max-files", handler.GetErrorLogsMaxFiles)
	r.Set(http.MethodPut, "/error-logs-max-files", handler.PutErrorLogsMaxFiles)
	r.Set(http.MethodPatch, "/error-logs-max-files", handler.PutErrorLogsMaxFiles)

	r.Set(http.MethodGet, "/usage-statistics-enabled", handler.GetUsageStatisticsEnabled)
	r.Set(http.MethodPut, "/usage-statistics-enabled", handler.PutUsageStatisticsEnabled)
	r.Set(http.MethodPatch, "/usage-statistics-enabled", handler.PutUsageStatisticsEnabled)

	r.Set(http.MethodGet, "/proxy-url", handler.GetProxyURL)
	r.Set(http.MethodPut, "/proxy-url", handler.PutProxyURL)
	r.Set(http.MethodPatch, "/proxy-url", handler.PutProxyURL)
	r.Set(http.MethodDelete, "/proxy-url", handler.DeleteProxyURL)

	r.Set(http.MethodPost, "/api-call", handler.APICall)

	r.Set(http.MethodGet, "/quota-exceeded/switch-project", handler.GetSwitchProject)
	r.Set(http.MethodPut, "/quota-exceeded/switch-project", handler.PutSwitchProject)
	r.Set(http.MethodPatch, "/quota-exceeded/switch-project", handler.PutSwitchProject)

	r.Set(http.MethodGet, "/quota-exceeded/switch-preview-model", handler.GetSwitchPreviewModel)
	r.Set(http.MethodPut, "/quota-exceeded/switch-preview-model", handler.PutSwitchPreviewModel)
	r.Set(http.MethodPatch, "/quota-exceeded/switch-preview-model", handler.PutSwitchPreviewModel)

	r.Set(http.MethodGet, "/api-keys", handler.GetAPIKeys)
	r.Set(http.MethodPut, "/api-keys", handler.PutAPIKeys)
	r.Set(http.MethodPatch, "/api-keys", handler.PatchAPIKeys)
	r.Set(http.MethodDelete, "/api-keys", handler.DeleteAPIKeys)
	r.Set(http.MethodGet, "/api-key-usage", handler.GetAPIKeyUsage)
	r.Set(http.MethodGet, "/usage-queue", handler.GetUsageQueue)

	r.Set(http.MethodGet, "/gemini-api-key", handler.GetGeminiKeys)
	r.Set(http.MethodPut, "/gemini-api-key", handler.PutGeminiKeys)
	r.Set(http.MethodPatch, "/gemini-api-key", handler.PatchGeminiKey)
	r.Set(http.MethodDelete, "/gemini-api-key", handler.DeleteGeminiKey)

	r.Set(http.MethodGet, "/logs", handler.GetLogs)
	r.Set(http.MethodDelete, "/logs", handler.DeleteLogs)
	r.Set(http.MethodGet, "/request-error-logs", handler.GetRequestErrorLogs)
	r.Set(http.MethodGet, "/request-error-logs/:name", handler.DownloadRequestErrorLog)
	r.Set(http.MethodGet, "/request-log-by-id/:id", handler.GetRequestLogByID)
	r.Set(http.MethodGet, "/request-log", handler.GetRequestLog)
	r.Set(http.MethodPut, "/request-log", handler.PutRequestLog)
	r.Set(http.MethodPatch, "/request-log", handler.PutRequestLog)
	r.Set(http.MethodGet, "/ws-auth", handler.GetWebsocketAuth)
	r.Set(http.MethodPut, "/ws-auth", handler.PutWebsocketAuth)
	r.Set(http.MethodPatch, "/ws-auth", handler.PutWebsocketAuth)

	r.Set(http.MethodGet, "/ampcode", handler.GetAmpCode)
	r.Set(http.MethodGet, "/ampcode/upstream-url", handler.GetAmpUpstreamURL)
	r.Set(http.MethodPut, "/ampcode/upstream-url", handler.PutAmpUpstreamURL)
	r.Set(http.MethodPatch, "/ampcode/upstream-url", handler.PutAmpUpstreamURL)
	r.Set(http.MethodDelete, "/ampcode/upstream-url", handler.DeleteAmpUpstreamURL)
	r.Set(http.MethodGet, "/ampcode/upstream-api-key", handler.GetAmpUpstreamAPIKey)
	r.Set(http.MethodPut, "/ampcode/upstream-api-key", handler.PutAmpUpstreamAPIKey)
	r.Set(http.MethodPatch, "/ampcode/upstream-api-key", handler.PutAmpUpstreamAPIKey)
	r.Set(http.MethodDelete, "/ampcode/upstream-api-key", handler.DeleteAmpUpstreamAPIKey)
	r.Set(http.MethodGet, "/ampcode/restrict-management-to-localhost", handler.GetAmpRestrictManagementToLocalhost)
	r.Set(http.MethodPut, "/ampcode/restrict-management-to-localhost", handler.PutAmpRestrictManagementToLocalhost)
	r.Set(http.MethodPatch, "/ampcode/restrict-management-to-localhost", handler.PutAmpRestrictManagementToLocalhost)
	r.Set(http.MethodGet, "/ampcode/model-mappings", handler.GetAmpModelMappings)
	r.Set(http.MethodPut, "/ampcode/model-mappings", handler.PutAmpModelMappings)
	r.Set(http.MethodPatch, "/ampcode/model-mappings", handler.PatchAmpModelMappings)
	r.Set(http.MethodDelete, "/ampcode/model-mappings", handler.DeleteAmpModelMappings)
	r.Set(http.MethodGet, "/ampcode/force-model-mappings", handler.GetAmpForceModelMappings)
	r.Set(http.MethodPut, "/ampcode/force-model-mappings", handler.PutAmpForceModelMappings)
	r.Set(http.MethodPatch, "/ampcode/force-model-mappings", handler.PutAmpForceModelMappings)
	r.Set(http.MethodGet, "/ampcode/upstream-api-keys", handler.GetAmpUpstreamAPIKeys)
	r.Set(http.MethodPut, "/ampcode/upstream-api-keys", handler.PutAmpUpstreamAPIKeys)
	r.Set(http.MethodPatch, "/ampcode/upstream-api-keys", handler.PatchAmpUpstreamAPIKeys)
	r.Set(http.MethodDelete, "/ampcode/upstream-api-keys", handler.DeleteAmpUpstreamAPIKeys)

	r.Set(http.MethodGet, "/request-retry", handler.GetRequestRetry)
	r.Set(http.MethodPut, "/request-retry", handler.PutRequestRetry)
	r.Set(http.MethodPatch, "/request-retry", handler.PutRequestRetry)
	r.Set(http.MethodGet, "/max-retry-interval", handler.GetMaxRetryInterval)
	r.Set(http.MethodPut, "/max-retry-interval", handler.PutMaxRetryInterval)
	r.Set(http.MethodPatch, "/max-retry-interval", handler.PutMaxRetryInterval)

	r.Set(http.MethodGet, "/force-model-prefix", handler.GetForceModelPrefix)
	r.Set(http.MethodPut, "/force-model-prefix", handler.PutForceModelPrefix)
	r.Set(http.MethodPatch, "/force-model-prefix", handler.PutForceModelPrefix)

	r.Set(http.MethodGet, "/routing/strategy", handler.GetRoutingStrategy)
	r.Set(http.MethodPut, "/routing/strategy", handler.PutRoutingStrategy)
	r.Set(http.MethodPatch, "/routing/strategy", handler.PutRoutingStrategy)

	r.Set(http.MethodGet, "/claude-api-key", handler.GetClaudeKeys)
	r.Set(http.MethodPut, "/claude-api-key", handler.PutClaudeKeys)
	r.Set(http.MethodPatch, "/claude-api-key", handler.PatchClaudeKey)
	r.Set(http.MethodDelete, "/claude-api-key", handler.DeleteClaudeKey)

	r.Set(http.MethodGet, "/codex-api-key", handler.GetCodexKeys)
	r.Set(http.MethodPut, "/codex-api-key", handler.PutCodexKeys)
	r.Set(http.MethodPatch, "/codex-api-key", handler.PatchCodexKey)
	r.Set(http.MethodDelete, "/codex-api-key", handler.DeleteCodexKey)

	r.Set(http.MethodGet, "/openai-compatibility", handler.GetOpenAICompat)
	r.Set(http.MethodPut, "/openai-compatibility", handler.PutOpenAICompat)
	r.Set(http.MethodPatch, "/openai-compatibility", handler.PatchOpenAICompat)
	r.Set(http.MethodDelete, "/openai-compatibility", handler.DeleteOpenAICompat)

	r.Set(http.MethodGet, "/vertex-api-key", handler.GetVertexCompatKeys)
	r.Set(http.MethodPut, "/vertex-api-key", handler.PutVertexCompatKeys)
	r.Set(http.MethodPatch, "/vertex-api-key", handler.PatchVertexCompatKey)
	r.Set(http.MethodDelete, "/vertex-api-key", handler.DeleteVertexCompatKey)

	r.Set(http.MethodGet, "/oauth-excluded-models", handler.GetOAuthExcludedModels)
	r.Set(http.MethodPut, "/oauth-excluded-models", handler.PutOAuthExcludedModels)
	r.Set(http.MethodPatch, "/oauth-excluded-models", handler.PatchOAuthExcludedModels)
	r.Set(http.MethodDelete, "/oauth-excluded-models", handler.DeleteOAuthExcludedModels)

	r.Set(http.MethodGet, "/oauth-model-alias", handler.GetOAuthModelAlias)
	r.Set(http.MethodPut, "/oauth-model-alias", handler.PutOAuthModelAlias)
	r.Set(http.MethodPatch, "/oauth-model-alias", handler.PatchOAuthModelAlias)
	r.Set(http.MethodDelete, "/oauth-model-alias", handler.DeleteOAuthModelAlias)

	r.Set(http.MethodGet, "/auth-files", handler.ListAuthFiles)
	r.Set(http.MethodGet, "/auth-files/models", handler.GetAuthFileModels)
	r.Set(http.MethodGet, "/model-definitions/:channel", handler.GetStaticModelDefinitions)
	r.Set(http.MethodGet, "/auth-files/download", handler.DownloadAuthFile)
	r.Set(http.MethodPost, "/auth-files", handler.UploadAuthFile)
	r.Set(http.MethodDelete, "/auth-files", handler.DeleteAuthFile)
	r.Set(http.MethodPatch, "/auth-files/status", handler.PatchAuthFileStatus)
	r.Set(http.MethodPatch, "/auth-files/fields", handler.PatchAuthFileFields)
	r.Set(http.MethodPost, "/vertex/import", handler.ImportVertexCredential)

	r.Set(http.MethodGet, "/anthropic-auth-url", handler.RequestAnthropicToken)
	r.Set(http.MethodGet, "/codex-auth-url", handler.RequestCodexToken)
	r.Set(http.MethodGet, "/gemini-cli-auth-url", handler.RequestGeminiCLIToken)
	r.Set(http.MethodGet, "/antigravity-auth-url", handler.RequestAntigravityToken)
	r.Set(http.MethodGet, "/kimi-auth-url", handler.RequestKimiToken)
	r.Set(http.MethodPost, "/oauth-callback", handler.PostOAuthCallback)
	r.Set(http.MethodGet, "/get-auth-status", handler.GetAuthStatus)

	return r
}
