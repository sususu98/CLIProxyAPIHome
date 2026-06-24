package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"github.com/router-for-me/CLIProxyAPIHome/internal/node"
)

func TestListNodesIncludesPluginTaskHealth(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	if errReplace := repo.ReplaceConfigSnapshot(context.Background(), map[string]any{
		"plugins": map[string]any{
			"enabled": true,
			"configs": map[string]any{
				"sample": map[string]any{
					"enabled": true,
					"store": map[string]any{
						"id":          "sample",
						"name":        "Sample",
						"description": "Adds sample support.",
						"author":      "owner",
						"version":     "0.2.0",
						"release-tag": "v0.2.0",
						"repository":  "https://github.com/owner/sample",
					},
				},
			},
		},
	}); errReplace != nil {
		t.Fatalf("ReplaceConfigSnapshot() error = %v", errReplace)
	}

	status := node.PluginTaskStatus{
		SchemaVersion: 1,
		Task:          "plugin-sync",
		NodeID:        "node-1",
		ClientIP:      "10.0.0.5",
		Status:        "failed",
		Phase:         "install",
		OK:            false,
		StartedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
		Plugins: []node.PluginTaskPlugin{{
			ID:            "sample",
			InstallStatus: "failed",
			Error:         "boom",
		}},
	}
	if errStore := repo.ReplacePluginStatus(context.Background(), node.PluginStatusNodeTypeCPA, status); errStore != nil {
		t.Fatalf("ReplacePluginStatus() error = %v", errStore)
	}

	node.GlobalRegistry().AddWithNodeID("10.0.0.5", "node-1", time.Now().UTC())
	defer node.GlobalRegistry().RemoveWithNodeID("10.0.0.5", "node-1")

	handler := NewHandler(repo, nil, "127.0.0.1", 0)
	engine := gin.New()
	engine.GET("/nodes", handler.ListNodes)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var body struct {
		PluginReportRequired bool `json:"plugin_report_required"`
		Nodes                []struct {
			NodeID            string                  `json:"node_id"`
			IP                string                  `json:"ip"`
			Healthy           bool                    `json:"healthy"`
			PluginReportState string                  `json:"plugin_report_state"`
			Statuses          []node.PluginTaskStatus `json:"plugin_report_statuses"`
		} `json:"nodes"`
		Statuses []node.PluginTaskStatus `json:"plugin_report_statuses"`
	}
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &body); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if !body.PluginReportRequired {
		t.Fatal("plugin_report_required = false, want true")
	}
	if len(body.Statuses) != 1 || body.Statuses[0].NodeID != "node-1" {
		t.Fatalf("top-level statuses = %+v, want node-1", body.Statuses)
	}
	if len(body.Nodes) != 1 {
		t.Fatalf("nodes len = %d, want 1", len(body.Nodes))
	}
	if body.Nodes[0].NodeID != "node-1" || body.Nodes[0].IP != "10.0.0.5" || !body.Nodes[0].Healthy || body.Nodes[0].PluginReportState != "reported_failed" || len(body.Nodes[0].Statuses) != 1 {
		t.Fatalf("node = %+v, want failed plugin report state", body.Nodes[0])
	}
}

func TestListNodesRequiresCurrentConfiguredPluginInReport(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	if errReplace := repo.ReplaceConfigSnapshot(context.Background(), map[string]any{
		"plugins": map[string]any{
			"enabled": true,
			"configs": map[string]any{
				"sample": map[string]any{
					"enabled": true,
					"store": map[string]any{
						"id":          "sample",
						"name":        "Sample",
						"description": "Adds sample support.",
						"author":      "owner",
						"version":     "0.2.0",
						"release-tag": "v0.2.0",
						"repository":  "https://github.com/owner/sample",
					},
				},
			},
		},
	}); errReplace != nil {
		t.Fatalf("ReplaceConfigSnapshot() error = %v", errReplace)
	}

	status := node.PluginTaskStatus{
		SchemaVersion: 1,
		Task:          "plugin-sync",
		NodeID:        "node-2",
		ClientIP:      "10.0.0.6",
		Status:        "success",
		Phase:         "load",
		OK:            true,
		StartedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
		Plugins: []node.PluginTaskPlugin{{
			ID:            "other",
			InstallStatus: "installed",
			LoadStatus:    "loaded",
		}},
	}
	if errStore := repo.ReplacePluginStatus(context.Background(), node.PluginStatusNodeTypeCPA, status); errStore != nil {
		t.Fatalf("ReplacePluginStatus() error = %v", errStore)
	}

	node.GlobalRegistry().AddWithNodeID("10.0.0.6", "node-2", time.Now().UTC())
	defer node.GlobalRegistry().RemoveWithNodeID("10.0.0.6", "node-2")

	handler := NewHandler(repo, nil, "127.0.0.1", 0)
	engine := gin.New()
	engine.GET("/nodes", handler.ListNodes)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Nodes []struct {
			NodeID            string `json:"node_id"`
			PluginReportState string `json:"plugin_report_state"`
		} `json:"nodes"`
	}
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &body); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	for _, item := range body.Nodes {
		if item.NodeID != "node-2" {
			continue
		}
		if item.PluginReportState != "reported_partial" {
			t.Fatalf("node-2 plugin_report_state = %q, want reported_partial", item.PluginReportState)
		}
		return
	}
	t.Fatalf("node-2 missing from response: %+v", body.Nodes)
}

func TestPluginReportStateRequiresLoadPhaseSuccess(t *testing.T) {
	state := pluginReportState([]node.PluginTaskStatus{{
		Status: "success",
		Phase:  "install",
		OK:     true,
		Plugins: []node.PluginTaskPlugin{{
			ID:            "sample",
			InstallStatus: "installed",
		}},
	}}, []string{"sample"})
	if state != "reported_partial" {
		t.Fatalf("pluginReportState(install success) = %q, want reported_partial", state)
	}

	state = pluginReportState([]node.PluginTaskStatus{{
		Status: "success",
		Phase:  "load",
		OK:     true,
		Plugins: []node.PluginTaskPlugin{{
			ID:            "sample",
			InstallStatus: "installed",
			LoadStatus:    "loaded",
		}},
	}}, []string{"sample"})
	if state != "reported_ok" {
		t.Fatalf("pluginReportState(load success) = %q, want reported_ok", state)
	}
}

func TestPluginReportStateRequiresConfiguredPluginIDs(t *testing.T) {
	state := pluginReportState([]node.PluginTaskStatus{{
		Status: "success",
		Phase:  "load",
		OK:     true,
	}}, []string{"sample"})
	if state != "reported_partial" {
		t.Fatalf("pluginReportState(empty plugins) = %q, want reported_partial", state)
	}

	state = pluginReportState([]node.PluginTaskStatus{{
		Status: "success",
		Phase:  "load",
		OK:     true,
		Plugins: []node.PluginTaskPlugin{{
			ID:            "other",
			InstallStatus: "installed",
			LoadStatus:    "loaded",
		}},
	}}, []string{"sample"})
	if state != "reported_partial" {
		t.Fatalf("pluginReportState(other plugin) = %q, want reported_partial", state)
	}

	state = pluginReportState([]node.PluginTaskStatus{{
		Status: "failed",
		Phase:  "install",
		OK:     false,
	}}, []string{"sample"})
	if state != "reported_failed" {
		t.Fatalf("pluginReportState(empty failed report) = %q, want reported_failed", state)
	}
}

func TestPluginReportStateLatestPluginWinsForDuplicatePluginID(t *testing.T) {
	now := time.Now().UTC()
	state := pluginReportState([]node.PluginTaskStatus{
		{
			Status:    "success",
			Phase:     "load",
			OK:        true,
			UpdatedAt: now,
			TaskID:    102,
			Plugins: []node.PluginTaskPlugin{{
				ID:            "sample",
				InstallStatus: "installed",
				LoadStatus:    "loaded",
			}},
		},
		{
			Status:    "failed",
			Phase:     "load",
			OK:        false,
			UpdatedAt: now.Add(-time.Minute),
			TaskID:    101,
			Plugins: []node.PluginTaskPlugin{{
				ID:            "sample",
				InstallStatus: "failed",
				Error:         "old retry failed",
			}},
		},
	}, []string{"sample"})
	if state != "reported_ok" {
		t.Fatalf("pluginReportState(duplicate plugin ids, latest ok) = %q, want reported_ok", state)
	}
}

func TestPluginReportStateIgnoresFailedUnrequiredReport(t *testing.T) {
	state := pluginReportState([]node.PluginTaskStatus{
		{
			Status: "failed",
			Phase:  "delete",
			OK:     false,
			Plugins: []node.PluginTaskPlugin{{
				ID:            "alpha",
				InstallStatus: "failed",
				Error:         "delete failed",
			}},
		},
		{
			Status: "success",
			Phase:  "load",
			OK:     true,
			Plugins: []node.PluginTaskPlugin{{
				ID:            "bravo",
				InstallStatus: "installed",
				LoadStatus:    "loaded",
			}},
		},
	}, []string{"bravo"})
	if state != "reported_ok" {
		t.Fatalf("pluginReportState(unrequired delete failure) = %q, want reported_ok", state)
	}
}
