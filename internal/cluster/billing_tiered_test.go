package cluster

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
)

func TestBillingMigrationPreservesLegacyPriceAsWildcardBaseBand(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, errOpen := OpenSQLite(ctx, filepath.Join(t.TempDir(), "legacy.db"))
	if errOpen != nil {
		t.Fatalf("OpenSQLite() error = %v", errOpen)
	}
	sqlDB, _ := db.DB()
	defer func() { _ = sqlDB.Close() }()
	if errCreate := db.Exec(`CREATE TABLE billing_model_price (
		id text PRIMARY KEY, provider text NOT NULL, model text NOT NULL,
		input_price_per_million real NOT NULL DEFAULT 0, output_price_per_million real NOT NULL DEFAULT 0,
		cache_read_price_per_million real NOT NULL DEFAULT 0, cache_write_price_per_million real NOT NULL DEFAULT 0,
		request_price real NOT NULL DEFAULT 0, source text NOT NULL DEFAULT 'manual', enabled numeric NOT NULL,
		note text, created_at datetime, updated_at datetime, deleted_at datetime
	)`).Error; errCreate != nil {
		t.Fatalf("create legacy table: %v", errCreate)
	}
	if errInsert := db.Exec(`INSERT INTO billing_model_price (id, provider, model, input_price_per_million, enabled) VALUES ('legacy', 'openai', 'gpt-5.5', 2, TRUE)`).Error; errInsert != nil {
		t.Fatalf("insert legacy row: %v", errInsert)
	}
	if errMigrate := AutoMigrate(db); errMigrate != nil {
		t.Fatalf("AutoMigrate() error = %v", errMigrate)
	}
	var record BillingModelPriceRecord
	if errFirst := db.First(&record, "id = ?", "legacy").Error; errFirst != nil {
		t.Fatalf("load migrated row: %v", errFirst)
	}
	if record.ServiceTier != BillingServiceTierWildcard || record.MinInputTokens != 0 || record.InputPricePerMillion != 2 {
		t.Fatalf("migrated record = %+v", record)
	}
}

func TestUsageRecordFromPayloadPreservesRequestAndResponseServiceTiers(t *testing.T) {
	t.Parallel()

	record, errRecord := UsageRecordFromPayload(`{"timestamp":"2026-07-11T00:00:00Z","service_tier":"priority","request_service_tier":"priority","response_service_tier":"default","tokens":{"input_tokens":1}}`, "")
	if errRecord != nil {
		t.Fatalf("UsageRecordFromPayload() error = %v", errRecord)
	}
	if record.ServiceTier != "priority" || record.RequestServiceTier != "priority" || record.ResponseServiceTier != "default" {
		t.Fatalf("tiers = legacy:%q request:%q response:%q", record.ServiceTier, record.RequestServiceTier, record.ResponseServiceTier)
	}
}

func TestBillingPriceMatchingUsesContextBandAndExactTier(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()

	for _, update := range []BillingModelPriceUpdate{
		{Provider: "openai", Model: "gpt-5.5", ServiceTier: "*", MinInputTokens: 0, InputPricePerMillion: 1, Enabled: true},
		{Provider: "openai", Model: "gpt-5.5", ServiceTier: "standard", MinInputTokens: 0, InputPricePerMillion: 2, Enabled: true},
		{Provider: "openai", Model: "gpt-5.5", ServiceTier: "standard", MinInputTokens: 272001, InputPricePerMillion: 4, Enabled: true},
	} {
		if _, errCreate := repo.CreateBillingModelPrice(ctx, update); errCreate != nil {
			t.Fatalf("CreateBillingModelPrice(%+v) error = %v", update, errCreate)
		}
	}
	db, errDB := repo.database()
	if errDB != nil {
		t.Fatalf("database() error = %v", errDB)
	}

	for _, tt := range []struct {
		name      string
		input     int64
		tier      string
		wantPrice float64
		wantMin   int64
		wantTier  string
	}{
		{name: "boundary stays short", input: 272000, tier: "default", wantPrice: 2, wantMin: 0, wantTier: "standard"},
		{name: "next token selects long", input: 272001, tier: "auto", wantPrice: 4, wantMin: 272001, wantTier: "standard"},
		{name: "wildcard fallback", input: 272001, tier: "flex", wantPrice: 1, wantMin: 0, wantTier: "*"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			usage := &UsageRecord{Provider: "openai", Model: "gpt-5.5", InputTokens: tt.input, ServiceTier: tt.tier, RequestServiceTier: tt.tier}
			_, snapshot, errSnapshot := billingPriceSnapshotForUsage(ctx, db, usage)
			if errSnapshot != nil {
				t.Fatalf("billingPriceSnapshotForUsage() error = %v", errSnapshot)
			}
			if snapshot.InputPricePerMillion != tt.wantPrice || snapshot.MinInputTokens != tt.wantMin || snapshot.MatchedServiceTier != tt.wantTier {
				t.Fatalf("snapshot = %+v", snapshot)
			}
		})
	}
}

func TestBillingResponseTierModeFallsBackAudibly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	if _, errCreate := repo.CreateBillingModelPrice(ctx, BillingModelPriceUpdate{Provider: "openai", Model: "gpt-5.5", ServiceTier: "priority", Enabled: true}); errCreate != nil {
		t.Fatalf("CreateBillingModelPrice() error = %v", errCreate)
	}
	if _, errPatch := repo.UpdateBillingSettings(ctx, BillingSettingsPatch{ServiceTierSource: stringPtr(BillingServiceTierSourceResponse)}); errPatch != nil {
		t.Fatalf("UpdateBillingSettings() error = %v", errPatch)
	}
	db, _ := repo.database()
	_, snapshot, errSnapshot := billingPriceSnapshotForUsage(ctx, db, &UsageRecord{Provider: "openai", Model: "gpt-5.5", ServiceTier: "priority", RequestServiceTier: "priority"})
	if errSnapshot != nil {
		t.Fatalf("billingPriceSnapshotForUsage() error = %v", errSnapshot)
	}
	if snapshot.ServiceTierSource != BillingServiceTierSourceResponse || snapshot.EffectiveServiceTier != "priority" || !snapshot.ResponseTierFallback {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.RequestedServiceTier != "priority" || snapshot.ResponseServiceTier != "" {
		t.Fatalf("snapshot audit tiers = %+v", snapshot)
	}
}

func TestBillingResponseTierModeUsesReportedResponseTier(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	for _, tier := range []string{"priority", "standard"} {
		if _, errCreate := repo.CreateBillingModelPrice(ctx, BillingModelPriceUpdate{Provider: "openai", Model: "gpt-5.5", ServiceTier: tier, RequestPrice: map[string]float64{"priority": 2, "standard": 1}[tier], Enabled: true}); errCreate != nil {
			t.Fatalf("CreateBillingModelPrice(%s) error = %v", tier, errCreate)
		}
	}
	if _, errPatch := repo.UpdateBillingSettings(ctx, BillingSettingsPatch{ServiceTierSource: stringPtr(BillingServiceTierSourceResponse)}); errPatch != nil {
		t.Fatalf("UpdateBillingSettings() error = %v", errPatch)
	}
	db, _ := repo.database()
	_, snapshot, errSnapshot := billingPriceSnapshotForUsage(ctx, db, &UsageRecord{Provider: "openai", Model: "gpt-5.5", RequestServiceTier: "priority", ResponseServiceTier: "default"})
	if errSnapshot != nil {
		t.Fatalf("billingPriceSnapshotForUsage() error = %v", errSnapshot)
	}
	if snapshot.RequestPrice != 1 || snapshot.EffectiveServiceTier != "standard" || snapshot.ResponseTierFallback {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestBillingChargeAmountSeparatesOpenAICacheReadAndWrite(t *testing.T) {
	t.Parallel()

	snapshot := BillingPriceSnapshot{InputPricePerMillion: 10, CacheReadPricePerMillion: 2, CacheWritePricePerMillion: 12.5}
	usage := &UsageRecord{InputTokens: 100, CachedTokens: 30, CacheCreationTokens: 20}
	if got, want := billingChargeAmount(usage, snapshot), 0.00081; math.Abs(got-want) > 1e-12 {
		t.Fatalf("billingChargeAmount() = %.9f, want %.9f", got, want)
	}

	snapshot.CacheWritePricePerMillion = 0
	if got, want := billingChargeAmount(usage, snapshot), 0.00076; math.Abs(got-want) > 1e-12 {
		t.Fatalf("billingChargeAmount(no cache-write fee) = %.9f, want %.9f", got, want)
	}
}

func TestBillingSettingsDefaultToRequest(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	settings, errSettings := repo.GetBillingSettings(ctx)
	if errSettings != nil {
		t.Fatalf("GetBillingSettings() error = %v", errSettings)
	}
	if settings.ServiceTierSource != BillingServiceTierSourceRequest {
		t.Fatalf("service tier source = %q, want request", settings.ServiceTierSource)
	}
}

func TestBillingSnapshotJSONIncludesTierAndBandAudit(t *testing.T) {
	t.Parallel()

	raw, errMarshal := json.Marshal(BillingPriceSnapshot{MatchedServiceTier: "standard", MinInputTokens: 272001, EffectiveServiceTier: "standard"})
	if errMarshal != nil {
		t.Fatalf("json.Marshal() error = %v", errMarshal)
	}
	for _, field := range []string{"matched_service_tier", "min_input_tokens", "effective_service_tier"} {
		if !json.Valid(raw) || !containsJSONField(raw, field) {
			t.Fatalf("snapshot JSON %s missing %q", raw, field)
		}
	}
}

func containsJSONField(raw []byte, field string) bool {
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	_, ok := value[field]
	return ok
}
