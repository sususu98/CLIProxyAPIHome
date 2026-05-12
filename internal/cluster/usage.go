package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type UsageRecord struct {
	ID                  uint      `gorm:"column:id;primaryKey;autoIncrement"`
	Timestamp           time.Time `gorm:"column:timestamp;type:timestamptz;not null;index:idx_usage_timestamp"`
	LatencyMS           int64     `gorm:"column:latency_ms;not null;default:0"`
	Source              string    `gorm:"column:source;index:idx_usage_source"`
	AuthIndex           string    `gorm:"column:auth_index;index:idx_usage_auth_index"`
	InputTokens         int64     `gorm:"column:input_tokens;not null;default:0"`
	OutputTokens        int64     `gorm:"column:output_tokens;not null;default:0"`
	ReasoningTokens     int64     `gorm:"column:reasoning_tokens;not null;default:0"`
	CachedTokens        int64     `gorm:"column:cached_tokens;not null;default:0"`
	CacheReadTokens     int64     `gorm:"column:cache_read_tokens;not null;default:0"`
	CacheCreationTokens int64     `gorm:"column:cache_creation_tokens;not null;default:0"`
	TotalTokens         int64     `gorm:"column:total_tokens;not null;default:0"`
	Failed              bool      `gorm:"column:failed;not null;default:false;index:idx_usage_failed"`
	FailStatusCode      int       `gorm:"column:fail_status_code;not null;default:0"`
	FailBody            string    `gorm:"column:fail_body;type:text"`
	Provider            string    `gorm:"column:provider;index:idx_usage_provider_model,priority:1"`
	Model               string    `gorm:"column:model;index:idx_usage_provider_model,priority:2"`
	Alias               string    `gorm:"column:alias"`
	Endpoint            string    `gorm:"column:endpoint;index:idx_usage_endpoint"`
	AuthType            string    `gorm:"column:auth_type"`
	APIKey              string    `gorm:"column:api_key"`
	RequestID           string    `gorm:"column:request_id;index:idx_usage_request_id"`
	TokensJSON          JSONB     `gorm:"column:tokens;type:jsonb"`
	FailJSON            JSONB     `gorm:"column:fail;type:jsonb"`
	PayloadJSON         JSONB     `gorm:"column:payload;type:jsonb;not null"`
	CreatedAt           time.Time `gorm:"column:created_at;type:timestamptz;not null"`
}

func (UsageRecord) TableName() string {
	return "usage"
}

func UsageRecordFromPayload(payload string) (*UsageRecord, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, fmt.Errorf("usage payload is empty")
	}
	if !json.Valid([]byte(payload)) {
		return nil, fmt.Errorf("usage payload is invalid json")
	}

	timestampRaw := strings.TrimSpace(gjson.Get(payload, "timestamp").String())
	if timestampRaw == "" {
		return nil, fmt.Errorf("usage timestamp is required")
	}
	timestamp, errTimestamp := time.Parse(time.RFC3339Nano, timestampRaw)
	if errTimestamp != nil {
		return nil, fmt.Errorf("parse usage timestamp: %w", errTimestamp)
	}

	record := &UsageRecord{
		Timestamp:           timestamp.UTC(),
		LatencyMS:           gjson.Get(payload, "latency_ms").Int(),
		Source:              strings.TrimSpace(gjson.Get(payload, "source").String()),
		AuthIndex:           strings.TrimSpace(gjson.Get(payload, "auth_index").String()),
		InputTokens:         gjson.Get(payload, "tokens.input_tokens").Int(),
		OutputTokens:        gjson.Get(payload, "tokens.output_tokens").Int(),
		ReasoningTokens:     gjson.Get(payload, "tokens.reasoning_tokens").Int(),
		CachedTokens:        gjson.Get(payload, "tokens.cached_tokens").Int(),
		CacheReadTokens:     gjson.Get(payload, "tokens.cache_read_tokens").Int(),
		CacheCreationTokens: gjson.Get(payload, "tokens.cache_creation_tokens").Int(),
		TotalTokens:         gjson.Get(payload, "tokens.total_tokens").Int(),
		Failed:              gjson.Get(payload, "failed").Bool(),
		FailStatusCode:      int(gjson.Get(payload, "fail.status_code").Int()),
		FailBody:            gjson.Get(payload, "fail.body").String(),
		Provider:            strings.TrimSpace(gjson.Get(payload, "provider").String()),
		Model:               strings.TrimSpace(gjson.Get(payload, "model").String()),
		Alias:               strings.TrimSpace(gjson.Get(payload, "alias").String()),
		Endpoint:            strings.TrimSpace(gjson.Get(payload, "endpoint").String()),
		AuthType:            strings.TrimSpace(gjson.Get(payload, "auth_type").String()),
		APIKey:              strings.TrimSpace(gjson.Get(payload, "api_key").String()),
		RequestID:           strings.TrimSpace(gjson.Get(payload, "request_id").String()),
		TokensJSON:          jsonbFromPayloadField(payload, "tokens"),
		FailJSON:            jsonbFromPayloadField(payload, "fail"),
		PayloadJSON:         JSONB(payload),
		CreatedAt:           time.Now().UTC(),
	}
	return record, nil
}

func (r *Repository) AppendUsage(ctx context.Context, payload string) (*UsageRecord, error) {
	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}

	record, errRecord := UsageRecordFromPayload(payload)
	if errRecord != nil {
		return nil, errRecord
	}
	if errCreate := db.WithContext(contextOrBackground(ctx)).Create(record).Error; errCreate != nil {
		return nil, errCreate
	}
	return record, nil
}

func jsonbFromPayloadField(payload string, path string) JSONB {
	value := gjson.Get(payload, path)
	if !value.Exists() || strings.TrimSpace(value.Raw) == "" {
		return nil
	}
	return JSONB(value.Raw)
}
