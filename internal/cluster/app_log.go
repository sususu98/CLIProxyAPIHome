package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type AppLogRecord struct {
	ID        uint      `gorm:"column:id;primaryKey;autoIncrement;index:idx_app_log_time_order,priority:2"`
	Timestamp time.Time `gorm:"column:timestamp;not null;index:idx_app_log_timestamp;index:idx_app_log_time_order,priority:1,sort:desc;index:idx_app_log_client_time,priority:2,sort:desc;index:idx_app_log_level_time,priority:2,sort:desc"`
	ClientIP  string    `gorm:"column:client_ip;index:idx_app_log_client_ip;index:idx_app_log_client_time,priority:1"`
	RequestID string    `gorm:"column:request_id;index:idx_app_log_request_id"`
	HomeIP    string    `gorm:"column:home_ip;index:idx_app_log_home_ip"`
	Level     string    `gorm:"column:level;index:idx_app_log_level;index:idx_app_log_level_time,priority:1"`
	Line      string    `gorm:"column:line;type:text;not null"`
	CreatedAt time.Time `gorm:"column:created_at;not null;index:idx_app_log_created_at"`
}

// TableName returns the database table name.
func (AppLogRecord) TableName() string {
	return "app_log"
}

// AppLogRecordFromPayload creates an app log record from a CPA payload.
func AppLogRecordFromPayload(clientIP string, homeIP string, payload string) (*AppLogRecord, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, fmt.Errorf("app log payload is empty")
	}
	if !json.Valid([]byte(payload)) {
		return nil, fmt.Errorf("app log payload is invalid json")
	}

	line := strings.TrimRight(gjson.Get(payload, "line").String(), "\r\n")
	if strings.TrimSpace(line) == "" {
		return nil, fmt.Errorf("app log line is required")
	}

	clientIP = strings.TrimSpace(clientIP)
	if clientIP == "" {
		clientIP = "unknown"
	}
	now := time.Now().UTC()

	return &AppLogRecord{
		Timestamp: appLogTimestampFromPayload(payload, now),
		ClientIP:  clientIP,
		RequestID: strings.TrimSpace(gjson.Get(payload, "request_id").String()),
		HomeIP:    strings.TrimSpace(homeIP),
		Level:     normalizeAppLogLevel(gjson.Get(payload, "level").String()),
		Line:      line,
		CreatedAt: now,
	}, nil
}

// AppendAppLog appends a CPA application log record.
func (r *Repository) AppendAppLog(ctx context.Context, clientIP string, homeIP string, payload string) (*AppLogRecord, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}

	record, errRecord := AppLogRecordFromPayload(clientIP, homeIP, payload)
	if errRecord != nil {
		return nil, errRecord
	}
	if errCreate := db.WithContext(contextOrBackground(ctx)).Create(record).Error; errCreate != nil {
		return nil, errCreate
	}
	return record, nil
}

func appLogTimestampFromPayload(payload string, fallback time.Time) time.Time {
	for _, key := range []string{"timestamp", "time", "ts"} {
		raw := strings.TrimSpace(gjson.Get(payload, key).String())
		if raw == "" {
			continue
		}
		if parsed, errParse := time.Parse(time.RFC3339Nano, raw); errParse == nil {
			return parsed.UTC()
		}
	}
	return fallback.UTC()
}

func normalizeAppLogLevel(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "warning" {
		return "warn"
	}
	return level
}
