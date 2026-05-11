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

func NewHandler(repo *cluster.Repository, runtime *home.Runtime) *Handler {
	return &Handler{repo: repo, runtime: runtime}
}

func (h *Handler) RegisterRoutes(group *gin.RouterGroup) {
	if h == nil || group == nil {
		return
	}
	group.GET("/config", h.GetConfig)
	group.GET("/config.yaml", h.GetConfigYAML)
	group.PUT("/config.yaml", h.PutConfigYAML)
	for _, route := range ConfigRootRoutes() {
		group.GET(route, h.GetConfigRoot(route))
		group.PUT(route, h.PutConfigRoot(route))
		group.PATCH(route, h.PutConfigRoot(route))
		group.DELETE(route, h.DeleteConfigRoot(route))
	}

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

	group.GET("/auth-files", h.ListAuthFiles)
	group.GET("/auth-files/download", h.DownloadAuthFile)
	group.POST("/auth-files", h.UploadAuthFile)
	group.DELETE("/auth-files", h.DeleteAuthFile)
	group.PATCH("/auth-files/status", h.PatchAuthFileStatus)
	group.PATCH("/auth-files/fields", h.PatchAuthFileFields)
	group.POST("/oauth-callback", h.PostOAuthCallback)
}

func (h *Handler) requestContext(c *gin.Context) (context.Context, context.CancelFunc) {
	ctx := context.Background()
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		ctx = c.Request.Context()
	}
	return context.WithTimeout(ctx, 10*time.Second)
}

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

func (h *Handler) refreshAuths(ctx context.Context) error {
	if h == nil || h.runtime == nil {
		return nil
	}
	return h.runtime.ReloadAuths(ctx)
}

func respondError(c *gin.Context, status int, code string, err error) {
	message := strings.TrimSpace(code)
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	c.JSON(status, gin.H{"error": code, "message": message})
}

func respondOK(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
