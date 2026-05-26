package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

type APIKeyEntry struct {
	APIKey      string
	UserID      *uint
	Channels    []uint
	ModelGroups []uint
}

type APIKeyEntryUpdate struct {
	APIKey      string
	UserID      *uint
	Channels    *[]uint
	ModelGroups *[]uint
}

// ListAPIKeyEntries returns API key rows with group bindings.
func (r *Repository) ListAPIKeyEntries(ctx context.Context) ([]APIKeyEntry, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}

	var records []APIKeyRecord
	if errFind := db.WithContext(contextOrBackground(ctx)).Order("id").Find(&records).Error; errFind != nil {
		return nil, errFind
	}

	out := make([]APIKeyEntry, 0, len(records))
	for _, record := range records {
		entry, errEntry := apiKeyEntryFromRecord(&record)
		if errEntry != nil {
			return nil, errEntry
		}
		out = append(out, entry)
	}
	return out, nil
}

// ReplaceAPIKeyEntries replaces active API key rows and updates explicit channel bindings.
func (r *Repository) ReplaceAPIKeyEntries(ctx context.Context, entries []APIKeyEntryUpdate) (APIKeyUpsertStats, error) {
	db, errDB := r.database()
	if errDB != nil {
		return APIKeyUpsertStats{}, errDB
	}

	ctx = contextOrBackground(ctx)
	var stats APIKeyUpsertStats
	errTransaction := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var errReplace error
		stats, errReplace = replaceAPIKeyEntriesTxWithStats(ctx, tx, entries)
		if errReplace != nil {
			return errReplace
		}
		deleteResult := tx.Delete(&ConfigRecord{}, "key = ?", configAPIKeysRootKey)
		if deleteResult.Error != nil {
			return deleteResult.Error
		}
		if !stats.Changed() && deleteResult.RowsAffected == 0 {
			return nil
		}
		return appendEvent(tx, "config", "upsert", configAPIKeysRootKey, time.Now().UTC().UnixNano())
	})
	return stats, errTransaction
}

// UpdateAPIKeyBindings updates user and group bindings for one API key.
func (r *Repository) UpdateAPIKeyBindings(ctx context.Context, apiKey string, userID *uint, channels *[]uint, modelGroups *[]uint) (*APIKeyRecord, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("api key is required")
	}

	record := &APIKeyRecord{}
	ctx = contextOrBackground(ctx)
	errTransaction := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if errFirst := tx.Where("api_key = ?", apiKey).First(record).Error; errFirst != nil {
			return errFirst
		}
		if userID != nil {
			nextUserID := normalizeOptionalUserID(userID)
			if nextUserID != nil {
				if errUser := ensureUserExists(ctx, tx, *nextUserID); errUser != nil {
					return errUser
				}
			}
			record.UserID = nextUserID
		}
		if channels != nil {
			channelsJSON, errChannels := apiKeyChannelsJSON(*channels)
			if errChannels != nil {
				return errChannels
			}
			record.Channels = channelsJSON
		}
		if modelGroups != nil {
			modelGroupsJSON, errModelGroups := apiKeyModelGroupsJSON(*modelGroups)
			if errModelGroups != nil {
				return errModelGroups
			}
			record.ModelGroups = modelGroupsJSON
		}
		if errSave := tx.Save(record).Error; errSave != nil {
			return errSave
		}
		return appendEvent(tx, "config", "upsert", configAPIKeysRootKey, time.Now().UTC().UnixNano())
	})
	if errTransaction != nil {
		return nil, errTransaction
	}
	return record, nil
}

// UpdateAPIKeyChannels updates channel bindings for one API key.
func (r *Repository) UpdateAPIKeyChannels(ctx context.Context, apiKey string, channels []uint) (*APIKeyRecord, error) {
	return r.UpdateAPIKeyBindings(ctx, apiKey, nil, &channels, nil)
}

// AllowedDispatchIDsForAPIKey returns auth and model IDs allowed by API-key bindings.
func (r *Repository) AllowedDispatchIDsForAPIKey(ctx context.Context, apiKey string) ([]string, []string, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, nil, errDB
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, nil, nil
	}

	record := APIKeyRecord{}
	errFirst := db.WithContext(contextOrBackground(ctx)).Where("api_key = ?", apiKey).First(&record).Error
	if errFirst != nil {
		return nil, nil, errFirst
	}

	authIDs, errAuthIDs := allowedAuthIDsForAPIKeyRecord(ctx, db, &record)
	if errAuthIDs != nil {
		return nil, nil, errAuthIDs
	}
	modelIDs, errModelIDs := allowedModelIDsForAPIKeyRecord(ctx, db, &record)
	if errModelIDs != nil {
		return nil, nil, errModelIDs
	}
	return authIDs, modelIDs, nil
}

// AllowedAuthIDsForAPIKey returns auth IDs allowed by the API key channel bindings.
func (r *Repository) AllowedAuthIDsForAPIKey(ctx context.Context, apiKey string) ([]string, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, nil
	}

	record := APIKeyRecord{}
	errFirst := db.WithContext(contextOrBackground(ctx)).Where("api_key = ?", apiKey).First(&record).Error
	if errFirst != nil {
		return nil, errFirst
	}

	return allowedAuthIDsForAPIKeyRecord(ctx, db, &record)
}

// AllowedModelIDsForAPIKey returns model IDs allowed by the API key model group bindings.
func (r *Repository) AllowedModelIDsForAPIKey(ctx context.Context, apiKey string) ([]string, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, nil
	}

	record := APIKeyRecord{}
	errFirst := db.WithContext(contextOrBackground(ctx)).Where("api_key = ?", apiKey).First(&record).Error
	if errFirst != nil {
		return nil, errFirst
	}

	return allowedModelIDsForAPIKeyRecord(ctx, db, &record)
}

func allowedAuthIDsForAPIKeyRecord(ctx context.Context, db *gorm.DB, record *APIKeyRecord) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	if record == nil {
		return nil, fmt.Errorf("api key record is nil")
	}
	channelIDs, errChannels := apiKeyChannelsFromJSON(record.Channels)
	if errChannels != nil {
		return nil, errChannels
	}
	if len(channelIDs) == 0 {
		return nil, nil
	}

	var details []ChannelGroupDetailRecord
	if errFind := db.WithContext(contextOrBackground(ctx)).
		Model(&ChannelGroupDetailRecord{}).
		Joins("JOIN channel_group ON channel_group.id = channel_group_detail.channel_group_id").
		Where("channel_group.deleted_at IS NULL").
		Where("channel_group.disabled = ?", false).
		Where("channel_group_detail.channel_group_id IN ?", channelIDs).
		Order("channel_group_detail.channel_group_id ASC, channel_group_detail.id ASC").
		Find(&details).Error; errFind != nil {
		return nil, errFind
	}

	allowed := make([]string, 0, len(details))
	seen := make(map[string]struct{}, len(details))
	for _, detail := range details {
		authID := strings.TrimSpace(detail.AuthID)
		if authID == "" {
			continue
		}
		if _, ok := seen[authID]; ok {
			continue
		}
		seen[authID] = struct{}{}
		allowed = append(allowed, authID)
	}
	return allowed, nil
}

func allowedModelIDsForAPIKeyRecord(ctx context.Context, db *gorm.DB, record *APIKeyRecord) ([]string, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	if record == nil {
		return nil, fmt.Errorf("api key record is nil")
	}
	modelGroupIDs, errModelGroups := apiKeyModelGroupsFromJSON(record.ModelGroups)
	if errModelGroups != nil {
		return nil, errModelGroups
	}
	if len(modelGroupIDs) == 0 {
		return nil, nil
	}

	var details []ModelGroupDetailRecord
	if errFind := db.WithContext(contextOrBackground(ctx)).
		Model(&ModelGroupDetailRecord{}).
		Joins("JOIN model_group ON model_group.id = model_group_detail.model_group_id").
		Where("model_group.deleted_at IS NULL").
		Where("model_group.disabled = ?", false).
		Where("model_group_detail.model_group_id IN ?", modelGroupIDs).
		Order("model_group_detail.model_group_id ASC, model_group_detail.id ASC").
		Find(&details).Error; errFind != nil {
		return nil, errFind
	}

	allowed := make([]string, 0, len(details))
	seen := make(map[string]struct{}, len(details))
	for _, detail := range details {
		modelID := strings.TrimSpace(detail.ModelID)
		if modelID == "" {
			continue
		}
		modelKey := strings.ToLower(modelID)
		if _, ok := seen[modelKey]; ok {
			continue
		}
		seen[modelKey] = struct{}{}
		allowed = append(allowed, modelID)
	}
	return allowed, nil
}

func replaceAPIKeyEntriesTxWithStats(ctx context.Context, tx *gorm.DB, entries []APIKeyEntryUpdate) (APIKeyUpsertStats, error) {
	if tx == nil {
		return APIKeyUpsertStats{}, fmt.Errorf("database connection is nil")
	}
	entries = normalizeAPIKeyEntryUpdates(entries)

	var existing []APIKeyRecord
	if errFind := tx.WithContext(contextOrBackground(ctx)).Unscoped().Order("id").Find(&existing).Error; errFind != nil {
		return APIKeyUpsertStats{}, errFind
	}

	stats := APIKeyUpsertStats{}
	existingByKey := make(map[string]*APIKeyRecord, len(existing))
	for i := range existing {
		record := &existing[i]
		key := strings.TrimSpace(record.APIKey)
		if key == "" {
			continue
		}
		existingByKey[key] = record
	}

	keep := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		key := strings.TrimSpace(entry.APIKey)
		if key == "" {
			continue
		}
		keep[key] = struct{}{}
		userID := normalizeOptionalUserID(entry.UserID)
		if userID != nil {
			if errUser := ensureUserExists(ctx, tx, *userID); errUser != nil {
				return APIKeyUpsertStats{}, errUser
			}
		}
		channelsJSON := emptyAPIKeyChannelsJSON()
		var errChannels error
		if entry.Channels != nil {
			channelsJSON, errChannels = apiKeyChannelsJSON(*entry.Channels)
			if errChannels != nil {
				return APIKeyUpsertStats{}, errChannels
			}
		}
		modelGroupsJSON := emptyAPIKeyModelGroupsJSON()
		var errModelGroups error
		if entry.ModelGroups != nil {
			modelGroupsJSON, errModelGroups = apiKeyModelGroupsJSON(*entry.ModelGroups)
			if errModelGroups != nil {
				return APIKeyUpsertStats{}, errModelGroups
			}
		}

		if record := existingByKey[key]; record != nil {
			updates := make(map[string]any)
			updatedBindings := false
			if record.DeletedAt.Valid {
				updates["deleted_at"] = nil
				stats.Restored++
			} else {
				stats.Unchanged++
			}
			if !sameOptionalUint(record.UserID, userID) {
				updates["user_id"] = userID
				updatedBindings = true
			}
			if entry.Channels != nil {
				currentChannels, errCurrent := apiKeyChannelsFromJSON(record.Channels)
				if errCurrent != nil {
					return APIKeyUpsertStats{}, errCurrent
				}
				if !reflect.DeepEqual(currentChannels, normalizeChannelGroupIDs(*entry.Channels)) {
					updates["channels"] = channelsJSON
					updatedBindings = true
				}
			}
			if entry.ModelGroups != nil {
				currentModelGroups, errCurrent := apiKeyModelGroupsFromJSON(record.ModelGroups)
				if errCurrent != nil {
					return APIKeyUpsertStats{}, errCurrent
				}
				if !reflect.DeepEqual(currentModelGroups, normalizeModelGroupIDs(*entry.ModelGroups)) {
					updates["model_groups"] = modelGroupsJSON
					updatedBindings = true
				}
			}
			if updatedBindings && !record.DeletedAt.Valid {
				stats.Updated++
				stats.Unchanged--
			}
			if len(updates) > 0 {
				if errUpdate := tx.WithContext(contextOrBackground(ctx)).Unscoped().
					Model(&APIKeyRecord{}).
					Where("id = ?", record.ID).
					Updates(updates).Error; errUpdate != nil {
					return APIKeyUpsertStats{}, errUpdate
				}
			}
			continue
		}

		if errCreate := tx.WithContext(contextOrBackground(ctx)).Create(&APIKeyRecord{
			APIKey:      key,
			UserID:      userID,
			Channels:    channelsJSON,
			ModelGroups: modelGroupsJSON,
		}).Error; errCreate != nil {
			return APIKeyUpsertStats{}, errCreate
		}
		stats.Created++
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
			return APIKeyUpsertStats{}, errDelete
		}
		stats.Removed++
	}
	return stats, nil
}

func apiKeyEntryUpdatesFromKeys(keys []string) []APIKeyEntryUpdate {
	keys = normalizeAPIKeys(keys)
	entries := make([]APIKeyEntryUpdate, 0, len(keys))
	for _, key := range keys {
		entries = append(entries, APIKeyEntryUpdate{APIKey: key})
	}
	return entries
}

func normalizeAPIKeyEntryUpdates(entries []APIKeyEntryUpdate) []APIKeyEntryUpdate {
	if len(entries) == 0 {
		return nil
	}
	normalized := make([]APIKeyEntryUpdate, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		key := strings.TrimSpace(entry.APIKey)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		next := APIKeyEntryUpdate{APIKey: key}
		next.UserID = normalizeOptionalUserID(entry.UserID)
		if entry.Channels != nil {
			channels := normalizeChannelGroupIDs(*entry.Channels)
			next.Channels = &channels
		}
		if entry.ModelGroups != nil {
			modelGroups := normalizeModelGroupIDs(*entry.ModelGroups)
			next.ModelGroups = &modelGroups
		}
		normalized = append(normalized, next)
	}
	return normalized
}

func apiKeyEntryFromRecord(record *APIKeyRecord) (APIKeyEntry, error) {
	if record == nil {
		return APIKeyEntry{}, fmt.Errorf("api key record is nil")
	}
	channels, errChannels := apiKeyChannelsFromJSON(record.Channels)
	if errChannels != nil {
		return APIKeyEntry{}, errChannels
	}
	modelGroups, errModelGroups := apiKeyModelGroupsFromJSON(record.ModelGroups)
	if errModelGroups != nil {
		return APIKeyEntry{}, errModelGroups
	}
	return APIKeyEntry{
		APIKey:      strings.TrimSpace(record.APIKey),
		UserID:      normalizeOptionalUserID(record.UserID),
		Channels:    channels,
		ModelGroups: modelGroups,
	}, nil
}

// APIKeyEntryFromRecord converts an API key record to a response entry.
func APIKeyEntryFromRecord(record *APIKeyRecord) (APIKeyEntry, error) {
	return apiKeyEntryFromRecord(record)
}

func apiKeyChannelsJSON(channels []uint) (JSONB, error) {
	channels = normalizeChannelGroupIDs(channels)
	raw, errMarshal := json.Marshal(channels)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return JSONB(raw), nil
}

func emptyAPIKeyChannelsJSON() JSONB {
	return JSONB("[]")
}

func apiKeyModelGroupsJSON(modelGroups []uint) (JSONB, error) {
	modelGroups = normalizeModelGroupIDs(modelGroups)
	raw, errMarshal := json.Marshal(modelGroups)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return JSONB(raw), nil
}

func emptyAPIKeyModelGroupsJSON() JSONB {
	return JSONB("[]")
}

func sameOptionalUint(left *uint, right *uint) bool {
	left = normalizeOptionalUserID(left)
	right = normalizeOptionalUserID(right)
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func migrateAPIKeyChannels(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}
	return db.Model(&APIKeyRecord{}).Where("channels IS NULL").Update("channels", emptyAPIKeyChannelsJSON()).Error
}

func migrateAPIKeyModelGroups(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}
	return db.Model(&APIKeyRecord{}).Where("model_groups IS NULL").Update("model_groups", emptyAPIKeyModelGroupsJSON()).Error
}

func apiKeyChannelsFromJSON(raw JSONB) ([]uint, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var channels []uint
	if errUnmarshal := json.Unmarshal(raw, &channels); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	return normalizeChannelGroupIDs(channels), nil
}

func apiKeyModelGroupsFromJSON(raw JSONB) ([]uint, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var modelGroups []uint
	if errUnmarshal := json.Unmarshal(raw, &modelGroups); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	return normalizeModelGroupIDs(modelGroups), nil
}

func normalizeChannelGroupIDs(ids []uint) []uint {
	return normalizeAPIKeyGroupIDs(ids)
}

func normalizeModelGroupIDs(ids []uint) []uint {
	return normalizeAPIKeyGroupIDs(ids)
}

func normalizeAPIKeyGroupIDs(ids []uint) []uint {
	if len(ids) == 0 {
		return nil
	}
	out := make([]uint, 0, len(ids))
	seen := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i] < out[j]
	})
	return out
}
