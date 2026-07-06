package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
)

func TestPutConfigYAMLPersistsCredentialRoots(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	handler := NewHandler(repo, nil, "127.0.0.1", 0)
	engine := gin.New()
	engine.PUT("/config.yaml", handler.PutConfigYAML)
	engine.GET("/config.yaml", handler.GetConfigYAML)

	payload := `port: 8327
api-keys:
  - user-key
codex-api-key:
  - api-key: codex-key
    base-url: https://api.example/codex
    proxy-url: ""
    models:
      - name: "gpt-5.5"
        alias: "gpt-5.5"
`

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/config.yaml", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/yaml")
	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var body struct {
		OK      bool     `json:"ok"`
		Changed []string `json:"changed"`
	}
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &body); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if !body.OK || !stringSliceContains(body.Changed, "config") || !stringSliceContains(body.Changed, "auth") {
		t.Fatalf("response = %+v, want config and auth changes", body)
	}

	auths, errAuths := repo.ListAuths(context.Background())
	if errAuths != nil {
		t.Fatalf("ListAuths() error = %v", errAuths)
	}
	if len(auths) != 1 {
		t.Fatalf("auth count = %d, want 1", len(auths))
	}
	auth := auths[0]
	if auth.Provider != "codex" || auth.Attributes["api_key"] != "codex-key" {
		t.Fatalf("auth = %+v, want codex api key auth", auth)
	}
	rawModels, errMarshal := json.Marshal(auth.Metadata["home_config_models"])
	if errMarshal != nil {
		t.Fatalf("marshal model metadata: %v", errMarshal)
	}
	if !strings.Contains(string(rawModels), "gpt-5.5") {
		t.Fatalf("home_config_models = %s, want gpt-5.5", string(rawModels))
	}

	getResp := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/config.yaml", nil)
	engine.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getResp.Code, getResp.Body.String())
	}
	if !strings.Contains(getResp.Body.String(), "codex-api-key:") || !strings.Contains(getResp.Body.String(), "gpt-5.5") {
		t.Fatalf("config yaml = %s, want persisted codex model", getResp.Body.String())
	}
}

func TestGetProviderKeyRoutesReturnConfiguredModels(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	handler := NewHandler(repo, nil, "127.0.0.1", 0)
	engine := gin.New()
	engine.PUT("/config.yaml", handler.PutConfigYAML)
	engine.GET("/gemini-api-key", handler.GetGeminiKeys)
	engine.GET("/claude-api-key", handler.GetClaudeKeys)
	engine.GET("/codex-api-key", handler.GetCodexKeys)
	engine.GET("/vertex-api-key", handler.GetVertexCompatKeys)

	payload := `port: 8327
gemini-api-key:
  - api-key: gemini-key
    models:
      - name: "gemini-upstream"
        alias: "gemini-alias"
claude-api-key:
  - api-key: claude-key
    models:
      - name: "claude-upstream"
        alias: "claude-alias"
codex-api-key:
  - api-key: codex-key
    base-url: "https://codex.example"
    models:
      - name: "codex-upstream"
        alias: "codex-alias"
vertex-api-key:
  - api-key: vertex-key
    base-url: "https://vertex.example"
    models:
      - name: "vertex-upstream"
        alias: "vertex-alias"
`

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/config.yaml", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/yaml")
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("put status = %d, body = %s", resp.Code, resp.Body.String())
	}

	cases := []struct {
		Path      string
		Key       string
		WantName  string
		WantAlias string
	}{
		{Path: "/gemini-api-key", Key: "gemini-api-key", WantName: "gemini-upstream", WantAlias: "gemini-alias"},
		{Path: "/claude-api-key", Key: "claude-api-key", WantName: "claude-upstream", WantAlias: "claude-alias"},
		{Path: "/codex-api-key", Key: "codex-api-key", WantName: "codex-upstream", WantAlias: "codex-alias"},
		{Path: "/vertex-api-key", Key: "vertex-api-key", WantName: "vertex-upstream", WantAlias: "vertex-alias"},
	}
	for _, tc := range cases {
		t.Run(tc.Key, func(t *testing.T) {
			getResp := httptest.NewRecorder()
			getReq := httptest.NewRequest(http.MethodGet, tc.Path, nil)
			engine.ServeHTTP(getResp, getReq)
			if getResp.Code != http.StatusOK {
				t.Fatalf("get status = %d, body = %s", getResp.Code, getResp.Body.String())
			}
			model := providerKeyFirstModel(t, getResp.Body.Bytes(), tc.Key)
			if gotName := stringFromAny(model["name"]); gotName != tc.WantName {
				t.Fatalf("model name = %q, want %q", gotName, tc.WantName)
			}
			if gotAlias := stringFromAny(model["alias"]); gotAlias != tc.WantAlias {
				t.Fatalf("model alias = %q, want %q", gotAlias, tc.WantAlias)
			}
			if _, exists := model["thinking"]; exists {
				t.Fatalf("model contains unexpected thinking field: %+v", model)
			}
		})
	}
}

func TestOAuthModelAliasForceMappingRoundTrip(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	handler := NewHandler(repo, nil, "127.0.0.1", 0)
	engine := gin.New()
	engine.PUT("/oauth-model-alias", handler.PutOAuthModelAlias)
	engine.GET("/oauth-model-alias", handler.GetOAuthModelAlias)
	engine.GET("/config", handler.GetConfig)
	engine.GET("/config.yaml", handler.GetConfigYAML)

	payload := `{
  "claude": [
    { "name": " claude-sonnet-4 ", "alias": " sonnet ", "fork": true, "force-mapping": true },
    { "name": "claude-haiku-4", "alias": "haiku" }
  ]
}`

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/oauth-model-alias", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("put status = %d, body = %s", resp.Code, resp.Body.String())
	}

	getResp := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/oauth-model-alias", nil)
	engine.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getResp.Code, getResp.Body.String())
	}
	assertOAuthModelAliasForceMapping(t, getResp.Body.Bytes())

	configResp := httptest.NewRecorder()
	configReq := httptest.NewRequest(http.MethodGet, "/config", nil)
	engine.ServeHTTP(configResp, configReq)
	if configResp.Code != http.StatusOK {
		t.Fatalf("config status = %d, body = %s", configResp.Code, configResp.Body.String())
	}
	assertOAuthModelAliasForceMapping(t, configResp.Body.Bytes())

	yamlResp := httptest.NewRecorder()
	yamlReq := httptest.NewRequest(http.MethodGet, "/config.yaml", nil)
	engine.ServeHTTP(yamlResp, yamlReq)
	if yamlResp.Code != http.StatusOK {
		t.Fatalf("yaml status = %d, body = %s", yamlResp.Code, yamlResp.Body.String())
	}
	if !strings.Contains(yamlResp.Body.String(), "force-mapping: true") {
		t.Fatalf("config yaml = %s, want force-mapping true", yamlResp.Body.String())
	}
	if strings.Contains(yamlResp.Body.String(), "force-mapping: false") {
		t.Fatalf("config yaml = %s, should omit false force-mapping", yamlResp.Body.String())
	}
}

func providerKeyFirstModel(t *testing.T, raw []byte, key string) map[string]any {
	t.Helper()

	var body map[string][]map[string]any
	if errDecode := json.Unmarshal(raw, &body); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	items := body[key]
	if len(items) != 1 {
		t.Fatalf("%s count = %d, want 1; body = %s", key, len(items), string(raw))
	}
	rawModels, ok := items[0]["models"].([]any)
	if !ok || len(rawModels) != 1 {
		t.Fatalf("%s models = %+v, want exactly one model", key, items[0]["models"])
	}
	model, ok := rawModels[0].(map[string]any)
	if !ok {
		t.Fatalf("%s first model = %+v, want object", key, rawModels[0])
	}
	return model
}

func assertOAuthModelAliasForceMapping(t *testing.T, raw []byte) {
	t.Helper()

	var root map[string]any
	if errDecode := json.Unmarshal(raw, &root); errDecode != nil {
		t.Fatalf("decode oauth model alias response: %v; body = %s", errDecode, string(raw))
	}
	aliasesRoot := root["oauth-model-alias"]
	aliases, ok := aliasesRoot.(map[string]any)
	if !ok {
		t.Fatalf("oauth-model-alias = %#v, want object; body = %s", aliasesRoot, string(raw))
	}
	rawClaude, ok := aliases["claude"].([]any)
	if !ok || len(rawClaude) != 2 {
		t.Fatalf("claude aliases = %#v, want two aliases; body = %s", aliases["claude"], string(raw))
	}
	first, ok := rawClaude[0].(map[string]any)
	if !ok {
		t.Fatalf("first alias = %#v, want object", rawClaude[0])
	}
	if first["name"] != "claude-sonnet-4" || first["alias"] != "sonnet" {
		t.Fatalf("first alias = %#v, want trimmed name and alias", first)
	}
	if first["force-mapping"] != true {
		t.Fatalf("first force-mapping = %#v, want true; alias = %#v", first["force-mapping"], first)
	}
	second, ok := rawClaude[1].(map[string]any)
	if !ok {
		t.Fatalf("second alias = %#v, want object", rawClaude[1])
	}
	if _, exists := second["force-mapping"]; exists {
		t.Fatalf("second alias = %#v, should omit false force-mapping", second)
	}
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestPutConfigYAMLForcesHomeModeScalarsAndPreservesRemoteManagement(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	handler := NewHandler(repo, nil, "127.0.0.1", 0)
	engine := gin.New()
	engine.PUT("/config.yaml", handler.PutConfigYAML)
	engine.GET("/config", handler.GetConfig)
	engine.GET("/config.yaml", handler.GetConfigYAML)

	payload := `port: 8327
usage-statistics-enabled: false
disable-cooling: false
ws-auth: true
remote-management:
  allow-remote: true
  disable-control-panel: false
api-keys:
  - user-key
plugins:
  enabled: true
  configs:
    sample-provider:
      enabled: true
      priority: 3
      load-in-home: true
      store:
        id: sample-provider
        name: Sample Provider
        version: 0.1.0
        release-tag: v0.1.0
        repository: https://example.test/sample-provider
      custom-option: keep-me
`

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/config.yaml", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/yaml")
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	getResp := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/config", nil)
	engine.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", getResp.Code, getResp.Body.String())
	}

	var cfg map[string]any
	if errDecode := json.Unmarshal(getResp.Body.Bytes(), &cfg); errDecode != nil {
		t.Fatalf("decode config: %v", errDecode)
	}
	if cfg["usage-statistics-enabled"] != true {
		t.Fatalf("usage-statistics-enabled = %v, want true", cfg["usage-statistics-enabled"])
	}
	if cfg["disable-cooling"] != true {
		t.Fatalf("disable-cooling = %v, want true", cfg["disable-cooling"])
	}
	if cfg["ws-auth"] != false {
		t.Fatalf("ws-auth = %v, want false", cfg["ws-auth"])
	}
	plugins, ok := cfg["plugins"].(map[string]any)
	if !ok {
		t.Fatalf("plugins = %#v, want object", cfg["plugins"])
	}
	configs, ok := plugins["configs"].(map[string]any)
	if !ok {
		t.Fatalf("plugins.configs = %#v, want object", plugins["configs"])
	}
	sample, ok := configs["sample-provider"].(map[string]any)
	if !ok {
		t.Fatalf("sample-provider config = %#v, want object", configs["sample-provider"])
	}
	if sample["load-in-home"] != true || sample["custom-option"] != "keep-me" {
		t.Fatalf("sample-provider config = %#v, want raw plugin fields preserved", sample)
	}
	store, ok := sample["store"].(map[string]any)
	if !ok {
		t.Fatalf("sample-provider.store = %#v, want object", sample["store"])
	}
	if store["repository"] != "https://example.test/sample-provider" || store["release-tag"] != "v0.1.0" {
		t.Fatalf("sample-provider.store = %#v, want plugin store manifest preserved", store)
	}
	entries, errEntries := repo.ListAPIKeyEntries(context.Background())
	if errEntries != nil {
		t.Fatalf("ListAPIKeyEntries() error = %v", errEntries)
	}
	if len(entries) != 1 || entries[0].APIKey != "user-key" {
		t.Fatalf("api key entries = %+v, want user-key preserved", entries)
	}
	yamlResp := httptest.NewRecorder()
	yamlReq := httptest.NewRequest(http.MethodGet, "/config.yaml", nil)
	engine.ServeHTTP(yamlResp, yamlReq)
	if yamlResp.Code != http.StatusOK {
		t.Fatalf("yaml status = %d, body = %s", yamlResp.Code, yamlResp.Body.String())
	}
	if !strings.Contains(yamlResp.Body.String(), "allow-remote: true") {
		t.Fatalf("config yaml = %s, want allow-remote preserved true", yamlResp.Body.String())
	}
	if !strings.Contains(yamlResp.Body.String(), "disable-control-panel: false") {
		t.Fatalf("config yaml = %s, want disable-control-panel preserved false", yamlResp.Body.String())
	}
}

func TestPutUsageStatisticsEnabledRejectsDisable(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	handler := NewHandler(cluster.NewRepository(db), nil, "127.0.0.1", 0)
	engine := gin.New()
	engine.PUT("/usage-statistics-enabled", handler.PutUsageStatisticsEnabled)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/usage-statistics-enabled", strings.NewReader(`{"value": false}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
}
