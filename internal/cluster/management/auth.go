package management

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	appconfig "github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/watcher/synthesizer"
)

func (h *Handler) GetGeminiKeys(c *gin.Context)         { h.getAPIKeyList(c, "gemini-api-key") }
func (h *Handler) PutGeminiKeys(c *gin.Context)         { h.putAPIKeyList(c, "gemini-api-key") }
func (h *Handler) PatchGeminiKey(c *gin.Context)        { h.patchAPIKey(c, "gemini-api-key") }
func (h *Handler) DeleteGeminiKey(c *gin.Context)       { h.deleteAPIKey(c, "gemini-api-key") }
func (h *Handler) GetVertexCompatKeys(c *gin.Context)   { h.getAPIKeyList(c, "vertex-api-key") }
func (h *Handler) PutVertexCompatKeys(c *gin.Context)   { h.putAPIKeyList(c, "vertex-api-key") }
func (h *Handler) PatchVertexCompatKey(c *gin.Context)  { h.patchAPIKey(c, "vertex-api-key") }
func (h *Handler) DeleteVertexCompatKey(c *gin.Context) { h.deleteAPIKey(c, "vertex-api-key") }
func (h *Handler) GetCodexKeys(c *gin.Context)          { h.getAPIKeyList(c, "codex-api-key") }
func (h *Handler) PutCodexKeys(c *gin.Context)          { h.putAPIKeyList(c, "codex-api-key") }
func (h *Handler) PatchCodexKey(c *gin.Context)         { h.patchAPIKey(c, "codex-api-key") }
func (h *Handler) DeleteCodexKey(c *gin.Context)        { h.deleteAPIKey(c, "codex-api-key") }
func (h *Handler) GetClaudeKeys(c *gin.Context)         { h.getAPIKeyList(c, "claude-api-key") }
func (h *Handler) PutClaudeKeys(c *gin.Context)         { h.putAPIKeyList(c, "claude-api-key") }
func (h *Handler) PatchClaudeKey(c *gin.Context)        { h.patchAPIKey(c, "claude-api-key") }
func (h *Handler) DeleteClaudeKey(c *gin.Context)       { h.deleteAPIKey(c, "claude-api-key") }
func (h *Handler) GetOpenAICompat(c *gin.Context)       { h.getAPIKeyList(c, "openai-compatibility") }
func (h *Handler) PutOpenAICompat(c *gin.Context)       { h.putAPIKeyList(c, "openai-compatibility") }
func (h *Handler) PatchOpenAICompat(c *gin.Context)     { h.patchAPIKey(c, "openai-compatibility") }
func (h *Handler) DeleteOpenAICompat(c *gin.Context)    { h.deleteAPIKey(c, "openai-compatibility") }

func (h *Handler) getAPIKeyList(c *gin.Context, key string) {
	ctx, cancel := h.requestContext(c)
	defer cancel()
	auths, errAuths := h.repo.ListAuths(ctx)
	if errAuths != nil {
		respondError(c, http.StatusInternalServerError, "auth_load_failed", errAuths)
		return
	}
	items := make([]map[string]any, 0)
	for _, auth := range auths {
		if !isAPIKeyAuthForKey(auth, key) {
			continue
		}
		items = append(items, apiKeyAuthToMap(auth, key))
	}
	sort.Slice(items, func(i, j int) bool {
		return fmt.Sprint(items[i]["auth_index"]) < fmt.Sprint(items[j]["auth_index"])
	})
	c.JSON(http.StatusOK, gin.H{key: items})
}

func (h *Handler) putAPIKeyList(c *gin.Context, key string) {
	body, errRead := io.ReadAll(c.Request.Body)
	if errRead != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errRead)
		return
	}
	auths, errSynthesize := h.synthesizeAPIKeyBody(key, body)
	if errSynthesize != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errSynthesize)
		return
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()
	if errReplace := h.replaceAPIKeyAuths(ctx, key, auths); errReplace != nil {
		respondError(c, http.StatusInternalServerError, "write_failed", errReplace)
		return
	}
	if errRefresh := h.refreshAuths(ctx); errRefresh != nil {
		respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
		return
	}
	respondOK(c)
}

func (h *Handler) patchAPIKey(c *gin.Context, key string) {
	body, errRead := io.ReadAll(c.Request.Body)
	if errRead != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errRead)
		return
	}
	var patch apiKeyPatchRequest
	if errUnmarshal := json.Unmarshal(body, &patch); errUnmarshal != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errUnmarshal)
		return
	}
	if patch.Value == nil {
		respondError(c, http.StatusBadRequest, "invalid body", fmt.Errorf("missing value"))
		return
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()
	auths, errAuths := h.repo.ListAuths(ctx)
	if errAuths != nil {
		respondError(c, http.StatusInternalServerError, "auth_load_failed", errAuths)
		return
	}
	target := findAPIKeyAuth(auths, key, patch.Identifier(c))
	if target == nil {
		respondError(c, http.StatusNotFound, "item not found", nil)
		return
	}
	entry := apiKeyAuthToMap(target, key)
	for patchKey, value := range patch.Value {
		entry[patchKey] = value
	}
	delete(entry, "auth_index")
	delete(entry, "id")
	delete(entry, "uuid")

	rawEntry, errMarshal := json.Marshal(entry)
	if errMarshal != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errMarshal)
		return
	}
	nextAuths, errSynthesize := h.synthesizeAPIKeyBody(key, rawEntry)
	if errSynthesize != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errSynthesize)
		return
	}
	if len(nextAuths) == 0 {
		if errDelete := h.repo.SoftDeleteAuth(ctx, target.ID); errDelete != nil {
			respondError(c, http.StatusInternalServerError, "write_failed", errDelete)
			return
		}
	} else {
		next := nextAuths[0]
		if next.ID != target.ID {
			if errDelete := h.repo.SoftDeleteAuth(ctx, target.ID); errDelete != nil {
				respondError(c, http.StatusInternalServerError, "write_failed", errDelete)
				return
			}
		}
		if _, errUpsert := h.repo.UpsertAuth(ctx, next, "update"); errUpsert != nil {
			respondError(c, http.StatusInternalServerError, "write_failed", errUpsert)
			return
		}
	}
	if errRefresh := h.refreshAuths(ctx); errRefresh != nil {
		respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
		return
	}
	respondOK(c)
}

func (h *Handler) deleteAPIKey(c *gin.Context, key string) {
	ctx, cancel := h.requestContext(c)
	defer cancel()
	auths, errAuths := h.repo.ListAuths(ctx)
	if errAuths != nil {
		respondError(c, http.StatusInternalServerError, "auth_load_failed", errAuths)
		return
	}
	bodyID := apiKeyIdentifierFromRequest(c)
	target := findAPIKeyAuth(auths, key, bodyID)
	if target == nil {
		respondError(c, http.StatusNotFound, "item not found", nil)
		return
	}
	if errDelete := h.repo.SoftDeleteAuth(ctx, target.ID); errDelete != nil {
		respondError(c, http.StatusInternalServerError, "write_failed", errDelete)
		return
	}
	if errRefresh := h.refreshAuths(ctx); errRefresh != nil {
		respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
		return
	}
	respondOK(c)
}

func (h *Handler) synthesizeAPIKeyBody(key string, body []byte) ([]*coreauth.Auth, error) {
	cfg := &appconfig.Config{}
	switch key {
	case "gemini-api-key":
		var entries []appconfig.GeminiKey
		if errDecode := decodeListBody(body, key, &entries); errDecode != nil {
			return nil, errDecode
		}
		cfg.GeminiKey = entries
		cfg.SanitizeGeminiKeys()
	case "vertex-api-key":
		var entries []appconfig.VertexCompatKey
		if errDecode := decodeListBody(body, key, &entries); errDecode != nil {
			return nil, errDecode
		}
		cfg.VertexCompatAPIKey = entries
		cfg.SanitizeVertexCompatKeys()
	case "codex-api-key":
		var entries []appconfig.CodexKey
		if errDecode := decodeListBody(body, key, &entries); errDecode != nil {
			return nil, errDecode
		}
		cfg.CodexKey = entries
		cfg.SanitizeCodexKeys()
	case "claude-api-key":
		var entries []appconfig.ClaudeKey
		if errDecode := decodeListBody(body, key, &entries); errDecode != nil {
			return nil, errDecode
		}
		cfg.ClaudeKey = entries
		cfg.SanitizeClaudeKeys()
	case "openai-compatibility":
		var entries []appconfig.OpenAICompatibility
		if errDecode := decodeListBody(body, key, &entries); errDecode != nil {
			return nil, errDecode
		}
		cfg.OpenAICompatibility = entries
		cfg.SanitizeOpenAICompatibility()
	default:
		return nil, fmt.Errorf("unsupported api key route %s", key)
	}
	return synthesizeConfigAuths(cfg), nil
}

func synthesizeConfigAuths(cfg *appconfig.Config) []*coreauth.Auth {
	now := time.Now().UTC()
	sctx := &synthesizer.SynthesisContext{
		Config:      cfg,
		Now:         now,
		IDGenerator: synthesizer.NewStableIDGenerator(),
		ClusterMode: true,
		UUIDForAuth: func(auth *coreauth.Auth) string {
			if auth == nil || auth.Attributes == nil {
				return ""
			}
			return cluster.DeterministicAPIKeyUUID(
				auth.Provider,
				auth.Attributes["base_url"],
				cluster.APIKeyHash(auth.Attributes["api_key"]),
				auth.Attributes["compat_name"],
				auth.Attributes["provider_key"],
			)
		},
	}
	auths, errSynthesize := synthesizer.NewConfigSynthesizer().Synthesize(sctx)
	if errSynthesize != nil {
		return nil
	}
	return auths
}

func (h *Handler) replaceAPIKeyAuths(ctx context.Context, key string, next []*coreauth.Auth) error {
	existing, errExisting := h.repo.ListAuths(ctx)
	if errExisting != nil {
		return errExisting
	}
	nextIDs := make(map[string]struct{}, len(next))
	for _, auth := range next {
		if auth == nil {
			continue
		}
		nextIDs[auth.ID] = struct{}{}
	}
	for _, auth := range existing {
		if !isAPIKeyAuthForKey(auth, key) {
			continue
		}
		if _, ok := nextIDs[auth.ID]; ok {
			continue
		}
		if errDelete := h.repo.SoftDeleteAuth(ctx, auth.ID); errDelete != nil {
			return errDelete
		}
	}
	for _, auth := range next {
		if auth == nil {
			continue
		}
		if _, errUpsert := h.repo.UpsertAuth(ctx, auth, "upsert"); errUpsert != nil {
			return errUpsert
		}
	}
	return nil
}

func decodeListBody[T any](body []byte, key string, out *[]T) error {
	if len(body) == 0 {
		*out = nil
		return nil
	}
	if errUnmarshal := json.Unmarshal(body, out); errUnmarshal == nil {
		return nil
	}
	var object map[string]json.RawMessage
	if errObject := json.Unmarshal(body, &object); errObject == nil && object != nil {
		for _, listKey := range []string{key, "items", "list", "data"} {
			if raw, ok := object[listKey]; ok {
				if errList := json.Unmarshal(raw, out); errList != nil {
					return errList
				}
				return nil
			}
		}
		var single T
		if errSingle := json.Unmarshal(body, &single); errSingle != nil {
			return errSingle
		}
		*out = []T{single}
		return nil
	}
	var single T
	if errSingle := json.Unmarshal(body, &single); errSingle != nil {
		return errSingle
	}
	*out = []T{single}
	return nil
}

type apiKeyPatchRequest struct {
	Index *int           `json:"index"`
	Match *string        `json:"match"`
	Name  *string        `json:"name"`
	ID    *string        `json:"id"`
	UUID  *string        `json:"uuid"`
	Value map[string]any `json:"value"`
}

type apiKeyIdentifier struct {
	ID      string
	Index   *int
	APIKey  string
	BaseURL string
	Name    string
}

func (p apiKeyPatchRequest) Identifier(c *gin.Context) apiKeyIdentifier {
	identifier := apiKeyIdentifier{Index: p.Index}
	if p.ID != nil {
		identifier.ID = strings.TrimSpace(*p.ID)
	}
	if identifier.ID == "" && p.UUID != nil {
		identifier.ID = strings.TrimSpace(*p.UUID)
	}
	if p.Match != nil {
		identifier.APIKey = strings.TrimSpace(*p.Match)
	}
	if p.Name != nil {
		identifier.Name = strings.TrimSpace(*p.Name)
	}
	if c != nil {
		identifier.BaseURL = strings.TrimSpace(c.Query("base-url"))
	}
	return identifier
}

func apiKeyIdentifierFromRequest(c *gin.Context) apiKeyIdentifier {
	identifier := apiKeyIdentifier{
		ID:      firstNonEmptyQuery(c, "id", "uuid", "auth_index", "index"),
		APIKey:  firstNonEmptyQuery(c, "api-key", "api_key", "match"),
		BaseURL: firstNonEmptyQuery(c, "base-url", "base_url"),
		Name:    firstNonEmptyQuery(c, "name"),
	}
	if idxRaw := strings.TrimSpace(c.Query("index")); idxRaw != "" {
		if idx, errAtoi := strconv.Atoi(idxRaw); errAtoi == nil {
			identifier.Index = &idx
		}
	}
	return identifier
}

func findAPIKeyAuth(auths []*coreauth.Auth, key string, identifier apiKeyIdentifier) *coreauth.Auth {
	filtered := make([]*coreauth.Auth, 0, len(auths))
	for _, auth := range auths {
		if isAPIKeyAuthForKey(auth, key) {
			filtered = append(filtered, auth)
		}
	}
	if identifier.Index != nil && *identifier.Index >= 0 && *identifier.Index < len(filtered) {
		return filtered[*identifier.Index]
	}
	for _, auth := range filtered {
		if identifier.ID != "" && (auth.ID == identifier.ID || auth.Index == identifier.ID) {
			return auth
		}
		attrs := auth.Attributes
		if attrs == nil {
			continue
		}
		if identifier.APIKey != "" && strings.TrimSpace(attrs["api_key"]) != identifier.APIKey {
			continue
		}
		if identifier.BaseURL != "" && strings.TrimSpace(attrs["base_url"]) != identifier.BaseURL {
			continue
		}
		if identifier.Name != "" && strings.TrimSpace(attrs["compat_name"]) != identifier.Name && strings.TrimSpace(auth.Label) != identifier.Name {
			continue
		}
		if identifier.ID != "" || identifier.APIKey != "" || identifier.Name != "" {
			return auth
		}
	}
	return nil
}

func isAPIKeyAuthForKey(auth *coreauth.Auth, key string) bool {
	if auth == nil || auth.Attributes == nil || strings.TrimSpace(auth.Attributes["api_key"]) == "" && key != "openai-compatibility" {
		return false
	}
	source := strings.TrimSpace(auth.Attributes["source"])
	switch key {
	case "gemini-api-key":
		return auth.Provider == "gemini" && strings.HasPrefix(source, "config:gemini[")
	case "claude-api-key":
		return auth.Provider == "claude" && strings.HasPrefix(source, "config:claude[")
	case "codex-api-key":
		return auth.Provider == "codex" && strings.HasPrefix(source, "config:codex[")
	case "vertex-api-key":
		return auth.Provider == "vertex" && strings.HasPrefix(source, "config:vertex-apikey[")
	case "openai-compatibility":
		return strings.TrimSpace(auth.Attributes["compat_name"]) != "" || (strings.HasPrefix(source, "config:") && strings.TrimSpace(auth.Attributes["provider_key"]) != "" && auth.Provider != "vertex")
	default:
		return false
	}
}

func apiKeyAuthToMap(auth *coreauth.Auth, key string) map[string]any {
	item := make(map[string]any)
	if auth == nil {
		return item
	}
	attrs := auth.Attributes
	if attrs == nil {
		attrs = map[string]string{}
	}
	item["auth_index"] = auth.ID
	item["id"] = auth.ID
	item["uuid"] = auth.ID
	item["api-key"] = attrs["api_key"]
	item["base-url"] = attrs["base_url"]
	item["prefix"] = auth.Prefix
	item["proxy-url"] = auth.ProxyURL
	item["disabled"] = auth.Disabled || auth.Status == coreauth.StatusDisabled
	if priority := strings.TrimSpace(attrs["priority"]); priority != "" {
		if priorityValue, errAtoi := strconv.Atoi(priority); errAtoi == nil {
			item["priority"] = priorityValue
		}
	}
	headers := make(map[string]string)
	for name, value := range attrs {
		if strings.HasPrefix(name, "header:") {
			headers[strings.TrimPrefix(name, "header:")] = value
		}
	}
	if len(headers) > 0 {
		item["headers"] = headers
	}
	if key == "openai-compatibility" {
		name := strings.TrimSpace(attrs["compat_name"])
		if name == "" {
			name = strings.TrimSpace(auth.Label)
		}
		item["name"] = name
		item["api-key-entries"] = []map[string]any{{
			"api-key":   attrs["api_key"],
			"proxy-url": auth.ProxyURL,
		}}
	}
	if key == "codex-api-key" && strings.EqualFold(attrs["websockets"], "true") {
		item["websockets"] = true
	}
	return item
}

func firstNonEmptyQuery(c *gin.Context, keys ...string) string {
	if c == nil {
		return ""
	}
	for _, key := range keys {
		if value := strings.TrimSpace(c.Query(key)); value != "" {
			return value
		}
	}
	return ""
}
