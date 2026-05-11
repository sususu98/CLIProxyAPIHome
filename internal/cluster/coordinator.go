package cluster

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultHeartbeatInterval = 5 * time.Second
	defaultHeartbeatTimeout  = 20 * time.Second
)

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

func NewCoordinator(repo *Repository, node NodeIdentity, opts CoordinatorOptions) *Coordinator {
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

func (c *Coordinator) Start(ctx context.Context) error {
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

func (c *Coordinator) IsMaster() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.isMaster
}

func (c *Coordinator) NodeSecret() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.node.Secret)
}

func (c *Coordinator) SetOnMasterChanged(callback func(bool)) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.onMasterChanged = callback
	c.mu.Unlock()
}

func (c *Coordinator) CurrentMaster(ctx context.Context) (*ClusterNodeRecord, error) {
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
	record := &ClusterNodeRecord{}
	errFirst := db.WithContext(contextOrBackground(ctx)).
		Where("is_master = ? AND last_seen_at >= ?", true, cutoff).
		Order("started_at ASC, ip ASC, port ASC").
		First(record).Error
	if errFirst != nil {
		if !errors.Is(errFirst, gorm.ErrRecordNotFound) {
			return nil, errFirst
		}
	}
	if record.IP != "" {
		return record, nil
	}

	var liveNodes []ClusterNodeRecord
	errFind := db.WithContext(contextOrBackground(ctx)).
		Where("last_seen_at >= ?", cutoff).
		Order("started_at ASC, ip ASC, port ASC").
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

func (c *Coordinator) heartbeatAndElect(ctx context.Context) error {
	db, errDB := c.repo.database()
	if errDB != nil {
		return errDB
	}

	now := time.Now().UTC()
	cutoff := now.Add(-c.heartbeatTimeout)
	record := ClusterNodeRecord{
		IP:         c.node.IP,
		Port:       c.node.Port,
		SecretHash: nodeSecretHash(c.node.Secret),
		StartedAt:  c.node.StartedAt,
		LastSeenAt: now,
	}

	var elected *ClusterNodeRecord
	errTransaction := db.WithContext(contextOrBackground(ctx)).Transaction(func(tx *gorm.DB) error {
		if errLock := tx.Exec(`LOCK TABLE "cluster" IN SHARE ROW EXCLUSIVE MODE`).Error; errLock != nil {
			return errLock
		}
		if errUpsert := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "ip"}, {Name: "port"}},
			DoUpdates: clause.Assignments(map[string]any{
				"started_at":   record.StartedAt,
				"last_seen_at": record.LastSeenAt,
				"secret_hash":  record.SecretHash,
			}),
		}).Create(&record).Error; errUpsert != nil {
			return errUpsert
		}

		var nodes []ClusterNodeRecord
		errFind := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("last_seen_at >= ?", cutoff).
			Find(&nodes).Error
		if errFind != nil {
			return errFind
		}
		if len(nodes) == 0 {
			return fmt.Errorf("cluster election found no live nodes")
		}

		sortClusterNodes(nodes)
		nextMaster := nodes[0]
		elected = &nextMaster

		if errClear := tx.Model(&ClusterNodeRecord{}).Where("is_master = ?", true).Update("is_master", false).Error; errClear != nil {
			return errClear
		}
		if errSet := tx.Model(&ClusterNodeRecord{}).
			Where("ip = ? AND port = ?", nextMaster.IP, nextMaster.Port).
			Update("is_master", true).Error; errSet != nil {
			return errSet
		}
		return nil
	})
	if errTransaction != nil {
		return errTransaction
	}

	c.setMaster(elected != nil && elected.IP == c.node.IP && elected.Port == c.node.Port)
	return nil
}

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

func sortClusterNodes(nodes []ClusterNodeRecord) {
	sort.Slice(nodes, func(i, j int) bool {
		if !nodes[i].StartedAt.Equal(nodes[j].StartedAt) {
			return nodes[i].StartedAt.Before(nodes[j].StartedAt)
		}
		return nodeSortKey(nodes[i]) < nodeSortKey(nodes[j])
	})
}

func nodeSortKey(node ClusterNodeRecord) string {
	return fmt.Sprintf("%s:%d", strings.TrimSpace(node.IP), node.Port)
}

func generateNodeSecret() string {
	token := make([]byte, 32)
	if _, errRead := cryptorand.Read(token); errRead == nil {
		return hex.EncodeToString(token)
	}
	fallback := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return hex.EncodeToString(fallback[:])
}

func nodeSecretHash(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
