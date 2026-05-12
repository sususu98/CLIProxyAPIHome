package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	appconfig "github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/registry"
	"github.com/router-for-me/CLIProxyAPIHome/internal/util"
	"gopkg.in/yaml.v3"
)

const defaultDisbandAuthDir = "~/.cli-proxy-api-home"

type DisbandOptions struct {
	ConfigPath string
	AuthDir    string
	Repository *Repository
}

type DisbandResult struct {
	ConfigPath          string
	AuthDir             string
	ConfigBytes         int
	AuthFiles           int
	GeminiKeys          int
	VertexKeys          int
	CodexKeys           int
	ClaudeKeys          int
	OpenAICompatibility int
}

// CredentialConfigCounts reports how many auth-backed config entries were restored.
type CredentialConfigCounts struct {
	GeminiKeys          int
	VertexKeys          int
	CodexKeys           int
	ClaudeKeys          int
	OpenAICompatibility int
}

// ApplyCredentialConfigToRoot restores auth-backed config keys into a config root.
func ApplyCredentialConfigToRoot(root map[string]any, auths []*coreauth.Auth) CredentialConfigCounts {
	if root == nil {
		return CredentialConfigCounts{}
	}
	result := &DisbandResult{}
	disbandApplyCredentialConfig(root, auths, result)
	return CredentialConfigCounts{
		GeminiKeys:          result.GeminiKeys,
		VertexKeys:          result.VertexKeys,
		CodexKeys:           result.CodexKeys,
		ClaudeKeys:          result.ClaudeKeys,
		OpenAICompatibility: result.OpenAICompatibility,
	}
}

type disbandModelPair struct {
	Name     string
	Alias    string
	Thinking *registry.ThinkingSupport
}

type disbandOpenAICompatGroup struct {
	Config      appconfig.OpenAICompatibility
	SeenEntry   map[string]struct{}
	SortKey     string
	FirstAuthID string
}

// Disband restores local files from the cluster database.
func Disband(ctx context.Context, opts DisbandOptions) (*DisbandResult, error) {
	// Keep validation before state changes so failures leave existing data intact.
	if opts.Repository == nil {
		return nil, fmt.Errorf("cluster disband repository is required")
	}
	ctx = contextOrBackground(ctx)

	configPath := strings.TrimSpace(opts.ConfigPath)
	if configPath == "" {
		configPath = "config.yaml"
	}

	snapshot, errSnapshot := opts.Repository.LoadConfigSnapshot(ctx)
	if errSnapshot != nil {
		return nil, errSnapshot
	}
	root, errRoot := ConfigRootFromSnapshot(snapshot)
	if errRoot != nil {
		return nil, errRoot
	}
	if root == nil {
		root = make(map[string]any)
	}

	auths, errAuths := opts.Repository.ListAuths(ctx)
	if errAuths != nil {
		return nil, errAuths
	}
	sort.Slice(auths, func(i, j int) bool {
		left := ""
		right := ""
		if auths[i] != nil {
			left = auths[i].ID
		}
		if auths[j] != nil {
			right = auths[j].ID
		}
		return left < right
	})

	authDirValue := disbandAuthDirValue(root, auths, opts.AuthDir)
	root["auth-dir"] = authDirValue

	result := &DisbandResult{
		ConfigPath: configPath,
		AuthDir:    authDirValue,
	}
	disbandApplyCredentialConfig(root, auths, result)

	if _, errNormalizeSecret := normalizeConfigRootSecrets(root); errNormalizeSecret != nil {
		return nil, errNormalizeSecret
	}

	data, errMarshal := yaml.Marshal(root)
	if errMarshal != nil {
		return nil, errMarshal
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("restored config is empty")
	}
	if errWriteConfig := writeDisbandFile(configPath, data, 0o600); errWriteConfig != nil {
		return nil, errWriteConfig
	}
	result.ConfigBytes = len(data)

	resolvedAuthDir, errResolveAuthDir := util.ResolveAuthDir(authDirValue)
	if errResolveAuthDir != nil {
		return nil, errResolveAuthDir
	}
	authFiles, errWriteAuthFiles := writeDisbandAuthFiles(auths, resolvedAuthDir)
	if errWriteAuthFiles != nil {
		return nil, errWriteAuthFiles
	}
	result.AuthFiles = authFiles
	return result, nil
}

// disbandAuthDirValue handles a disband auth dir value.
func disbandAuthDirValue(root map[string]any, auths []*coreauth.Auth, explicit string) string {
	if value := strings.TrimSpace(explicit); value != "" {
		return value
	}
	if rawValue, ok := root["auth-dir"]; ok {
		if value := strings.TrimSpace(stringFromAny(rawValue)); value != "" {
			return value
		}
	}
	if inferred := inferDisbandAuthDir(auths); inferred != "" {
		return inferred
	}
	return defaultDisbandAuthDir
}

// inferDisbandAuthDir infers a disband auth dir.
func inferDisbandAuthDir(auths []*coreauth.Auth) string {
	for _, auth := range auths {
		if !isDisbandOAuthAuth(auth) {
			continue
		}
		for _, pathValue := range []string{authAttribute(auth, "path"), authAttribute(auth, "source")} {
			pathValue = strings.TrimSpace(pathValue)
			if pathValue == "" || strings.HasPrefix(pathValue, "config:") {
				continue
			}
			dir := filepath.Dir(pathValue)
			if dir == "." || dir == "" {
				continue
			}
			return dir
		}
	}
	return ""
}

// disbandApplyCredentialConfig handles a disband apply credential config.
func disbandApplyCredentialConfig(root map[string]any, auths []*coreauth.Auth, result *DisbandResult) {
	// Normalize source data before building the derived payload.
	geminiKeys := make([]appconfig.GeminiKey, 0)
	vertexKeys := make([]appconfig.VertexCompatKey, 0)
	codexKeys := make([]appconfig.CodexKey, 0)
	claudeKeys := make([]appconfig.ClaudeKey, 0)
	openAICompat := make(map[string]*disbandOpenAICompatGroup)

	for _, auth := range auths {
		switch disbandConfigAuthKind(auth) {
		case "gemini-api-key":
			geminiKeys = append(geminiKeys, disbandGeminiKey(auth))
		case "vertex-api-key":
			vertexKeys = append(vertexKeys, disbandVertexKey(auth))
		case "codex-api-key":
			codexKeys = append(codexKeys, disbandCodexKey(auth))
		case "claude-api-key":
			claudeKeys = append(claudeKeys, disbandClaudeKey(auth))
		case "openai-compatibility":
			disbandAddOpenAICompat(openAICompat, auth)
		}
	}

	if len(geminiKeys) > 0 {
		root["gemini-api-key"] = geminiKeys
		result.GeminiKeys = len(geminiKeys)
	}
	if len(vertexKeys) > 0 {
		root["vertex-api-key"] = vertexKeys
		result.VertexKeys = len(vertexKeys)
	}
	if len(codexKeys) > 0 {
		root["codex-api-key"] = codexKeys
		result.CodexKeys = len(codexKeys)
	}
	if len(claudeKeys) > 0 {
		root["claude-api-key"] = claudeKeys
		result.ClaudeKeys = len(claudeKeys)
	}
	if len(openAICompat) > 0 {
		groups := make([]*disbandOpenAICompatGroup, 0, len(openAICompat))
		for _, group := range openAICompat {
			groups = append(groups, group)
		}
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].SortKey == groups[j].SortKey {
				return groups[i].FirstAuthID < groups[j].FirstAuthID
			}
			return groups[i].SortKey < groups[j].SortKey
		})
		items := make([]appconfig.OpenAICompatibility, 0, len(groups))
		for _, group := range groups {
			items = append(items, group.Config)
		}
		root["openai-compatibility"] = items
		result.OpenAICompatibility = len(items)
	}
}

// disbandConfigAuthKind handles a disband config auth kind.
func disbandConfigAuthKind(auth *coreauth.Auth) string {
	if auth == nil || auth.Attributes == nil {
		return ""
	}
	source := strings.TrimSpace(auth.Attributes["source"])
	switch {
	case auth.Provider == "gemini" && strings.HasPrefix(source, "config:gemini["):
		return "gemini-api-key"
	case auth.Provider == "vertex" && strings.HasPrefix(source, "config:vertex-apikey["):
		return "vertex-api-key"
	case auth.Provider == "codex" && strings.HasPrefix(source, "config:codex["):
		return "codex-api-key"
	case auth.Provider == "claude" && strings.HasPrefix(source, "config:claude["):
		return "claude-api-key"
	case isDisbandOpenAICompatAuth(auth):
		return "openai-compatibility"
	default:
		return ""
	}
}

// isDisbandOpenAICompatAuth reports whether disband open ai compat auth.
func isDisbandOpenAICompatAuth(auth *coreauth.Auth) bool {
	if auth == nil || auth.Attributes == nil || auth.Provider == "vertex" {
		return false
	}
	attrs := auth.Attributes
	if strings.TrimSpace(attrs["compat_name"]) != "" {
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(attrs["source"]), "config:") && strings.TrimSpace(attrs["provider_key"]) != ""
}

// disbandGeminiKey handles a disband gemini key.
func disbandGeminiKey(auth *coreauth.Auth) appconfig.GeminiKey {
	return appconfig.GeminiKey{
		APIKey:         authAttribute(auth, "api_key"),
		Priority:       disbandPriority(auth),
		Prefix:         strings.TrimSpace(auth.Prefix),
		BaseURL:        authAttribute(auth, "base_url"),
		ProxyURL:       strings.TrimSpace(auth.ProxyURL),
		Models:         disbandGeminiModels(auth),
		Headers:        disbandHeaders(auth),
		ExcludedModels: disbandExcludedModels(auth),
		DisableCooling: disbandDisableCooling(auth),
	}
}

// disbandVertexKey handles a disband vertex key.
func disbandVertexKey(auth *coreauth.Auth) appconfig.VertexCompatKey {
	return appconfig.VertexCompatKey{
		APIKey:         authAttribute(auth, "api_key"),
		Priority:       disbandPriority(auth),
		Prefix:         strings.TrimSpace(auth.Prefix),
		BaseURL:        authAttribute(auth, "base_url"),
		ProxyURL:       strings.TrimSpace(auth.ProxyURL),
		Models:         disbandVertexModels(auth),
		Headers:        disbandHeaders(auth),
		ExcludedModels: disbandExcludedModels(auth),
	}
}

// disbandCodexKey handles a disband codex key.
func disbandCodexKey(auth *coreauth.Auth) appconfig.CodexKey {
	return appconfig.CodexKey{
		APIKey:         authAttribute(auth, "api_key"),
		Priority:       disbandPriority(auth),
		Prefix:         strings.TrimSpace(auth.Prefix),
		BaseURL:        authAttribute(auth, "base_url"),
		Websockets:     strings.EqualFold(authAttribute(auth, "websockets"), "true"),
		ProxyURL:       strings.TrimSpace(auth.ProxyURL),
		Models:         disbandCodexModels(auth),
		Headers:        disbandHeaders(auth),
		ExcludedModels: disbandExcludedModels(auth),
		DisableCooling: disbandDisableCooling(auth),
	}
}

// disbandClaudeKey handles a disband claude key.
func disbandClaudeKey(auth *coreauth.Auth) appconfig.ClaudeKey {
	return appconfig.ClaudeKey{
		APIKey:         authAttribute(auth, "api_key"),
		Priority:       disbandPriority(auth),
		Prefix:         strings.TrimSpace(auth.Prefix),
		BaseURL:        authAttribute(auth, "base_url"),
		ProxyURL:       strings.TrimSpace(auth.ProxyURL),
		Models:         disbandClaudeModels(auth),
		Headers:        disbandHeaders(auth),
		ExcludedModels: disbandExcludedModels(auth),
		DisableCooling: disbandDisableCooling(auth),
	}
}

// disbandAddOpenAICompat handles a disband add open ai compat.
func disbandAddOpenAICompat(groups map[string]*disbandOpenAICompatGroup, auth *coreauth.Auth) {
	// Keep validation before state changes so failures leave existing data intact.
	if auth == nil {
		return
	}
	name := authAttribute(auth, "compat_name")
	if name == "" {
		name = strings.TrimSpace(auth.Label)
	}
	if name == "" {
		name = authAttribute(auth, "provider_key")
	}
	baseURL := authAttribute(auth, "base_url")
	prefix := strings.TrimSpace(auth.Prefix)
	priority := disbandPriority(auth)
	headers := disbandHeaders(auth)
	disableCooling := disbandDisableCooling(auth)
	groupKey := strings.Join([]string{
		strings.ToLower(name),
		baseURL,
		prefix,
		strconv.Itoa(priority),
		disbandHeadersKey(headers),
		strconv.FormatBool(disableCooling),
	}, "\x00")

	group := groups[groupKey]
	if group == nil {
		group = &disbandOpenAICompatGroup{
			Config: appconfig.OpenAICompatibility{
				Name:           name,
				Priority:       priority,
				Prefix:         prefix,
				BaseURL:        baseURL,
				Models:         disbandOpenAIModels(auth),
				Headers:        headers,
				DisableCooling: disableCooling,
			},
			SeenEntry:   make(map[string]struct{}),
			SortKey:     groupKey,
			FirstAuthID: strings.TrimSpace(auth.ID),
		}
		groups[groupKey] = group
	}

	apiKey := authAttribute(auth, "api_key")
	proxyURL := strings.TrimSpace(auth.ProxyURL)
	if apiKey == "" && proxyURL == "" {
		return
	}
	entryKey := apiKey + "\x00" + proxyURL
	if _, ok := group.SeenEntry[entryKey]; ok {
		return
	}
	group.SeenEntry[entryKey] = struct{}{}
	group.Config.APIKeyEntries = append(group.Config.APIKeyEntries, appconfig.OpenAICompatibilityAPIKey{
		APIKey:   apiKey,
		ProxyURL: proxyURL,
	})
}

// disbandGeminiModels handles a disband gemini models.
func disbandGeminiModels(auth *coreauth.Auth) []appconfig.GeminiModel {
	pairs := disbandModelPairs(auth)
	out := make([]appconfig.GeminiModel, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, appconfig.GeminiModel{Name: pair.Name, Alias: pair.Alias})
	}
	return out
}

// disbandVertexModels handles a disband vertex models.
func disbandVertexModels(auth *coreauth.Auth) []appconfig.VertexCompatModel {
	pairs := disbandModelPairs(auth)
	out := make([]appconfig.VertexCompatModel, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, appconfig.VertexCompatModel{Name: pair.Name, Alias: pair.Alias})
	}
	return out
}

// disbandCodexModels handles a disband codex models.
func disbandCodexModels(auth *coreauth.Auth) []appconfig.CodexModel {
	pairs := disbandModelPairs(auth)
	out := make([]appconfig.CodexModel, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, appconfig.CodexModel{Name: pair.Name, Alias: pair.Alias})
	}
	return out
}

// disbandClaudeModels handles a disband claude models.
func disbandClaudeModels(auth *coreauth.Auth) []appconfig.ClaudeModel {
	pairs := disbandModelPairs(auth)
	out := make([]appconfig.ClaudeModel, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, appconfig.ClaudeModel{Name: pair.Name, Alias: pair.Alias})
	}
	return out
}

// disbandOpenAIModels handles a disband open ai models.
func disbandOpenAIModels(auth *coreauth.Auth) []appconfig.OpenAICompatibilityModel {
	pairs := disbandModelPairs(auth)
	out := make([]appconfig.OpenAICompatibilityModel, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, appconfig.OpenAICompatibilityModel{Name: pair.Name, Alias: pair.Alias, Thinking: pair.Thinking})
	}
	return out
}

// disbandModelPairs handles a disband model pairs.
func disbandModelPairs(auth *coreauth.Auth) []disbandModelPair {
	// Normalize source data before building the derived payload.
	if auth == nil || auth.Metadata == nil {
		return nil
	}
	raw := auth.Metadata["home_config_models"]
	models, ok := raw.([]any)
	if !ok || len(models) == 0 {
		return nil
	}

	out := make([]disbandModelPair, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, rawModel := range models {
		modelMap, okMap := rawModel.(map[string]any)
		if !okMap {
			continue
		}
		alias := strings.TrimSpace(stringFromAny(modelMap["id"]))
		if alias == "" {
			continue
		}
		if isNonUserCodexBuiltin(modelMap, alias) {
			continue
		}
		name := strings.TrimSpace(stringFromAny(modelMap["display_name"]))
		if name == "" {
			name = strings.TrimSpace(stringFromAny(modelMap["name"]))
		}
		if name == "" {
			name = alias
		}
		key := strings.ToLower(alias)
		if _, okSeen := seen[key]; okSeen {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, disbandModelPair{
			Name:     name,
			Alias:    alias,
			Thinking: disbandThinking(modelMap["thinking"]),
		})
	}
	return out
}

// isNonUserCodexBuiltin reports whether non user codex builtin.
func isNonUserCodexBuiltin(modelMap map[string]any, alias string) bool {
	if !strings.EqualFold(alias, "gpt-image-2") {
		return false
	}
	userDefined, ok := boolFromAny(modelMap["user_defined"])
	return ok && !userDefined
}

// disbandThinking handles a disband thinking.
func disbandThinking(value any) *registry.ThinkingSupport {
	if value == nil {
		return nil
	}
	raw, errMarshal := json.Marshal(value)
	if errMarshal != nil || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var thinking registry.ThinkingSupport
	if errUnmarshal := json.Unmarshal(raw, &thinking); errUnmarshal != nil {
		return nil
	}
	if thinking.Min == 0 && thinking.Max == 0 && !thinking.ZeroAllowed && !thinking.DynamicAllowed && len(thinking.Levels) == 0 {
		return nil
	}
	return &thinking
}

// disbandHeaders handles a disband headers.
func disbandHeaders(auth *coreauth.Auth) map[string]string {
	if auth == nil || len(auth.Attributes) == 0 {
		return nil
	}
	headers := make(map[string]string)
	for key, value := range auth.Attributes {
		if !strings.HasPrefix(key, "header:") {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(key, "header:"))
		value = strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		headers[name] = value
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

// disbandHeadersKey handles a disband headers key.
func disbandHeadersKey(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+headers[key])
	}
	return strings.Join(parts, "\n")
}

// disbandPriority handles a disband priority.
func disbandPriority(auth *coreauth.Auth) int {
	priority := strings.TrimSpace(authAttribute(auth, "priority"))
	if priority == "" {
		return 0
	}
	value, errAtoi := strconv.Atoi(priority)
	if errAtoi != nil {
		return 0
	}
	return value
}

// disbandDisableCooling handles a disband disable cooling.
func disbandDisableCooling(auth *coreauth.Auth) bool {
	if auth == nil || auth.Metadata == nil {
		return false
	}
	for _, key := range []string{"disable_cooling", "disable-cooling"} {
		if value, ok := boolFromAny(auth.Metadata[key]); ok {
			return value
		}
	}
	return false
}

// disbandExcludedModels handles a disband excluded models.
func disbandExcludedModels(auth *coreauth.Auth) []string {
	raw := strings.TrimSpace(authAttribute(auth, "excluded_models"))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		model := strings.TrimSpace(part)
		if model == "" {
			continue
		}
		key := strings.ToLower(model)
		if _, okSeen := seen[key]; okSeen {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, model)
	}
	return out
}

// authAttribute handles an auth attribute.
func authAttribute(auth *coreauth.Auth, key string) string {
	if auth == nil || auth.Attributes == nil {
		return ""
	}
	return strings.TrimSpace(auth.Attributes[key])
}

// writeDisbandAuthFiles writes a disband auth files.
func writeDisbandAuthFiles(auths []*coreauth.Auth, authDir string) (int, error) {
	// Normalize auth state before updating runtime indexes.
	authDir = strings.TrimSpace(authDir)
	if authDir == "" {
		return 0, nil
	}
	if errMkdir := os.MkdirAll(authDir, 0o700); errMkdir != nil {
		return 0, errMkdir
	}

	count := 0
	seen := make(map[string]struct{}, len(auths))
	for _, auth := range auths {
		if !isDisbandOAuthAuth(auth) {
			continue
		}
		relName := disbandAuthFileName(auth)
		if relName == "" {
			continue
		}
		fullPath, errPath := disbandAuthFilePath(authDir, relName)
		if errPath != nil {
			return count, errPath
		}
		if _, okSeen := seen[fullPath]; okSeen {
			continue
		}
		seen[fullPath] = struct{}{}

		payload := cloneAnyMap(auth.Metadata)
		if strings.TrimSpace(stringFromAny(payload["uuid"])) == "" && strings.TrimSpace(auth.ID) != "" {
			payload["uuid"] = strings.TrimSpace(auth.ID)
		}
		if auth.Disabled || auth.Status == coreauth.StatusDisabled {
			payload["disabled"] = true
		}
		raw, errMarshal := json.MarshalIndent(payload, "", "  ")
		if errMarshal != nil {
			return count, errMarshal
		}
		raw = append(raw, '\n')
		if errWrite := writeDisbandFile(fullPath, raw, 0o600); errWrite != nil {
			return count, errWrite
		}
		count++
	}
	return count, nil
}

// isDisbandOAuthAuth reports whether disband o auth auth.
func isDisbandOAuthAuth(auth *coreauth.Auth) bool {
	if auth == nil || auth.Metadata == nil {
		return false
	}
	if strings.TrimSpace(stringFromAny(auth.Metadata["type"])) == "" {
		return false
	}
	if auth.Attributes != nil {
		source := strings.TrimSpace(auth.Attributes["source"])
		if strings.HasPrefix(source, "config:") {
			return false
		}
		if strings.EqualFold(strings.TrimSpace(auth.Attributes["runtime_only"]), "true") {
			return false
		}
	}
	if virtual, ok := boolFromAny(auth.Metadata["virtual"]); ok && virtual {
		return false
	}
	return true
}

// disbandAuthFileName handles a disband auth file name.
func disbandAuthFileName(auth *coreauth.Auth) string {
	// Normalize auth state before updating runtime indexes.
	if auth == nil {
		return ""
	}
	if auth.Metadata != nil {
		if name := strings.TrimSpace(stringFromAny(auth.Metadata["filename"])); name != "" {
			return sanitizeDisbandRelativePath(name)
		}
	}
	for _, pathValue := range []string{authAttribute(auth, "path"), authAttribute(auth, "source")} {
		if pathValue == "" || strings.HasPrefix(pathValue, "config:") {
			continue
		}
		if baseName := filepath.Base(pathValue); baseName != "." && baseName != string(os.PathSeparator) {
			return sanitizeDisbandRelativePath(baseName)
		}
	}
	if auth.Metadata != nil {
		if uuidValue := strings.TrimSpace(stringFromAny(auth.Metadata["uuid"])); uuidValue != "" {
			return sanitizeDisbandRelativePath(uuidValue + ".json")
		}
	}
	if authID := strings.TrimSpace(auth.ID); authID != "" {
		if strings.HasSuffix(strings.ToLower(authID), ".json") {
			return sanitizeDisbandRelativePath(filepath.Base(authID))
		}
		return sanitizeDisbandRelativePath(authID + ".json")
	}
	return ""
}

// sanitizeDisbandRelativePath sanitizes a disband relative path.
func sanitizeDisbandRelativePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if filepath.IsAbs(path) {
		path = filepath.Base(path)
	}
	for strings.HasPrefix(path, ".."+string(os.PathSeparator)) || path == ".." {
		path = filepath.Base(path)
	}
	if path == "." || path == string(os.PathSeparator) {
		return ""
	}
	return path
}

// disbandAuthFilePath handles a disband auth file path.
func disbandAuthFilePath(authDir, relName string) (string, error) {
	baseDir := filepath.Clean(authDir)
	fullPath := filepath.Clean(filepath.Join(baseDir, relName))
	relPath, errRel := filepath.Rel(baseDir, fullPath)
	if errRel != nil {
		return "", errRel
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("auth file path escapes auth-dir: %s", relName)
	}
	return fullPath, nil
}

// writeDisbandFile writes a disband file.
func writeDisbandFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if errMkdir := os.MkdirAll(dir, 0o700); errMkdir != nil {
			return errMkdir
		}
	}
	return os.WriteFile(path, data, perm)
}

// cloneAnyMap clones an any map.
func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return make(map[string]any)
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

// stringFromAny derives string from any.
func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case []byte:
		return string(typed)
	default:
		if value == nil {
			return ""
		}
		return fmt.Sprint(value)
	}
}

// boolFromAny derives bool from any.
func boolFromAny(value any) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		parsed, errParseBool := strconv.ParseBool(strings.TrimSpace(typed))
		if errParseBool != nil {
			return false, false
		}
		return parsed, true
	default:
		return false, false
	}
}
