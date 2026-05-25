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
	APIKey   string
	Channels []uint
}

type APIKeyEntryUpdate struct {
	APIKey   string
	Channels *[]uint
}

// ListAPIKeyEntries returns API key rows with channel bindings.
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

// UpdateAPIKeyChannels updates channel bindings for one API key.
func (r *Repository) UpdateAPIKeyChannels(ctx context.Context, apiKey string, channels []uint) (*APIKeyRecord, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("api key is required")
	}
	channelsJSON, errChannels := apiKeyChannelsJSON(channels)
	if errChannels != nil {
		return nil, errChannels
	}

	record := &APIKeyRecord{}
	ctx = contextOrBackground(ctx)
	errTransaction := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if errFirst := tx.Where("api_key = ?", apiKey).First(record).Error; errFirst != nil {
			return errFirst
		}
		record.Channels = channelsJSON
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
		channelsJSON := emptyAPIKeyChannelsJSON()
		var errChannels error
		if entry.Channels != nil {
			channelsJSON, errChannels = apiKeyChannelsJSON(*entry.Channels)
			if errChannels != nil {
				return APIKeyUpsertStats{}, errChannels
			}
		}

		if record := existingByKey[key]; record != nil {
			updates := make(map[string]any)
			if record.DeletedAt.Valid {
				updates["deleted_at"] = nil
				stats.Restored++
			} else {
				stats.Unchanged++
			}
			if entry.Channels != nil {
				currentChannels, errCurrent := apiKeyChannelsFromJSON(record.Channels)
				if errCurrent != nil {
					return APIKeyUpsertStats{}, errCurrent
				}
				if !reflect.DeepEqual(currentChannels, normalizeChannelGroupIDs(*entry.Channels)) {
					updates["channels"] = channelsJSON
					if !record.DeletedAt.Valid {
						stats.Updated++
						stats.Unchanged--
					}
				}
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
			APIKey:   key,
			Channels: channelsJSON,
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
		if entry.Channels != nil {
			channels := normalizeChannelGroupIDs(*entry.Channels)
			next.Channels = &channels
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
	return APIKeyEntry{
		APIKey:   strings.TrimSpace(record.APIKey),
		Channels: channels,
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

func migrateAPIKeyChannels(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database connection is nil")
	}
	return db.Model(&APIKeyRecord{}).Where("channels IS NULL").Update("channels", emptyAPIKeyChannelsJSON()).Error
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

func normalizeChannelGroupIDs(ids []uint) []uint {
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
