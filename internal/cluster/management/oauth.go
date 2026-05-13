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

// ListAuthFiles returns an auth files.
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

// DownloadAuthFile downloads an auth file.
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

// UploadAuthFile handles upload auth file.
func (h *Handler) UploadAuthFile(c *gin.Context) {
	// Validate request inputs before mutating persisted state.
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
	name, errStore := h.storeOAuthPayload(c, body, "")
	if errStore != nil {
		respondError(c, http.StatusBadRequest, "upload_failed", errStore)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "name": name})
}

// DeleteAuthFile deletes an auth file.
func (h *Handler) DeleteAuthFile(c *gin.Context) {
	// Validate request inputs before mutating persisted state.
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

// PatchAuthFileStatus applies a partial update to an auth file status.
func (h *Handler) PatchAuthFileStatus(c *gin.Context) {
	// Validate request inputs before mutating persisted state.
	var req struct {
		Name     string `json:"name"`
		Disabled *bool  `json:"disabled"`
	}
	if errBindJSON := c.ShouldBindJSON(&req); errBindJSON != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if req.Disabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "disabled is required"})
		return
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()
	auth, errAuth := h.findOAuthAuth(ctx, authIdentifier{ID: name, Name: name})
	if errAuth != nil {
		respondError(c, http.StatusInternalServerError, "auth_load_failed", errAuth)
		return
	}
	if auth == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "auth file not found"})
		return
	}
	disabled := *req.Disabled
	auths, errAuths := h.repo.ListAuths(ctx)
	if errAuths != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update auth: %v", errAuths)})
		return
	}
	targets := authStatusUpdateTargets(auth, auths)
	for _, target := range targets {
		applyAuthDisabledStatus(target, disabled)
		if _, errUpsert := h.repo.UpsertAuth(ctx, target, "update"); errUpsert != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update auth: %v", errUpsert)})
			return
		}
	}
	if errRefresh := h.refreshAuths(ctx); errRefresh != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to update auth: %v", errRefresh)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "disabled": disabled})
}

// PatchAuthFileFields applies a partial update to an auth file fields.
func (h *Handler) PatchAuthFileFields(c *gin.Context) {
	// Validate request inputs before mutating persisted state.
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

// PostOAuthCallback handles a post o auth callback.
func (h *Handler) PostOAuthCallback(c *gin.Context) {
	h.handleOAuthCallback(c)
}

// storeUploadedOAuth stores an uploaded o auth.
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
	return h.storeOAuthPayload(c, data, header.Filename)
}

// storeOAuthPayload stores an o auth payload.
func (h *Handler) storeOAuthPayload(c *gin.Context, raw []byte, originalFilename string) (string, error) {
	ctx, cancel := h.requestContext(c)
	defer cancel()
	return h.storeOAuthPayloadWithContext(ctx, raw, originalFilename)
}

// storeOAuthPayloadWithContext stores an o auth payload with context.
func (h *Handler) storeOAuthPayloadWithContext(ctx context.Context, raw []byte, originalFilename string) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", fmt.Errorf("empty credential json")
	}
	updatedRaw, fileUUID, _, errUUID := cluster.EnsureOAuthPayloadUUID(raw)
	if errUUID != nil {
		return "", errUUID
	}

	auths := h.synthesizeOAuthPayload(updatedRaw, fileUUID, originalFilename)
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

// synthesizeOAuthPayload handles a synthesize o auth payload.
func (h *Handler) synthesizeOAuthPayload(raw []byte, fileUUID string, originalFilename string) []*coreauth.Auth {
	// Resolve credential context before calling upstream OAuth services.
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
	auths := synthesizer.SynthesizeAuthFile(sctx, authPath, raw)
	cluster.ApplyOriginalAuthFileName(auths, originalFilename)
	return auths
}

// replaceOAuthPayloadAuths handles a replace o auth payload auths.
func (h *Handler) replaceOAuthPayloadAuths(ctx context.Context, fileUUID string, auths []*coreauth.Auth) error {
	// Resolve credential context before calling upstream OAuth services.
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

// isOAuthPayloadAuth reports whether o auth payload auth.
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

// findOAuthAuth handles a find o auth auth.
func (h *Handler) findOAuthAuth(ctx context.Context, identifier authIdentifier) (*coreauth.Auth, error) {
	auths, errAuths := h.repo.ListAuths(ctx)
	if errAuths != nil {
		return nil, errAuths
	}
	return findOAuthAuthInList(auths, identifier), nil
}

// deleteOAuthAuthAndChildren deletes an o auth auth and children.
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

// authIdentifierFromRequest derives auth identifier from request.
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

// authIdentifierFromBodyAndRequest derives auth identifier from body and request.
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

// findOAuthAuthInList handles a find o auth auth in list.
func findOAuthAuthInList(auths []*coreauth.Auth, identifier authIdentifier) *coreauth.Auth {
	// Resolve credential context before calling upstream OAuth services.
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
	}
	if identifier.Name != "" {
		for _, auth := range filtered {
			if authFileDisplayName(auth) == identifier.Name {
				return auth
			}
		}
		for _, auth := range filtered {
			if authFileName(auth) == identifier.Name && !isVirtualOAuthAuth(auth) {
				return auth
			}
		}
	}
	for _, auth := range filtered {
		if identifier.Name != "" && authFileName(auth) == identifier.Name {
			return auth
		}
	}
	return nil
}

// isOAuthAuth reports whether o auth auth.
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

// isVirtualOAuthAuth reports whether virtual o auth auth.
func isVirtualOAuthAuth(auth *coreauth.Auth) bool {
	if auth == nil {
		return false
	}
	if auth.Metadata != nil && boolFromAny(auth.Metadata["virtual"]) {
		return true
	}
	return auth.Attributes != nil && strings.EqualFold(auth.Attributes["runtime_only"], "true")
}

// isGeminiVirtualPrimaryAuth reports whether auth is the source record for virtual Gemini auths.
func isGeminiVirtualPrimaryAuth(auth *coreauth.Auth) bool {
	if auth == nil || auth.Attributes == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(auth.Attributes["gemini_virtual_primary"]), "true")
}

// authStatusUpdateTargets returns the auth records affected by a status toggle.
func authStatusUpdateTargets(auth *coreauth.Auth, auths []*coreauth.Auth) []*coreauth.Auth {
	if auth == nil {
		return nil
	}
	targets := []*coreauth.Auth{auth}
	if !isGeminiVirtualPrimaryAuth(auth) {
		return targets
	}
	seen := map[string]struct{}{strings.TrimSpace(auth.ID): {}}
	for _, child := range auths {
		if child == nil || child.Attributes == nil {
			continue
		}
		if strings.TrimSpace(child.Attributes["gemini_virtual_parent"]) != auth.ID {
			continue
		}
		childID := strings.TrimSpace(child.ID)
		if _, ok := seen[childID]; ok {
			continue
		}
		seen[childID] = struct{}{}
		targets = append(targets, child)
	}
	return targets
}

// applyAuthDisabledStatus updates disabled status fields and persisted metadata.
func applyAuthDisabledStatus(auth *coreauth.Auth, disabled bool) {
	if auth == nil {
		return
	}
	auth.Disabled = disabled
	if disabled {
		auth.Status = coreauth.StatusDisabled
		auth.StatusMessage = "disabled via management API"
	} else {
		auth.Status = coreauth.StatusActive
		auth.StatusMessage = ""
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["disabled"] = disabled
	auth.UpdatedAt = time.Now()
}

// authFileEntry handles an auth file entry.
func authFileEntry(auth *coreauth.Auth) gin.H {
	// Validate request inputs before mutating persisted state.
	entry := gin.H{
		"id":             auth.ID,
		"auth_index":     auth.ID,
		"name":           authFileDisplayName(auth),
		"file_name":      authFileName(auth),
		"type":           auth.Provider,
		"provider":       auth.Provider,
		"label":          auth.Label,
		"status":         auth.Status,
		"status_message": auth.StatusMessage,
		"disabled":       auth.Disabled,
		"unavailable":    auth.Unavailable,
		"runtime_only":   auth.Attributes != nil && strings.EqualFold(auth.Attributes["runtime_only"], "true"),
		"source":         "db",
	}
	if isGeminiVirtualPrimaryAuth(auth) {
		entry["virtual_primary"] = true
		if auth.Attributes != nil {
			entry["virtual_children"] = strings.TrimSpace(auth.Attributes["virtual_children"])
		}
	}
	if isVirtualOAuthAuth(auth) {
		entry["virtual"] = true
		if parentID := geminiVirtualParentID(auth); parentID != "" {
			entry["virtual_parent_id"] = parentID
		}
		if projectID := geminiVirtualProjectID(auth); projectID != "" {
			entry["virtual_project"] = projectID
			entry["project_id"] = projectID
		}
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

// authFileName handles an auth file name.
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

// authFileDisplayName returns a stable management-list name for the auth entry.
func authFileDisplayName(auth *coreauth.Auth) string {
	name := authFileName(auth)
	if !isVirtualOAuthAuth(auth) {
		return name
	}
	projectID := geminiVirtualProjectID(auth)
	if projectID == "" {
		return name
	}
	baseName := strings.TrimSpace(name)
	if baseName == "" {
		baseName = strings.TrimSpace(auth.ID) + ".json"
	}
	return baseName + "::" + sanitizeVirtualDisplayPart(projectID)
}

// sanitizeVirtualDisplayPart normalizes virtual auth display suffixes.
func sanitizeVirtualDisplayPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "project"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_")
	return replacer.Replace(value)
}

// geminiVirtualParentID returns the parent auth id for a virtual Gemini auth.
func geminiVirtualParentID(auth *coreauth.Auth) string {
	if auth == nil || auth.Attributes == nil {
		return ""
	}
	return strings.TrimSpace(auth.Attributes["gemini_virtual_parent"])
}

// geminiVirtualProjectID returns the project id for a virtual Gemini auth.
func geminiVirtualProjectID(auth *coreauth.Auth) string {
	if auth == nil {
		return ""
	}
	if auth.Attributes != nil {
		if projectID := strings.TrimSpace(auth.Attributes["gemini_virtual_project"]); projectID != "" {
			return projectID
		}
	}
	if auth.Metadata != nil {
		return stringFromAny(auth.Metadata["project_id"])
	}
	return ""
}

// applyOAuthFieldPatch applies an o auth field patch.
func applyOAuthFieldPatch(auth *coreauth.Auth, fields map[string]any) (bool, error) {
	// Resolve credential context before calling upstream OAuth services.
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

// applyOAuthHeadersPatch applies an o auth headers patch.
func applyOAuthHeadersPatch(auth *coreauth.Auth, headers map[string]string) bool {
	// Resolve credential context before calling upstream OAuth services.
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

// requiredBoolField handles a required bool field.
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

// optionalStringField handles an optional string field.
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

// optionalIntField handles an optional int field.
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

// optionalHeadersField handles an optional headers field.
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

// multipartHeaders handles a multipart headers.
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

// stringFromAny derives string from any.
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

// firstStringField handles a first string field.
func firstStringField(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringFromAny(values[key]); value != "" {
			return value
		}
	}
	return ""
}

// boolFromAny derives bool from any.
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

// priorityFromAuth derives priority from auth.
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

// noteFromAuth derives note from auth.
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
