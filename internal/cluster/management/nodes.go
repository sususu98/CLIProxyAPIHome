package management

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/node"
)

const (
	pluginInstallStatusFailed    = "failed"
	pluginInstallStatusInstalled = "installed"
	pluginInstallStatusSkipped   = "skipped"
	pluginLoadStatusFailed       = "failed"
	pluginLoadStatusLoaded       = "loaded"
)

type activeCPANode struct {
	NodeID      string
	IP          string
	Connected   time.Time
	ClientCount int
	HomeIP      string
	HomePort    int
	LastSeenAt  time.Time
}

// ListNodes returns a nodes.
func (h *Handler) ListNodes(c *gin.Context) {
	if c == nil {
		return
	}
	ctx, cancel := h.requestContext(c)
	defer cancel()
	taskStatuses, errStatuses := h.pluginTaskStatuses(ctx)
	if errStatuses != nil {
		respondError(c, http.StatusInternalServerError, "plugin_status_load_failed", errStatuses)
		return
	}
	statusesByNodeID, statusesByIP := pluginStatusesByNode(taskStatuses)
	requiredPluginIDs := h.pluginTaskRequiredIDs(ctx)
	activeNodes, errNodes := h.activeCPANodes(ctx)
	if errNodes != nil {
		respondError(c, http.StatusInternalServerError, "node_load_failed", errNodes)
		return
	}
	nodes := make([]gin.H, 0, len(activeNodes))
	for _, activeNode := range activeNodes {
		statuses := statusesByNodeID[strings.TrimSpace(activeNode.NodeID)]
		if len(statuses) == 0 {
			statuses = statusesByIP[activeNode.IP]
		}
		state := pluginReportState(statuses, requiredPluginIDs)
		homeID := topologyHomeID(activeNode.HomeIP, activeNode.HomePort)
		nodes = append(nodes, gin.H{
			"node_id":                activeNode.NodeID,
			"ip":                     activeNode.IP,
			"connected_time":         activeNode.Connected,
			"last_seen_at":           activeNode.LastSeenAt,
			"client_count":           activeNode.ClientCount,
			"healthy":                activeNode.ClientCount > 0,
			"home_id":                homeID,
			"home_ip":                activeNode.HomeIP,
			"home_port":              activeNode.HomePort,
			"plugin_report_state":    state,
			"plugin_report_statuses": statuses,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"nodes":                  nodes,
		"plugin_report_required": len(requiredPluginIDs) > 0,
		"plugin_report_statuses": taskStatuses,
	})
}

func (h *Handler) activeCPANodes(ctx context.Context) ([]activeCPANode, error) {
	nodesByKey := make(map[string]activeCPANode)
	if h != nil && h.repo != nil {
		cutoff := time.Time{}
		if h.heartbeatTimeout > 0 {
			cutoff = time.Now().UTC().Add(-h.heartbeatTimeout)
		}
		records, errRecords := h.repo.ListLiveCPANodes(ctx, cutoff)
		if errRecords != nil {
			return nil, errRecords
		}
		for _, record := range records {
			key := activeCPANodeKey(record.HomeIP, record.HomePort, record.NodeID, record.ClientIP)
			if key == "" {
				continue
			}
			nodesByKey[key] = activeCPANode{
				NodeID:      strings.TrimSpace(record.NodeID),
				IP:          strings.TrimSpace(record.ClientIP),
				Connected:   record.ConnectedAt,
				ClientCount: record.ClientCount,
				HomeIP:      strings.TrimSpace(record.HomeIP),
				HomePort:    record.HomePort,
				LastSeenAt:  record.LastSeenAt,
			}
		}
	}

	localHomeIP := ""
	localHomePort := 0
	if h != nil {
		localHomeIP = strings.TrimSpace(h.nodeIP)
		localHomePort = h.nodePort
	}
	for _, localNode := range node.GlobalRegistry().List() {
		key := activeCPANodeKey(localHomeIP, localHomePort, localNode.NodeID, localNode.IP)
		if key == "" {
			continue
		}
		nodesByKey[key] = activeCPANode{
			NodeID:      strings.TrimSpace(localNode.NodeID),
			IP:          strings.TrimSpace(localNode.IP),
			Connected:   localNode.Connected,
			ClientCount: localNode.ClientCount,
			HomeIP:      localHomeIP,
			HomePort:    localHomePort,
			LastSeenAt:  time.Now().UTC(),
		}
	}

	nodes := make([]activeCPANode, 0, len(nodesByKey))
	for _, item := range nodesByKey {
		nodes = append(nodes, item)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Connected.Equal(nodes[j].Connected) {
			if nodes[i].IP == nodes[j].IP {
				if nodes[i].NodeID == nodes[j].NodeID {
					if nodes[i].HomeIP == nodes[j].HomeIP {
						return nodes[i].HomePort < nodes[j].HomePort
					}
					return nodes[i].HomeIP < nodes[j].HomeIP
				}
				return nodes[i].NodeID < nodes[j].NodeID
			}
			return nodes[i].IP < nodes[j].IP
		}
		return nodes[i].Connected.Before(nodes[j].Connected)
	})
	return nodes, nil
}

func activeCPANodeKey(homeIP string, homePort int, nodeID string, clientIP string) string {
	homeIP = strings.TrimSpace(homeIP)
	nodeID = strings.TrimSpace(nodeID)
	clientIP = strings.TrimSpace(clientIP)
	nodeKey := ""
	if nodeID != "" {
		nodeKey = "node:" + nodeID
	} else if clientIP != "" {
		nodeKey = "ip:" + clientIP
	}
	if nodeKey == "" {
		return ""
	}
	return homeIP + ":" + strconv.Itoa(homePort) + "/" + nodeKey
}

func (h *Handler) pluginTaskStatuses(ctx context.Context) ([]node.PluginTaskStatus, error) {
	if h == nil || h.repo == nil {
		return nil, nil
	}
	return h.repo.ListPluginStatuses(ctx, node.PluginStatusNodeTypeCPA)
}

func (h *Handler) pluginTaskRequiredIDs(ctx context.Context) []string {
	cfg, _, errConfig := h.currentConfig(ctx)
	if errConfig != nil || cfg == nil || !cfg.Plugins.Enabled {
		return nil
	}
	ids := make([]string, 0, len(cfg.Plugins.Configs))
	for id, item := range cfg.Plugins.Configs {
		if !pluginInstanceEnabled(item) {
			continue
		}
		manifest, configured := configuredPluginStoreManifest(id, cfg)
		if !configured {
			continue
		}
		pluginID := strings.TrimSpace(manifest.ID)
		if pluginID == "" {
			pluginID = strings.TrimSpace(id)
		}
		if pluginID != "" {
			ids = append(ids, pluginID)
		}
	}
	sort.Strings(ids)
	return ids
}

func pluginReportState(statuses []node.PluginTaskStatus, requiredPluginIDs []string) string {
	if len(requiredPluginIDs) == 0 {
		return "not_required"
	}
	if len(statuses) == 0 {
		return "missing_report"
	}

	plugins := make(map[string]node.PluginTaskPlugin)
	seen := make(map[string]struct{})
	for _, status := range statuses {
		for _, plugin := range status.Plugins {
			id := strings.TrimSpace(plugin.ID)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			plugins[id] = plugin
			seen[id] = struct{}{}
		}
	}

	for _, status := range statuses {
		if len(status.Plugins) == 0 && (!status.OK || !strings.EqualFold(strings.TrimSpace(status.Status), "success")) {
			return "reported_failed"
		}
	}

	for _, id := range requiredPluginIDs {
		plugin, ok := plugins[id]
		if !ok {
			return "reported_partial"
		}
		state := pluginReportPluginState(plugin)
		if state == "reported_failed" {
			return "reported_failed"
		}
		if state != "reported_ok" {
			return "reported_partial"
		}
	}
	return "reported_ok"
}

func pluginReportPluginState(plugin node.PluginTaskPlugin) string {
	if strings.TrimSpace(plugin.Error) != "" {
		return "reported_failed"
	}
	installStatus := strings.ToLower(strings.TrimSpace(plugin.InstallStatus))
	loadStatus := strings.ToLower(strings.TrimSpace(plugin.LoadStatus))
	if installStatus == pluginInstallStatusFailed || loadStatus == pluginLoadStatusFailed {
		return "reported_failed"
	}
	if installStatus != pluginInstallStatusInstalled && installStatus != pluginInstallStatusSkipped {
		return "reported_partial"
	}
	if loadStatus != pluginLoadStatusLoaded {
		return "reported_partial"
	}
	return "reported_ok"
}
