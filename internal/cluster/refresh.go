package cluster

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	log "github.com/sirupsen/logrus"
)

type RefreshController struct {
	coordinator      *Coordinator
	runtime          *home.Runtime
	repo             *Repository
	forwardTLSConfig *tls.Config
}

// NewRefreshController creates a new refresh controller.
func NewRefreshController(coordinator *Coordinator, runtime *home.Runtime, repo *Repository, forwardTLSConfig *tls.Config) *RefreshController {
	return &RefreshController{
		coordinator:      coordinator,
		runtime:          runtime,
		repo:             repo,
		forwardTLSConfig: forwardTLSConfig,
	}
}

// OnMasterChanged handles an on master changed.
func (c *RefreshController) OnMasterChanged(isMaster bool) {
	if c == nil || c.runtime == nil {
		return
	}
	if isMaster {
		if c.CanAutoRefresh() {
			c.runtime.StartAutoRefresh(context.Background())
		}
		return
	}
	c.runtime.StopAutoRefresh()
}

// CanAutoRefresh reports whether can auto refresh.
func (c *RefreshController) CanAutoRefresh() bool {
	if c == nil || c.coordinator == nil {
		return false
	}
	return c.coordinator.IsMaster()
}

// RefreshNow refreshes refresh now.
func (c *RefreshController) RefreshNow(ctx context.Context, authIndex string) ([]byte, error) {
	// Resolve credential context before calling upstream OAuth services.
	if c == nil || c.runtime == nil {
		return nil, fmt.Errorf("cluster refresh: runtime is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if c.coordinator == nil || c.repo == nil {
		return c.runtime.RefreshNowLocal(ctx, authIndex)
	}
	if c.coordinator.IsMaster() {
		return c.refreshLocalWithLock(ctx, authIndex)
	}

	forwardAuthUUID := strings.TrimSpace(authIndex)
	if core := c.runtime.CoreManager(); core != nil {
		if targetUUID, _, errTarget := c.refreshTarget(core, authIndex); errTarget == nil && strings.TrimSpace(targetUUID) != "" {
			forwardAuthUUID = targetUUID
		}
	}
	master, errMaster := c.coordinator.CurrentMaster(ctx)
	if errMaster == nil && master != nil && strings.TrimSpace(master.IP) != "" && master.Port > 0 {
		if c.isSelf(master) {
			return c.refreshLocalWithLock(ctx, authIndex)
		}
		nodeSecret := c.coordinator.NodeSecret()
		if strings.TrimSpace(nodeSecret) == "" {
			log.Warnf("cluster refresh node secret missing, falling back to local refresh")
			return c.refreshLocalWithLock(ctx, authIndex)
		}
		if payload, errForward := ForwardRefreshToMaster(ctx, master, forwardAuthUUID, nodeSecret, c.forwardTLSConfig); errForward == nil {
			return payload, nil
		} else {
			log.Warnf("cluster refresh forward failed, falling back to local refresh: %v", errForward)
		}
	} else if errMaster != nil {
		log.Warnf("cluster refresh master lookup failed, falling back to local refresh: %v", errMaster)
	} else {
		log.Warnf("cluster refresh master unavailable, falling back to local refresh")
	}

	return c.refreshLocalWithLock(ctx, authIndex)
}

// isSelf reports whether self.
func (c *RefreshController) isSelf(node *ClusterNodeRecord) bool {
	if c == nil || c.coordinator == nil || node == nil {
		return false
	}
	return strings.TrimSpace(node.IP) == strings.TrimSpace(c.coordinator.node.IP) && node.Port == c.coordinator.node.Port
}

// refreshLocalWithLock refreshes a local with lock.
func (c *RefreshController) refreshLocalWithLock(ctx context.Context, authIndex string) ([]byte, error) {
	// Resolve credential context before calling upstream OAuth services.
	if c == nil || c.runtime == nil || c.repo == nil {
		return nil, fmt.Errorf("cluster refresh: controller is not ready")
	}
	core := c.runtime.CoreManager()
	if core == nil {
		return nil, fmt.Errorf("cluster refresh: core manager is nil")
	}

	requestedIndex := strings.TrimSpace(authIndex)
	if requestedIndex == "" {
		return nil, fmt.Errorf("auth manager: missing auth index")
	}
	targetUUID, targetIndex, errTarget := c.refreshTarget(core, requestedIndex)
	if errTarget != nil {
		return nil, errTarget
	}

	var refreshErr error
	updated, errLock := c.repo.WithAuthRefreshLock(ctx, targetUUID, func(tx *Repository, auth *coreauth.Auth) (*coreauth.Auth, error) {
		refreshed, errRefresh := core.RefreshAuthCredential(ctx, auth)
		refreshErr = errRefresh
		if refreshed == nil {
			return nil, errRefresh
		}
		if _, errSave := tx.UpsertAuth(ctx, refreshed, "update"); errSave != nil {
			return nil, errSave
		}
		return refreshed, nil
	})
	if errLock != nil {
		return nil, errLock
	}
	if updated == nil {
		return nil, fmt.Errorf("auth manager: auth not found")
	}
	if errIndex := c.runtime.RefreshClusterAuthIndex(ctx, targetUUID); errIndex != nil {
		return nil, errIndex
	}
	if _, errUpdate := c.runtime.UpdateAuthInMemory(ctx, updated); errUpdate != nil {
		return nil, errUpdate
	}
	if refreshErr != nil {
		return nil, refreshErr
	}
	if targetIndex != requestedIndex {
		if requested, ok := core.GetByIndex(requestedIndex); ok && requested != nil {
			return home.BuildRefreshPayload(requested)
		}
		return nil, fmt.Errorf("auth manager: auth not found")
	}
	return home.BuildRefreshPayload(updated)
}

// refreshTarget refreshes a target.
func (c *RefreshController) refreshTarget(core *coreauth.Manager, authIndex string) (string, string, error) {
	// Resolve credential context before calling upstream OAuth services.
	if core == nil {
		return "", "", fmt.Errorf("cluster refresh: core manager is nil")
	}
	requested, okRequested := core.GetByIndex(authIndex)
	if !okRequested || requested == nil {
		return "", "", fmt.Errorf("auth manager: auth not found")
	}

	target := requested
	targetIndex := authIndex
	if requested.Attributes != nil {
		if parent := strings.TrimSpace(requested.Attributes["gemini_virtual_parent"]); parent != "" {
			parentAuth, okParent := core.GetByID(parent)
			if !okParent || parentAuth == nil {
				return "", "", fmt.Errorf("auth manager: auth not found")
			}
			target = parentAuth
			targetIndex = strings.TrimSpace(parentAuth.Index)
			if targetIndex == "" {
				targetIndex = strings.TrimSpace(parentAuth.EnsureIndex())
			}
		}
	}

	uuid := strings.TrimSpace(target.ID)
	if uuid == "" {
		uuid = strings.TrimSpace(target.Index)
	}
	if uuid == "" {
		return "", "", fmt.Errorf("auth manager: auth not found")
	}
	if targetIndex == "" {
		targetIndex = uuid
	}
	return uuid, targetIndex, nil
}
