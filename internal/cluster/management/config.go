package management

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	appconfig "github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"gopkg.in/yaml.v3"
)

// GetConfig returns a config.
func (h *Handler) GetConfig(c *gin.Context) {
	ctx, cancel := h.requestContext(c)
	defer cancel()

	root, errRoot := h.configRoot(ctx)
	if errRoot != nil {
		respondError(c, http.StatusInternalServerError, "config_load_failed", errRoot)
		return
	}
	if errCredential := h.applyCredentialConfig(ctx, root); errCredential != nil {
		respondError(c, http.StatusInternalServerError, "auth_load_failed", errCredential)
		return
	}
	cfg, errConfig := configFromRoot(root)
	if errConfig != nil {
		respondError(c, http.StatusInternalServerError, "config_load_failed", errConfig)
		return
	}
	c.JSON(http.StatusOK, cfg)
}

// GetConfigYAML returns a config yaml.
func (h *Handler) GetConfigYAML(c *gin.Context) {
	// Normalize source data before building the derived payload.
	ctx, cancel := h.requestContext(c)
	defer cancel()

	root, errSnapshot := h.configRoot(ctx)
	if errSnapshot != nil {
		respondError(c, http.StatusInternalServerError, "config_load_failed", errSnapshot)
		return
	}
	if errCredential := h.applyCredentialConfig(ctx, root); errCredential != nil {
		respondError(c, http.StatusInternalServerError, "auth_load_failed", errCredential)
		return
	}
	data, errMarshal := yaml.Marshal(root)
	if errMarshal != nil {
		respondError(c, http.StatusInternalServerError, "config_marshal_failed", errMarshal)
		return
	}
	c.Header("Content-Type", "application/yaml; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	_, _ = c.Writer.Write(data)
}

// PutConfigYAML replaces a config yaml.
func (h *Handler) PutConfigYAML(c *gin.Context) {
	// Normalize source data before building the derived payload.
	body, errRead := io.ReadAll(c.Request.Body)
	if errRead != nil {
		respondError(c, http.StatusBadRequest, "invalid_yaml", fmt.Errorf("cannot read request body"))
		return
	}
	fullRoot, errRoot := rawConfigRootFromYAML(body)
	if errRoot != nil {
		respondError(c, http.StatusBadRequest, "invalid_yaml", errRoot)
		return
	}
	root := configRootWithoutCredentials(fullRoot)
	if _, errConfig := configFromRoot(root); errConfig != nil {
		respondError(c, http.StatusUnprocessableEntity, "invalid_config", errConfig)
		return
	}
	credentialAuths, credentialsChanged, errCredentials := h.credentialConfigAuthsFromRoot(fullRoot)
	if errCredentials != nil {
		respondError(c, http.StatusBadRequest, "invalid_body", errCredentials)
		return
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()
	if errReplace := h.repo.ReplaceConfigSnapshot(ctx, root); errReplace != nil {
		respondError(c, http.StatusInternalServerError, "write_failed", errReplace)
		return
	}
	if credentialsChanged {
		if errReplaceCredentials := h.replaceCredentialConfigAuths(ctx, credentialAuths); errReplaceCredentials != nil {
			respondError(c, http.StatusInternalServerError, "write_failed", errReplaceCredentials)
			return
		}
	}
	if errRefresh := h.refreshConfig(ctx); errRefresh != nil {
		respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
		return
	}
	if credentialsChanged {
		if errRefreshAuths := h.refreshAuths(ctx); errRefreshAuths != nil {
			respondError(c, http.StatusInternalServerError, "reload_failed", errRefreshAuths)
			return
		}
	}
	changed := []string{"config"}
	if credentialsChanged {
		changed = append(changed, "auth")
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "changed": changed})
}

type credentialConfigAuthList struct {
	Key   string
	Auths []*coreauth.Auth
}

// credentialConfigAuthsFromRoot synthesizes DB-backed credentials present in YAML config.
func (h *Handler) credentialConfigAuthsFromRoot(root map[string]any) ([]credentialConfigAuthList, bool, error) {
	out := make([]credentialConfigAuthList, 0, 5)
	for _, key := range []string{"gemini-api-key", "vertex-api-key", "codex-api-key", "claude-api-key", "openai-compatibility"} {
		value, exists := root[key]
		if !exists {
			continue
		}
		rawJSON, errMarshal := json.Marshal(value)
		if errMarshal != nil {
			return nil, false, errMarshal
		}
		auths, errSynthesize := h.synthesizeAPIKeyBody(key, rawJSON)
		if errSynthesize != nil {
			return nil, false, errSynthesize
		}
		out = append(out, credentialConfigAuthList{Key: key, Auths: auths})
	}
	return out, len(out) > 0, nil
}

// replaceCredentialConfigAuths replaces DB-backed credentials present in YAML config.
func (h *Handler) replaceCredentialConfigAuths(ctx context.Context, entries []credentialConfigAuthList) error {
	for _, entry := range entries {
		if errReplace := h.replaceAPIKeyAuths(ctx, entry.Key, entry.Auths); errReplace != nil {
			return errReplace
		}
	}
	return nil
}

// GetConfigRoot returns a config root.
func (h *Handler) GetConfigRoot(route string) gin.HandlerFunc {
	key := strings.Trim(strings.TrimSpace(route), "/")
	return func(c *gin.Context) {
		ctx, cancel := h.requestContext(c)
		defer cancel()

		root, errSnapshot := h.configRoot(ctx)
		if errSnapshot != nil {
			respondError(c, http.StatusInternalServerError, "config_load_failed", errSnapshot)
			return
		}
		c.JSON(http.StatusOK, gin.H{key: root[key]})
	}
}

// PutConfigRoot replaces a config root.
func (h *Handler) PutConfigRoot(route string) gin.HandlerFunc {
	// Normalize source data before building the derived payload.
	key := strings.Trim(strings.TrimSpace(route), "/")
	return func(c *gin.Context) {
		value, errValue := readConfigValue(c, key)
		if errValue != nil {
			respondError(c, http.StatusBadRequest, "invalid body", errValue)
			return
		}
		ctx, cancel := h.requestContext(c)
		defer cancel()
		root, errSnapshot := h.configRoot(ctx)
		if errSnapshot != nil {
			respondError(c, http.StatusInternalServerError, "config_load_failed", errSnapshot)
			return
		}
		root[key] = value
		if _, errConfig := configFromRoot(root); errConfig != nil {
			respondError(c, http.StatusUnprocessableEntity, "invalid_config", errConfig)
			return
		}
		if errUpsert := h.repo.UpsertConfigValue(ctx, key, value); errUpsert != nil {
			respondError(c, http.StatusInternalServerError, "write_failed", errUpsert)
			return
		}
		if errRefresh := h.refreshConfig(ctx); errRefresh != nil {
			respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
			return
		}
		respondOK(c)
	}
}

// DeleteConfigRoot deletes a config root.
func (h *Handler) DeleteConfigRoot(route string) gin.HandlerFunc {
	// Normalize source data before building the derived payload.
	key := strings.Trim(strings.TrimSpace(route), "/")
	return func(c *gin.Context) {
		ctx, cancel := h.requestContext(c)
		defer cancel()
		root, errSnapshot := h.configRoot(ctx)
		if errSnapshot != nil {
			respondError(c, http.StatusInternalServerError, "config_load_failed", errSnapshot)
			return
		}
		delete(root, key)
		if _, errConfig := configFromRoot(root); errConfig != nil {
			respondError(c, http.StatusUnprocessableEntity, "invalid_config", errConfig)
			return
		}
		if errReplace := h.repo.ReplaceConfigSnapshot(ctx, root); errReplace != nil {
			respondError(c, http.StatusInternalServerError, "write_failed", errReplace)
			return
		}
		if errRefresh := h.refreshConfig(ctx); errRefresh != nil {
			respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
			return
		}
		respondOK(c)
	}
}

// configRoot handles a config root.
func (h *Handler) configRoot(ctx context.Context) (map[string]any, error) {
	if h == nil || h.repo == nil {
		return nil, fmt.Errorf("cluster management repository is nil")
	}
	snapshot, errSnapshot := h.repo.LoadConfigSnapshot(ctx)
	if errSnapshot != nil {
		return nil, errSnapshot
	}
	return cluster.ConfigRootFromSnapshot(snapshot)
}

// applyCredentialConfig applies a credential config.
func (h *Handler) applyCredentialConfig(ctx context.Context, root map[string]any) error {
	if h == nil || h.repo == nil {
		return fmt.Errorf("cluster management repository is nil")
	}
	if root == nil {
		return fmt.Errorf("config root is nil")
	}
	auths, errAuths := h.repo.ListAuths(ctx)
	if errAuths != nil {
		return errAuths
	}
	cluster.ApplyCredentialConfigToRoot(root, auths)
	return nil
}

// rawConfigRootFromYAML derives config root from yaml without dropping credential roots.
func rawConfigRootFromYAML(data []byte) (map[string]any, error) {
	var root map[string]any
	if errUnmarshal := yaml.Unmarshal(data, &root); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if root == nil {
		root = make(map[string]any)
	}
	return root, nil
}

// configRootFromYAML derives config root from yaml.
func configRootFromYAML(data []byte) (map[string]any, error) {
	root, errRoot := rawConfigRootFromYAML(data)
	if errRoot != nil {
		return nil, errRoot
	}
	return configRootWithoutCredentials(root), nil
}

// configRootWithoutCredentials returns the config roots persisted in config snapshot.
func configRootWithoutCredentials(root map[string]any) map[string]any {
	out := make(map[string]any, len(root))
	for key, value := range root {
		if isCredentialConfigKey(key) {
			continue
		}
		out[key] = value
	}
	return out
}

// configFromRoot derives config from root.
func configFromRoot(root map[string]any) (*appconfig.Config, error) {
	cfg, _, errConfig := cluster.RuntimeConfigFromRoot(root)
	return cfg, errConfig
}

// readConfigValue reads a config value.
func readConfigValue(c *gin.Context, key string) (any, error) {
	// Normalize source data before building the derived payload.
	body, errRead := io.ReadAll(c.Request.Body)
	if errRead != nil {
		return nil, errRead
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	var value any
	if errUnmarshal := json.Unmarshal(body, &value); errUnmarshal == nil {
		if object, ok := value.(map[string]any); ok {
			if rawValue, exists := object["value"]; exists {
				return rawValue, nil
			}
			if rawValue, exists := object[key]; exists {
				return rawValue, nil
			}
		}
		return value, nil
	}
	if errUnmarshalYAML := yaml.Unmarshal(body, &value); errUnmarshalYAML != nil {
		return nil, errUnmarshalYAML
	}
	if object, ok := value.(map[string]any); ok {
		if rawValue, exists := object["value"]; exists {
			return rawValue, nil
		}
		if rawValue, exists := object[key]; exists {
			return rawValue, nil
		}
	}
	return value, nil
}

// isCredentialConfigKey reports whether credential config key.
func isCredentialConfigKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "auth-dir", "gemini-api-key", "vertex-api-key", "codex-api-key", "claude-api-key", "openai-compatibility":
		return true
	default:
		return false
	}
}
