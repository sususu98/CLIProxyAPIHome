package management

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginstore"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"github.com/router-for-me/CLIProxyAPIHome/internal/node"
)

func TestInstallPluginFromStoreWritesManifestConfig(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	if errReplace := repo.ReplaceConfigSnapshot(context.Background(), map[string]any{
		"plugins": map[string]any{
			"enabled": true,
			"dir":     t.TempDir(),
		},
	}); errReplace != nil {
		t.Fatalf("ReplaceConfigSnapshot() error = %v", errReplace)
	}

	handler := NewHandler(repo, nil, "127.0.0.1", 0)
	handler.SetPluginStoreHTTPClient(fakePluginStoreHTTPClient{
		pluginstore.DefaultRegistryURL: []byte(`{
			"schema_version": 1,
			"plugins": [{
				"id": "sample-provider",
				"name": "Sample Provider",
				"description": "Adds sample provider support.",
				"author": "author-name",
				"repository": "https://github.com/author-name/sample-provider"
			}]
		}`),
		"https://api.github.com/repos/author-name/sample-provider/releases/latest": []byte(`{
			"tag_name": "v0.2.0",
			"assets": []
		}`),
	})
	engine := gin.New()
	engine.POST("/plugin-store/:id/install", handler.InstallPluginFromStore)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/plugin-store/sample-provider/install", nil)
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	cfg, _, errConfig := repo.LoadConfigAsRuntimeConfig(context.Background())
	if errConfig != nil {
		t.Fatalf("LoadConfigAsRuntimeConfig() error = %v", errConfig)
	}
	item, okItem := cfg.Plugins.Configs["sample-provider"]
	if !okItem {
		t.Fatal("plugin config missing after install")
	}
	if item.Enabled == nil || !*item.Enabled {
		t.Fatalf("plugin enabled = %v, want true", item.Enabled)
	}
	storeNode := yamlMappingValue(&item.Raw, "store")
	if storeNode == nil {
		t.Fatal("plugin store manifest missing")
	}
	var manifest pluginstore.Manifest
	if errDecode := storeNode.Decode(&manifest); errDecode != nil {
		t.Fatalf("decode manifest: %v", errDecode)
	}
	if manifest.ID != "sample-provider" || manifest.Version != "0.2.0" || manifest.ReleaseTag != "v0.2.0" ||
		manifest.Repository != "https://github.com/author-name/sample-provider" || manifest.SourceID != pluginstore.DefaultSourceID {
		t.Fatalf("manifest = %+v, want pinned store metadata", manifest)
	}
}

type fakePluginStoreHTTPClient map[string][]byte

func (c fakePluginStoreHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body, ok := c[req.URL.String()]
	if !ok {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader("not found")),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestListPluginStoreReportsManifestStatus(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	customSourceURL := "https://plugins.example.test/registry.json?team=a&env=b"
	if errReplace := repo.ReplaceConfigSnapshot(context.Background(), map[string]any{
		"plugins": map[string]any{
			"enabled":       true,
			"store-sources": []string{customSourceURL},
			"configs": map[string]any{
				"sample-provider": map[string]any{
					"enabled": true,
					"store": map[string]any{
						"id":          "sample-provider",
						"name":        "Sample Provider",
						"description": "Adds sample provider support.",
						"author":      "author-name",
						"version":     "0.2.0",
						"release-tag": "v0.2.0",
						"repository":  "https://github.com/author-name/sample-provider",
						"source-id":   pluginstore.DefaultSourceID,
						"source-name": pluginstore.DefaultSourceName,
						"source-url":  pluginstore.DefaultRegistryURL,
					},
				},
			},
		},
	}); errReplace != nil {
		t.Fatalf("ReplaceConfigSnapshot() error = %v", errReplace)
	}

	handler := NewHandler(repo, nil, "127.0.0.1", 0)
	handler.SetPluginStoreHTTPClient(fakePluginStoreHTTPClient{
		pluginstore.DefaultRegistryURL: []byte(`{
			"schema_version": 1,
			"plugins": [{
				"id": "sample-provider",
				"name": "Sample Provider",
				"description": "Adds sample provider support.",
				"author": "author-name",
				"version": "0.2.0",
				"repository": "https://github.com/author-name/sample-provider"
			}]
		}`),
		customSourceURL: []byte(`{
			"schema_version": 1,
			"plugins": []
		}`),
	})
	engine := gin.New()
	engine.GET("/plugin-store", handler.ListPluginStore)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/plugin-store", nil)
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Sources []struct {
			URL string `json:"url"`
		} `json:"sources"`
		Plugins []struct {
			ID               string `json:"id"`
			Installed        bool   `json:"installed"`
			InstalledVersion string `json:"installed_version"`
			Enabled          bool   `json:"enabled"`
			EffectiveEnabled bool   `json:"effective_enabled"`
		} `json:"plugins"`
	}
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &body); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if len(body.Plugins) != 1 {
		t.Fatalf("plugins len = %d, want 1", len(body.Plugins))
	}
	foundCustomSource := false
	for _, source := range body.Sources {
		if source.URL == customSourceURL {
			foundCustomSource = true
		}
		if strings.Contains(source.URL, "&amp;") {
			t.Fatalf("source URL was HTML-escaped: %q", source.URL)
		}
	}
	if !foundCustomSource {
		t.Fatalf("custom source URL missing from response: %+v", body.Sources)
	}
	entry := body.Plugins[0]
	if entry.ID != "sample-provider" || !entry.Installed || entry.InstalledVersion != "0.2.0" || !entry.Enabled || !entry.EffectiveEnabled {
		t.Fatalf("plugin entry = %+v, want manifest installed status", entry)
	}
}

func TestUninstallPluginFromStoreRemovesConfigAndCreatesTask(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	if errReplace := repo.ReplaceConfigSnapshot(context.Background(), map[string]any{
		"plugins": map[string]any{
			"enabled": true,
			"configs": map[string]any{
				"sample-provider": map[string]any{
					"enabled": true,
					"store": map[string]any{
						"id":          "sample-provider",
						"name":        "Sample Provider",
						"description": "Adds sample provider support.",
						"author":      "author-name",
						"version":     "0.2.0",
						"release-tag": "v0.2.0",
						"repository":  "https://github.com/author-name/sample-provider",
					},
				},
			},
		},
	}); errReplace != nil {
		t.Fatalf("ReplaceConfigSnapshot() error = %v", errReplace)
	}

	handler := NewHandler(repo, nil, "127.0.0.1", 0)
	engine := gin.New()
	engine.POST("/plugin-store/:id/uninstall", handler.UninstallPluginFromStore)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/plugin-store/sample-provider/uninstall", nil)
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	cfg, _, errConfig := repo.LoadConfigAsRuntimeConfig(context.Background())
	if errConfig != nil {
		t.Fatalf("LoadConfigAsRuntimeConfig() error = %v", errConfig)
	}
	if _, ok := cfg.Plugins.Configs["sample-provider"]; ok {
		t.Fatal("plugin config still exists after delete")
	}
	tasks, errTasks := repo.ListPendingPluginTasks(context.Background(), node.PluginStatusNodeTypeCPA, "node-1")
	if errTasks != nil {
		t.Fatalf("ListPendingPluginTasks() error = %v", errTasks)
	}
	if len(tasks) != 1 || tasks[0].Operation != node.PluginTaskOperationDelete || tasks[0].PluginID != "sample-provider" || tasks[0].TargetNodeType != node.PluginTaskTargetAll {
		t.Fatalf("tasks = %+v, want one global delete task", tasks)
	}
}

func TestUninstallPluginFromStoreIgnoresTargetQueryAndCreatesGlobalTask(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	if errReplace := repo.ReplaceConfigSnapshot(context.Background(), map[string]any{
		"plugins": map[string]any{
			"enabled": true,
			"configs": map[string]any{
				"sample-provider": map[string]any{
					"enabled": true,
					"store": map[string]any{
						"id":          "sample-provider",
						"name":        "Sample Provider",
						"description": "Adds sample provider support.",
						"author":      "author-name",
						"version":     "0.2.0",
						"release-tag": "v0.2.0",
						"repository":  "https://github.com/author-name/sample-provider",
					},
				},
			},
		},
	}); errReplace != nil {
		t.Fatalf("ReplaceConfigSnapshot() error = %v", errReplace)
	}

	handler := NewHandler(repo, nil, "127.0.0.1", 0)
	engine := gin.New()
	engine.POST("/plugin-store/:id/uninstall", handler.UninstallPluginFromStore)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/plugin-store/sample-provider/uninstall?node_id=node-1&remove_config=false", nil)
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	cfg, _, errConfig := repo.LoadConfigAsRuntimeConfig(context.Background())
	if errConfig != nil {
		t.Fatalf("LoadConfigAsRuntimeConfig() error = %v", errConfig)
	}
	if _, ok := cfg.Plugins.Configs["sample-provider"]; ok {
		t.Fatal("plugin config still exists after delete")
	}

	for _, nodeID := range []string{"node-1", "node-2"} {
		tasks, errTasks := repo.ListPendingPluginTasks(context.Background(), node.PluginStatusNodeTypeCPA, nodeID)
		if errTasks != nil {
			t.Fatalf("ListPendingPluginTasks(%s) error = %v", nodeID, errTasks)
		}
		if len(tasks) != 1 || tasks[0].Operation != node.PluginTaskOperationDelete || tasks[0].PluginID != "sample-provider" || tasks[0].TargetNodeType != node.PluginTaskTargetAll || tasks[0].TargetNodeID != "" {
			t.Fatalf("%s tasks = %+v, want one global delete task", nodeID, tasks)
		}
	}
}

func TestUninstallPluginFromStoreIgnoresInvalidTargetQuery(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	if errReplace := repo.ReplaceConfigSnapshot(context.Background(), map[string]any{
		"plugins": map[string]any{
			"enabled": true,
			"configs": map[string]any{
				"sample-provider": map[string]any{
					"enabled": true,
					"store": map[string]any{
						"id":          "sample-provider",
						"name":        "Sample Provider",
						"description": "Adds sample provider support.",
						"author":      "author-name",
						"version":     "0.2.0",
						"release-tag": "v0.2.0",
						"repository":  "https://github.com/author-name/sample-provider",
					},
				},
			},
		},
	}); errReplace != nil {
		t.Fatalf("ReplaceConfigSnapshot() error = %v", errReplace)
	}

	handler := NewHandler(repo, nil, "127.0.0.1", 0)
	engine := gin.New()
	engine.POST("/plugin-store/:id/uninstall", handler.UninstallPluginFromStore)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/plugin-store/sample-provider/uninstall?node_id=node-1&node_type=bad", nil)
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	cfg, _, errConfig := repo.LoadConfigAsRuntimeConfig(context.Background())
	if errConfig != nil {
		t.Fatalf("LoadConfigAsRuntimeConfig() error = %v", errConfig)
	}
	if _, ok := cfg.Plugins.Configs["sample-provider"]; ok {
		t.Fatal("plugin config still exists after delete")
	}
	tasks, errTasks := repo.ListPendingPluginTasks(context.Background(), node.PluginStatusNodeTypeCPA, "node-1")
	if errTasks != nil {
		t.Fatalf("ListPendingPluginTasks() error = %v", errTasks)
	}
	if len(tasks) != 1 || tasks[0].Operation != node.PluginTaskOperationDelete || tasks[0].PluginID != "sample-provider" || tasks[0].TargetNodeType != node.PluginTaskTargetAll || tasks[0].TargetNodeID != "" {
		t.Fatalf("tasks = %+v, want one global delete task", tasks)
	}
}
