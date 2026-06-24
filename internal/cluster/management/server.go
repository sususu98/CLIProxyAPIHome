package management

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginstore"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
)

type Handler struct {
	repo                  *cluster.Repository
	runtime               *home.Runtime
	nodeIP                string
	nodePort              int
	forwardTLSConfig      *tls.Config
	pluginStoreHTTPClient pluginstore.HTTPDoer
}

// NewHandler creates a new handler.
func NewHandler(repo *cluster.Repository, runtime *home.Runtime, nodeIP string, nodePort int) *Handler {
	return &Handler{repo: repo, runtime: runtime, nodeIP: strings.TrimSpace(nodeIP), nodePort: nodePort}
}

// SetForwardTLSConfig sets the TLS config used for cluster HTTP forwarding.
func (h *Handler) SetForwardTLSConfig(tlsConfig *tls.Config) {
	if h == nil {
		return
	}
	h.forwardTLSConfig = tlsConfig
}

func (h *Handler) SetPluginStoreHTTPClient(client pluginstore.HTTPDoer) {
	if h == nil {
		return
	}
	h.pluginStoreHTTPClient = client
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

func (h *Handler) publishCurrentConfig(ctx context.Context) error {
	if h == nil || h.runtime == nil {
		return nil
	}
	_, payload, errConfig := h.repo.LoadConfigAsRuntimeConfig(ctx)
	if errConfig != nil {
		return errConfig
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
