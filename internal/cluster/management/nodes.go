package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/node"
)

// ListNodes returns a nodes.
func (h *Handler) ListNodes(c *gin.Context) {
	if c == nil {
		return
	}
	c.JSON(http.StatusOK, gin.H{"nodes": node.GlobalRegistry().List()})
}
