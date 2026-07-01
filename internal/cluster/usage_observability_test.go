package cluster

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
)

func TestListUsageObservabilityRecordsJoinsBillingAndMasksClientKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()

	seedUsageObservabilityRecord(t, ctx, repo)

	result, errRecords := repo.ListUsageObservabilityRecords(ctx, UsageObservabilityRecordQuery{Limit: 10, Sort: "timestamp_desc"})
	if errRecords != nil {
		t.Fatalf("ListUsageObservabilityRecords() error = %v", errRecords)
	}
	if result.Total != 1 {
		t.Fatalf("total = %d, want 1", result.Total)
	}
	if len(result.Records) != 1 {
		t.Fatalf("record count = %d, want 1", len(result.Records))
	}

	record := result.Records[0]
	if record.UsageID == 0 {
		t.Fatal("usage id was not populated")
	}
	if record.RequestID != "req-obs-1" {
		t.Fatalf("request id = %q, want req-obs-1", record.RequestID)
	}
	if record.Client.APIKeyID == nil || *record.Client.APIKeyID == 0 {
		t.Fatalf("client api key id = %v, want populated", record.Client.APIKeyID)
	}
	if record.Client.UserID == nil || *record.Client.UserID == 0 {
		t.Fatalf("client user id = %v, want populated", record.Client.UserID)
	}
	if record.Client.Username != "usage-user" {
		t.Fatalf("username = %q, want usage-user", record.Client.Username)
	}
	if strings.Contains(record.Client.APIKeyMasked, "client-key-secret") {
		t.Fatalf("api key mask leaked raw key: %q", record.Client.APIKeyMasked)
	}
	if record.Client.APIKeyMasked != "clie...1234" {
		t.Fatalf("api key mask = %q, want clie...1234", record.Client.APIKeyMasked)
	}
	if record.Credential.CredentialID != "auth-observability" {
		t.Fatalf("credential id = %q, want auth-observability", record.Credential.CredentialID)
	}
	if record.Credential.Label != "Primary OAuth" {
		t.Fatalf("credential label = %q, want Primary OAuth", record.Credential.Label)
	}
	if record.Billing.Amount == nil || *record.Billing.Amount != 2 {
		t.Fatalf("billing amount = %v, want 2", record.Billing.Amount)
	}
	if record.Billing.Currency != UsageObservabilityCurrencyCredits {
		t.Fatalf("currency = %q, want %q", record.Billing.Currency, UsageObservabilityCurrencyCredits)
	}
}

func TestListUsageObservabilityAggregatesSortsBeforePagination(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()

	seedUsageObservabilityRecord(t, ctx, repo)
	if _, errCreatePrice := repo.CreateBillingModelPrice(ctx, BillingModelPriceUpdate{Provider: "openai", Model: "gpt-4.1", RequestPrice: 5, Enabled: true}); errCreatePrice != nil {
		t.Fatalf("CreateBillingModelPrice(second) error = %v", errCreatePrice)
	}
	payload := `{"timestamp":"2026-06-10T01:03:03Z","provider":"openai","model":"gpt-4.1","api_key":"client-key-secret-1234","request_id":"req-obs-2","endpoint":"/v1/chat/completions","executor_type":"CodexWebsocketsExecutor","auth_index":"auth-observability","auth_type":"oauth","latency_ms":2460,"tokens":{"input_tokens":200,"output_tokens":100,"total_tokens":300}}`
	if _, errUsage := repo.AppendUsage(ctx, payload, "192.0.2.10"); errUsage != nil {
		t.Fatalf("AppendUsage(second) error = %v", errUsage)
	}

	result, errAggregates := repo.ListUsageObservabilityAggregates(ctx, UsageObservabilityAggregateQuery{
		GroupBy:   "model",
		Metric:    "total_amount",
		Direction: "desc",
		Limit:     1,
	})
	if errAggregates != nil {
		t.Fatalf("ListUsageObservabilityAggregates() error = %v", errAggregates)
	}
	if result.Total != 2 {
		t.Fatalf("total = %d, want 2", result.Total)
	}
	if len(result.Items) != 1 {
		t.Fatalf("item count = %d, want 1", len(result.Items))
	}
	item := result.Items[0]
	if item.ID != "gpt-4.1" {
		t.Fatalf("top model = %q, want gpt-4.1", item.ID)
	}
	if item.TotalAmount == nil || *item.TotalAmount != 5 {
		t.Fatalf("top total amount = %v, want 5", item.TotalAmount)
	}
	if item.P95LatencyMS == nil || *item.P95LatencyMS != 2460 {
		t.Fatalf("top p95 latency = %v, want 2460", item.P95LatencyMS)
	}
}

func TestGetUsageObservabilityRecordReturnsRecord(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()

	seedUsageObservabilityRecord(t, ctx, repo)

	record, errRecord := repo.GetUsageObservabilityRecord(ctx, "1")
	if errRecord != nil {
		t.Fatalf("GetUsageObservabilityRecord() error = %v", errRecord)
	}
	if record.UsageID != 1 {
		t.Fatalf("usage id = %d, want 1", record.UsageID)
	}
	if record.Client.APIKeyMasked != "clie...1234" {
		t.Fatalf("api key mask = %q, want clie...1234", record.Client.APIKeyMasked)
	}
}

func TestListUsageObservabilityRecordsResolvesCredentialByRuntimeIndex(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()

	username := "usage-user"
	credits := 100.0
	user, errCreateUser := repo.CreateUser(ctx, UserUpdate{Username: &username, Credits: &credits})
	if errCreateUser != nil {
		t.Fatalf("CreateUser() error = %v", errCreateUser)
	}
	clientKey := "client-key-secret-1234"
	if _, errCreateKey := repo.CreateAPIKeyForUser(ctx, user.ID, APIKeyUserUpdate{APIKey: &clientKey}); errCreateKey != nil {
		t.Fatalf("CreateAPIKeyForUser() error = %v", errCreateKey)
	}
	auth := &coreauth.Auth{
		ID:        "auth-runtime-uuid",
		Index:     "auth-runtime-uuid",
		Provider:  "codex",
		Label:     "Runtime Index OAuth",
		Status:    coreauth.StatusActive,
		CreatedAt: time.Date(2026, time.June, 10, 1, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.June, 10, 1, 0, 0, 0, time.UTC),
	}
	authJSON, errMarshal := json.Marshal(auth)
	if errMarshal != nil {
		t.Fatalf("marshal auth: %v", errMarshal)
	}
	db, errDB := repo.database()
	if errDB != nil {
		t.Fatalf("database() error = %v", errDB)
	}
	authRecord := &AuthRecord{
		UUID:      "auth-runtime-uuid",
		AuthJSON:  JSONB(authJSON),
		Version:   1,
		ID:        "auth-runtime-uuid",
		Index:     "runtime-index-1",
		Provider:  "codex",
		Label:     "Runtime Index OAuth",
		Status:    coreauth.StatusActive,
		CreatedAt: time.Date(2026, time.June, 10, 1, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.June, 10, 1, 0, 0, 0, time.UTC),
	}
	if errCreateAuth := db.WithContext(ctx).Create(authRecord).Error; errCreateAuth != nil {
		t.Fatalf("Create(auth) error = %v", errCreateAuth)
	}
	if _, errCreatePrice := repo.CreateBillingModelPrice(ctx, BillingModelPriceUpdate{Provider: "openai", Model: "gpt-4.1-mini", RequestPrice: 2, Enabled: true}); errCreatePrice != nil {
		t.Fatalf("CreateBillingModelPrice() error = %v", errCreatePrice)
	}
	payload := `{"timestamp":"2026-06-10T01:02:03Z","provider":"openai","model":"gpt-4.1-mini","api_key":"client-key-secret-1234","request_id":"req-runtime-index","endpoint":"/v1/chat/completions","executor_type":"CodexWebsocketsExecutor","auth_index":"runtime-index-1","auth_type":"oauth","latency_ms":1460,"tokens":{"input_tokens":100,"output_tokens":50,"total_tokens":150}}`
	if _, errUsage := repo.AppendUsage(ctx, payload, "192.0.2.10"); errUsage != nil {
		t.Fatalf("AppendUsage() error = %v", errUsage)
	}

	result, errRecords := repo.ListUsageObservabilityRecords(ctx, UsageObservabilityRecordQuery{Limit: 10, Sort: "timestamp_desc"})
	if errRecords != nil {
		t.Fatalf("ListUsageObservabilityRecords() error = %v", errRecords)
	}
	if len(result.Records) != 1 {
		t.Fatalf("record count = %d, want 1", len(result.Records))
	}
	record := result.Records[0]
	if record.Credential.CredentialID != "auth-runtime-uuid" {
		t.Fatalf("credential id = %q, want auth-runtime-uuid", record.Credential.CredentialID)
	}
	if record.Credential.AuthIndex != "runtime-index-1" {
		t.Fatalf("credential auth index = %q, want runtime-index-1", record.Credential.AuthIndex)
	}
	if record.Credential.Label != "Runtime Index OAuth" {
		t.Fatalf("credential label = %q, want Runtime Index OAuth", record.Credential.Label)
	}
}

func seedUsageObservabilityRecord(t *testing.T, ctx context.Context, repo *Repository) {
	t.Helper()

	username := "usage-user"
	credits := 100.0
	user, errCreateUser := repo.CreateUser(ctx, UserUpdate{Username: &username, Credits: &credits})
	if errCreateUser != nil {
		t.Fatalf("CreateUser() error = %v", errCreateUser)
	}
	clientKey := "client-key-secret-1234"
	if _, errCreateKey := repo.CreateAPIKeyForUser(ctx, user.ID, APIKeyUserUpdate{APIKey: &clientKey}); errCreateKey != nil {
		t.Fatalf("CreateAPIKeyForUser() error = %v", errCreateKey)
	}
	auth := &coreauth.Auth{
		ID:        "auth-observability",
		Index:     "auth-observability",
		Provider:  "codex",
		Label:     "Primary OAuth",
		Status:    coreauth.StatusActive,
		CreatedAt: time.Date(2026, time.June, 10, 1, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, time.June, 10, 1, 0, 0, 0, time.UTC),
	}
	if _, errAuth := repo.UpsertAuth(ctx, auth, "test"); errAuth != nil {
		t.Fatalf("UpsertAuth() error = %v", errAuth)
	}
	if _, errCreatePrice := repo.CreateBillingModelPrice(ctx, BillingModelPriceUpdate{Provider: "openai", Model: "gpt-4.1-mini", RequestPrice: 2, Enabled: true}); errCreatePrice != nil {
		t.Fatalf("CreateBillingModelPrice() error = %v", errCreatePrice)
	}
	payload := `{"timestamp":"2026-06-10T01:02:03Z","provider":"openai","model":"gpt-4.1-mini","api_key":"client-key-secret-1234","request_id":"req-obs-1","endpoint":"/v1/chat/completions","executor_type":"CodexWebsocketsExecutor","auth_index":"auth-observability","auth_type":"oauth","latency_ms":1460,"ttft_ms":333,"tokens":{"input_tokens":100,"output_tokens":50,"total_tokens":150}}`
	if _, errUsage := repo.AppendUsage(ctx, payload, "192.0.2.10"); errUsage != nil {
		t.Fatalf("AppendUsage() error = %v", errUsage)
	}
}
