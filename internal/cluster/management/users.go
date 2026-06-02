package management

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type userWriteRequest struct {
	Username     *string         `json:"username"`
	UserName     *string         `json:"user_name"`
	UserNameDash *string         `json:"user-name"`
	Password     *string         `json:"password"`
	Credits      *float64        `json:"credits"`
	MFA          json.RawMessage `json:"mfa"`
	Passkey      json.RawMessage `json:"passkey"`
}

// ListUsers returns users.
func (h *Handler) ListUsers(c *gin.Context) {
	ctx, cancel := h.requestContext(c)
	defer cancel()

	records, errRecords := h.repo.ListUsers(ctx)
	if errRecords != nil {
		respondError(c, http.StatusInternalServerError, "user_load_failed", errRecords)
		return
	}
	items := make([]gin.H, 0, len(records))
	for _, record := range records {
		items = append(items, userRecordToMap(&record))
	}
	c.JSON(http.StatusOK, gin.H{"users": items})
}

// GetUser returns a user.
func (h *Handler) GetUser(c *gin.Context) {
	id, ok := userIDFromParam(c)
	if !ok {
		return
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()
	record, errRecord := h.repo.GetUser(ctx, id)
	if errRecord != nil {
		respondUserRecordError(c, "user_load_failed", errRecord)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": userRecordToMap(record)})
}

// CreateUser creates a user.
func (h *Handler) CreateUser(c *gin.Context) {
	var body userWriteRequest
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errBindJSON)
		return
	}
	update, ok := userUpdateFromRequest(c, body, true)
	if !ok {
		return
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()
	record, errCreate := h.repo.CreateUser(ctx, update)
	if errCreate != nil {
		if cluster.IsUserConflictError(errCreate) {
			respondError(c, http.StatusConflict, "user_exists", errCreate)
			return
		}
		respondError(c, http.StatusInternalServerError, "user_create_failed", errCreate)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": userRecordToMap(record)})
}

// UpdateUser updates a user.
func (h *Handler) UpdateUser(c *gin.Context) {
	id, ok := userIDFromParam(c)
	if !ok {
		return
	}
	var body userWriteRequest
	if errBindJSON := c.ShouldBindJSON(&body); errBindJSON != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errBindJSON)
		return
	}
	update, ok := userUpdateFromRequest(c, body, false)
	if !ok {
		return
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()
	record, errUpdate := h.repo.UpdateUser(ctx, id, update)
	if errUpdate != nil {
		respondUserRecordError(c, "user_update_failed", errUpdate)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": userRecordToMap(record)})
}

// DeleteUser deletes a user.
func (h *Handler) DeleteUser(c *gin.Context) {
	id, ok := userIDFromParam(c)
	if !ok {
		return
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()
	if errDelete := h.repo.DeleteUser(ctx, id); errDelete != nil {
		respondUserRecordError(c, "user_delete_failed", errDelete)
		return
	}
	respondOK(c)
}

func userUpdateFromRequest(c *gin.Context, body userWriteRequest, requireUsername bool) (cluster.UserUpdate, bool) {
	update := cluster.UserUpdate{}
	username := body.username()
	if username != nil {
		if strings.TrimSpace(*username) == "" {
			respondError(c, http.StatusBadRequest, "invalid body", errRequired("username"))
			return update, false
		}
		update.Username = username
	} else if requireUsername {
		respondError(c, http.StatusBadRequest, "invalid body", errRequired("username"))
		return update, false
	}
	password, errPassword := managementPasswordValue(body.Password)
	if errPassword != nil {
		respondError(c, http.StatusBadRequest, "invalid body", errPassword)
		return update, false
	}
	update.Password = password
	update.Credits = body.Credits
	if len(body.MFA) > 0 {
		mfa, errMFA := cluster.NormalizeJSONB(body.MFA)
		if errMFA != nil {
			respondError(c, http.StatusBadRequest, "invalid body", errMFA)
			return update, false
		}
		update.MFA = mfa
	}
	if len(body.Passkey) > 0 {
		passkey, errPasskey := cluster.NormalizeJSONB(body.Passkey)
		if errPasskey != nil {
			respondError(c, http.StatusBadRequest, "invalid body", errPasskey)
			return update, false
		}
		update.Passkey = passkey
	}
	return update, true
}

func (r userWriteRequest) username() *string {
	for _, value := range []*string{r.Username, r.UserName, r.UserNameDash} {
		if value == nil {
			continue
		}
		username := strings.TrimSpace(*value)
		return &username
	}
	return nil
}

func userIDFromParam(c *gin.Context) (uint, bool) {
	id, errID := cluster.ParseUserRecordID(c.Param("id"))
	if errID != nil {
		respondError(c, http.StatusBadRequest, "invalid id", errID)
		return 0, false
	}
	return id, true
}

func respondUserRecordError(c *gin.Context, code string, err error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respondError(c, http.StatusNotFound, "not_found", err)
		return
	}
	if cluster.IsUserConflictError(err) {
		respondError(c, http.StatusConflict, "user_exists", err)
		return
	}
	respondError(c, http.StatusInternalServerError, code, err)
}

func managementPasswordValue(password *string) (*string, error) {
	if password == nil {
		return nil, nil
	}
	value := *password
	if value == "" || isBcryptPasswordHash(value) {
		return &value, nil
	}
	hashed, errHash := bcrypt.GenerateFromPassword([]byte(value), bcrypt.DefaultCost)
	if errHash != nil {
		return nil, fmt.Errorf("password hash failed: %w", errHash)
	}
	next := string(hashed)
	return &next, nil
}

func isBcryptPasswordHash(value string) bool {
	if value == "" {
		return false
	}
	_, errCost := bcrypt.Cost([]byte(value))
	return errCost == nil
}

func userRecordToMap(record *cluster.UserRecord) gin.H {
	if record == nil {
		return gin.H{}
	}
	return gin.H{
		"id":           record.ID,
		"username":     record.Username,
		"password_set": record.Password != "",
		"credits":      record.Credits,
		"mfa":          record.MFA,
		"passkey":      record.Passkey,
		"created_at":   record.CreatedAt,
		"updated_at":   record.UpdatedAt,
		"deleted_at":   deletedAtValue(record.DeletedAt),
	}
}
