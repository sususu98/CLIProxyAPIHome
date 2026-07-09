package cluster

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPIHome/internal/node"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm/clause"
)

const (
	defaultHeartbeatInterval = 5 * time.Second
	defaultHeartbeatTimeout  = 20 * time.Second
)

// DefaultHeartbeatTimeout returns the default cluster heartbeat timeout.
func DefaultHeartbeatTimeout() time.Duration {
	return defaultHeartbeatTimeout
}

type CoordinatorOptions struct {
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
	OnMasterChanged   func(bool)
}

type Coordinator struct {
	repo              *Repository
	node              NodeIdentity
	heartbeatInterval time.Duration
	heartbeatTimeout  time.Duration
	onMasterChanged   func(bool)

	mu          sync.RWMutex
	isMaster    bool
	masterKnown bool
}

type NodeIdentity struct {
	IP        string
	Port      int
	Secret    string
	StartedAt time.Time
}

// NewCoordinator creates a new coordinator.
func NewCoordinator(repo *Repository, node NodeIdentity, opts CoordinatorOptions) *Coordinator {
	// Keep validation before state changes so failures leave existing data intact.
	interval := opts.HeartbeatInterval
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	timeout := opts.HeartbeatTimeout
	if timeout <= 0 {
		timeout = defaultHeartbeatTimeout
	}
	if node.StartedAt.IsZero() {
		node.StartedAt = time.Now().UTC()
	} else {
		node.StartedAt = node.StartedAt.UTC()
	}
	node.IP = strings.TrimSpace(node.IP)
	node.Secret = strings.TrimSpace(node.Secret)
	if node.Secret == "" {
		node.Secret = generateNodeSecret()
	}

	return &Coordinator{
		repo:              repo,
		node:              node,
		heartbeatInterval: interval,
		heartbeatTimeout:  timeout,
		onMasterChanged:   opts.OnMasterChanged,
	}
}

// Start starts the process.
func (c *Coordinator) Start(ctx context.Context) error {
	// Keep validation before state changes so failures leave existing data intact.
	if c == nil {
		return fmt.Errorf("cluster coordinator is nil")
	}
	if c.repo == nil {
		return fmt.Errorf("cluster coordinator repository is nil")
	}
	if strings.TrimSpace(c.node.IP) == "" {
		return fmt.Errorf("cluster node ip is required")
	}
	if c.node.Port <= 0 {
		return fmt.Errorf("cluster node port must be greater than 0")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if errBeat := c.heartbeatAndElect(ctx); errBeat != nil {
		return errBeat
	}

	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.setMaster(false)
			return nil
		case <-ticker.C:
			if errBeat := c.heartbeatAndElect(ctx); errBeat != nil {
				return errBeat
			}
		}
	}
}

// IsMaster reports whether is master.
func (c *Coordinator) IsMaster() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isMaster
}

// NodeSecret handles a node secret.
func (c *Coordinator) NodeSecret() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.node.Secret)
}

// UpdateClientCount stores the current active CPA client count for this node.
func (c *Coordinator) UpdateClientCount(ctx context.Context, clientCount int) error {
	if c == nil {
		return fmt.Errorf("cluster coordinator is nil")
	}
	if c.repo == nil {
		return fmt.Errorf("cluster coordinator repository is nil")
	}
	if clientCount < 0 {
		clientCount = 0
	}
	db, errDB := c.repo.database()
	if errDB != nil {
		return errDB
	}
	if errUpdate := db.WithContext(contextOrBackground(ctx)).
		Model(&ClusterNodeRecord{}).
		Where("ip = ? AND port = ?", c.node.IP, c.node.Port).
		Update("client_count", clientCount).Error; errUpdate != nil {
		return errUpdate
	}
	return c.syncCPANodeSnapshot(ctx, time.Now().UTC())
}

// SetOnMasterChanged sets an on master changed.
func (c *Coordinator) SetOnMasterChanged(callback func(bool)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.onMasterChanged = callback
	c.mu.Unlock()
}

// CurrentMaster returns a current master.
func (c *Coordinator) CurrentMaster(ctx context.Context) (*ClusterNodeRecord, error) {
	// Keep validation before state changes so failures leave existing data intact.
	if c == nil {
		return nil, fmt.Errorf("cluster coordinator is nil")
	}
	if c.repo == nil {
		return nil, fmt.Errorf("cluster coordinator repository is nil")
	}
	db, errDB := c.repo.database()
	if errDB != nil {
		return nil, errDB
	}

	cutoff := time.Now().UTC().Add(-c.heartbeatTimeout)
	var liveNodes []ClusterNodeRecord
	errFind := db.WithContext(contextOrBackground(ctx)).
		Where("last_seen_at >= ?", cutoff).
		Find(&liveNodes).Error
	if errFind != nil {
		return nil, errFind
	}
	if len(liveNodes) == 0 {
		return nil, nil
	}
	sortClusterNodes(liveNodes)
	return &liveNodes[0], nil
}

// heartbeatAndElect handles a heartbeat and elect.
func (c *Coordinator) heartbeatAndElect(ctx context.Context) error {
	// Keep validation before state changes so failures leave existing data intact.
	db, errDB := c.repo.database()
	if errDB != nil {
		return errDB
	}

	now := time.Now().UTC()
	record := ClusterNodeRecord{
		IP:          c.node.IP,
		Port:        c.node.Port,
		SecretHash:  nodeSecretHash(c.node.Secret),
		IsMaster:    c.IsMaster(),
		ClientCount: node.GlobalRegistry().TotalCount(),
		StartedAt:   c.node.StartedAt,
		LastSeenAt:  now,
	}

	if errUpsert := db.WithContext(contextOrBackground(ctx)).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "ip"}, {Name: "port"}},
		DoUpdates: clause.Assignments(map[string]any{
			"started_at":   record.StartedAt,
			"last_seen_at": record.LastSeenAt,
			"secret_hash":  record.SecretHash,
			"client_count": record.ClientCount,
			"is_master":    record.IsMaster,
		}),
	}).Create(&record).Error; errUpsert != nil {
		return errUpsert
	}
	if errSnapshot := c.syncCPANodeSnapshot(ctx, now); errSnapshot != nil {
		log.Warnf("failed to sync CPA node snapshot: %v", errSnapshot)
	}

	elected, errMaster := c.CurrentMaster(ctx)
	if errMaster != nil {
		return errMaster
	}
	if elected == nil {
		return fmt.Errorf("cluster election found no live nodes")
	}

	c.setMaster(clusterNodeMatches(*elected, c.node.IP, c.node.Port))
	return nil
}

func (c *Coordinator) syncCPANodeSnapshot(ctx context.Context, seenAt time.Time) error {
	if c == nil || c.repo == nil {
		return nil
	}
	return c.repo.ReplaceCPANodeSnapshot(ctx, c.node.IP, c.node.Port, node.GlobalRegistry().List(), seenAt)
}

// setMaster sets a master.
func (c *Coordinator) setMaster(next bool) {
	c.mu.Lock()
	changed := !c.masterKnown || c.isMaster != next
	c.isMaster = next
	c.masterKnown = true
	callback := c.onMasterChanged
	c.mu.Unlock()

	if changed && callback != nil {
		callback(next)
	}
}

// sortClusterNodes sorts a cluster nodes.
func sortClusterNodes(nodes []ClusterNodeRecord) {
	sort.Slice(nodes, func(i, j int) bool {
		if !nodes[i].StartedAt.Equal(nodes[j].StartedAt) {
			return nodes[i].StartedAt.Before(nodes[j].StartedAt)
		}
		return nodeSortKey(nodes[i]) < nodeSortKey(nodes[j])
	})
}

// nodeSortKey handles a node sort key.
func nodeSortKey(node ClusterNodeRecord) string {
	return fmt.Sprintf("%s:%d", strings.TrimSpace(node.IP), node.Port)
}

func clusterNodeMatches(node ClusterNodeRecord, ip string, port int) bool {
	return strings.TrimSpace(node.IP) == strings.TrimSpace(ip) && node.Port == port
}

// generateNodeSecret generates a node secret.
func generateNodeSecret() string {
	token := make([]byte, 32)
	if _, errRead := cryptorand.Read(token); errRead == nil {
		return hex.EncodeToString(token)
	}
	fallback := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return hex.EncodeToString(fallback[:])
}

// nodeSecretHash handles a node secret hash.
func nodeSecretHash(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
