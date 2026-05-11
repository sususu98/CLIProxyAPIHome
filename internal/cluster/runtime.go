package cluster

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"gorm.io/gorm"
)

type RuntimeAdapter struct {
	repo      *Repository
	index     map[string]AuthIndex
	fullCache map[string]*coreauth.Auth
	mu        sync.RWMutex
}

func NewRuntimeAdapter(repo *Repository) *RuntimeAdapter {
	return &RuntimeAdapter{
		repo:      repo,
		index:     make(map[string]AuthIndex),
		fullCache: make(map[string]*coreauth.Auth),
	}
}

func (a *RuntimeAdapter) Enabled() bool {
	return a != nil && a.repo != nil
}

func (a *RuntimeAdapter) LoadIndex(ctx context.Context) error {
	if !a.Enabled() {
		return fmt.Errorf("cluster runtime adapter is disabled")
	}
	indexes, errIndexes := a.repo.ListAuthIndex(ctx)
	if errIndexes != nil {
		return errIndexes
	}

	next := make(map[string]AuthIndex, len(indexes))
	for _, item := range indexes {
		uuid := strings.TrimSpace(item.UUID)
		if uuid == "" {
			continue
		}
		item.UUID = uuid
		item.ID = uuid
		item.Index = uuid
		item.Attributes = cloneStringMap(item.Attributes)
		next[uuid] = item
	}

	a.mu.Lock()
	a.index = next
	a.fullCache = make(map[string]*coreauth.Auth)
	a.mu.Unlock()
	return nil
}

func (a *RuntimeAdapter) LoadAuthIndex(ctx context.Context) error {
	return a.LoadIndex(ctx)
}

func (a *RuntimeAdapter) LoadConfigYAML(ctx context.Context) ([]byte, error) {
	if !a.Enabled() {
		return nil, fmt.Errorf("cluster runtime adapter is disabled")
	}
	_, payload, errConfig := a.repo.LoadConfigAsRuntimeConfig(ctx)
	if errConfig != nil {
		return nil, errConfig
	}
	return payload, nil
}

func (a *RuntimeAdapter) StoreUsagePayload(ctx context.Context, payload string) error {
	if !a.Enabled() {
		return fmt.Errorf("cluster runtime adapter is disabled")
	}
	_, errAppend := a.repo.AppendUsage(ctx, payload)
	return errAppend
}

func (a *RuntimeAdapter) List(ctx context.Context) ([]*coreauth.Auth, error) {
	if !a.Enabled() {
		return nil, fmt.Errorf("cluster runtime adapter is disabled")
	}
	auths, errAuths := a.repo.ListAuths(ctx)
	if errAuths != nil {
		return nil, errAuths
	}
	for _, auth := range auths {
		normalizeAuthUUID(auth)
	}
	return auths, nil
}

func (a *RuntimeAdapter) Save(ctx context.Context, auth *coreauth.Auth) (string, error) {
	if !a.Enabled() {
		return "", fmt.Errorf("cluster runtime adapter is disabled")
	}
	auth = normalizeAuthUUID(auth)
	if auth == nil || strings.TrimSpace(auth.ID) == "" {
		return "", fmt.Errorf("cluster auth uuid is required")
	}
	record, errRecord := a.repo.UpsertAuth(ctx, auth, "update")
	if errRecord != nil {
		return "", errRecord
	}

	item := authIndexFromRecord(record, auth)
	a.mu.Lock()
	if a.index == nil {
		a.index = make(map[string]AuthIndex)
	}
	item.Attributes = cloneStringMap(item.Attributes)
	a.index[auth.ID] = item
	if a.fullCache == nil {
		a.fullCache = make(map[string]*coreauth.Auth)
	}
	a.fullCache[auth.ID] = auth.Clone()
	a.mu.Unlock()
	return auth.ID, nil
}

func (a *RuntimeAdapter) Delete(ctx context.Context, id string) error {
	if !a.Enabled() {
		return fmt.Errorf("cluster runtime adapter is disabled")
	}
	uuid := strings.TrimSpace(id)
	if uuid == "" {
		return fmt.Errorf("cluster auth uuid is required")
	}
	if errDelete := a.repo.SoftDeleteAuth(ctx, uuid); errDelete != nil {
		return errDelete
	}
	a.RemoveAuthIndex(uuid)
	return nil
}

func (a *RuntimeAdapter) RefreshAuthIndex(ctx context.Context, uuid string) error {
	if !a.Enabled() {
		return fmt.Errorf("cluster runtime adapter is disabled")
	}
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return fmt.Errorf("cluster auth uuid is required")
	}

	auth, record, errAuth := a.repo.GetAuth(ctx, uuid)
	if errAuth != nil {
		if errors.Is(errAuth, gorm.ErrRecordNotFound) {
			a.RemoveAuthIndex(uuid)
			return nil
		}
		return errAuth
	}

	item := authIndexFromRecord(record, auth)
	a.mu.Lock()
	if a.index == nil {
		a.index = make(map[string]AuthIndex)
	}
	item.Attributes = cloneStringMap(item.Attributes)
	a.index[uuid] = item
	if a.fullCache != nil {
		delete(a.fullCache, uuid)
	}
	a.mu.Unlock()
	return nil
}

func normalizeAuthUUID(auth *coreauth.Auth) *coreauth.Auth {
	if auth == nil {
		return nil
	}
	uuid := strings.TrimSpace(auth.ID)
	if uuid == "" {
		uuid = strings.TrimSpace(auth.Index)
	}
	auth.ID = uuid
	auth.Index = uuid
	return auth
}

func (a *RuntimeAdapter) ApplyEvent(ctx context.Context, event ClusterEventRecord) error {
	if !strings.EqualFold(strings.TrimSpace(event.Scope), "auth") {
		return nil
	}
	uuid := strings.TrimSpace(event.EntityUUID)
	if uuid == "" {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(event.Op), "delete") {
		a.RemoveAuthIndex(uuid)
		return nil
	}
	return a.RefreshAuthIndex(ctx, uuid)
}

func (a *RuntimeAdapter) RemoveAuthIndex(uuid string) {
	if a == nil {
		return
	}
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return
	}
	a.mu.Lock()
	if a.index != nil {
		delete(a.index, uuid)
	}
	if a.fullCache != nil {
		delete(a.fullCache, uuid)
	}
	a.mu.Unlock()
}

func (a *RuntimeAdapter) GetFullAuth(ctx context.Context, uuid string) (*coreauth.Auth, error) {
	if !a.Enabled() {
		return nil, fmt.Errorf("cluster runtime adapter is disabled")
	}
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return nil, fmt.Errorf("cluster auth uuid is required")
	}

	a.mu.RLock()
	if cached := a.fullCache[uuid]; cached != nil {
		a.mu.RUnlock()
		return cached.Clone(), nil
	}
	a.mu.RUnlock()

	auth, _, errAuth := a.repo.GetAuth(ctx, uuid)
	if errAuth != nil {
		if errors.Is(errAuth, gorm.ErrRecordNotFound) {
			a.RemoveAuthIndex(uuid)
			return nil, coreauth.ErrFullAuthNotFound
		}
		return nil, errAuth
	}
	if auth == nil {
		return nil, nil
	}
	auth.ID = uuid
	auth.Index = uuid

	a.mu.Lock()
	if a.fullCache == nil {
		a.fullCache = make(map[string]*coreauth.Auth)
	}
	a.fullCache[uuid] = auth.Clone()
	a.mu.Unlock()
	return auth.Clone(), nil
}

func (a *RuntimeAdapter) InvalidateFullAuth(uuid string) {
	if a == nil {
		return
	}
	uuid = strings.TrimSpace(uuid)
	if uuid == "" {
		return
	}
	a.mu.Lock()
	if a.fullCache != nil {
		delete(a.fullCache, uuid)
	}
	a.mu.Unlock()
}

func (a *RuntimeAdapter) ListMinimalAuths() []*coreauth.Auth {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	keys := make([]string, 0, len(a.index))
	for uuid := range a.index {
		keys = append(keys, uuid)
	}
	sort.Strings(keys)
	out := make([]*coreauth.Auth, 0, len(keys))
	for _, uuid := range keys {
		item := a.index[uuid]
		out = append(out, authFromIndex(item))
	}
	a.mu.RUnlock()
	return out
}

func authIndexFromRecord(record *AuthRecord, auth *coreauth.Auth) AuthIndex {
	item := AuthIndex{}
	if record != nil {
		item.UUID = strings.TrimSpace(record.UUID)
		item.ID = item.UUID
		item.Index = item.UUID
		item.Provider = record.Provider
		item.Label = record.Label
		item.Prefix = record.Prefix
		item.Status = record.Status
		item.Disabled = record.Disabled
		item.Unavailable = record.Unavailable
		item.BaseURL = record.BaseURL
		item.ModelsHash = record.ModelsHash
	}
	if auth != nil {
		if item.UUID == "" {
			item.UUID = strings.TrimSpace(auth.ID)
			item.ID = item.UUID
			item.Index = item.UUID
		}
		item.Attributes = cloneStringMap(auth.Attributes)
	}
	return item
}

func authFromIndex(item AuthIndex) *coreauth.Auth {
	uuid := strings.TrimSpace(item.UUID)
	if uuid == "" {
		uuid = strings.TrimSpace(item.ID)
	}
	attrs := cloneStringMap(item.Attributes)
	if attrs == nil {
		attrs = make(map[string]string)
	}
	if item.BaseURL != "" {
		if _, ok := attrs["base_url"]; !ok {
			attrs["base_url"] = item.BaseURL
		}
	}
	if item.ModelsHash != "" {
		if _, ok := attrs["models_hash"]; !ok {
			attrs["models_hash"] = item.ModelsHash
		}
	}
	return &coreauth.Auth{
		ID:          uuid,
		Index:       uuid,
		Provider:    item.Provider,
		Label:       item.Label,
		Prefix:      item.Prefix,
		Status:      item.Status,
		Disabled:    item.Disabled,
		Unavailable: item.Unavailable,
		Attributes:  attrs,
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
