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

func TestListNodesIncludesCPASnapshotsFromAllHomeNodes(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	now := time.Now().UTC()
	if errSnapshot := repo.ReplaceCPANodeSnapshot(context.Background(), "home-a", 8327, []node.Node{
		{NodeID: "cpa-a-1", IP: "10.0.1.1", Connected: now.Add(-4 * time.Minute), ClientCount: 1},
		{NodeID: "cpa-a-2", IP: "10.0.1.2", Connected: now.Add(-3 * time.Minute), ClientCount: 1},
	}, now); errSnapshot != nil {
		t.Fatalf("ReplaceCPANodeSnapshot(home-a) error = %v", errSnapshot)
	}
	if errSnapshot := repo.ReplaceCPANodeSnapshot(context.Background(), "home-b", 8328, []node.Node{
		{NodeID: "cpa-b-1", IP: "10.0.2.1", Connected: now.Add(-2 * time.Minute), ClientCount: 1},
		{NodeID: "cpa-b-2", IP: "10.0.2.2", Connected: now.Add(-1 * time.Minute), ClientCount: 1},
	}, now); errSnapshot != nil {
		t.Fatalf("ReplaceCPANodeSnapshot(home-b) error = %v", errSnapshot)
	}

	handler := NewHandler(repo, nil, "home-a", 8327)
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
			NodeID      string `json:"node_id"`
			IP          string `json:"ip"`
			ClientCount int    `json:"client_count"`
			Healthy     bool   `json:"healthy"`
			HomeIP      string `json:"home_ip"`
			HomePort    int    `json:"home_port"`
		} `json:"nodes"`
	}
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &body); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if len(body.Nodes) != 4 {
		t.Fatalf("nodes len = %d, want 4: %+v", len(body.Nodes), body.Nodes)
	}

	got := make(map[string]struct {
		HomeIP   string
		HomePort int
	})
	for _, item := range body.Nodes {
		if item.ClientCount != 1 || !item.Healthy {
			t.Fatalf("node = %+v, want one healthy client", item)
		}
		got[item.NodeID] = struct {
			HomeIP   string
			HomePort int
		}{HomeIP: item.HomeIP, HomePort: item.HomePort}
	}
	for nodeID, want := range map[string]struct {
		HomeIP   string
		HomePort int
	}{
		"cpa-a-1": {HomeIP: "home-a", HomePort: 8327},
		"cpa-a-2": {HomeIP: "home-a", HomePort: 8327},
		"cpa-b-1": {HomeIP: "home-b", HomePort: 8328},
		"cpa-b-2": {HomeIP: "home-b", HomePort: 8328},
	} {
		if got[nodeID] != want {
			t.Fatalf("node %s home = %+v, want %+v; all nodes = %+v", nodeID, got[nodeID], want, body.Nodes)
		}
	}
}

func TestListNodesUsesConfiguredHeartbeatTimeout(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	now := time.Now().UTC()
	lastSeenAt := now.Add(-30 * time.Second)
	if errSnapshot := repo.ReplaceCPANodeSnapshot(context.Background(), "home-timeout", 8327, []node.Node{
		{NodeID: "cpa-timeout", IP: "10.0.3.1", Connected: now.Add(-2 * time.Minute), ClientCount: 1},
	}, lastSeenAt); errSnapshot != nil {
		t.Fatalf("ReplaceCPANodeSnapshot() error = %v", errSnapshot)
	}

	handler := NewHandler(repo, nil, "home-timeout", 8327)
	handler.SetHeartbeatTimeout(time.Minute)
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
			NodeID string `json:"node_id"`
		} `json:"nodes"`
	}
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &body); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if len(body.Nodes) != 1 || body.Nodes[0].NodeID != "cpa-timeout" {
		t.Fatalf("nodes = %+v, want cpa-timeout within configured timeout", body.Nodes)
	}
}

type topologyTestHomeItem struct {
	ID              string `json:"id"`
	Role            string `json:"role"`
	IsMaster        bool   `json:"is_master"`
	ReportedMaster  bool   `json:"reported_master"`
	Health          string `json:"health"`
	CPACount        int    `json:"cpa_count"`
	HealthyCPACount int    `json:"healthy_cpa_count"`
}

type topologyTestCPAItem struct {
	NodeID   string `json:"node_id"`
	HomeID   string `json:"home_id"`
	HomeIP   string `json:"home_ip"`
	HomePort int    `json:"home_port"`
	Health   string `json:"health"`
	Healthy  bool   `json:"healthy"`
}

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

	handler := NewHandler(repo, nil, "127.0.0.1", 8327)
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
			HomeID            string                  `json:"home_id"`
			HomeIP            string                  `json:"home_ip"`
			HomePort          int                     `json:"home_port"`
			LastSeenAt        time.Time               `json:"last_seen_at"`
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
	if body.Nodes[0].NodeID != "node-1" || body.Nodes[0].IP != "10.0.0.5" || body.Nodes[0].HomeID != "127.0.0.1:8327" || body.Nodes[0].HomeIP != "127.0.0.1" || body.Nodes[0].HomePort != 8327 || body.Nodes[0].LastSeenAt.IsZero() || !body.Nodes[0].Healthy || body.Nodes[0].PluginReportState != "reported_failed" || len(body.Nodes[0].Statuses) != 1 {
		t.Fatalf("node = %+v, want failed plugin report state", body.Nodes[0])
	}
}

func TestGetTopologyReturnsHomeAndCPANodes(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	now := time.Now().UTC()
	homeRecords := []cluster.ClusterNodeRecord{
		{
			IP:          "home-a",
			Port:        8327,
			IsMaster:    true,
			ClientCount: 1,
			StartedAt:   now.Add(-time.Minute),
			LastSeenAt:  now,
		},
		{
			IP:          "home-b",
			Port:        8327,
			IsMaster:    false,
			ClientCount: 1,
			StartedAt:   now,
			LastSeenAt:  now,
		},
	}
	if errCreate := db.Create(&homeRecords).Error; errCreate != nil {
		t.Fatalf("create home records: %v", errCreate)
	}

	if errSnapshot := repo.ReplaceCPANodeSnapshot(context.Background(), "home-a", 8327, []node.Node{
		{NodeID: "cpa-a", IP: "10.0.0.5", ClientCount: 1, Connected: now.Add(-30 * time.Second)},
	}, now); errSnapshot != nil {
		t.Fatalf("replace cpa-a snapshot: %v", errSnapshot)
	}
	if errSnapshot := repo.ReplaceCPANodeSnapshot(context.Background(), "home-b", 8327, []node.Node{
		{NodeID: "cpa-b", IP: "10.0.0.6", ClientCount: 1, Connected: now.Add(-20 * time.Second)},
	}, now); errSnapshot != nil {
		t.Fatalf("replace cpa-b snapshot: %v", errSnapshot)
	}

	handler := NewHandler(repo, nil, "home-a", 8327)
	engine := gin.New()
	engine.GET("/topology", handler.GetTopology)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/topology", nil)
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var body struct {
		Summary struct {
			HomeCount        int  `json:"home_count"`
			HealthyHomeCount int  `json:"healthy_home_count"`
			CPACount         int  `json:"cpa_count"`
			HealthyCPACount  int  `json:"healthy_cpa_count"`
			MissingMaster    bool `json:"missing_master"`
		} `json:"summary"`
		Management struct {
			HomeID string `json:"home_id"`
		} `json:"management"`
		Master struct {
			ID string `json:"id"`
		} `json:"master"`
		Homes []topologyTestHomeItem `json:"homes"`
		CPAs  []topologyTestCPAItem  `json:"cpas"`
	}
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &body); errDecode != nil {
		t.Fatalf("decode topology: %v; body=%s", errDecode, resp.Body.String())
	}
	if body.Summary.HomeCount != 2 || body.Summary.HealthyHomeCount != 2 || body.Summary.CPACount != 2 || body.Summary.HealthyCPACount != 2 || body.Summary.MissingMaster {
		t.Fatalf("summary = %+v, want two healthy homes and cpas with master", body.Summary)
	}
	if body.Management.HomeID != "home-a:8327" {
		t.Fatalf("management home id = %q, want home-a:8327", body.Management.HomeID)
	}
	if body.Master.ID != "home-a:8327" {
		t.Fatalf("master id = %q, want home-a:8327", body.Master.ID)
	}
	if topologyTestHome(body.Homes, "home-a:8327").CPACount != 1 || topologyTestHome(body.Homes, "home-b:8327").CPACount != 1 {
		t.Fatalf("homes = %+v, want one CPA under each Home", body.Homes)
	}
	cpaA := topologyTestCPA(body.CPAs, "cpa-a")
	if cpaA.HomeID != "home-a:8327" || cpaA.HomeIP != "home-a" || cpaA.HomePort != 8327 || cpaA.Health != "healthy" || !cpaA.Healthy {
		t.Fatalf("cpa-a = %+v, want connected to home-a and healthy", cpaA)
	}
	cpaB := topologyTestCPA(body.CPAs, "cpa-b")
	if cpaB.HomeID != "home-b:8327" || cpaB.HomeIP != "home-b" || cpaB.HomePort != 8327 || cpaB.Health != "healthy" || !cpaB.Healthy {
		t.Fatalf("cpa-b = %+v, want connected to home-b and healthy", cpaB)
	}
}

func TestGetTopologyReportsMissingMasterWithoutHomeHeartbeat(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	handler := NewHandler(repo, nil, "home-a", 8327)
	engine := gin.New()
	engine.GET("/topology", handler.GetTopology)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/topology", nil)
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var body struct {
		Summary struct {
			HomeCount      int  `json:"home_count"`
			CPACount       int  `json:"cpa_count"`
			MissingMaster  bool `json:"missing_master"`
			AttentionCount int  `json:"attention_count"`
		} `json:"summary"`
		Master *topologyTestHomeItem `json:"master"`
	}
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &body); errDecode != nil {
		t.Fatalf("decode topology: %v; body=%s", errDecode, resp.Body.String())
	}
	if body.Summary.HomeCount != 0 || body.Summary.CPACount != 0 || !body.Summary.MissingMaster || body.Summary.AttentionCount != 1 {
		t.Fatalf("summary = %+v, want empty topology with missing master attention", body.Summary)
	}
	if body.Master != nil {
		t.Fatalf("master = %+v, want nil for empty topology", body.Master)
	}
}

func TestGetTopologyMarksCPAUnknownWhenServingHomeIsMissing(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	now := time.Now().UTC()
	if errSnapshot := repo.ReplaceCPANodeSnapshot(context.Background(), "home-missing", 8327, []node.Node{
		{NodeID: "cpa-orphan", IP: "10.0.0.10", ClientCount: 1, Connected: now.Add(-time.Minute)},
	}, now); errSnapshot != nil {
		t.Fatalf("replace orphan cpa snapshot: %v", errSnapshot)
	}

	handler := NewHandler(repo, nil, "home-a", 8327)
	engine := gin.New()
	engine.GET("/topology", handler.GetTopology)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/topology", nil)
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var body struct {
		Summary struct {
			HomeCount       int  `json:"home_count"`
			CPACount        int  `json:"cpa_count"`
			UnknownCPACount int  `json:"unknown_cpa_count"`
			MissingMaster   bool `json:"missing_master"`
			AttentionCount  int  `json:"attention_count"`
		} `json:"summary"`
		CPAs []topologyTestCPAItem `json:"cpas"`
	}
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &body); errDecode != nil {
		t.Fatalf("decode topology: %v; body=%s", errDecode, resp.Body.String())
	}
	if body.Summary.HomeCount != 0 || body.Summary.CPACount != 1 || body.Summary.UnknownCPACount != 1 || !body.Summary.MissingMaster || body.Summary.AttentionCount != 2 {
		t.Fatalf("summary = %+v, want orphan cpa counted unknown plus missing master", body.Summary)
	}
	cpa := topologyTestCPA(body.CPAs, "cpa-orphan")
	if cpa.HomeID != "home-missing:8327" || cpa.Health != "unknown" || cpa.Healthy {
		t.Fatalf("cpa-orphan = %+v, want unknown cpa under missing home", cpa)
	}
}

func TestGetTopologyIncludesStaleCPASnapshots(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	now := time.Now().UTC()
	staleSeenAt := now.Add(-2 * time.Minute)
	homeRecord := cluster.ClusterNodeRecord{
		IP:          "home-stale",
		Port:        8327,
		IsMaster:    true,
		ClientCount: 1,
		StartedAt:   now.Add(-3 * time.Minute),
		LastSeenAt:  staleSeenAt,
	}
	if errCreate := db.Create(&homeRecord).Error; errCreate != nil {
		t.Fatalf("create stale home record: %v", errCreate)
	}
	if errSnapshot := repo.ReplaceCPANodeSnapshot(context.Background(), "home-stale", 8327, []node.Node{
		{NodeID: "cpa-stale", IP: "10.0.0.7", ClientCount: 1, Connected: staleSeenAt.Add(-time.Minute)},
	}, staleSeenAt); errSnapshot != nil {
		t.Fatalf("replace stale cpa snapshot: %v", errSnapshot)
	}

	handler := NewHandler(repo, nil, "home-stale", 8327)
	handler.SetHeartbeatTimeout(30 * time.Second)
	engine := gin.New()
	engine.GET("/topology", handler.GetTopology)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/topology", nil)
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var body struct {
		Summary struct {
			CPACount              int  `json:"cpa_count"`
			StaleCPACount         int  `json:"stale_cpa_count"`
			HomeCount             int  `json:"home_count"`
			StaleHome             int  `json:"stale_home_count"`
			MissingMaster         bool `json:"missing_master"`
			RetentionAfterSeconds int  `json:"retention_after_seconds"`
		} `json:"summary"`
		Master *topologyTestHomeItem  `json:"master"`
		Homes  []topologyTestHomeItem `json:"homes"`
		CPAs   []topologyTestCPAItem  `json:"cpas"`
	}
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &body); errDecode != nil {
		t.Fatalf("decode topology: %v; body=%s", errDecode, resp.Body.String())
	}
	if body.Summary.HomeCount != 1 || body.Summary.StaleHome != 1 || body.Summary.CPACount != 1 || body.Summary.StaleCPACount != 1 {
		t.Fatalf("summary = %+v, want one stale home and one stale cpa", body.Summary)
	}
	if !body.Summary.MissingMaster {
		t.Fatalf("missing_master = false, want true for stale-only topology")
	}
	if body.Summary.RetentionAfterSeconds != 180 {
		t.Fatalf("retention_after_seconds = %d, want 180", body.Summary.RetentionAfterSeconds)
	}
	if body.Master != nil {
		t.Fatalf("master = %+v, want nil for stale-only topology", body.Master)
	}
	home := topologyTestHome(body.Homes, "home-stale:8327")
	if home.Role != "unknown" || home.IsMaster || !home.ReportedMaster || home.Health != "stale" {
		t.Fatalf("home-stale = %+v, want stale reported master without current master role", home)
	}
	cpa := topologyTestCPA(body.CPAs, "cpa-stale")
	if cpa.HomeID != "home-stale:8327" || cpa.Health != "stale" || cpa.Healthy {
		t.Fatalf("cpa-stale = %+v, want stale cpa under stale home", cpa)
	}
}

func TestGetTopologyPrunesExpiredSnapshots(t *testing.T) {
	db, cleanup := openManagementLogTestDB(t)
	defer cleanup()

	repo := cluster.NewRepository(db)
	now := time.Now().UTC()
	recentSeenAt := now.Add(-2 * time.Minute)
	expiredSeenAt := now.Add(-10 * time.Minute)
	homeRecords := []cluster.ClusterNodeRecord{
		{
			IP:          "home-recent",
			Port:        8327,
			IsMaster:    false,
			ClientCount: 1,
			StartedAt:   recentSeenAt.Add(-time.Minute),
			LastSeenAt:  recentSeenAt,
		},
		{
			IP:          "home-expired",
			Port:        8327,
			IsMaster:    false,
			ClientCount: 1,
			StartedAt:   expiredSeenAt.Add(-time.Minute),
			LastSeenAt:  expiredSeenAt,
		},
	}
	if errCreate := db.Create(&homeRecords).Error; errCreate != nil {
		t.Fatalf("create home records: %v", errCreate)
	}
	if errSnapshot := repo.ReplaceCPANodeSnapshot(context.Background(), "home-recent", 8327, []node.Node{
		{NodeID: "cpa-recent", IP: "10.0.0.8", ClientCount: 1, Connected: recentSeenAt.Add(-time.Minute)},
	}, recentSeenAt); errSnapshot != nil {
		t.Fatalf("replace recent cpa snapshot: %v", errSnapshot)
	}
	if errSnapshot := repo.ReplaceCPANodeSnapshot(context.Background(), "home-expired", 8327, []node.Node{
		{NodeID: "cpa-expired", IP: "10.0.0.9", ClientCount: 1, Connected: expiredSeenAt.Add(-time.Minute)},
	}, expiredSeenAt); errSnapshot != nil {
		t.Fatalf("replace expired cpa snapshot: %v", errSnapshot)
	}

	handler := NewHandler(repo, nil, "home-recent", 8327)
	handler.SetHeartbeatTimeout(30 * time.Second)
	engine := gin.New()
	engine.GET("/topology", handler.GetTopology)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/topology", nil)
	engine.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	var body struct {
		Summary struct {
			HomeCount int `json:"home_count"`
			CPACount  int `json:"cpa_count"`
		} `json:"summary"`
		Homes []topologyTestHomeItem `json:"homes"`
		CPAs  []topologyTestCPAItem  `json:"cpas"`
	}
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &body); errDecode != nil {
		t.Fatalf("decode topology: %v; body=%s", errDecode, resp.Body.String())
	}
	if body.Summary.HomeCount != 1 || body.Summary.CPACount != 1 {
		t.Fatalf("summary = %+v, want only recent stale snapshots within retention", body.Summary)
	}
	if topologyTestHome(body.Homes, "home-recent:8327").ID == "" {
		t.Fatalf("homes = %+v, want home-recent", body.Homes)
	}
	if topologyTestHome(body.Homes, "home-expired:8327").ID != "" {
		t.Fatalf("homes = %+v, want home-expired pruned", body.Homes)
	}
	if topologyTestCPA(body.CPAs, "cpa-recent").NodeID == "" {
		t.Fatalf("cpas = %+v, want cpa-recent", body.CPAs)
	}
	if topologyTestCPA(body.CPAs, "cpa-expired").NodeID != "" {
		t.Fatalf("cpas = %+v, want cpa-expired pruned", body.CPAs)
	}
}

func topologyTestHome(items []topologyTestHomeItem, id string) topologyTestHomeItem {
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	return topologyTestHomeItem{}
}

func topologyTestCPA(items []topologyTestCPAItem, nodeID string) topologyTestCPAItem {
	for _, item := range items {
		if item.NodeID == nodeID {
			return item
		}
	}
	return topologyTestCPAItem{}
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
