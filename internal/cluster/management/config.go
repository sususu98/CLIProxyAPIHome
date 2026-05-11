package management

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	appconfig "github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"gopkg.in/yaml.v3"
)

func ConfigRootRoutes() []string {
	routes := make([]string, 0, len(configRootKeys()))
	for _, key := range configRootKeys() {
		if isCredentialConfigKey(key) {
			continue
		}
		routes = append(routes, "/"+key)
	}
	sort.Strings(routes)
	return routes
}

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

func (h *Handler) GetConfigYAML(c *gin.Context) {
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

func (h *Handler) PutConfigYAML(c *gin.Context) {
	body, errRead := io.ReadAll(c.Request.Body)
	if errRead != nil {
		respondError(c, http.StatusBadRequest, "invalid_yaml", fmt.Errorf("cannot read request body"))
		return
	}
	root, errRoot := configRootFromYAML(body)
	if errRoot != nil {
		respondError(c, http.StatusBadRequest, "invalid_yaml", errRoot)
		return
	}
	if _, errConfig := configFromRoot(root); errConfig != nil {
		respondError(c, http.StatusUnprocessableEntity, "invalid_config", errConfig)
		return
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()
	if errReplace := h.repo.ReplaceConfigSnapshot(ctx, root); errReplace != nil {
		respondError(c, http.StatusInternalServerError, "write_failed", errReplace)
		return
	}
	if errRefresh := h.refreshConfig(ctx); errRefresh != nil {
		respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "changed": []string{"config"}})
}

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

func (h *Handler) PutConfigRoot(route string) gin.HandlerFunc {
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

func (h *Handler) DeleteConfigRoot(route string) gin.HandlerFunc {
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

func configRootFromYAML(data []byte) (map[string]any, error) {
	var root map[string]any
	if errUnmarshal := yaml.Unmarshal(data, &root); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if root == nil {
		root = make(map[string]any)
	}
	for key := range root {
		if isCredentialConfigKey(key) {
			delete(root, key)
		}
	}
	return root, nil
}

func configFromRoot(root map[string]any) (*appconfig.Config, error) {
	cfg, _, errConfig := cluster.RuntimeConfigFromRoot(root)
	return cfg, errConfig
}

func readConfigValue(c *gin.Context, key string) (any, error) {
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

func configRootKeys() []string {
	cfgType := reflect.TypeOf(appconfig.Config{})
	keys := make([]string, 0, cfgType.NumField())
	for i := 0; i < cfgType.NumField(); i++ {
		field := cfgType.Field(i)
		if field.Anonymous {
			keys = append(keys, sdkConfigRootKeys()...)
			continue
		}
		key := yamlTagName(field.Tag.Get("yaml"))
		if key == "" || key == "-" {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func sdkConfigRootKeys() []string {
	cfgType := reflect.TypeOf(appconfig.SDKConfig{})
	keys := make([]string, 0, cfgType.NumField())
	for i := 0; i < cfgType.NumField(); i++ {
		key := yamlTagName(cfgType.Field(i).Tag.Get("yaml"))
		if key == "" || key == "-" {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func yamlTagName(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	if idx := strings.Index(tag, ","); idx >= 0 {
		tag = tag[:idx]
	}
	return strings.TrimSpace(tag)
}

func isCredentialConfigKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "auth-dir", "gemini-api-key", "vertex-api-key", "codex-api-key", "claude-api-key", "openai-compatibility":
		return true
	default:
		return false
	}
}
