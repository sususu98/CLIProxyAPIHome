package management

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"github.com/router-for-me/CLIProxyAPIHome/internal/node"
)

const (
	topologyHealthHealthy = "healthy"
	topologyHealthStale   = "stale"
	topologyHealthUnknown = "unknown"
	topologyRoleMaster    = "master"
	topologyRoleFollower  = "follower"
	topologyRoleUnknown   = "unknown"

	topologyRetentionHeartbeatMultiplier = 6
)

type topologyHomeStats struct {
	CPA        int
	HealthyCPA int
	StaleCPA   int
	UnknownCPA int
}

// GetTopology returns a Home and CPA cluster topology snapshot.
func (h *Handler) GetTopology(c *gin.Context) {
	if c == nil {
		return
	}
	if h == nil || h.repo == nil {
		respondError(c, http.StatusInternalServerError, "repository_unavailable", nil)
		return
	}
	ctx, cancel := h.requestContext(c)
	defer cancel()

	taskStatuses, errStatuses := h.pluginTaskStatuses(ctx)
	if errStatuses != nil {
		respondError(c, http.StatusInternalServerError, "plugin_status_load_failed", errStatuses)
		return
	}

	now := time.Now().UTC()
	heartbeatTimeout := h.heartbeatTimeout
	if heartbeatTimeout <= 0 {
		heartbeatTimeout = cluster.DefaultHeartbeatTimeout()
	}
	cutoff := now.Add(-heartbeatTimeout)
	retention := topologySnapshotRetention(heartbeatTimeout)
	retentionCutoff := now.Add(-retention)
	homeRecords, errHomes := h.repo.ListClusterNodes(ctx, retentionCutoff)
	if errHomes != nil {
		respondError(c, http.StatusInternalServerError, "home_nodes_load_failed", errHomes)
		return
	}
	cpaRecords, errCPAs := h.repo.ListCPANodeSnapshots(ctx, retentionCutoff)
	if errCPAs != nil {
		respondError(c, http.StatusInternalServerError, "cpa_nodes_load_failed", errCPAs)
		return
	}
	requiredPluginIDs := h.pluginTaskRequiredIDs(ctx)
	statusesByNodeID, statusesByIP := pluginStatusesByNode(taskStatuses)

	homeStats := make(map[string]*topologyHomeStats, len(homeRecords))
	homeHealth := make(map[string]string, len(homeRecords))
	for _, homeRecord := range homeRecords {
		homeID := topologyHomeID(homeRecord.IP, homeRecord.Port)
		homeStats[homeID] = &topologyHomeStats{}
		homeHealth[homeID] = topologyHealth(homeRecord.LastSeenAt, cutoff)
	}

	cpaItems := make([]gin.H, 0, len(cpaRecords))
	healthyCPACount := 0
	staleCPACount := 0
	unknownCPACount := 0
	pluginAttentionCount := 0
	cpaAttentionCount := 0
	for _, cpaRecord := range cpaRecords {
		homeID := topologyHomeID(cpaRecord.HomeIP, cpaRecord.HomePort)
		health := topologyCPAHealth(cpaRecord, homeHealth[homeID], cutoff)
		healthy := health == topologyHealthHealthy
		if healthy {
			healthyCPACount++
		} else if health == topologyHealthStale {
			staleCPACount++
		} else {
			unknownCPACount++
		}
		if stats := homeStats[homeID]; stats != nil {
			stats.CPA++
			switch health {
			case topologyHealthHealthy:
				stats.HealthyCPA++
			case topologyHealthStale:
				stats.StaleCPA++
			default:
				stats.UnknownCPA++
			}
		}

		statuses := statusesByNodeID[strings.TrimSpace(cpaRecord.NodeID)]
		if len(statuses) == 0 {
			statuses = statusesByIP[strings.TrimSpace(cpaRecord.ClientIP)]
		}
		pluginState := pluginReportState(statuses, requiredPluginIDs)
		pluginNeedsAttention := topologyPluginNeedsAttention(pluginState)
		if pluginNeedsAttention {
			pluginAttentionCount++
		}
		if health != topologyHealthHealthy || pluginNeedsAttention {
			cpaAttentionCount++
		}

		cpaItems = append(cpaItems, gin.H{
			"node_id":                cpaRecord.NodeID,
			"ip":                     cpaRecord.ClientIP,
			"connected_time":         cpaRecord.ConnectedAt,
			"last_seen_at":           cpaRecord.LastSeenAt,
			"client_count":           cpaRecord.ClientCount,
			"healthy":                healthy,
			"health":                 health,
			"home_id":                homeID,
			"home_ip":                cpaRecord.HomeIP,
			"home_port":              cpaRecord.HomePort,
			"plugin_report_state":    pluginState,
			"plugin_report_statuses": statuses,
		})
	}

	masterHomeID := topologyCurrentMasterHomeID(homeRecords, cutoff)
	homeItems := make([]gin.H, 0, len(homeRecords))
	healthyHomeCount := 0
	staleHomeCount := 0
	unknownHomeCount := 0
	var masterHome gin.H
	for _, homeRecord := range homeRecords {
		homeID := topologyHomeID(homeRecord.IP, homeRecord.Port)
		health := homeHealth[homeID]
		healthy := health == topologyHealthHealthy
		if healthy {
			healthyHomeCount++
		} else if health == topologyHealthStale {
			staleHomeCount++
		} else {
			unknownHomeCount++
		}
		role := topologyRoleUnknown
		if healthy {
			if homeID == masterHomeID {
				role = topologyRoleMaster
			} else {
				role = topologyRoleFollower
			}
		}
		stats := homeStats[homeID]
		if stats == nil {
			stats = &topologyHomeStats{}
		}
		item := gin.H{
			"id":                homeID,
			"ip":                homeRecord.IP,
			"port":              homeRecord.Port,
			"role":              role,
			"is_master":         role == topologyRoleMaster,
			"reported_master":   homeRecord.IsMaster,
			"health":            health,
			"healthy":           healthy,
			"client_count":      homeRecord.ClientCount,
			"started_at":        homeRecord.StartedAt,
			"last_seen_at":      homeRecord.LastSeenAt,
			"cpa_count":         stats.CPA,
			"healthy_cpa_count": stats.HealthyCPA,
			"stale_cpa_count":   stats.StaleCPA,
			"unknown_cpa_count": stats.UnknownCPA,
		}
		if homeID == masterHomeID {
			masterHome = item
		}
		homeItems = append(homeItems, item)
	}

	missingMaster := masterHomeID == ""
	attentionCount := staleHomeCount + unknownHomeCount + cpaAttentionCount
	if missingMaster {
		attentionCount++
	}

	c.JSON(http.StatusOK, gin.H{
		"summary": gin.H{
			"home_count":              len(homeItems),
			"healthy_home_count":      healthyHomeCount,
			"stale_home_count":        staleHomeCount,
			"unknown_home_count":      unknownHomeCount,
			"cpa_count":               len(cpaItems),
			"healthy_cpa_count":       healthyCPACount,
			"stale_cpa_count":         staleCPACount,
			"unknown_cpa_count":       unknownCPACount,
			"plugin_attention_count":  pluginAttentionCount,
			"attention_count":         attentionCount,
			"missing_master":          missingMaster,
			"stale_after_seconds":     int(heartbeatTimeout.Seconds()),
			"retention_after_seconds": int(retention.Seconds()),
		},
		"management": gin.H{
			"home_id":   topologyHomeID(h.nodeIP, h.nodePort),
			"home_ip":   h.nodeIP,
			"home_port": h.nodePort,
		},
		"master": masterHome,
		"homes":  homeItems,
		"cpas":   cpaItems,
	})
}

func pluginStatusesByNode(statuses []node.PluginTaskStatus) (map[string][]node.PluginTaskStatus, map[string][]node.PluginTaskStatus) {
	statusesByNodeID := make(map[string][]node.PluginTaskStatus)
	statusesByIP := make(map[string][]node.PluginTaskStatus)
	for _, status := range statuses {
		nodeID := strings.TrimSpace(status.NodeID)
		if nodeID != "" {
			statusesByNodeID[nodeID] = append(statusesByNodeID[nodeID], status)
		}
		clientIP := strings.TrimSpace(status.ClientIP)
		if clientIP != "" {
			statusesByIP[clientIP] = append(statusesByIP[clientIP], status)
		}
	}
	return statusesByNodeID, statusesByIP
}

func topologyHomeID(ip string, port int) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || port <= 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", ip, port)
}

func topologyHealth(lastSeenAt time.Time, cutoff time.Time) string {
	if lastSeenAt.IsZero() {
		return topologyHealthUnknown
	}
	if !lastSeenAt.Before(cutoff) {
		return topologyHealthHealthy
	}
	return topologyHealthStale
}

func topologySnapshotRetention(heartbeatTimeout time.Duration) time.Duration {
	if heartbeatTimeout <= 0 {
		heartbeatTimeout = cluster.DefaultHeartbeatTimeout()
	}
	return heartbeatTimeout * topologyRetentionHeartbeatMultiplier
}

func topologyCPAHealth(record cluster.CPANodeRecord, homeHealth string, cutoff time.Time) string {
	if strings.TrimSpace(record.HomeIP) == "" || record.HomePort <= 0 || homeHealth == "" {
		return topologyHealthUnknown
	}
	if homeHealth == topologyHealthUnknown {
		return topologyHealthUnknown
	}
	if homeHealth != topologyHealthHealthy {
		return topologyHealthStale
	}
	if record.ClientCount <= 0 {
		return topologyHealthStale
	}
	return topologyHealth(record.LastSeenAt, cutoff)
}

func topologyCurrentMasterHomeID(records []cluster.ClusterNodeRecord, cutoff time.Time) string {
	live := make([]cluster.ClusterNodeRecord, 0, len(records))
	for _, record := range records {
		if topologyHealth(record.LastSeenAt, cutoff) == topologyHealthHealthy {
			live = append(live, record)
		}
	}
	if len(live) == 0 {
		return ""
	}
	sort.Slice(live, func(i, j int) bool {
		if !live[i].StartedAt.Equal(live[j].StartedAt) {
			return live[i].StartedAt.Before(live[j].StartedAt)
		}
		return topologyHomeID(live[i].IP, live[i].Port) < topologyHomeID(live[j].IP, live[j].Port)
	})
	return topologyHomeID(live[0].IP, live[0].Port)
}

func topologyPluginNeedsAttention(state string) bool {
	switch strings.TrimSpace(state) {
	case "missing_report", "reported_partial", "reported_failed":
		return true
	default:
		return false
	}
}
