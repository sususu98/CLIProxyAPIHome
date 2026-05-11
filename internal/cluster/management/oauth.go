package management

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"github.com/router-for-me/CLIProxyAPIHome/internal/watcher/synthesizer"
)

func (h *Handler) ListAuthFiles(c *gin.Context) {
	ctx, cancel := h.requestContext(c)
	defer cancel()
	auths, errAuths := h.repo.ListAuths(ctx)
	if errAuths != nil {
		respondError(c, http.StatusInternalServerError, "auth_load_failed", errAuths)
		return
	}
	files := make([]gin.H, 0, len(auths))
	for _, auth := range auths {
		if !isOAuthAuth(auth) {
			continue
		}
		files = append(files, authFileEntry(auth))
	}
	sort.Slice(files, func(i, j int) bool {
		return fmt.Sprint(files[i]["name"]) < fmt.Sprint(files[j]["name"])
	})
	c.JSON(http.StatusOK, gin.H{"files": files})
}

func (h *Handler) DownloadAuthFile(c *gin.Context) {
	ctx, cancel := h.requestContext(c)
	defer cancel()
	auth, errAuth := h.findOAuthAuth(ctx, authIdentifierFromRequest(c))
	if errAuth != nil {
		respondError(c, http.StatusInternalServerError, "auth_load_failed", errAuth)
		return
	}
	if auth == nil {
		respondError(c, http.StatusNotFound, "not_found", nil)
		return
	}
	data, errMarshal := json.MarshalIndent(auth.Metadata, "", "  ")
	if errMarshal != nil {
		respondError(c, http.StatusInternalServerError, "marshal_failed", errMarshal)
		return
	}
	data = append(data, '\n')
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", authFileName(auth)))
	_, _ = c.Writer.Write(data)
}

func (h *Handler) UploadAuthFile(c *gin.Context) {
	if strings.HasPrefix(strings.ToLower(c.ContentType()), "multipart/form-data") {
		headers, errHeaders := multipartHeaders(c)
		if errHeaders != nil {
			respondError(c, http.StatusBadRequest, "invalid multipart", errHeaders)
			return
		}
		if len(headers) == 0 {
			respondError(c, http.StatusBadRequest, "no files uploaded", nil)
			return
		}
		uploaded := make([]string, 0, len(headers))
		for _, header := range headers {
			name, errStore := h.storeUploadedOAuth(c, header)
			if errStore != nil {
				respondError(c, http.StatusBadRequest, "upload_failed", errStore)
				return
			}
			uploaded = append(uploaded, name)
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "uploaded": len(uploaded), "files": uploaded})
		return
	}
	body, errRead := io.ReadAll(c.Request.Body)
	if errRead != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errRead)
		return
	}
	name, errStore := h.storeOAuthPayload(c, body)
	if errStore != nil {
		respondError(c, http.StatusBadRequest, "upload_failed", errStore)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "name": name})
}

func (h *Handler) DeleteAuthFile(c *gin.Context) {
	ctx, cancel := h.requestContext(c)
	defer cancel()
	auths, errAuths := h.repo.ListAuths(ctx)
	if errAuths != nil {
		respondError(c, http.StatusInternalServerError, "auth_load_failed", errAuths)
		return
	}
	if all := strings.TrimSpace(c.Query("all")); all == "1" || strings.EqualFold(all, "true") || all == "*" {
		deleted := 0
		for _, auth := range auths {
			if !isOAuthAuth(auth) {
				continue
			}
			if errDelete := h.repo.SoftDeleteAuth(ctx, auth.ID); errDelete != nil {
				respondError(c, http.StatusInternalServerError, "write_failed", errDelete)
				return
			}
			deleted++
		}
		if errRefresh := h.refreshAuths(ctx); errRefresh != nil {
			respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok", "deleted": deleted})
		return
	}

	auth := findOAuthAuthInList(auths, authIdentifierFromRequest(c))
	if auth == nil {
		respondError(c, http.StatusNotFound, "not_found", nil)
		return
	}
	if errDelete := h.deleteOAuthAuthAndChildren(ctx, auth, auths); errDelete != nil {
		respondError(c, http.StatusInternalServerError, "write_failed", errDelete)
		return
	}
	if errRefresh := h.refreshAuths(ctx); errRefresh != nil {
		respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) PatchAuthFileStatus(c *gin.Context) {
	var body map[string]any
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errBindJSON)
		return
	}
	disabled, errDisabled := requiredBoolField(body, "disabled")
	if errDisabled != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errDisabled)
		return
	}
	ctx, cancel := h.requestContext(c)
	defer cancel()
	auth, errAuth := h.findOAuthAuth(ctx, authIdentifierFromBodyAndRequest(c, body))
	if errAuth != nil {
		respondError(c, http.StatusInternalServerError, "auth_load_failed", errAuth)
		return
	}
	if auth == nil {
		respondError(c, http.StatusNotFound, "not_found", nil)
		return
	}
	auth.Disabled = disabled
	if disabled {
		auth.Status = coreauth.StatusDisabled
	} else {
		auth.Status = coreauth.StatusActive
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["disabled"] = disabled
	auth.UpdatedAt = time.Now().UTC()
	if _, errUpsert := h.repo.UpsertAuth(ctx, auth, "update"); errUpsert != nil {
		respondError(c, http.StatusInternalServerError, "write_failed", errUpsert)
		return
	}
	if errRefresh := h.refreshAuths(ctx); errRefresh != nil {
		respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
		return
	}
	respondOK(c)
}

func (h *Handler) PatchAuthFileFields(c *gin.Context) {
	var body map[string]any
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errBindJSON)
		return
	}
	ctx, cancel := h.requestContext(c)
	defer cancel()
	auth, errAuth := h.findOAuthAuth(ctx, authIdentifierFromBodyAndRequest(c, body))
	if errAuth != nil {
		respondError(c, http.StatusInternalServerError, "auth_load_failed", errAuth)
		return
	}
	if auth == nil {
		respondError(c, http.StatusNotFound, "not_found", nil)
		return
	}
	changed, errPatch := applyOAuthFieldPatch(auth, body)
	if errPatch != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errPatch)
		return
	}
	if !changed {
		respondError(c, http.StatusBadRequest, "no fields to update", nil)
		return
	}
	auth.UpdatedAt = time.Now().UTC()
	if _, errUpsert := h.repo.UpsertAuth(ctx, auth, "update"); errUpsert != nil {
		respondError(c, http.StatusInternalServerError, "write_failed", errUpsert)
		return
	}
	if errRefresh := h.refreshAuths(ctx); errRefresh != nil {
		respondError(c, http.StatusInternalServerError, "reload_failed", errRefresh)
		return
	}
	respondOK(c)
}

func (h *Handler) PostOAuthCallback(c *gin.Context) {
	body, errRead := io.ReadAll(c.Request.Body)
	if errRead != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errRead)
		return
	}
	name, errStore := h.storeOAuthPayload(c, body)
	if errStore != nil {
		respondError(c, http.StatusBadRequest, "oauth_callback_failed", errStore)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "name": name})
}

func (h *Handler) storeUploadedOAuth(c *gin.Context, header *multipart.FileHeader) (string, error) {
	if header == nil {
		return "", fmt.Errorf("file is required")
	}
	if !strings.HasSuffix(strings.ToLower(filepath.Base(header.Filename)), ".json") {
		return "", fmt.Errorf("file must be .json")
	}
	src, errOpen := header.Open()
	if errOpen != nil {
		return "", errOpen
	}
	data, errRead := io.ReadAll(src)
	if errClose := src.Close(); errClose != nil && errRead == nil {
		errRead = errClose
	}
	if errRead != nil {
		return "", errRead
	}
	return h.storeOAuthPayload(c, data)
}

func (h *Handler) storeOAuthPayload(c *gin.Context, raw []byte) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", fmt.Errorf("empty credential json")
	}
	updatedRaw, fileUUID, _, errUUID := cluster.EnsureOAuthPayloadUUID(raw)
	if errUUID != nil {
		return "", errUUID
	}
	ctx, cancel := h.requestContext(c)
	defer cancel()

	auths := h.synthesizeOAuthPayload(updatedRaw, fileUUID)
	if len(auths) == 0 {
		return "", fmt.Errorf("unsupported credential json")
	}
	if errReplace := h.replaceOAuthPayloadAuths(ctx, fileUUID, auths); errReplace != nil {
		return "", errReplace
	}
	if errRefresh := h.refreshAuths(ctx); errRefresh != nil {
		return "", errRefresh
	}
	return fileUUID + ".json", nil
}

func (h *Handler) synthesizeOAuthPayload(raw []byte, fileUUID string) []*coreauth.Auth {
	cfg := h.runtime.Config()
	authPath := fileUUID + ".json"
	legacyUUIDs := make(map[string]string)
	sctx := &synthesizer.SynthesisContext{
		Config:      cfg,
		AuthDir:     "",
		Now:         time.Now().UTC(),
		IDGenerator: synthesizer.NewStableIDGenerator(),
		ClusterMode: true,
	}
	sctx.UUIDForAuth = func(auth *coreauth.Auth) string {
		if auth == nil {
			return ""
		}
		legacyID := auth.ID
		if auth.Attributes != nil {
			if parentID := strings.TrimSpace(auth.Attributes["gemini_virtual_parent"]); parentID != "" {
				parentUUID := strings.TrimSpace(legacyUUIDs[parentID])
				if parentUUID == "" {
					parentUUID = parentID
				}
				auth.Attributes["gemini_virtual_parent"] = parentUUID
				if auth.Metadata != nil {
					auth.Metadata["virtual_parent_id"] = parentUUID
				}
				return cluster.DeterministicVirtualUUID(parentUUID, auth.Attributes["gemini_virtual_project"])
			}
		}
		legacyUUIDs[legacyID] = fileUUID
		return fileUUID
	}
	return synthesizer.SynthesizeAuthFile(sctx, authPath, raw)
}

func (h *Handler) replaceOAuthPayloadAuths(ctx context.Context, fileUUID string, auths []*coreauth.Auth) error {
	nextIDs := make(map[string]struct{}, len(auths))
	for _, auth := range auths {
		if auth == nil {
			continue
		}
		nextIDs[auth.ID] = struct{}{}
		if _, errUpsert := h.repo.UpsertAuth(ctx, auth, "upsert"); errUpsert != nil {
			return errUpsert
		}
	}

	existingAuths, errExisting := h.repo.ListAuths(ctx)
	if errExisting != nil {
		return errExisting
	}
	for _, existing := range existingAuths {
		if !isOAuthPayloadAuth(existing, fileUUID) {
			continue
		}
		if _, ok := nextIDs[existing.ID]; ok {
			continue
		}
		if errDelete := h.repo.SoftDeleteAuth(ctx, existing.ID); errDelete != nil {
			return errDelete
		}
	}
	return nil
}

func isOAuthPayloadAuth(auth *coreauth.Auth, fileUUID string) bool {
	fileUUID = strings.TrimSpace(fileUUID)
	if auth == nil || fileUUID == "" {
		return false
	}
	if strings.TrimSpace(auth.ID) == fileUUID {
		return true
	}
	if auth.Attributes != nil {
		return strings.TrimSpace(auth.Attributes["gemini_virtual_parent"]) == fileUUID
	}
	return false
}

func (h *Handler) findOAuthAuth(ctx context.Context, identifier authIdentifier) (*coreauth.Auth, error) {
	auths, errAuths := h.repo.ListAuths(ctx)
	if errAuths != nil {
		return nil, errAuths
	}
	return findOAuthAuthInList(auths, identifier), nil
}

func (h *Handler) deleteOAuthAuthAndChildren(ctx context.Context, auth *coreauth.Auth, auths []*coreauth.Auth) error {
	if auth == nil {
		return nil
	}
	if errDelete := h.repo.SoftDeleteAuth(ctx, auth.ID); errDelete != nil {
		return errDelete
	}
	for _, child := range auths {
		if child == nil || child.Attributes == nil {
			continue
		}
		if strings.TrimSpace(child.Attributes["gemini_virtual_parent"]) != auth.ID {
			continue
		}
		if errDelete := h.repo.SoftDeleteAuth(ctx, child.ID); errDelete != nil {
			return errDelete
		}
	}
	return nil
}

type authIdentifier struct {
	ID    string
	Name  string
	Index *int
}

func authIdentifierFromRequest(c *gin.Context) authIdentifier {
	identifier := authIdentifier{
		ID:   firstNonEmptyQuery(c, "id", "uuid", "auth_index"),
		Name: firstNonEmptyQuery(c, "name", "file", "filename"),
	}
	if idxRaw := strings.TrimSpace(c.Query("index")); idxRaw != "" {
		if idx, errAtoi := strconv.Atoi(idxRaw); errAtoi == nil {
			identifier.Index = &idx
		}
	}
	return identifier
}

func authIdentifierFromBodyAndRequest(c *gin.Context, body map[string]any) authIdentifier {
	identifier := authIdentifierFromRequest(c)
	for _, key := range []string{"id", "uuid", "auth_index", "index"} {
		if identifier.ID == "" {
			identifier.ID = stringFromAny(body[key])
		}
	}
	if identifier.Name == "" {
		for _, key := range []string{"name", "file", "filename"} {
			identifier.Name = stringFromAny(body[key])
			if identifier.Name != "" {
				break
			}
		}
	}
	return identifier
}

func findOAuthAuthInList(auths []*coreauth.Auth, identifier authIdentifier) *coreauth.Auth {
	filtered := make([]*coreauth.Auth, 0, len(auths))
	for _, auth := range auths {
		if isOAuthAuth(auth) {
			filtered = append(filtered, auth)
		}
	}
	if identifier.Index != nil && *identifier.Index >= 0 && *identifier.Index < len(filtered) {
		return filtered[*identifier.Index]
	}
	for _, auth := range filtered {
		if identifier.ID != "" && (auth.ID == identifier.ID || auth.Index == identifier.ID) {
			return auth
		}
		if identifier.Name != "" && authFileName(auth) == identifier.Name {
			return auth
		}
	}
	return nil
}

func isOAuthAuth(auth *coreauth.Auth) bool {
	if auth == nil || auth.Metadata == nil {
		return false
	}
	if auth.Attributes != nil && strings.HasPrefix(strings.TrimSpace(auth.Attributes["source"]), "config:") {
		return false
	}
	typeValue := stringFromAny(auth.Metadata["type"])
	return typeValue != ""
}

func authFileEntry(auth *coreauth.Auth) gin.H {
	entry := gin.H{
		"id":           auth.ID,
		"auth_index":   auth.ID,
		"name":         authFileName(auth),
		"type":         auth.Provider,
		"provider":     auth.Provider,
		"label":        auth.Label,
		"status":       auth.Status,
		"disabled":     auth.Disabled,
		"unavailable":  auth.Unavailable,
		"runtime_only": auth.Attributes != nil && strings.EqualFold(auth.Attributes["runtime_only"], "true"),
		"source":       "db",
	}
	if email := stringFromAny(auth.Metadata["email"]); email != "" {
		entry["email"] = email
	}
	if priority := priorityFromAuth(auth); priority != nil {
		entry["priority"] = *priority
	}
	if note := noteFromAuth(auth); note != "" {
		entry["note"] = note
	}
	if !auth.CreatedAt.IsZero() {
		entry["created_at"] = auth.CreatedAt
	}
	if !auth.UpdatedAt.IsZero() {
		entry["updated_at"] = auth.UpdatedAt
		entry["modtime"] = auth.UpdatedAt
	}
	return entry
}

func authFileName(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Metadata != nil {
		if name := stringFromAny(auth.Metadata["filename"]); name != "" {
			return name
		}
	}
	return strings.TrimSpace(auth.ID) + ".json"
}

func applyOAuthFieldPatch(auth *coreauth.Auth, fields map[string]any) (bool, error) {
	changed := false
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	if value, ok, errString := optionalStringField(fields, "prefix"); errString != nil {
		return false, errString
	} else if ok {
		prefix := strings.TrimSpace(value)
		auth.Prefix = prefix
		if prefix == "" {
			delete(auth.Metadata, "prefix")
		} else {
			auth.Metadata["prefix"] = prefix
		}
		changed = true
	}
	if value, ok, errString := optionalStringField(fields, "proxy_url", "proxy-url"); errString != nil {
		return false, errString
	} else if ok {
		proxyURL := strings.TrimSpace(value)
		auth.ProxyURL = proxyURL
		if proxyURL == "" {
			delete(auth.Metadata, "proxy_url")
		} else {
			auth.Metadata["proxy_url"] = proxyURL
		}
		changed = true
	}
	if headers, ok, errHeaders := optionalHeadersField(fields, "headers"); errHeaders != nil {
		return false, errHeaders
	} else if ok && len(headers) > 0 {
		if applyOAuthHeadersPatch(auth, headers) {
			changed = true
		}
	}
	if priority, ok, errPriority := optionalIntField(fields, "priority"); errPriority != nil {
		return false, errPriority
	} else if ok {
		if priority == 0 {
			delete(auth.Attributes, "priority")
			delete(auth.Metadata, "priority")
		} else {
			auth.Attributes["priority"] = strconv.Itoa(priority)
			auth.Metadata["priority"] = priority
		}
		changed = true
	}
	if value, ok, errString := optionalStringField(fields, "note"); errString != nil {
		return false, errString
	} else if ok {
		note := strings.TrimSpace(value)
		if note == "" {
			delete(auth.Attributes, "note")
			delete(auth.Metadata, "note")
		} else {
			auth.Attributes["note"] = note
			auth.Metadata["note"] = note
		}
		changed = true
	}
	return changed, nil
}

func applyOAuthHeadersPatch(auth *coreauth.Auth, headers map[string]string) bool {
	currentHeaders := coreauth.ExtractCustomHeadersFromMetadata(auth.Metadata)
	nextHeaders := make(map[string]string, len(currentHeaders))
	for name, value := range currentHeaders {
		nextHeaders[name] = value
	}
	headerChanged := false
	for key, value := range headers {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		headerValue := strings.TrimSpace(value)
		if headerValue == "" {
			if _, ok := nextHeaders[name]; ok {
				headerChanged = true
			}
			delete(nextHeaders, name)
			delete(auth.Attributes, "header:"+name)
			continue
		}
		if currentValue, ok := nextHeaders[name]; !ok || currentValue != headerValue {
			headerChanged = true
		}
		nextHeaders[name] = headerValue
		auth.Attributes["header:"+name] = headerValue
	}
	if !headerChanged {
		return false
	}
	if len(nextHeaders) == 0 {
		delete(auth.Metadata, "headers")
		return true
	}
	metadataHeaders := make(map[string]any, len(nextHeaders))
	for name, value := range nextHeaders {
		metadataHeaders[name] = value
	}
	auth.Metadata["headers"] = metadataHeaders
	return true
}

func requiredBoolField(fields map[string]any, key string) (bool, error) {
	raw, ok := fields[key]
	if !ok {
		return false, fmt.Errorf("%s is required", key)
	}
	value, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("%s must be boolean", key)
	}
	return value, nil
}

func optionalStringField(fields map[string]any, keys ...string) (string, bool, error) {
	for _, key := range keys {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		value, ok := raw.(string)
		if !ok {
			return "", true, fmt.Errorf("%s must be string", key)
		}
		return value, true, nil
	}
	return "", false, nil
}

func optionalIntField(fields map[string]any, key string) (int, bool, error) {
	raw, ok := fields[key]
	if !ok {
		return 0, false, nil
	}
	value, ok := raw.(float64)
	if !ok {
		return 0, true, fmt.Errorf("%s must be integer", key)
	}
	intValue := int(value)
	if float64(intValue) != value {
		return 0, true, fmt.Errorf("%s must be integer", key)
	}
	return intValue, true, nil
}

func optionalHeadersField(fields map[string]any, key string) (map[string]string, bool, error) {
	raw, ok := fields[key]
	if !ok {
		return nil, false, nil
	}
	if raw == nil {
		return nil, true, nil
	}
	values, ok := raw.(map[string]any)
	if !ok {
		return nil, true, fmt.Errorf("%s must be object", key)
	}
	headers := make(map[string]string, len(values))
	for name, value := range values {
		headerValue, ok := value.(string)
		if !ok {
			return nil, true, fmt.Errorf("%s.%s must be string", key, name)
		}
		headers[name] = headerValue
	}
	return headers, true, nil
}

func multipartHeaders(c *gin.Context) ([]*multipart.FileHeader, error) {
	form, errForm := c.MultipartForm()
	if errForm != nil {
		return nil, errForm
	}
	if form == nil {
		return nil, nil
	}
	keys := make([]string, 0, len(form.File))
	for key := range form.File {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	headers := make([]*multipart.FileHeader, 0)
	for _, key := range keys {
		headers = append(headers, form.File[key]...)
	}
	return headers, nil
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case fmt.Stringer:
		return strings.TrimSpace(typed.String())
	default:
		return ""
	}
}

func firstStringField(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringFromAny(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "1" || strings.EqualFold(typed, "true")
	default:
		return false
	}
}

func priorityFromAuth(auth *coreauth.Auth) *int {
	if auth == nil {
		return nil
	}
	if auth.Attributes != nil {
		if priorityRaw := strings.TrimSpace(auth.Attributes["priority"]); priorityRaw != "" {
			if priority, errAtoi := strconv.Atoi(priorityRaw); errAtoi == nil {
				return &priority
			}
		}
	}
	if auth.Metadata != nil {
		if priorityRaw, ok := auth.Metadata["priority"].(float64); ok {
			priority := int(priorityRaw)
			return &priority
		}
	}
	return nil
}

func noteFromAuth(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Attributes != nil {
		if note := strings.TrimSpace(auth.Attributes["note"]); note != "" {
			return note
		}
	}
	if auth.Metadata != nil {
		return stringFromAny(auth.Metadata["note"])
	}
	return ""
}
