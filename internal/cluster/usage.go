package cluster

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

const defaultUsageServiceTier = "default"

type UsageRecord struct {
	ID                  uint      `gorm:"column:id;primaryKey;autoIncrement;index:idx_usage_time_order,priority:2"`
	Timestamp           time.Time `gorm:"column:timestamp;not null;index:idx_usage_timestamp;index:idx_usage_time_order,priority:1,sort:desc;index:idx_usage_source_time,priority:2,sort:desc;index:idx_usage_auth_time,priority:2,sort:desc;index:idx_usage_failed_time,priority:2,sort:desc;index:idx_usage_provider_model_time,priority:3,sort:desc;index:idx_usage_endpoint_time,priority:2,sort:desc"`
	LatencyMS           int64     `gorm:"column:latency_ms;not null;default:0"`
	TTFTMS              int64     `gorm:"column:ttft_ms;not null;default:0"`
	Source              string    `gorm:"column:source;index:idx_usage_source;index:idx_usage_source_time,priority:1"`
	AuthIndex           string    `gorm:"column:auth_index;index:idx_usage_auth_index;index:idx_usage_auth_time,priority:1"`
	InputTokens         int64     `gorm:"column:input_tokens;not null;default:0"`
	OutputTokens        int64     `gorm:"column:output_tokens;not null;default:0"`
	ReasoningTokens     int64     `gorm:"column:reasoning_tokens;not null;default:0"`
	CachedTokens        int64     `gorm:"column:cached_tokens;not null;default:0"`
	CacheReadTokens     int64     `gorm:"column:cache_read_tokens;not null;default:0"`
	CacheCreationTokens int64     `gorm:"column:cache_creation_tokens;not null;default:0"`
	TotalTokens         int64     `gorm:"column:total_tokens;not null;default:0"`
	Failed              bool      `gorm:"column:failed;not null;default:false;index:idx_usage_failed;index:idx_usage_failed_time,priority:1"`
	FailStatusCode      int       `gorm:"column:fail_status_code;not null;default:0"`
	FailBody            string    `gorm:"column:fail_body;type:text"`
	Provider            string    `gorm:"column:provider;index:idx_usage_provider_model,priority:1;index:idx_usage_provider_model_time,priority:1"`
	ExecutorType        string    `gorm:"column:executor_type"`
	Model               string    `gorm:"column:model;index:idx_usage_provider_model,priority:2;index:idx_usage_provider_model_time,priority:2"`
	Alias               string    `gorm:"column:alias"`
	Effort              string    `gorm:"column:effort"`
	ServiceTier         string    `gorm:"column:service_tier"`
	Endpoint            string    `gorm:"column:endpoint;index:idx_usage_endpoint;index:idx_usage_endpoint_time,priority:1"`
	AuthType            string    `gorm:"column:auth_type"`
	APIKey              string    `gorm:"column:api_key"`
	RequestID           string    `gorm:"column:request_id;index:idx_usage_request_id"`
	HomeIP              string    `gorm:"column:home_ip;index:idx_usage_home_ip"`
	TokensJSON          JSONB     `gorm:"column:tokens"`
	FailJSON            JSONB     `gorm:"column:fail"`
	PayloadJSON         JSONB     `gorm:"column:payload;not null"`
	CreatedAt           time.Time `gorm:"column:created_at;not null"`
}

// TableName returns the database table name.
func (UsageRecord) TableName() string {
	return "usage"
}

// UsageRecordFromPayload derives usage record from payload.
func UsageRecordFromPayload(payload string, homeIP string) (*UsageRecord, error) {
	// Validate input data before converting it into runtime state.
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
		TTFTMS:              gjson.Get(payload, "ttft_ms").Int(),
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
		ExecutorType:        strings.TrimSpace(gjson.Get(payload, "executor_type").String()),
		Model:               strings.TrimSpace(gjson.Get(payload, "model").String()),
		Alias:               strings.TrimSpace(gjson.Get(payload, "alias").String()),
		Effort:              strings.TrimSpace(gjson.Get(payload, "reasoning_effort").String()),
		ServiceTier:         usageServiceTierFromPayload(payload),
		Endpoint:            strings.TrimSpace(gjson.Get(payload, "endpoint").String()),
		AuthType:            strings.TrimSpace(gjson.Get(payload, "auth_type").String()),
		APIKey:              strings.TrimSpace(gjson.Get(payload, "api_key").String()),
		RequestID:           strings.TrimSpace(gjson.Get(payload, "request_id").String()),
		HomeIP:              strings.TrimSpace(homeIP),
		TokensJSON:          jsonbFromPayloadField(payload, "tokens"),
		FailJSON:            jsonbFromPayloadField(payload, "fail"),
		PayloadJSON:         JSONB(payload),
		CreatedAt:           time.Now().UTC(),
	}
	return record, nil
}

// usageServiceTierFromPayload returns the reported service tier or the default tier.
func usageServiceTierFromPayload(payload string) string {
	serviceTier := strings.TrimSpace(gjson.Get(payload, "service_tier").String())
	if serviceTier == "" {
		return defaultUsageServiceTier
	}
	return serviceTier
}

// AppendUsage appends an usage.
func (r *Repository) AppendUsage(ctx context.Context, payload string, homeIP string) (*UsageRecord, error) {
	record, errRecord := UsageRecordFromPayload(payload, homeIP)
	if errRecord != nil {
		return nil, errRecord
	}

	db, errDB := r.database()
	if errDB != nil {
		return nil, errDB
	}

	ctx = contextOrBackground(ctx)
	errTransaction := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if errCreate := tx.WithContext(ctx).Create(record).Error; errCreate != nil {
			return errCreate
		}
		return r.createBillingChargeForUsageTx(ctx, tx, record, payload)
	})
	if errTransaction != nil {
		return nil, errTransaction
	}
	return record, nil
}

// jsonbFromPayloadField derives jsonb from payload field.
func jsonbFromPayloadField(payload string, path string) JSONB {
	value := gjson.Get(payload, path)
	if !value.Exists() || strings.TrimSpace(value.Raw) == "" {
		return nil
	}
	return JSONB(value.Raw)
}
