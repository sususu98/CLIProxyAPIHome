package management

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type optionalJSONFloat struct {
	set   bool
	clear bool
	value float64
}

func (o *optionalJSONFloat) UnmarshalJSON(data []byte) error {
	o.set = true
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		o.clear = true
		return nil
	}
	var value float64
	if errUnmarshal := json.Unmarshal(data, &value); errUnmarshal != nil {
		return errUnmarshal
	}
	o.value = value
	return nil
}

type userWriteRequest struct {
	Username         *string           `json:"username"`
	UserName         *string           `json:"user_name"`
	UserNameDash     *string           `json:"user-name"`
	Password         *string           `json:"password"`
	Credits          *float64          `json:"credits"`
	CreditsUnlimited *bool             `json:"credits_unlimited"`
	Timezone         *string           `json:"timezone"`
	Limit5h          optionalJSONFloat `json:"limit_5h_credits"`
	WindowMode5h     *string           `json:"window_mode_5h"`
	Limit1d          optionalJSONFloat `json:"limit_1d_credits"`
	WindowMode1d     *string           `json:"window_mode_1d"`
	Limit7d          optionalJSONFloat `json:"limit_7d_credits"`
	WindowMode7d     *string           `json:"window_mode_7d"`
	WeekResetDay     *int              `json:"week_reset_day"`
	WeekResetHour    *int              `json:"week_reset_hour"`
	Limit30d         optionalJSONFloat `json:"limit_30d_credits"`
	WindowMode30d    *string           `json:"window_mode_30d"`
	MFA              json.RawMessage   `json:"mfa"`
	Passkey          json.RawMessage   `json:"passkey"`
}

type userPeriodLimitResetRequest struct {
	Windows []string `json:"windows"`
	Mode    string   `json:"mode"`
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
		if isUserPeriodLimitValidationError(errCreate) {
			respondError(c, http.StatusBadRequest, "invalid body", errCreate)
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
		if isUserPeriodLimitValidationError(errUpdate) {
			respondError(c, http.StatusBadRequest, "invalid body", errUpdate)
			return
		}
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

// GetUserPeriodLimits returns period-limit configuration and current usage.
func (h *Handler) GetUserPeriodLimits(c *gin.Context) {
	id, ok := userIDFromParam(c)
	if !ok {
		return
	}
	ctx, cancel := h.requestContext(c)
	defer cancel()
	status, errStatus := h.repo.BuildUserPeriodLimitsStatus(ctx, id, time.Now().UTC())
	if errStatus != nil {
		respondUserRecordError(c, "user_period_limits_load_failed", errStatus)
		return
	}
	c.JSON(http.StatusOK, status)
}

// ResetUserPeriodLimits resets period-limit counters for a user.
func (h *Handler) ResetUserPeriodLimits(c *gin.Context) {
	id, ok := userIDFromParam(c)
	if !ok {
		return
	}
	var body userPeriodLimitResetRequest
	if c.Request != nil && c.Request.Body != nil {
		decoder := json.NewDecoder(c.Request.Body)
		if errDecode := decoder.Decode(&body); errDecode != nil && !errors.Is(errDecode, io.EOF) {
			respondError(c, http.StatusBadRequest, "invalid body", errDecode)
			return
		}
	}
	ctx, cancel := h.requestContext(c)
	defer cancel()
	result, errReset := h.repo.ResetUserPeriodLimits(ctx, id, body.Windows, body.Mode, time.Now().UTC())
	if errReset != nil {
		if isUserPeriodLimitValidationError(errReset) {
			respondError(c, http.StatusBadRequest, "invalid body", errReset)
			return
		}
		respondUserRecordError(c, "user_period_limits_reset_failed", errReset)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"user_id": result.UserID,
		"reset": gin.H{
			"mode":    result.Mode,
			"windows": result.Windows,
			"at":      result.At,
		},
		"limits": result.Limits,
	})
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
	update.CreditsUnlimited = body.CreditsUnlimited
	update.Timezone = body.Timezone
	update.Limit5hCredits = optionalFloatToUpdate(body.Limit5h)
	update.WindowMode5h = body.WindowMode5h
	update.Limit1dCredits = optionalFloatToUpdate(body.Limit1d)
	update.WindowMode1d = body.WindowMode1d
	update.Limit7dCredits = optionalFloatToUpdate(body.Limit7d)
	update.WindowMode7d = body.WindowMode7d
	update.WeekResetDay = body.WeekResetDay
	update.WeekResetHour = body.WeekResetHour
	update.Limit30dCredits = optionalFloatToUpdate(body.Limit30d)
	update.WindowMode30d = body.WindowMode30d
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

func optionalFloatToUpdate(value optionalJSONFloat) cluster.OptionalFloatUpdate {
	if !value.set {
		return cluster.OptionalFloatUpdate{}
	}
	if value.clear {
		return cluster.OptionalFloatUpdate{Set: true, Clear: true}
	}
	return cluster.OptionalFloatUpdate{Set: true, Value: value.value}
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

func isUserPeriodLimitValidationError(err error) bool {
	if err == nil {
		return false
	}
	var configErr cluster.PeriodLimitConfigError
	if errors.As(err, &configErr) {
		return true
	}
	// Fallback for wrapped field-prefix errors from applyUserPeriodLimitUpdate.
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "limit_"),
		strings.Contains(message, "window mode"),
		strings.Contains(message, "window_mode"),
		strings.Contains(message, "timezone"),
		strings.Contains(message, "week_reset"),
		strings.Contains(message, "period window"),
		strings.Contains(message, "reset mode"),
		strings.Contains(message, "non-negative"):
		return true
	default:
		return false
	}
}

func userRecordToMap(record *cluster.UserRecord) gin.H {
	if record == nil {
		return gin.H{}
	}
	timezone := strings.TrimSpace(record.Timezone)
	if timezone == "" {
		timezone = cluster.DefaultUserTimezone
	}
	windowMode5h := cluster.CanonicalPeriodWindowMode(record.WindowMode5h, false)
	windowMode1d := cluster.CanonicalPeriodWindowMode(record.WindowMode1d, true)
	windowMode7d := cluster.CanonicalPeriodWindowMode(record.WindowMode7d, true)
	windowMode30d := cluster.CanonicalPeriodWindowMode(record.WindowMode30d, true)
	weekResetDay := record.WeekResetDay
	if weekResetDay == 0 {
		weekResetDay = cluster.DefaultWeekResetDay
	}
	return gin.H{
		"id":                      record.ID,
		"username":                record.Username,
		"password_set":            record.Password != "",
		"credits":                 record.Credits,
		"credits_unlimited":       record.CreditsUnlimited,
		"timezone":                timezone,
		"limit_5h_credits":        record.Limit5hCredits,
		"window_mode_5h":          windowMode5h,
		"limit_1d_credits":        record.Limit1dCredits,
		"window_mode_1d":          windowMode1d,
		"limit_7d_credits":        record.Limit7dCredits,
		"window_mode_7d":          windowMode7d,
		"week_reset_day":          weekResetDay,
		"week_reset_hour":         record.WeekResetHour,
		"limit_30d_credits":       record.Limit30dCredits,
		"window_mode_30d":         windowMode30d,
		"period_window_start_5h":  record.PeriodWindowStart5h,
		"period_window_start_1d":  record.PeriodWindowStart1d,
		"period_window_start_7d":  record.PeriodWindowStart7d,
		"period_window_start_30d": record.PeriodWindowStart30d,
		"usage_epoch_5h":          record.UsageEpoch5h,
		"usage_epoch_1d":          record.UsageEpoch1d,
		"usage_epoch_7d":          record.UsageEpoch7d,
		"usage_epoch_30d":         record.UsageEpoch30d,
		"mfa":                     record.MFA,
		"passkey":                 record.Passkey,
		"created_at":              record.CreatedAt,
		"updated_at":              record.UpdatedAt,
		"deleted_at":              deletedAtValue(record.DeletedAt),
	}
}
