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

// Value converts the value for database storage.
func (j JSONB) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	if !json.Valid(j) {
		return nil, fmt.Errorf("invalid jsonb value")
	}
	return string(j), nil
}

// Scan loads the value from database storage.
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

// MarshalJSON encodes a json.
func (j JSONB) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON decodes a json.
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
	ID               string          `gorm:"column:id;index:idx_auth_active_order,priority:2"`
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
	DeletedAt        gorm.DeletedAt  `gorm:"column:deleted_at;index;index:idx_auth_active_order,priority:1"`
}

// TableName returns the database table name.
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

// TableName returns the database table name.
func (ConfigRecord) TableName() string {
	return "config"
}

type APIKeyRecord struct {
	ID        uint           `gorm:"column:id;primaryKey;autoIncrement"`
	APIKey    string         `gorm:"column:api_key;not null;uniqueIndex"`
	CreatedAt time.Time      `gorm:"column:created_at"`
	UpdatedAt time.Time      `gorm:"column:updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"column:deleted_at;index"`
}

// TableName returns the database table name.
func (APIKeyRecord) TableName() string {
	return "api_key"
}

type ClusterNodeRecord struct {
	IP          string    `gorm:"column:ip;primaryKey;index:idx_cluster_auth_lookup,priority:1;index:idx_cluster_live_nodes,priority:3;index:idx_cluster_master_nodes,priority:4"`
	Port        int       `gorm:"column:port;primaryKey;index:idx_cluster_auth_lookup,priority:5;index:idx_cluster_live_nodes,priority:4;index:idx_cluster_master_nodes,priority:5"`
	SecretHash  string    `gorm:"column:secret_hash;index:idx_cluster_auth_lookup,priority:2"`
	IsMaster    bool      `gorm:"column:is_master;index:idx_cluster_master_nodes,priority:1"`
	ClientCount int       `gorm:"column:client_count"`
	StartedAt   time.Time `gorm:"column:started_at;index:idx_cluster_auth_lookup,priority:4;index:idx_cluster_live_nodes,priority:2;index:idx_cluster_master_nodes,priority:3"`
	LastSeenAt  time.Time `gorm:"column:last_seen_at;index:idx_cluster_auth_lookup,priority:3;index:idx_cluster_live_nodes,priority:1;index:idx_cluster_master_nodes,priority:2"`
}

// TableName returns the database table name.
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

// TableName returns the database table name.
func (ClusterEventRecord) TableName() string {
	return "cluster_events"
}

type OAuthSessionRecord struct {
	State       string     `gorm:"column:state;primaryKey"`
	Provider    string     `gorm:"column:provider;index"`
	Status      string     `gorm:"column:status"`
	Error       string     `gorm:"column:error"`
	Data        JSONB      `gorm:"column:data;type:jsonb"`
	CreatedAt   time.Time  `gorm:"column:created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at"`
	ExpiresAt   time.Time  `gorm:"column:expires_at;index"`
	CompletedAt *time.Time `gorm:"column:completed_at"`
}

// TableName returns the database table name.
func (OAuthSessionRecord) TableName() string {
	return "oauth_sessions"
}

type CertificateRecord struct {
	ID                   string    `gorm:"column:id;primaryKey"`
	ClusterID            string    `gorm:"column:cluster_id;index"`
	CertificatePEM       string    `gorm:"column:certificate_pem;type:text"`
	PrivateKeyPEM        string    `gorm:"column:private_key_pem;type:text"`
	CSRPEM               string    `gorm:"column:csr_pem;type:text"`
	IP                   string    `gorm:"column:ip;index:idx_certificate_server_ip,priority:2"`
	CAFingerprint        string    `gorm:"column:ca_fingerprint"`
	EnrollmentSecretHash string    `gorm:"column:enrollment_secret_hash"`
	IsCA                 bool      `gorm:"column:is_ca;index"`
	IsServer             bool      `gorm:"column:is_server;index:idx_certificate_server_ip,priority:1"`
	IsClient             bool      `gorm:"column:is_client;index"`
	SerialNumber         string    `gorm:"column:serial_number"`
	NotBefore            time.Time `gorm:"column:not_before"`
	NotAfter             time.Time `gorm:"column:not_after"`
	CreatedAt            time.Time `gorm:"column:created_at"`
	UpdatedAt            time.Time `gorm:"column:updated_at"`
}

// TableName returns the database table name.
func (CertificateRecord) TableName() string {
	return "certificate"
}

type AuthIndex struct {
	UUID          string
	ID            string
	Index         string
	Provider      string
	Label         string
	Prefix        string
	Status        coreauth.Status
	Disabled      bool
	Unavailable   bool
	BaseURL       string
	ModelsHash    string
	Attributes    map[string]string
	ModelMetadata map[string]any
}
