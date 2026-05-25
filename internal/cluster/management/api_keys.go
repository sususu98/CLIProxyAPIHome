package management

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"gorm.io/gorm"
)

type apiKeyEntryBody struct {
	APIKey     *string `json:"api_key"`
	APIKeyDash *string `json:"api-key"`
	Key        *string `json:"key"`
	Value      *string `json:"value"`
	Channels   *[]uint `json:"channels"`
}

type apiKeyPatchBody struct {
	Old        *string         `json:"old"`
	New        *string         `json:"new"`
	Index      *int            `json:"index"`
	Value      json.RawMessage `json:"value"`
	APIKey     *string         `json:"api_key"`
	APIKeyDash *string         `json:"api-key"`
	Key        *string         `json:"key"`
	Channels   *[]uint         `json:"channels"`
}

func apiKeyEntriesResponse(entries []cluster.APIKeyEntry) gin.H {
	keys := make([]string, 0, len(entries))
	items := make([]gin.H, 0, len(entries))
	for _, entry := range entries {
		key := strings.TrimSpace(entry.APIKey)
		if key == "" {
			continue
		}
		channels := append([]uint(nil), entry.Channels...)
		if channels == nil {
			channels = []uint{}
		}
		keys = append(keys, key)
		items = append(items, gin.H{
			"api-key":  key,
			"api_key":  key,
			"channels": channels,
		})
	}
	return gin.H{
		"api-keys":        keys,
		"items":           items,
		"api_key_entries": items,
	}
}

func decodeAPIKeyEntryUpdates(data []byte) ([]cluster.APIKeyEntryUpdate, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	if entries, errEntries := decodeAPIKeyEntryArray(data); errEntries == nil {
		return entries, nil
	}

	var wrapper map[string]json.RawMessage
	if errUnmarshal := json.Unmarshal(data, &wrapper); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	for _, key := range []string{"items", "api-keys", "api_keys"} {
		raw := wrapper[key]
		if len(raw) == 0 {
			continue
		}
		return decodeAPIKeyEntryArray(raw)
	}
	return nil, fmt.Errorf("missing api key items")
}

func decodeAPIKeyEntryArray(data []byte) ([]cluster.APIKeyEntryUpdate, error) {
	var rawItems []json.RawMessage
	if errUnmarshal := json.Unmarshal(data, &rawItems); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	entries := make([]cluster.APIKeyEntryUpdate, 0, len(rawItems))
	for _, raw := range rawItems {
		entry, errEntry := decodeAPIKeyEntry(raw)
		if errEntry != nil {
			return nil, errEntry
		}
		if strings.TrimSpace(entry.APIKey) == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func decodeAPIKeyEntry(data []byte) (cluster.APIKeyEntryUpdate, error) {
	var key string
	if errString := json.Unmarshal(data, &key); errString == nil {
		return cluster.APIKeyEntryUpdate{APIKey: strings.TrimSpace(key)}, nil
	}

	var body apiKeyEntryBody
	if errUnmarshal := json.Unmarshal(data, &body); errUnmarshal != nil {
		return cluster.APIKeyEntryUpdate{}, errUnmarshal
	}
	return cluster.APIKeyEntryUpdate{
		APIKey:   body.apiKey(),
		Channels: body.Channels,
	}, nil
}

func (b apiKeyEntryBody) apiKey() string {
	for _, value := range []*string{b.APIKey, b.APIKeyDash, b.Key, b.Value} {
		if value == nil {
			continue
		}
		if key := strings.TrimSpace(*value); key != "" {
			return key
		}
	}
	return ""
}

func (h *Handler) patchAPIKeyEntries(c *gin.Context) error {
	var body apiKeyPatchBody
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		return fmt.Errorf("invalid body")
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()

	if body.Channels != nil {
		key := body.apiKey()
		if key == "" && len(body.Value) > 0 {
			if entry, errEntry := decodeAPIKeyEntry(body.Value); errEntry == nil {
				key = strings.TrimSpace(entry.APIKey)
			}
		}
		if key == "" {
			return fmt.Errorf("missing api_key")
		}
		record, errUpdate := h.repo.UpdateAPIKeyChannels(ctx, key, *body.Channels)
		if errUpdate != nil {
			if errors.Is(errUpdate, gorm.ErrRecordNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "api key not found"})
				return nil
			}
			respondError(c, http.StatusInternalServerError, "write_failed", errUpdate)
			return nil
		}
		entry, errEntry := clusterAPIKeyEntryFromRecord(record)
		if errEntry != nil {
			respondError(c, http.StatusInternalServerError, "api_key_load_failed", errEntry)
			return nil
		}
		c.JSON(http.StatusOK, gin.H{"api_key": apiKeyEntryToMap(entry)})
		return nil
	}

	entries, errEntries := h.repo.ListAPIKeyEntries(ctx)
	if errEntries != nil {
		respondError(c, http.StatusInternalServerError, "api_keys_load_failed", errEntries)
		return nil
	}

	changed := false
	if body.Index != nil && len(body.Value) > 0 {
		if *body.Index < 0 || *body.Index >= len(entries) {
			return fmt.Errorf("invalid index")
		}
		next, errEntry := decodeAPIKeyEntry(body.Value)
		if errEntry != nil {
			return fmt.Errorf("invalid value")
		}
		if strings.TrimSpace(next.APIKey) == "" {
			return fmt.Errorf("invalid value")
		}
		if next.Channels == nil {
			channels := append([]uint(nil), entries[*body.Index].Channels...)
			next.Channels = &channels
		}
		entries[*body.Index] = cluster.APIKeyEntry{
			APIKey:   strings.TrimSpace(next.APIKey),
			Channels: channelsOrEmpty(next.Channels),
		}
		changed = true
	} else if body.Old != nil && body.New != nil {
		oldKey := strings.TrimSpace(*body.Old)
		newKey := strings.TrimSpace(*body.New)
		if oldKey == "" || newKey == "" {
			return fmt.Errorf("missing fields")
		}
		for i := range entries {
			if strings.TrimSpace(entries[i].APIKey) != oldKey {
				continue
			}
			entries[i].APIKey = newKey
			changed = true
			break
		}
		if !changed {
			entries = append(entries, cluster.APIKeyEntry{APIKey: newKey})
			changed = true
		}
	}
	if !changed {
		return fmt.Errorf("missing fields")
	}

	if _, errReplace := h.repo.ReplaceAPIKeyEntries(ctx, apiKeyEntryUpdatesFromEntries(entries)); errReplace != nil {
		respondError(c, http.StatusInternalServerError, "write_failed", errReplace)
		return nil
	}
	if errRefresh := h.refreshConfig(ctx); errRefresh != nil {
		respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
		return nil
	}
	respondOK(c)
	return nil
}

func (h *Handler) deleteAPIKeyEntry(c *gin.Context) error {
	ctx, cancel := h.requestContext(c)
	defer cancel()

	entries, errEntries := h.repo.ListAPIKeyEntries(ctx)
	if errEntries != nil {
		respondError(c, http.StatusInternalServerError, "api_keys_load_failed", errEntries)
		return nil
	}

	deleted := false
	if idxRaw := c.Query("index"); idxRaw != "" {
		index, errIndex := strconv.Atoi(idxRaw)
		if errIndex == nil && index >= 0 && index < len(entries) {
			entries = append(entries[:index], entries[index+1:]...)
			deleted = true
		}
	}
	if !deleted {
		key := firstNonEmptyQuery(c, "value", "api_key", "api-key", "key")
		if key != "" {
			next := make([]cluster.APIKeyEntry, 0, len(entries))
			for _, entry := range entries {
				if strings.TrimSpace(entry.APIKey) == key {
					deleted = true
					continue
				}
				next = append(next, entry)
			}
			entries = next
		}
	}
	if !deleted {
		return fmt.Errorf("missing index or value")
	}

	if _, errReplace := h.repo.ReplaceAPIKeyEntries(ctx, apiKeyEntryUpdatesFromEntries(entries)); errReplace != nil {
		respondError(c, http.StatusInternalServerError, "write_failed", errReplace)
		return nil
	}
	if errRefresh := h.refreshConfig(ctx); errRefresh != nil {
		respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
		return nil
	}
	respondOK(c)
	return nil
}

func (b apiKeyPatchBody) apiKey() string {
	for _, value := range []*string{b.APIKey, b.APIKeyDash, b.Key} {
		if value == nil {
			continue
		}
		if key := strings.TrimSpace(*value); key != "" {
			return key
		}
	}
	return ""
}

func apiKeyEntryUpdatesFromEntries(entries []cluster.APIKeyEntry) []cluster.APIKeyEntryUpdate {
	updates := make([]cluster.APIKeyEntryUpdate, 0, len(entries))
	for _, entry := range entries {
		key := strings.TrimSpace(entry.APIKey)
		if key == "" {
			continue
		}
		channels := append([]uint(nil), entry.Channels...)
		updates = append(updates, cluster.APIKeyEntryUpdate{
			APIKey:   key,
			Channels: &channels,
		})
	}
	return updates
}

func channelsOrEmpty(channels *[]uint) []uint {
	if channels == nil {
		return nil
	}
	return append([]uint(nil), *channels...)
}

func clusterAPIKeyEntryFromRecord(record *cluster.APIKeyRecord) (cluster.APIKeyEntry, error) {
	return cluster.APIKeyEntryFromRecord(record)
}

func apiKeyEntryToMap(entry cluster.APIKeyEntry) gin.H {
	channels := append([]uint(nil), entry.Channels...)
	if channels == nil {
		channels = []uint{}
	}
	return gin.H{
		"api-key":  strings.TrimSpace(entry.APIKey),
		"api_key":  strings.TrimSpace(entry.APIKey),
		"channels": channels,
	}
}
