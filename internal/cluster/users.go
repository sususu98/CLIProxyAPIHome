package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

// ErrUserNotFound indicates that the referenced user record does not exist.
var ErrUserNotFound = errors.New("user not found")

// IsUserConflictError reports whether an error is a username uniqueness conflict.
func IsUserConflictError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "idx_user_username_active_unique") {
		return true
	}
	if strings.Contains(message, "unique constraint") && strings.Contains(message, "user") && strings.Contains(message, "username") {
		return true
	}
	return strings.Contains(message, "duplicate key") && strings.Contains(message, "username")
}

type UserUpdate struct {
	Username *string
	Password *string
	Credits  *float64
	MFA      *JSONB
	Passkey  *JSONB
}

// ListUsers returns users.
func (r *Repository) ListUsers(ctx context.Context) ([]UserRecord, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}

	var records []UserRecord
	if errFind := db.WithContext(contextOrBackground(ctx)).Order("id").Find(&records).Error; errFind != nil {
		return nil, errFind
	}
	return records, nil
}

// GetUser returns a user by ID.
func (r *Repository) GetUser(ctx context.Context, id uint) (*UserRecord, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}
	if id == 0 {
		return nil, fmt.Errorf("user id is required")
	}

	record := &UserRecord{}
	if errFirst := db.WithContext(contextOrBackground(ctx)).Where("id = ?", id).First(record).Error; errFirst != nil {
		return nil, errFirst
	}
	return record, nil
}

// GetUserByUsername returns a user by username.
func (r *Repository) GetUserByUsername(ctx context.Context, username string) (*UserRecord, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}

	record := &UserRecord{}
	if errFirst := db.WithContext(contextOrBackground(ctx)).Where("username = ?", username).Order("id").First(record).Error; errFirst != nil {
		return nil, errFirst
	}
	return record, nil
}

// CreateUser creates a user.
func (r *Repository) CreateUser(ctx context.Context, update UserUpdate) (*UserRecord, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}
	if update.Username == nil {
		return nil, fmt.Errorf("username is required")
	}
	username := strings.TrimSpace(*update.Username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}

	record := &UserRecord{
		Username: username,
	}
	if update.Password != nil {
		record.Password = *update.Password
	}
	if update.Credits != nil {
		record.Credits = *update.Credits
	}
	if update.MFA != nil {
		record.MFA = cloneJSONB(*update.MFA)
	}
	if update.Passkey != nil {
		record.Passkey = cloneJSONB(*update.Passkey)
	}
	if errCreate := db.WithContext(contextOrBackground(ctx)).Create(record).Error; errCreate != nil {
		return nil, errCreate
	}
	return record, nil
}

// UpdateUser updates a user.
func (r *Repository) UpdateUser(ctx context.Context, id uint, update UserUpdate) (*UserRecord, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}
	if id == 0 {
		return nil, fmt.Errorf("user id is required")
	}

	record := &UserRecord{}
	ctx = contextOrBackground(ctx)
	errTransaction := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if errFirst := tx.Where("id = ?", id).First(record).Error; errFirst != nil {
			return errFirst
		}
		if update.Username != nil {
			username := strings.TrimSpace(*update.Username)
			if username == "" {
				return fmt.Errorf("username is required")
			}
			record.Username = username
		}
		if update.Password != nil {
			record.Password = *update.Password
		}
		if update.Credits != nil {
			record.Credits = *update.Credits
		}
		if update.MFA != nil {
			record.MFA = cloneJSONB(*update.MFA)
		}
		if update.Passkey != nil {
			record.Passkey = cloneJSONB(*update.Passkey)
		}
		return tx.Save(record).Error
	})
	if errTransaction != nil {
		return nil, errTransaction
	}
	return record, nil
}

// DeleteUser deletes a user.
func (r *Repository) DeleteUser(ctx context.Context, id uint) error {
	db, errDB := r.database()
	if errDB != nil {
		return errDB
	}
	if id == 0 {
		return fmt.Errorf("user id is required")
	}

	record := UserRecord{}
	ctx = contextOrBackground(ctx)
	if errFirst := db.WithContext(ctx).Where("id = ?", id).First(&record).Error; errFirst != nil {
		return errFirst
	}
	return db.WithContext(ctx).Delete(&record).Error
}

// ParseUserRecordID parses a positive unsigned user record ID.
func ParseUserRecordID(value string) (uint, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("user id is required")
	}
	parsed, errParse := strconv.ParseUint(value, 10, 64)
	if errParse != nil {
		return 0, errParse
	}
	if parsed == 0 {
		return 0, fmt.Errorf("user id is required")
	}
	return uint(parsed), nil
}

func normalizeOptionalUserID(userID *uint) *uint {
	if userID == nil || *userID == 0 {
		return nil
	}
	value := *userID
	return &value
}

func ensureUserExists(ctx context.Context, tx *gorm.DB, id uint) error {
	if tx == nil {
		return fmt.Errorf("database connection is nil")
	}
	if id == 0 {
		return nil
	}
	record := UserRecord{}
	errFirst := tx.WithContext(contextOrBackground(ctx)).Where("id = ?", id).First(&record).Error
	if errors.Is(errFirst, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: %d", ErrUserNotFound, id)
	}
	return errFirst
}

func cloneJSONB(value JSONB) JSONB {
	if len(value) == 0 {
		return nil
	}
	return append(JSONB(nil), value...)
}

// NormalizeJSONB validates and normalizes raw JSON for JSONB fields.
func NormalizeJSONB(raw json.RawMessage) (*JSONB, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("invalid json value")
	}
	if string(raw) == "null" {
		empty := JSONB(nil)
		return &empty, nil
	}
	value := JSONB(append([]byte(nil), raw...))
	return &value, nil
}
