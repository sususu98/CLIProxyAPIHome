package management

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	appconfig "github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"gopkg.in/yaml.v3"
)

func (h *Handler) loadRuntimeConfig(c *gin.Context) (context.Context, context.CancelFunc, *appconfig.Config, bool) {
	ctx, cancel := h.requestContext(c)
	cfg, _, errConfig := h.currentConfig(ctx)
	if errConfig != nil {
		cancel()
		respondError(c, http.StatusInternalServerError, "config_load_failed", errConfig)
		return nil, nil, nil, false
	}
	return ctx, cancel, cfg, true
}

func (h *Handler) persistConfigRootKey(c *gin.Context, ctx context.Context, cfg *appconfig.Config, key string) bool {
	if errPersist := h.saveRuntimeConfigRootKey(ctx, cfg, key); errPersist != nil {
		respondError(c, http.StatusInternalServerError, "write_failed", errPersist)
		return false
	}
	if errRefresh := h.refreshConfig(ctx); errRefresh != nil {
		respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
		return false
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
	return true
}

func (h *Handler) saveRuntimeConfigRootKey(ctx context.Context, cfg *appconfig.Config, key string) error {
	root, errRoot := runtimeConfigRoot(cfg)
	if errRoot != nil {
		return errRoot
	}
	if value, ok := root[key]; ok {
		return h.repo.UpsertConfigValue(ctx, key, value)
	}

	currentRoot, errCurrentRoot := h.configRoot(ctx)
	if errCurrentRoot != nil {
		return errCurrentRoot
	}
	delete(currentRoot, key)
	return h.repo.ReplaceConfigSnapshot(ctx, currentRoot)
}

func runtimeConfigRoot(cfg *appconfig.Config) (map[string]any, error) {
	if cfg == nil {
		cfg = &appconfig.Config{}
	}
	data, errMarshal := yaml.Marshal(cfg)
	if errMarshal != nil {
		return nil, errMarshal
	}
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

func (h *Handler) updateBoolConfigField(c *gin.Context, rootKey string, set func(*appconfig.Config, bool)) {
	var body struct {
		Value *bool `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	set(cfg, *body.Value)
	h.persistConfigRootKey(c, ctx, cfg, rootKey)
}

func (h *Handler) updateIntConfigField(c *gin.Context, rootKey string, set func(*appconfig.Config, int)) {
	var body struct {
		Value *int `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	set(cfg, *body.Value)
	h.persistConfigRootKey(c, ctx, cfg, rootKey)
}

func (h *Handler) updateStringConfigField(c *gin.Context, rootKey string, set func(*appconfig.Config, string)) {
	var body struct {
		Value *string `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	set(cfg, *body.Value)
	h.persistConfigRootKey(c, ctx, cfg, rootKey)
}

func (h *Handler) GetDebug(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"debug": cfg.Debug})
}

func (h *Handler) PutDebug(c *gin.Context) {
	h.updateBoolConfigField(c, "debug", func(cfg *appconfig.Config, value bool) { cfg.Debug = value })
}

func (h *Handler) GetUsageStatisticsEnabled(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"usage-statistics-enabled": cfg.UsageStatisticsEnabled})
}

func (h *Handler) PutUsageStatisticsEnabled(c *gin.Context) {
	h.updateBoolConfigField(c, "usage-statistics-enabled", func(cfg *appconfig.Config, value bool) { cfg.UsageStatisticsEnabled = value })
}

func (h *Handler) GetLoggingToFile(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"logging-to-file": cfg.LoggingToFile})
}

func (h *Handler) PutLoggingToFile(c *gin.Context) {
	h.updateBoolConfigField(c, "logging-to-file", func(cfg *appconfig.Config, value bool) { cfg.LoggingToFile = value })
}

func (h *Handler) GetLogsMaxTotalSizeMB(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"logs-max-total-size-mb": cfg.LogsMaxTotalSizeMB})
}

func (h *Handler) PutLogsMaxTotalSizeMB(c *gin.Context) {
	var body struct {
		Value *int `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	value := *body.Value
	if value < 0 {
		value = 0
	}
	cfg.LogsMaxTotalSizeMB = value
	h.persistConfigRootKey(c, ctx, cfg, "logs-max-total-size-mb")
}

func (h *Handler) GetErrorLogsMaxFiles(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"error-logs-max-files": cfg.ErrorLogsMaxFiles})
}

func (h *Handler) PutErrorLogsMaxFiles(c *gin.Context) {
	var body struct {
		Value *int `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	value := *body.Value
	if value < 0 {
		value = 10
	}
	cfg.ErrorLogsMaxFiles = value
	h.persistConfigRootKey(c, ctx, cfg, "error-logs-max-files")
}

func (h *Handler) GetRequestLog(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"request-log": cfg.RequestLog})
}

func (h *Handler) PutRequestLog(c *gin.Context) {
	h.updateBoolConfigField(c, "request-log", func(cfg *appconfig.Config, value bool) { cfg.RequestLog = value })
}

func (h *Handler) GetWebsocketAuth(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"ws-auth": cfg.WebsocketAuth})
}

func (h *Handler) PutWebsocketAuth(c *gin.Context) {
	h.updateBoolConfigField(c, "ws-auth", func(cfg *appconfig.Config, value bool) { cfg.WebsocketAuth = value })
}

func (h *Handler) GetRequestRetry(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"request-retry": cfg.RequestRetry})
}

func (h *Handler) PutRequestRetry(c *gin.Context) {
	h.updateIntConfigField(c, "request-retry", func(cfg *appconfig.Config, value int) { cfg.RequestRetry = value })
}

func (h *Handler) GetMaxRetryInterval(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"max-retry-interval": cfg.MaxRetryInterval})
}

func (h *Handler) PutMaxRetryInterval(c *gin.Context) {
	h.updateIntConfigField(c, "max-retry-interval", func(cfg *appconfig.Config, value int) { cfg.MaxRetryInterval = value })
}

func (h *Handler) GetForceModelPrefix(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"force-model-prefix": cfg.ForceModelPrefix})
}

func (h *Handler) PutForceModelPrefix(c *gin.Context) {
	h.updateBoolConfigField(c, "force-model-prefix", func(cfg *appconfig.Config, value bool) { cfg.ForceModelPrefix = value })
}

func normalizeClusterRoutingStrategy(strategy string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(strategy))
	switch normalized {
	case "", "round-robin", "roundrobin", "rr":
		return "round-robin", true
	case "fill-first", "fillfirst", "ff":
		return "fill-first", true
	default:
		return "", false
	}
}

func (h *Handler) GetRoutingStrategy(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	strategy, okNormalize := normalizeClusterRoutingStrategy(cfg.Routing.Strategy)
	if !okNormalize {
		c.JSON(http.StatusOK, gin.H{"strategy": strings.TrimSpace(cfg.Routing.Strategy)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"strategy": strategy})
}

func (h *Handler) PutRoutingStrategy(c *gin.Context) {
	var body struct {
		Value *string `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	normalized, okNormalize := normalizeClusterRoutingStrategy(*body.Value)
	if !okNormalize {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid strategy"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	cfg.Routing.Strategy = normalized
	h.persistConfigRootKey(c, ctx, cfg, "routing")
}

func (h *Handler) GetProxyURL(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"proxy-url": cfg.ProxyURL})
}

func (h *Handler) PutProxyURL(c *gin.Context) {
	h.updateStringConfigField(c, "proxy-url", func(cfg *appconfig.Config, value string) { cfg.ProxyURL = value })
}

func (h *Handler) DeleteProxyURL(c *gin.Context) {
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	cfg.ProxyURL = ""
	h.persistConfigRootKey(c, ctx, cfg, "proxy-url")
}

func (h *Handler) GetSwitchProject(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"switch-project": cfg.QuotaExceeded.SwitchProject})
}

func (h *Handler) PutSwitchProject(c *gin.Context) {
	h.updateBoolConfigField(c, "quota-exceeded", func(cfg *appconfig.Config, value bool) { cfg.QuotaExceeded.SwitchProject = value })
}

func (h *Handler) GetSwitchPreviewModel(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"switch-preview-model": cfg.QuotaExceeded.SwitchPreviewModel})
}

func (h *Handler) PutSwitchPreviewModel(c *gin.Context) {
	h.updateBoolConfigField(c, "quota-exceeded", func(cfg *appconfig.Config, value bool) { cfg.QuotaExceeded.SwitchPreviewModel = value })
}

func (h *Handler) GetAPIKeys(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"api-keys": cfg.APIKeys})
}

func (h *Handler) PutAPIKeys(c *gin.Context) {
	h.putStringList(c, "api-keys", func(cfg *appconfig.Config, values []string) {
		cfg.APIKeys = append([]string(nil), values...)
	})
}

func (h *Handler) PatchAPIKeys(c *gin.Context) {
	h.patchStringList(c, "api-keys", func(cfg *appconfig.Config) *[]string { return &cfg.APIKeys })
}

func (h *Handler) DeleteAPIKeys(c *gin.Context) {
	h.deleteFromStringList(c, "api-keys", func(cfg *appconfig.Config) *[]string { return &cfg.APIKeys })
}

func (h *Handler) putStringList(c *gin.Context, rootKey string, set func(*appconfig.Config, []string)) {
	data, errData := c.GetRawData()
	if errData != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}
	var values []string
	if errUnmarshal := json.Unmarshal(data, &values); errUnmarshal != nil {
		var obj struct {
			Items []string `json:"items"`
		}
		if errUnmarshalObj := json.Unmarshal(data, &obj); errUnmarshalObj != nil || len(obj.Items) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		values = obj.Items
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	set(cfg, values)
	h.persistConfigRootKey(c, ctx, cfg, rootKey)
}

func (h *Handler) patchStringList(c *gin.Context, rootKey string, target func(*appconfig.Config) *[]string) {
	var body struct {
		Old   *string `json:"old"`
		New   *string `json:"new"`
		Index *int    `json:"index"`
		Value *string `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	values := target(cfg)
	if body.Index != nil && body.Value != nil && *body.Index >= 0 && *body.Index < len(*values) {
		(*values)[*body.Index] = *body.Value
		h.persistConfigRootKey(c, ctx, cfg, rootKey)
		return
	}
	if body.Old != nil && body.New != nil {
		for i := range *values {
			if (*values)[i] == *body.Old {
				(*values)[i] = *body.New
				h.persistConfigRootKey(c, ctx, cfg, rootKey)
				return
			}
		}
		*values = append(*values, *body.New)
		h.persistConfigRootKey(c, ctx, cfg, rootKey)
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "missing fields"})
}

func (h *Handler) deleteFromStringList(c *gin.Context, rootKey string, target func(*appconfig.Config) *[]string) {
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	values := target(cfg)
	if idxRaw := c.Query("index"); idxRaw != "" {
		idx, errAtoi := strconv.Atoi(idxRaw)
		if errAtoi == nil && idx >= 0 && idx < len(*values) {
			*values = append((*values)[:idx], (*values)[idx+1:]...)
			h.persistConfigRootKey(c, ctx, cfg, rootKey)
			return
		}
	}
	if value := strings.TrimSpace(c.Query("value")); value != "" {
		out := make([]string, 0, len(*values))
		for _, current := range *values {
			if strings.TrimSpace(current) != value {
				out = append(out, current)
			}
		}
		*values = out
		h.persistConfigRootKey(c, ctx, cfg, rootKey)
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "missing index or value"})
}

func (h *Handler) GetOAuthExcludedModels(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"oauth-excluded-models": appconfig.NormalizeOAuthExcludedModels(cfg.OAuthExcludedModels)})
}

func (h *Handler) PutOAuthExcludedModels(c *gin.Context) {
	data, errData := c.GetRawData()
	if errData != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}
	var entries map[string][]string
	if errUnmarshal := json.Unmarshal(data, &entries); errUnmarshal != nil {
		var wrapper struct {
			Items map[string][]string `json:"items"`
		}
		if errUnmarshalWrapper := json.Unmarshal(data, &wrapper); errUnmarshalWrapper != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		entries = wrapper.Items
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	cfg.OAuthExcludedModels = appconfig.NormalizeOAuthExcludedModels(entries)
	h.persistConfigRootKey(c, ctx, cfg, "oauth-excluded-models")
}

func (h *Handler) PatchOAuthExcludedModels(c *gin.Context) {
	var body struct {
		Provider *string  `json:"provider"`
		Models   []string `json:"models"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || body.Provider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	provider := strings.ToLower(strings.TrimSpace(*body.Provider))
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid provider"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	normalized := appconfig.NormalizeExcludedModels(body.Models)
	if len(normalized) == 0 {
		if cfg.OAuthExcludedModels == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
			return
		}
		if _, exists := cfg.OAuthExcludedModels[provider]; !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
			return
		}
		delete(cfg.OAuthExcludedModels, provider)
		if len(cfg.OAuthExcludedModels) == 0 {
			cfg.OAuthExcludedModels = nil
		}
		h.persistConfigRootKey(c, ctx, cfg, "oauth-excluded-models")
		return
	}
	if cfg.OAuthExcludedModels == nil {
		cfg.OAuthExcludedModels = make(map[string][]string)
	}
	cfg.OAuthExcludedModels[provider] = normalized
	h.persistConfigRootKey(c, ctx, cfg, "oauth-excluded-models")
}

func (h *Handler) DeleteOAuthExcludedModels(c *gin.Context) {
	provider := strings.ToLower(strings.TrimSpace(c.Query("provider")))
	if provider == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing provider"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	if cfg.OAuthExcludedModels == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	if _, exists := cfg.OAuthExcludedModels[provider]; !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "provider not found"})
		return
	}
	delete(cfg.OAuthExcludedModels, provider)
	if len(cfg.OAuthExcludedModels) == 0 {
		cfg.OAuthExcludedModels = nil
	}
	h.persistConfigRootKey(c, ctx, cfg, "oauth-excluded-models")
}

func (h *Handler) GetOAuthModelAlias(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"oauth-model-alias": sanitizedOAuthModelAlias(cfg.OAuthModelAlias)})
}

func (h *Handler) PutOAuthModelAlias(c *gin.Context) {
	data, errData := c.GetRawData()
	if errData != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
		return
	}
	var entries map[string][]appconfig.OAuthModelAlias
	if errUnmarshal := json.Unmarshal(data, &entries); errUnmarshal != nil {
		var wrapper struct {
			Items map[string][]appconfig.OAuthModelAlias `json:"items"`
		}
		if errUnmarshalWrapper := json.Unmarshal(data, &wrapper); errUnmarshalWrapper != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		entries = wrapper.Items
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	cfg.OAuthModelAlias = sanitizedOAuthModelAlias(entries)
	h.persistConfigRootKey(c, ctx, cfg, "oauth-model-alias")
}

func (h *Handler) PatchOAuthModelAlias(c *gin.Context) {
	var body struct {
		Provider *string                     `json:"provider"`
		Channel  *string                     `json:"channel"`
		Aliases  []appconfig.OAuthModelAlias `json:"aliases"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	channelRaw := ""
	if body.Channel != nil {
		channelRaw = *body.Channel
	} else if body.Provider != nil {
		channelRaw = *body.Provider
	}
	channel := strings.ToLower(strings.TrimSpace(channelRaw))
	if channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	normalizedMap := sanitizedOAuthModelAlias(map[string][]appconfig.OAuthModelAlias{channel: body.Aliases})
	normalized := normalizedMap[channel]
	if len(normalized) == 0 {
		if cfg.OAuthModelAlias == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
			return
		}
		if _, exists := cfg.OAuthModelAlias[channel]; !exists {
			c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
			return
		}
		delete(cfg.OAuthModelAlias, channel)
		if len(cfg.OAuthModelAlias) == 0 {
			cfg.OAuthModelAlias = nil
		}
		h.persistConfigRootKey(c, ctx, cfg, "oauth-model-alias")
		return
	}
	if cfg.OAuthModelAlias == nil {
		cfg.OAuthModelAlias = make(map[string][]appconfig.OAuthModelAlias)
	}
	cfg.OAuthModelAlias[channel] = normalized
	h.persistConfigRootKey(c, ctx, cfg, "oauth-model-alias")
}

func (h *Handler) DeleteOAuthModelAlias(c *gin.Context) {
	channel := strings.ToLower(strings.TrimSpace(c.Query("channel")))
	if channel == "" {
		channel = strings.ToLower(strings.TrimSpace(c.Query("provider")))
	}
	if channel == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing channel"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	if cfg.OAuthModelAlias == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}
	if _, exists := cfg.OAuthModelAlias[channel]; !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}
	delete(cfg.OAuthModelAlias, channel)
	if len(cfg.OAuthModelAlias) == 0 {
		cfg.OAuthModelAlias = nil
	}
	h.persistConfigRootKey(c, ctx, cfg, "oauth-model-alias")
}

func sanitizedOAuthModelAlias(entries map[string][]appconfig.OAuthModelAlias) map[string][]appconfig.OAuthModelAlias {
	if len(entries) == 0 {
		return nil
	}
	copied := make(map[string][]appconfig.OAuthModelAlias, len(entries))
	for channel, aliases := range entries {
		if len(aliases) == 0 {
			continue
		}
		copied[channel] = append([]appconfig.OAuthModelAlias(nil), aliases...)
	}
	if len(copied) == 0 {
		return nil
	}
	cfg := appconfig.Config{OAuthModelAlias: copied}
	cfg.SanitizeOAuthModelAlias()
	if len(cfg.OAuthModelAlias) == 0 {
		return nil
	}
	return cfg.OAuthModelAlias
}

func (h *Handler) GetAmpCode(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"ampcode": cfg.AmpCode})
}

func (h *Handler) GetAmpUpstreamURL(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"upstream-url": cfg.AmpCode.UpstreamURL})
}

func (h *Handler) PutAmpUpstreamURL(c *gin.Context) {
	h.updateStringConfigField(c, "ampcode", func(cfg *appconfig.Config, value string) { cfg.AmpCode.UpstreamURL = strings.TrimSpace(value) })
}

func (h *Handler) DeleteAmpUpstreamURL(c *gin.Context) {
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	cfg.AmpCode.UpstreamURL = ""
	h.persistConfigRootKey(c, ctx, cfg, "ampcode")
}

func (h *Handler) GetAmpUpstreamAPIKey(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"upstream-api-key": cfg.AmpCode.UpstreamAPIKey})
}

func (h *Handler) PutAmpUpstreamAPIKey(c *gin.Context) {
	h.updateStringConfigField(c, "ampcode", func(cfg *appconfig.Config, value string) { cfg.AmpCode.UpstreamAPIKey = strings.TrimSpace(value) })
}

func (h *Handler) DeleteAmpUpstreamAPIKey(c *gin.Context) {
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	cfg.AmpCode.UpstreamAPIKey = ""
	h.persistConfigRootKey(c, ctx, cfg, "ampcode")
}

func (h *Handler) GetAmpRestrictManagementToLocalhost(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"restrict-management-to-localhost": cfg.AmpCode.RestrictManagementToLocalhost})
}

func (h *Handler) PutAmpRestrictManagementToLocalhost(c *gin.Context) {
	h.updateBoolConfigField(c, "ampcode", func(cfg *appconfig.Config, value bool) { cfg.AmpCode.RestrictManagementToLocalhost = value })
}

func (h *Handler) GetAmpModelMappings(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"model-mappings": cfg.AmpCode.ModelMappings})
}

func (h *Handler) PutAmpModelMappings(c *gin.Context) {
	var body struct {
		Value []appconfig.AmpModelMapping `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	cfg.AmpCode.ModelMappings = body.Value
	h.persistConfigRootKey(c, ctx, cfg, "ampcode")
}

func (h *Handler) PatchAmpModelMappings(c *gin.Context) {
	var body struct {
		Value []appconfig.AmpModelMapping `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	existing := make(map[string]int)
	for i, mapping := range cfg.AmpCode.ModelMappings {
		existing[strings.TrimSpace(mapping.From)] = i
	}
	for _, newMapping := range body.Value {
		from := strings.TrimSpace(newMapping.From)
		if idx, exists := existing[from]; exists {
			cfg.AmpCode.ModelMappings[idx] = newMapping
		} else {
			cfg.AmpCode.ModelMappings = append(cfg.AmpCode.ModelMappings, newMapping)
			existing[from] = len(cfg.AmpCode.ModelMappings) - 1
		}
	}
	h.persistConfigRootKey(c, ctx, cfg, "ampcode")
}

func (h *Handler) DeleteAmpModelMappings(c *gin.Context) {
	var body struct {
		Value []string `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil || len(body.Value) == 0 {
		ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
		if !ok {
			return
		}
		defer cancel()
		cfg.AmpCode.ModelMappings = nil
		h.persistConfigRootKey(c, ctx, cfg, "ampcode")
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	toRemove := make(map[string]bool)
	for _, from := range body.Value {
		toRemove[strings.TrimSpace(from)] = true
	}
	newMappings := make([]appconfig.AmpModelMapping, 0, len(cfg.AmpCode.ModelMappings))
	for _, mapping := range cfg.AmpCode.ModelMappings {
		if !toRemove[strings.TrimSpace(mapping.From)] {
			newMappings = append(newMappings, mapping)
		}
	}
	cfg.AmpCode.ModelMappings = newMappings
	h.persistConfigRootKey(c, ctx, cfg, "ampcode")
}

func (h *Handler) GetAmpForceModelMappings(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"force-model-mappings": cfg.AmpCode.ForceModelMappings})
}

func (h *Handler) PutAmpForceModelMappings(c *gin.Context) {
	h.updateBoolConfigField(c, "ampcode", func(cfg *appconfig.Config, value bool) { cfg.AmpCode.ForceModelMappings = value })
}

func (h *Handler) GetAmpUpstreamAPIKeys(c *gin.Context) {
	_, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	c.JSON(http.StatusOK, gin.H{"upstream-api-keys": cfg.AmpCode.UpstreamAPIKeys})
}

func (h *Handler) PutAmpUpstreamAPIKeys(c *gin.Context) {
	var body struct {
		Value []appconfig.AmpUpstreamAPIKeyEntry `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	cfg.AmpCode.UpstreamAPIKeys = normalizeAmpUpstreamAPIKeyEntries(body.Value)
	h.persistConfigRootKey(c, ctx, cfg, "ampcode")
}

func (h *Handler) PatchAmpUpstreamAPIKeys(c *gin.Context) {
	var body struct {
		Value []appconfig.AmpUpstreamAPIKeyEntry `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	existing := make(map[string]int)
	for i, entry := range cfg.AmpCode.UpstreamAPIKeys {
		existing[strings.TrimSpace(entry.UpstreamAPIKey)] = i
	}
	for _, newEntry := range body.Value {
		upstreamKey := strings.TrimSpace(newEntry.UpstreamAPIKey)
		if upstreamKey == "" {
			continue
		}
		normalizedEntry := appconfig.AmpUpstreamAPIKeyEntry{
			UpstreamAPIKey: upstreamKey,
			APIKeys:        normalizeAPIKeysList(newEntry.APIKeys),
		}
		if idx, exists := existing[upstreamKey]; exists {
			cfg.AmpCode.UpstreamAPIKeys[idx] = normalizedEntry
		} else {
			cfg.AmpCode.UpstreamAPIKeys = append(cfg.AmpCode.UpstreamAPIKeys, normalizedEntry)
			existing[upstreamKey] = len(cfg.AmpCode.UpstreamAPIKeys) - 1
		}
	}
	h.persistConfigRootKey(c, ctx, cfg, "ampcode")
}

func (h *Handler) DeleteAmpUpstreamAPIKeys(c *gin.Context) {
	var body struct {
		Value []string `json:"value"`
	}
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	if body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing value"})
		return
	}
	ctx, cancel, cfg, ok := h.loadRuntimeConfig(c)
	if !ok {
		return
	}
	defer cancel()
	if len(body.Value) == 0 {
		cfg.AmpCode.UpstreamAPIKeys = nil
		h.persistConfigRootKey(c, ctx, cfg, "ampcode")
		return
	}
	toRemove := make(map[string]bool)
	for _, key := range body.Value {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			toRemove[trimmed] = true
		}
	}
	if len(toRemove) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "empty value"})
		return
	}
	newEntries := make([]appconfig.AmpUpstreamAPIKeyEntry, 0, len(cfg.AmpCode.UpstreamAPIKeys))
	for _, entry := range cfg.AmpCode.UpstreamAPIKeys {
		if !toRemove[strings.TrimSpace(entry.UpstreamAPIKey)] {
			newEntries = append(newEntries, entry)
		}
	}
	cfg.AmpCode.UpstreamAPIKeys = newEntries
	h.persistConfigRootKey(c, ctx, cfg, "ampcode")
}

func normalizeAmpUpstreamAPIKeyEntries(entries []appconfig.AmpUpstreamAPIKeyEntry) []appconfig.AmpUpstreamAPIKeyEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]appconfig.AmpUpstreamAPIKeyEntry, 0, len(entries))
	for _, entry := range entries {
		upstreamKey := strings.TrimSpace(entry.UpstreamAPIKey)
		if upstreamKey == "" {
			continue
		}
		out = append(out, appconfig.AmpUpstreamAPIKeyEntry{
			UpstreamAPIKey: upstreamKey,
			APIKeys:        normalizeAPIKeysList(entry.APIKeys),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func normalizeAPIKeysList(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		trimmed := strings.TrimSpace(key)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
