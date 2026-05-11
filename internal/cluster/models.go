package cluster

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"gorm.io/gorm"
)

type JSONB []byte

func (j JSONB) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	if !json.Valid(j) {
		return nil, fmt.Errorf("invalid jsonb value")
	}
	return string(j), nil
}

func (j *JSONB) Scan(value any) error {
	if j == nil {
		return fmt.Errorf("jsonb scan target is nil")
	}
	switch data := value.(type) {
	case nil:
		*j = nil
		return nil
	case []byte:
		*j = append((*j)[:0], data...)
		return nil
	case string:
		*j = append((*j)[:0], data...)
		return nil
	default:
		return fmt.Errorf("unsupported jsonb scan type %T", value)
	}
}

func (j JSONB) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

func (j *JSONB) UnmarshalJSON(data []byte) error {
	if j == nil {
		return fmt.Errorf("jsonb unmarshal target is nil")
	}
	if len(data) == 0 {
		*j = nil
		return nil
	}
	if !json.Valid(data) {
		return fmt.Errorf("invalid jsonb value")
	}
	*j = append((*j)[:0], data...)
	return nil
}

type AuthRecord struct {
	UUID             string          `gorm:"column:uuid;primaryKey;type:uuid"`
	AuthJSON         JSONB           `gorm:"column:auth_json;type:jsonb;not null"`
	Version          int64           `gorm:"column:version;not null;default:1"`
	ID               string          `gorm:"column:id"`
	Index            string          `gorm:"column:index"`
	Provider         string          `gorm:"column:provider"`
	Label            string          `gorm:"column:label"`
	Prefix           string          `gorm:"column:prefix"`
	Status           coreauth.Status `gorm:"column:status"`
	Disabled         bool            `gorm:"column:disabled"`
	Unavailable      bool            `gorm:"column:unavailable"`
	BaseURL          string          `gorm:"column:base_url"`
	APIKeyHash       string          `gorm:"column:api_key_hash"`
	CompatName       string          `gorm:"column:compat_name"`
	ProviderKey      string          `gorm:"column:provider_key"`
	ModelsHash       string          `gorm:"column:models_hash"`
	CreatedAt        time.Time       `gorm:"column:created_at"`
	UpdatedAt        time.Time       `gorm:"column:updated_at"`
	LastRefreshedAt  *time.Time      `gorm:"column:last_refreshed_at"`
	NextRefreshAfter *time.Time      `gorm:"column:next_refresh_after"`
	DeletedAt        gorm.DeletedAt  `gorm:"column:deleted_at;index"`
}

func (AuthRecord) TableName() string {
	return "auth"
}

type ConfigRecord struct {
	Key       string    `gorm:"column:key;primaryKey"`
	Value     JSONB     `gorm:"column:value;type:jsonb;not null"`
	Version   int64     `gorm:"column:version;not null;default:1"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (ConfigRecord) TableName() string {
	return "config"
}

type ClusterNodeRecord struct {
	IP         string    `gorm:"column:ip;primaryKey"`
	Port       int       `gorm:"column:port;primaryKey"`
	SecretHash string    `gorm:"column:secret_hash"`
	IsMaster   bool      `gorm:"column:is_master"`
	StartedAt  time.Time `gorm:"column:started_at"`
	LastSeenAt time.Time `gorm:"column:last_seen_at"`
}

func (ClusterNodeRecord) TableName() string {
	return "cluster"
}

type ClusterEventRecord struct {
	ID         uint      `gorm:"column:id;primaryKey;autoIncrement"`
	Scope      string    `gorm:"column:scope"`
	Op         string    `gorm:"column:op"`
	EntityUUID string    `gorm:"column:entity_uuid"`
	Version    int64     `gorm:"column:version"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (ClusterEventRecord) TableName() string {
	return "cluster_events"
}

type AuthIndex struct {
	UUID        string
	ID          string
	Index       string
	Provider    string
	Label       string
	Prefix      string
	Status      coreauth.Status
	Disabled    bool
	Unavailable bool
	BaseURL     string
	ModelsHash  string
	Attributes  map[string]string
}
