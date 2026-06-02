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

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
