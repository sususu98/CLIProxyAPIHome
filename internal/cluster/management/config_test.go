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

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
