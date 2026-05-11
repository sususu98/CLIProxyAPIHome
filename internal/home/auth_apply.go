package home

import (
	"context"
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPIHome/internal/registry"
	"github.com/router-for-me/CLIProxyAPIHome/internal/runtime/geminicli"
	log "github.com/sirupsen/logrus"
)

func (r *Runtime) applyCoreAuthAddOrUpdate(ctx context.Context, auth *coreauth.Auth) {
	if r == nil || r.coreManager == nil || auth == nil || auth.ID == "" {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	auth = auth.Clone()

	op := "register"
	var err error
	if existing, ok := r.coreManager.GetByID(auth.ID); ok && existing != nil {
		auth.CreatedAt = existing.CreatedAt
		if !existing.Disabled && existing.Status != coreauth.StatusDisabled && !auth.Disabled && auth.Status != coreauth.StatusDisabled {
			auth.LastRefreshedAt = existing.LastRefreshedAt
			auth.NextRefreshAfter = existing.NextRefreshAfter
			if len(auth.ModelStates) == 0 && len(existing.ModelStates) > 0 {
				auth.ModelStates = existing.ModelStates
			}
		}
		op = "update"
		_, err = r.coreManager.Update(ctx, auth)
	} else {
		_, err = r.coreManager.Register(ctx, auth)
	}
	if err != nil {
		log.Errorf("failed to %s auth %s: %v", op, auth.ID, err)
		current, ok := r.coreManager.GetByID(auth.ID)
		if !ok || current == nil || current.Disabled {
			registry.GetGlobalRegistry().UnregisterClient(auth.ID)
			return
		}
		auth = current
	}

	r.registerModelsForAuth(auth)
	r.coreManager.ReconcileRegistryModelStates(ctx, auth.ID)
	r.coreManager.RefreshSchedulerEntry(auth.ID)
}

func (r *Runtime) loadClusterAuths(ctx context.Context, adapter ClusterAdapter) error {
	if r == nil || r.coreManager == nil || adapter == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if errLoadIndex := adapter.LoadAuthIndex(ctx); errLoadIndex != nil {
		return errLoadIndex
	}

	auths := adapter.ListMinimalAuths()
	desired := make(map[string]*coreauth.Auth, len(auths))
	ctxSkipPersist := coreauth.WithSkipPersist(ctx)
	for _, auth := range auths {
		if auth == nil || strings.TrimSpace(auth.ID) == "" {
			continue
		}
		auth.ID = strings.TrimSpace(auth.ID)
		auth.Index = auth.ID
		desired[auth.ID] = auth
		r.applyCoreAuthAddOrUpdate(ctxSkipPersist, auth)
	}

	removed := 0
	current := r.coreManager.List()
	for _, auth := range current {
		if auth == nil || strings.TrimSpace(auth.ID) == "" {
			continue
		}
		if _, ok := desired[auth.ID]; ok {
			continue
		}
		r.applyCoreAuthRemove(ctxSkipPersist, auth.ID)
		removed++
	}

	log.Infof("loaded cluster auth index (auths=%d removed=%d)", len(desired), removed)
	return nil
}

func (r *Runtime) registerModelRefreshCallback() {
	registry.SetModelRefreshCallback(func(changedProviders []string) {
		if r == nil || r.coreManager == nil || len(changedProviders) == 0 {
			return
		}

		providerSet := make(map[string]bool, len(changedProviders))
		for _, p := range changedProviders {
			providerSet[strings.ToLower(strings.TrimSpace(p))] = true
		}

		auths := r.coreManager.List()
	refreshedLoop:
		for _, item := range auths {
			if item == nil || item.ID == "" {
				continue
			}
			auth, ok := r.coreManager.GetByID(item.ID)
			if !ok || auth == nil || auth.Disabled {
				continue
			}
			provider := strings.ToLower(strings.TrimSpace(auth.Provider))
			if !providerSet[provider] {
				continue
			}
			r.registerModelsForAuth(auth)
			r.coreManager.ReconcileRegistryModelStates(context.Background(), auth.ID)
			r.coreManager.RefreshSchedulerEntry(auth.ID)
			continue refreshedLoop
		}
	})
}

func (r *Runtime) applyCoreAuthRemove(ctx context.Context, authID string) {
	if r == nil || r.coreManager == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return
	}

	// Best-effort logging context before deletion.
	provider := ""
	label := ""
	if auth, ok := r.coreManager.GetByID(authID); ok && auth != nil {
		provider = strings.TrimSpace(auth.Provider)
		label = strings.TrimSpace(auth.Label)
	}

	if errDel := r.coreManager.Delete(ctx, authID); errDel != nil {
		log.Errorf("failed to delete auth %s: %v", authID, errDel)
	}
	registry.GetGlobalRegistry().UnregisterClient(authID)

	if provider != "" {
		log.Infof("auth removed (auth=%s provider=%s label=%s)", authID, provider, label)
	} else {
		log.Infof("auth removed (auth=%s)", authID)
	}
}

func (r *Runtime) availableProviderKeys() []string {
	if r == nil || r.coreManager == nil {
		return nil
	}

	auths := r.coreManager.List()
	seen := make(map[string]struct{}, len(auths))
	out := make([]string, 0, len(auths))
	for _, auth := range auths {
		if auth == nil || auth.Disabled {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(auth.Provider))
		if provider == "" {
			continue
		}
		if _, ok := seen[provider]; ok {
			continue
		}
		seen[provider] = struct{}{}
		out = append(out, provider)
	}
	return out
}

func extractAccessToken(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}

	metadata := auth.Metadata
	if shared := geminicli.ResolveSharedCredential(auth.Runtime); shared != nil {
		if snapshot := shared.MetadataSnapshot(); len(snapshot) > 0 {
			metadata = snapshot
		}
	}
	if metadata == nil {
		return ""
	}

	if v, ok := metadata["access_token"].(string); ok {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}

	for _, nestedKey := range []string{"token", "Token"} {
		raw, ok := metadata[nestedKey]
		if !ok || raw == nil {
			continue
		}
		switch tokenMap := raw.(type) {
		case map[string]any:
			if v, ok := tokenMap["access_token"].(string); ok {
				if trimmed := strings.TrimSpace(v); trimmed != "" {
					return trimmed
				}
			}
		case map[string]string:
			if v, ok := tokenMap["access_token"]; ok {
				if trimmed := strings.TrimSpace(v); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
