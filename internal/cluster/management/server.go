package management

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
)

type Handler struct {
	repo    *cluster.Repository
	runtime *home.Runtime
}

// NewHandler creates a new handler.
func NewHandler(repo *cluster.Repository, runtime *home.Runtime) *Handler {
	return &Handler{repo: repo, runtime: runtime}
}

// RegisterRoutes handles a register routes.
func (h *Handler) RegisterRoutes(group *gin.RouterGroup) {
	// Validate request inputs before mutating persisted state.
	if h == nil || group == nil {
		return
	}
	group.GET("/nodes", h.ListNodes)
	group.GET("/latest-version", h.GetLatestVersion)
	group.GET("/config", h.GetConfig)
	group.GET("/config.yaml", h.GetConfigYAML)
	group.PUT("/config.yaml", h.PutConfigYAML)
	group.GET("/debug", h.GetDebug)
	group.PUT("/debug", h.PutDebug)
	group.PATCH("/debug", h.PutDebug)
	group.GET("/logging-to-file", h.GetLoggingToFile)
	group.PUT("/logging-to-file", h.PutLoggingToFile)
	group.PATCH("/logging-to-file", h.PutLoggingToFile)
	group.GET("/logs-max-total-size-mb", h.GetLogsMaxTotalSizeMB)
	group.PUT("/logs-max-total-size-mb", h.PutLogsMaxTotalSizeMB)
	group.PATCH("/logs-max-total-size-mb", h.PutLogsMaxTotalSizeMB)
	group.GET("/error-logs-max-files", h.GetErrorLogsMaxFiles)
	group.PUT("/error-logs-max-files", h.PutErrorLogsMaxFiles)
	group.PATCH("/error-logs-max-files", h.PutErrorLogsMaxFiles)
	group.GET("/usage-statistics-enabled", h.GetUsageStatisticsEnabled)
	group.PUT("/usage-statistics-enabled", h.PutUsageStatisticsEnabled)
	group.PATCH("/usage-statistics-enabled", h.PutUsageStatisticsEnabled)
	group.GET("/proxy-url", h.GetProxyURL)
	group.PUT("/proxy-url", h.PutProxyURL)
	group.PATCH("/proxy-url", h.PutProxyURL)
	group.DELETE("/proxy-url", h.DeleteProxyURL)
	group.GET("/quota-exceeded/switch-project", h.GetSwitchProject)
	group.PUT("/quota-exceeded/switch-project", h.PutSwitchProject)
	group.PATCH("/quota-exceeded/switch-project", h.PutSwitchProject)
	group.GET("/quota-exceeded/switch-preview-model", h.GetSwitchPreviewModel)
	group.PUT("/quota-exceeded/switch-preview-model", h.PutSwitchPreviewModel)
	group.PATCH("/quota-exceeded/switch-preview-model", h.PutSwitchPreviewModel)
	group.GET("/api-keys", h.GetAPIKeys)
	group.PUT("/api-keys", h.PutAPIKeys)
	group.PATCH("/api-keys", h.PatchAPIKeys)
	group.DELETE("/api-keys", h.DeleteAPIKeys)
	group.GET("/request-log", h.GetRequestLog)
	group.PUT("/request-log", h.PutRequestLog)
	group.PATCH("/request-log", h.PutRequestLog)
	group.GET("/ws-auth", h.GetWebsocketAuth)
	group.PUT("/ws-auth", h.PutWebsocketAuth)
	group.PATCH("/ws-auth", h.PutWebsocketAuth)
	group.GET("/ampcode", h.GetAmpCode)
	group.GET("/ampcode/upstream-url", h.GetAmpUpstreamURL)
	group.PUT("/ampcode/upstream-url", h.PutAmpUpstreamURL)
	group.PATCH("/ampcode/upstream-url", h.PutAmpUpstreamURL)
	group.DELETE("/ampcode/upstream-url", h.DeleteAmpUpstreamURL)
	group.GET("/ampcode/upstream-api-key", h.GetAmpUpstreamAPIKey)
	group.PUT("/ampcode/upstream-api-key", h.PutAmpUpstreamAPIKey)
	group.PATCH("/ampcode/upstream-api-key", h.PutAmpUpstreamAPIKey)
	group.DELETE("/ampcode/upstream-api-key", h.DeleteAmpUpstreamAPIKey)
	group.GET("/ampcode/restrict-management-to-localhost", h.GetAmpRestrictManagementToLocalhost)
	group.PUT("/ampcode/restrict-management-to-localhost", h.PutAmpRestrictManagementToLocalhost)
	group.PATCH("/ampcode/restrict-management-to-localhost", h.PutAmpRestrictManagementToLocalhost)
	group.GET("/ampcode/model-mappings", h.GetAmpModelMappings)
	group.PUT("/ampcode/model-mappings", h.PutAmpModelMappings)
	group.PATCH("/ampcode/model-mappings", h.PatchAmpModelMappings)
	group.DELETE("/ampcode/model-mappings", h.DeleteAmpModelMappings)
	group.GET("/ampcode/force-model-mappings", h.GetAmpForceModelMappings)
	group.PUT("/ampcode/force-model-mappings", h.PutAmpForceModelMappings)
	group.PATCH("/ampcode/force-model-mappings", h.PutAmpForceModelMappings)
	group.GET("/ampcode/upstream-api-keys", h.GetAmpUpstreamAPIKeys)
	group.PUT("/ampcode/upstream-api-keys", h.PutAmpUpstreamAPIKeys)
	group.PATCH("/ampcode/upstream-api-keys", h.PatchAmpUpstreamAPIKeys)
	group.DELETE("/ampcode/upstream-api-keys", h.DeleteAmpUpstreamAPIKeys)
	group.GET("/request-retry", h.GetRequestRetry)
	group.PUT("/request-retry", h.PutRequestRetry)
	group.PATCH("/request-retry", h.PutRequestRetry)
	group.GET("/max-retry-interval", h.GetMaxRetryInterval)
	group.PUT("/max-retry-interval", h.PutMaxRetryInterval)
	group.PATCH("/max-retry-interval", h.PutMaxRetryInterval)
	group.GET("/force-model-prefix", h.GetForceModelPrefix)
	group.PUT("/force-model-prefix", h.PutForceModelPrefix)
	group.PATCH("/force-model-prefix", h.PutForceModelPrefix)
	group.GET("/routing/strategy", h.GetRoutingStrategy)
	group.PUT("/routing/strategy", h.PutRoutingStrategy)
	group.PATCH("/routing/strategy", h.PutRoutingStrategy)

	group.GET("/gemini-api-key", h.GetGeminiKeys)
	group.PUT("/gemini-api-key", h.PutGeminiKeys)
	group.PATCH("/gemini-api-key", h.PatchGeminiKey)
	group.DELETE("/gemini-api-key", h.DeleteGeminiKey)
	group.GET("/vertex-api-key", h.GetVertexCompatKeys)
	group.PUT("/vertex-api-key", h.PutVertexCompatKeys)
	group.PATCH("/vertex-api-key", h.PatchVertexCompatKey)
	group.DELETE("/vertex-api-key", h.DeleteVertexCompatKey)
	group.GET("/codex-api-key", h.GetCodexKeys)
	group.PUT("/codex-api-key", h.PutCodexKeys)
	group.PATCH("/codex-api-key", h.PatchCodexKey)
	group.DELETE("/codex-api-key", h.DeleteCodexKey)
	group.GET("/claude-api-key", h.GetClaudeKeys)
	group.PUT("/claude-api-key", h.PutClaudeKeys)
	group.PATCH("/claude-api-key", h.PatchClaudeKey)
	group.DELETE("/claude-api-key", h.DeleteClaudeKey)
	group.GET("/openai-compatibility", h.GetOpenAICompat)
	group.PUT("/openai-compatibility", h.PutOpenAICompat)
	group.PATCH("/openai-compatibility", h.PatchOpenAICompat)
	group.DELETE("/openai-compatibility", h.DeleteOpenAICompat)
	group.GET("/oauth-excluded-models", h.GetOAuthExcludedModels)
	group.PUT("/oauth-excluded-models", h.PutOAuthExcludedModels)
	group.PATCH("/oauth-excluded-models", h.PatchOAuthExcludedModels)
	group.DELETE("/oauth-excluded-models", h.DeleteOAuthExcludedModels)
	group.GET("/oauth-model-alias", h.GetOAuthModelAlias)
	group.PUT("/oauth-model-alias", h.PutOAuthModelAlias)
	group.PATCH("/oauth-model-alias", h.PatchOAuthModelAlias)
	group.DELETE("/oauth-model-alias", h.DeleteOAuthModelAlias)

	group.GET("/auth-files", h.ListAuthFiles)
	group.GET("/auth-files/models", h.GetAuthFileModels)
	group.GET("/auth-files/download", h.DownloadAuthFile)
	group.POST("/auth-files", h.UploadAuthFile)
	group.DELETE("/auth-files", h.DeleteAuthFile)
	group.PATCH("/auth-files/status", h.PatchAuthFileStatus)
	group.PATCH("/auth-files/fields", h.PatchAuthFileFields)
	group.GET("/model-definitions/:channel", h.GetStaticModelDefinitions)
	group.GET("/anthropic-auth-url", h.RequestAnthropicToken)
	group.GET("/antigravity-auth-url", h.RequestAntigravityToken)
	group.GET("/codex-auth-url", h.RequestCodexToken)
	group.GET("/gemini-cli-auth-url", h.RequestGeminiCLIToken)
	group.GET("/kimi-auth-url", h.RequestKimiToken)
	group.GET("/get-auth-status", h.GetAuthStatus)
	group.POST("/vertex/import", h.ImportVertexCredential)
	group.POST("/api-call", h.APICall)
	group.POST("/oauth-callback", h.PostOAuthCallback)
}

// requestContext handles a request context.
func (h *Handler) requestContext(c *gin.Context) (context.Context, context.CancelFunc) {
	ctx := context.Background()
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		ctx = c.Request.Context()
	}
	return context.WithTimeout(ctx, 10*time.Second)
}

// currentConfig handles a current config.
func (h *Handler) currentConfig(ctx context.Context) (*config.Config, map[string]any, error) {
	root, errSnapshot := h.configRoot(ctx)
	if errSnapshot != nil {
		return nil, nil, errSnapshot
	}
	cfg, errConfig := configFromRoot(root)
	if errConfig != nil {
		return nil, nil, errConfig
	}
	return cfg, root, nil
}

// refreshConfig refreshes a config.
func (h *Handler) refreshConfig(ctx context.Context) error {
	if h == nil || h.runtime == nil {
		return nil
	}
	cfg, payload, errConfig := h.repo.LoadConfigAsRuntimeConfig(ctx)
	if errConfig != nil {
		return errConfig
	}
	if errApply := h.runtime.ApplyConfigFromCluster(ctx, cfg); errApply != nil {
		return errApply
	}
	h.runtime.PublishConfigYAML(payload)
	return nil
}

// refreshAuths refreshes an auths.
func (h *Handler) refreshAuths(ctx context.Context) error {
	if h == nil || h.runtime == nil {
		return nil
	}
	return h.runtime.ReloadAuths(ctx)
}

// respondError handles a respond error.
func respondError(c *gin.Context, status int, code string, err error) {
	message := strings.TrimSpace(code)
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	c.JSON(status, gin.H{"error": code, "message": message})
}

// respondOK handles a respond ok.
func respondOK(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
