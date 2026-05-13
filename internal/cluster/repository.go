package cluster

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPIHome/internal/runtime/geminicli"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

// NewRepository creates a new repository.
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// AuthToRecord converts auth to record.
func AuthToRecord(auth *coreauth.Auth) (*AuthRecord, error) {
	// Normalize auth state before updating runtime indexes.
	if auth == nil {
		return nil, fmt.Errorf("auth is nil")
	}
	if strings.TrimSpace(auth.ID) == "" {
		return nil, fmt.Errorf("auth id is required")
	}
	if strings.TrimSpace(auth.Index) == "" {
		return nil, fmt.Errorf("auth index is required")
	}
	if auth.ID != auth.Index {
		return nil, fmt.Errorf("auth id %q and index %q must match", auth.ID, auth.Index)
	}

	rawJSON, errMarshal := json.Marshal(auth)
	if errMarshal != nil {
		return nil, errMarshal
	}

	record := &AuthRecord{
		UUID:        auth.ID,
		AuthJSON:    JSONB(rawJSON),
		Version:     1,
		ID:          auth.ID,
		Index:       auth.Index,
		Provider:    auth.Provider,
		Label:       auth.Label,
		Prefix:      auth.Prefix,
		Status:      auth.Status,
		Disabled:    auth.Disabled,
		Unavailable: auth.Unavailable,
		CreatedAt:   auth.CreatedAt,
		UpdatedAt:   auth.UpdatedAt,
	}

	if auth.Attributes != nil {
		record.BaseURL = strings.TrimSpace(auth.Attributes["base_url"])
		record.APIKeyHash = hashValue(auth.Attributes["api_key"])
		record.CompatName = strings.TrimSpace(auth.Attributes["compat_name"])
		record.ProviderKey = strings.TrimSpace(auth.Attributes["provider_key"])
		record.ModelsHash = strings.TrimSpace(auth.Attributes["models_hash"])
	}
	record.LastRefreshedAt = timePtrOrNil(auth.LastRefreshedAt)
	record.NextRefreshAfter = timePtrOrNil(auth.NextRefreshAfter)

	return record, nil
}

// RecordToAuth records a to auth.
func RecordToAuth(record *AuthRecord) (*coreauth.Auth, error) {
	// Resolve credential context before calling upstream OAuth services.
	if record == nil {
		return nil, fmt.Errorf("auth record is nil")
	}
	if strings.TrimSpace(record.UUID) == "" {
		return nil, fmt.Errorf("auth record uuid is required")
	}
	if record.ID != record.UUID {
		return nil, fmt.Errorf("auth record id %q must match uuid %q", record.ID, record.UUID)
	}
	if record.Index != record.UUID {
		return nil, fmt.Errorf("auth record index %q must match uuid %q", record.Index, record.UUID)
	}

	auth := &coreauth.Auth{}
	if errUnmarshal := json.Unmarshal([]byte(record.AuthJSON), auth); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if auth.ID != record.UUID {
		return nil, fmt.Errorf("auth json id %q must match uuid %q", auth.ID, record.UUID)
	}
	auth.Index = record.Index

	return auth, nil
}

// UpsertAuth inserts or updates an auth.
func (r *Repository) UpsertAuth(ctx context.Context, auth *coreauth.Auth, op string) (*AuthRecord, error) {
	// Normalize auth state before updating runtime indexes.
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}
	record, errRecord := AuthToRecord(auth)
	if errRecord != nil {
		return nil, errRecord
	}
	if strings.TrimSpace(op) == "" {
		op = "upsert"
	}

	ctx = contextOrBackground(ctx)
	errTransaction := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing := AuthRecord{}
		errFirst := tx.Unscoped().Where("uuid = ?", record.UUID).First(&existing).Error
		switch {
		case errors.Is(errFirst, gorm.ErrRecordNotFound):
			record.Version = 1
			if errCreate := tx.Create(record).Error; errCreate != nil {
				return errCreate
			}
		case errFirst != nil:
			return errFirst
		default:
			record.Version = existing.Version + 1
			record.DeletedAt = gorm.DeletedAt{}
			if errUpdate := tx.Unscoped().Select("*").Where("uuid = ?", record.UUID).Updates(record).Error; errUpdate != nil {
				return errUpdate
			}
		}
		return appendEvent(tx, "auth", op, record.UUID, record.Version)
	})
	if errTransaction != nil {
		return nil, errTransaction
	}

	return record, nil
}

// GetAuth returns an auth.
func (r *Repository) GetAuth(ctx context.Context, uuid string) (*coreauth.Auth, *AuthRecord, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, nil, errDB
	}

	record := &AuthRecord{}
	errFirst := db.WithContext(contextOrBackground(ctx)).Where("uuid = ?", uuid).First(record).Error
	if errFirst != nil {
		return nil, nil, errFirst
	}

	auth, errAuth := RecordToAuth(record)
	if errAuth != nil {
		return nil, nil, errAuth
	}
	if errHydrate := r.hydrateAuthRuntime(ctx, db, auth); errHydrate != nil {
		return nil, nil, errHydrate
	}
	return auth, record, nil
}

// WithAuthRefreshLock applies the auth refresh lock option.
func (r *Repository) WithAuthRefreshLock(ctx context.Context, uuid string, fn func(tx *Repository, auth *coreauth.Auth) (*coreauth.Auth, error)) (*coreauth.Auth, error) {
	// Resolve credential context before calling upstream OAuth services.
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return nil, fmt.Errorf("cluster auth uuid is required")
	}
	if fn == nil {
		return nil, fmt.Errorf("cluster auth refresh lock callback is nil")
	}

	var out *coreauth.Auth
	ctx = contextOrBackground(ctx)
	errTransaction := db.WithContext(ctx).Transaction(func(txDB *gorm.DB) error {
		record := &AuthRecord{}
		errFirst := txDB.Clauses(clause.Locking{Strength: "UPDATE"}).Where("uuid = ?", uuid).First(record).Error
		if errFirst != nil {
			return errFirst
		}

		auth, errAuth := RecordToAuth(record)
		if errAuth != nil {
			return errAuth
		}
		auth.ID = uuid
		auth.Index = uuid
		if errHydrate := r.hydrateAuthRuntime(ctx, txDB, auth); errHydrate != nil {
			return errHydrate
		}

		refreshed, errRefresh := fn(&Repository{db: txDB}, auth)
		if errRefresh != nil {
			return errRefresh
		}
		out = refreshed
		return nil
	})
	if errTransaction != nil {
		return nil, errTransaction
	}
	return out, nil
}

// ListAuthIndex returns an auth index.
func (r *Repository) ListAuthIndex(ctx context.Context) ([]AuthIndex, error) {
	// Normalize auth state before updating runtime indexes.
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}

	var records []AuthRecord
	if errFind := db.WithContext(contextOrBackground(ctx)).Order("id").Find(&records).Error; errFind != nil {
		return nil, errFind
	}

	out := make([]AuthIndex, 0, len(records))
	for _, record := range records {
		auth, errAuth := RecordToAuth(&record)
		if errAuth != nil {
			return nil, errAuth
		}
		out = append(out, AuthIndex{
			UUID:        record.UUID,
			ID:          record.ID,
			Index:       record.Index,
			Provider:    record.Provider,
			Label:       record.Label,
			Prefix:      record.Prefix,
			Status:      record.Status,
			Disabled:    record.Disabled,
			Unavailable: record.Unavailable,
			BaseURL:     record.BaseURL,
			ModelsHash:  record.ModelsHash,
			Attributes:  auth.Attributes,
		})
	}
	return out, nil
}

// LiveClusterNodeByIPAndSecret handles a live cluster node by ip and secret.
func (r *Repository) LiveClusterNodeByIPAndSecret(ctx context.Context, ip string, secret string, cutoff time.Time) (*ClusterNodeRecord, error) {
	// Keep validation before state changes so failures leave existing data intact.
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil, nil
	}
	secretHash := nodeSecretHash(secret)
	if secretHash == "" {
		return nil, nil
	}
	if cutoff.IsZero() {
		cutoff = time.Now().UTC().Add(-defaultHeartbeatTimeout)
	}

	record := &ClusterNodeRecord{}
	errFirst := db.WithContext(contextOrBackground(ctx)).
		Where("ip = ? AND secret_hash = ? AND port > ? AND last_seen_at >= ?", ip, secretHash, 0, cutoff).
		Order("started_at ASC, port ASC").
		First(record).Error
	if errFirst != nil {
		if errors.Is(errFirst, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, errFirst
	}
	return record, nil
}

// ListLiveClusterNodes returns live cluster nodes.
func (r *Repository) ListLiveClusterNodes(ctx context.Context, cutoff time.Time) ([]ClusterNodeRecord, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}
	if cutoff.IsZero() {
		cutoff = time.Now().UTC().Add(-defaultHeartbeatTimeout)
	}

	var records []ClusterNodeRecord
	errFind := db.WithContext(contextOrBackground(ctx)).
		Where("port > ? AND last_seen_at >= ?", 0, cutoff).
		Order("client_count ASC, started_at ASC, ip ASC, port ASC").
		Find(&records).Error
	if errFind != nil {
		return nil, errFind
	}
	return records, nil
}

// ListAuths returns an auths.
func (r *Repository) ListAuths(ctx context.Context) ([]*coreauth.Auth, error) {
	// Normalize auth state before updating runtime indexes.
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}

	var records []AuthRecord
	if errFind := db.WithContext(contextOrBackground(ctx)).Order("id").Find(&records).Error; errFind != nil {
		return nil, errFind
	}

	auths := make([]*coreauth.Auth, 0, len(records))
	for _, record := range records {
		auth, errAuth := RecordToAuth(&record)
		if errAuth != nil {
			return nil, errAuth
		}
		auth.ID = record.UUID
		auth.Index = record.UUID
		auths = append(auths, auth)
	}
	hydrateAuthListRuntimes(auths)
	return auths, nil
}

// SoftDeleteAuth handles a soft delete auth.
func (r *Repository) SoftDeleteAuth(ctx context.Context, uuid string) error {
	// Validate request inputs before mutating persisted state.
	db, errDB := r.database()
	if errDB != nil {
		return errDB
	}

	ctx = contextOrBackground(ctx)
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record := AuthRecord{}
		if errFirst := tx.Where("uuid = ?", uuid).First(&record).Error; errFirst != nil {
			return errFirst
		}
		newVersion := record.Version + 1
		if errUpdate := tx.Model(&record).Update("version", newVersion).Error; errUpdate != nil {
			return errUpdate
		}
		record.Version = newVersion
		if errDelete := tx.Delete(&record).Error; errDelete != nil {
			return errDelete
		}
		return appendEvent(tx, "auth", "delete", record.UUID, record.Version)
	})
}

const configAPIKeysRootKey = "api-keys"

// normalizeAPIKeys normalizes API keys sourced from config.yaml.
func normalizeAPIKeys(keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		if _, exists := seen[trimmedKey]; exists {
			continue
		}
		seen[trimmedKey] = struct{}{}
		normalized = append(normalized, trimmedKey)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// normalizeAPIKeysFromAny converts a config.yaml api-keys value into a normalized string slice.
func normalizeAPIKeysFromAny(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return normalizeAPIKeys([]string{typed})
	case []string:
		return normalizeAPIKeys(typed)
	case []any:
		keys := make([]string, 0, len(typed))
		for _, item := range typed {
			str, ok := item.(string)
			if !ok {
				continue
			}
			keys = append(keys, str)
		}
		return normalizeAPIKeys(keys)
	default:
		return nil
	}
}

// replaceAPIKeysTx replaces the active API keys in the api_key table using soft deletes.
func replaceAPIKeysTx(ctx context.Context, tx *gorm.DB, keys []string) error {
	// Normalize source data before building the derived payload.
	if tx == nil {
		return fmt.Errorf("database connection is nil")
	}
	keys = normalizeAPIKeys(keys)

	var existing []APIKeyRecord
	if errFind := tx.WithContext(contextOrBackground(ctx)).Unscoped().Order("id").Find(&existing).Error; errFind != nil {
		return errFind
	}

	existingByKey := make(map[string]*APIKeyRecord, len(existing))
	for i := range existing {
		record := &existing[i]
		key := strings.TrimSpace(record.APIKey)
		if key == "" {
			continue
		}
		existingByKey[key] = record
	}

	keep := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		keep[key] = struct{}{}
		if record := existingByKey[key]; record != nil {
			if record.DeletedAt.Valid {
				if errRestore := tx.WithContext(contextOrBackground(ctx)).Unscoped().
					Model(&APIKeyRecord{}).
					Where("id = ?", record.ID).
					Updates(map[string]any{"deleted_at": nil}).Error; errRestore != nil {
					return errRestore
				}
			}
			continue
		}
		if errCreate := tx.WithContext(contextOrBackground(ctx)).Create(&APIKeyRecord{APIKey: key}).Error; errCreate != nil {
			return errCreate
		}
	}

	for _, record := range existing {
		key := strings.TrimSpace(record.APIKey)
		if key == "" {
			continue
		}
		if _, ok := keep[key]; ok {
			continue
		}
		if record.DeletedAt.Valid {
			continue
		}
		if errDelete := tx.WithContext(contextOrBackground(ctx)).Delete(&APIKeyRecord{}, "id = ?", record.ID).Error; errDelete != nil {
			return errDelete
		}
	}
	return nil
}

// UpsertConfigValue inserts or updates a config value.
func (r *Repository) UpsertConfigValue(ctx context.Context, key string, value any) error {
	// Normalize source data before building the derived payload.
	db, errDB := r.database()
	if errDB != nil {
		return errDB
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("config key is required")
	}

	if key == configAPIKeysRootKey {
		ctx = contextOrBackground(ctx)
		apiKeys := normalizeAPIKeysFromAny(value)
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if errReplace := replaceAPIKeysTx(ctx, tx, apiKeys); errReplace != nil {
				return errReplace
			}
			if errDelete := tx.Delete(&ConfigRecord{}, "key = ?", configAPIKeysRootKey).Error; errDelete != nil {
				return errDelete
			}
			return appendEvent(tx, "config", "upsert", configAPIKeysRootKey, time.Now().UTC().UnixNano())
		})
	}

	rawJSON, errMarshal := json.Marshal(value)
	if errMarshal != nil {
		return errMarshal
	}

	ctx = contextOrBackground(ctx)
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		record := ConfigRecord{}
		errFirst := tx.Where("key = ?", key).First(&record).Error
		switch {
		case errors.Is(errFirst, gorm.ErrRecordNotFound):
			record = ConfigRecord{Key: key, Value: JSONB(rawJSON), Version: 1}
			if errCreate := tx.Create(&record).Error; errCreate != nil {
				return errCreate
			}
		case errFirst != nil:
			return errFirst
		default:
			record.Value = JSONB(rawJSON)
			record.Version++
			if errSave := tx.Save(&record).Error; errSave != nil {
				return errSave
			}
		}
		return appendEvent(tx, "config", "upsert", key, record.Version)
	})
}

// ReplaceConfigSnapshot handles a replace config snapshot.
func (r *Repository) ReplaceConfigSnapshot(ctx context.Context, values map[string]any) error {
	// Normalize source data before building the derived payload.
	db, errDB := r.database()
	if errDB != nil {
		return errDB
	}

	apiKeys := normalizeAPIKeysFromAny(values[configAPIKeysRootKey])
	clean := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if key == configAPIKeysRootKey {
			continue
		}
		rawJSON, errMarshal := json.Marshal(value)
		if errMarshal != nil {
			return errMarshal
		}
		clean[key] = rawJSON
	}

	ctx = contextOrBackground(ctx)
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if errReplaceAPIKeys := replaceAPIKeysTx(ctx, tx, apiKeys); errReplaceAPIKeys != nil {
			return errReplaceAPIKeys
		}

		var existing []ConfigRecord
		if errFind := tx.Find(&existing).Error; errFind != nil {
			return errFind
		}

		seen := make(map[string]struct{}, len(clean))
		eventVersion := int64(0)
		for key, rawJSON := range clean {
			seen[key] = struct{}{}
			record := ConfigRecord{}
			errFirst := tx.Where("key = ?", key).First(&record).Error
			switch {
			case errors.Is(errFirst, gorm.ErrRecordNotFound):
				record = ConfigRecord{Key: key, Value: JSONB(rawJSON), Version: 1}
				if errCreate := tx.Create(&record).Error; errCreate != nil {
					return errCreate
				}
			case errFirst != nil:
				return errFirst
			default:
				record.Value = JSONB(rawJSON)
				record.Version++
				if errSave := tx.Save(&record).Error; errSave != nil {
					return errSave
				}
			}
			if record.Version > eventVersion {
				eventVersion = record.Version
			}
		}

		for _, record := range existing {
			if _, ok := seen[record.Key]; ok {
				continue
			}
			nextVersion := record.Version + 1
			if errDelete := tx.Delete(&ConfigRecord{}, "key = ?", record.Key).Error; errDelete != nil {
				return errDelete
			}
			if nextVersion > eventVersion {
				eventVersion = nextVersion
			}
		}
		if eventVersion == 0 {
			eventVersion = 1
		}
		return appendEvent(tx, "config", "replace", "config", eventVersion)
	})
}

// LoadConfigSnapshot loads a config snapshot.
func (r *Repository) LoadConfigSnapshot(ctx context.Context) (map[string]json.RawMessage, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}

	var records []ConfigRecord
	if errFind := db.WithContext(contextOrBackground(ctx)).Find(&records).Error; errFind != nil {
		return nil, errFind
	}

	out := make(map[string]json.RawMessage, len(records)+1)
	for _, record := range records {
		if strings.TrimSpace(record.Key) == configAPIKeysRootKey {
			continue
		}
		out[record.Key] = json.RawMessage(record.Value)
	}

	var apiKeyRecords []APIKeyRecord
	if errFind := db.WithContext(contextOrBackground(ctx)).Order("id").Find(&apiKeyRecords).Error; errFind != nil {
		return nil, errFind
	}
	if len(apiKeyRecords) > 0 {
		keys := make([]string, 0, len(apiKeyRecords))
		for _, record := range apiKeyRecords {
			key := strings.TrimSpace(record.APIKey)
			if key == "" {
				continue
			}
			keys = append(keys, key)
		}
		keys = normalizeAPIKeys(keys)
		if len(keys) > 0 {
			rawJSON, errMarshal := json.Marshal(keys)
			if errMarshal != nil {
				return nil, errMarshal
			}
			out[configAPIKeysRootKey] = rawJSON
		}
	}
	return out, nil
}

// AppendEvent appends an event.
func (r *Repository) AppendEvent(ctx context.Context, scope, op, entity string, version int64) error {
	db, errDB := r.database()
	if errDB != nil {
		return errDB
	}
	return appendEvent(db.WithContext(contextOrBackground(ctx)), scope, op, entity, version)
}

// MaxEventID handles a max event id.
func (r *Repository) MaxEventID(ctx context.Context) (int64, error) {
	db, errDB := r.database()
	if errDB != nil {
		return 0, errDB
	}
	var maxID int64
	if errScan := db.WithContext(contextOrBackground(ctx)).Model(&ClusterEventRecord{}).Select("COALESCE(MAX(id), 0)").Scan(&maxID).Error; errScan != nil {
		return 0, errScan
	}
	return maxID, nil
}

// database handles a database.
func (r *Repository) database() (*gorm.DB, error) {
	if r == nil {
		return nil, fmt.Errorf("cluster repository is nil")
	}
	if r.db == nil {
		return nil, fmt.Errorf("cluster repository database is nil")
	}
	return r.db, nil
}

// appendEvent appends an event.
func appendEvent(db *gorm.DB, scope, op, entity string, version int64) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}
	event := ClusterEventRecord{
		Scope:      scope,
		Op:         op,
		EntityUUID: entity,
		Version:    version,
		CreatedAt:  time.Now().UTC(),
	}
	return db.Create(&event).Error
}

// contextOrBackground handles a context or background.
func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// timePtrOrNil handles a time ptr or nil.
func timePtrOrNil(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	utcValue := value.UTC()
	return &utcValue
}

// hashValue reports whether h value is present.
func hashValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// hydrateAuthRuntime handles a hydrate auth runtime.
func (r *Repository) hydrateAuthRuntime(ctx context.Context, db *gorm.DB, auth *coreauth.Auth) error {
	// Normalize auth state before updating runtime indexes.
	if auth == nil {
		return nil
	}
	if shared := geminiSharedCredential(auth, ""); shared != nil {
		auth.Runtime = shared
	}

	parentID := geminiVirtualParentID(auth)
	if parentID == "" {
		return nil
	}

	parent := &coreauth.Auth{}
	if strings.TrimSpace(parentID) == strings.TrimSpace(auth.ID) {
		parent = auth
	} else {
		parentRecord := &AuthRecord{}
		errFirst := db.WithContext(contextOrBackground(ctx)).Where("uuid = ?", parentID).First(parentRecord).Error
		if errFirst != nil {
			if errors.Is(errFirst, gorm.ErrRecordNotFound) {
				return nil
			}
			return errFirst
		}
		parentAuth, errAuth := RecordToAuth(parentRecord)
		if errAuth != nil {
			return errAuth
		}
		parent = parentAuth
	}

	applyGeminiVirtualRuntime(auth, parent)
	return nil
}

// hydrateAuthListRuntimes handles a hydrate auth list runtimes.
func hydrateAuthListRuntimes(auths []*coreauth.Auth) {
	// Normalize auth state before updating runtime indexes.
	if len(auths) == 0 {
		return
	}

	byID := make(map[string]*coreauth.Auth, len(auths))
	sharedByID := make(map[string]*geminicli.SharedCredential, len(auths))
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		authID := strings.TrimSpace(auth.ID)
		if authID == "" {
			continue
		}
		byID[authID] = auth
		if shared := geminiSharedCredential(auth, ""); shared != nil {
			auth.Runtime = shared
			sharedByID[authID] = shared
		}
	}

	for _, auth := range auths {
		if auth == nil {
			continue
		}
		parentID := geminiVirtualParentID(auth)
		if parentID == "" {
			continue
		}
		parent := byID[parentID]
		if parent == nil {
			continue
		}
		shared := sharedByID[parentID]
		if shared == nil {
			shared = geminiSharedCredential(parent, geminiVirtualProjectID(auth))
			if shared == nil {
				continue
			}
			parent.Runtime = shared
			sharedByID[parentID] = shared
		}
		projectID := geminiVirtualProjectID(auth)
		if projectID == "" {
			continue
		}
		auth.Runtime = geminicli.NewVirtualCredential(projectID, shared)
	}
}

// applyGeminiVirtualRuntime applies a gemini virtual runtime.
func applyGeminiVirtualRuntime(auth *coreauth.Auth, parent *coreauth.Auth) {
	if auth == nil || parent == nil {
		return
	}
	projectID := geminiVirtualProjectID(auth)
	if projectID == "" {
		return
	}
	shared := geminicli.ResolveSharedCredential(parent.Runtime)
	if shared == nil {
		shared = geminiSharedCredential(parent, projectID)
		if shared == nil {
			return
		}
		parent.Runtime = shared
	}
	auth.Runtime = geminicli.NewVirtualCredential(projectID, shared)
}

// geminiSharedCredential handles a gemini shared credential.
func geminiSharedCredential(auth *coreauth.Auth, fallbackProjectID string) *geminicli.SharedCredential {
	if auth == nil || !isGeminiCLIAuth(auth) {
		return nil
	}
	if auth.Metadata == nil && auth.Attributes == nil {
		return nil
	}

	email := metadataString(auth.Metadata, "email")
	projects := geminiProjectIDs(auth)
	if fallbackProjectID != "" {
		projects = appendUniqueString(projects, fallbackProjectID)
	}
	return geminicli.NewSharedCredential(auth.ID, email, auth.Metadata, projects)
}

// isGeminiCLIAuth reports whether gemini cli auth.
func isGeminiCLIAuth(auth *coreauth.Auth) bool {
	if auth == nil {
		return false
	}
	provider := strings.TrimSpace(auth.Provider)
	if strings.EqualFold(provider, "gemini-cli") || strings.EqualFold(provider, "gemini") {
		return true
	}
	if auth.Attributes != nil {
		if strings.TrimSpace(auth.Attributes["gemini_virtual_primary"]) != "" {
			return true
		}
		if strings.TrimSpace(auth.Attributes["gemini_virtual_parent"]) != "" {
			return true
		}
	}
	return false
}

// geminiVirtualParentID handles a gemini virtual parent id.
func geminiVirtualParentID(auth *coreauth.Auth) string {
	if auth == nil || auth.Attributes == nil {
		return ""
	}
	return strings.TrimSpace(auth.Attributes["gemini_virtual_parent"])
}

// geminiVirtualProjectID handles a gemini virtual project id.
func geminiVirtualProjectID(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Attributes != nil {
		if projectID := strings.TrimSpace(auth.Attributes["gemini_virtual_project"]); projectID != "" {
			return projectID
		}
	}
	return metadataString(auth.Metadata, "project_id")
}

// geminiProjectIDs handles a gemini project i ds.
func geminiProjectIDs(auth *coreauth.Auth) []string {
	if auth == nil {
		return nil
	}
	out := make([]string, 0, 4)
	if auth.Attributes != nil {
		for _, id := range splitCommaValues(auth.Attributes["virtual_children"]) {
			out = appendUniqueString(out, id)
		}
	}
	for _, id := range splitCommaValues(metadataString(auth.Metadata, "project_id")) {
		out = appendUniqueString(out, id)
	}
	return out
}

// metadataString handles a metadata string.
func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

// splitCommaValues splits a comma values.
func splitCommaValues(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

// appendUniqueString appends an unique string.
func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
