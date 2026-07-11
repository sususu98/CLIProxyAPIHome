package cluster

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBillingModelPriceImportAppliesAtomicallyAndReplaysIdempotently(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	preview, errPreview := repo.CreateBillingModelPriceImportPreview(ctx, billingImportTestInput("missing"), billingImportTestCatalog())
	if errPreview != nil {
		t.Fatalf("CreateBillingModelPriceImportPreview() error = %v", errPreview)
	}
	if len(preview.Rows) != 1 || preview.Rows[0].Action != "create" || !preview.Rows[0].Applicable {
		t.Fatalf("preview rows = %#v", preview.Rows)
	}
	if preview.Rows[0].Final == nil || !preview.Rows[0].Final.CacheWriteConfigured {
		t.Fatalf("preview did not preserve configured zero cache write: %#v", preview.Rows[0])
	}
	input := BillingModelPriceImportApplyInput{PreviewID: preview.PreviewID, PreviewRevision: preview.PreviewRevision, SelectedKeys: []string{preview.Rows[0].RowKey}, IdempotencyKey: "replay-key"}
	first, errApply := repo.ApplyBillingModelPriceImport(ctx, input)
	if errApply != nil {
		t.Fatalf("ApplyBillingModelPriceImport() error = %v", errApply)
	}
	second, errReplay := repo.ApplyBillingModelPriceImport(ctx, input)
	if errReplay != nil {
		t.Fatalf("replay ApplyBillingModelPriceImport() error = %v", errReplay)
	}
	if first.OperationID == "" || second.OperationID != first.OperationID || second.Status != "applied" || len(first.Rows) != 1 || first.Rows[0].ResourceID == "" {
		t.Fatalf("operations = %#v / %#v", first, second)
	}
	rules, errRules := repo.ListBillingModelPrices(ctx, BillingModelPriceQuery{Provider: "openai", Model: "gpt-import"})
	if errRules != nil {
		t.Fatalf("ListBillingModelPrices() error = %v", errRules)
	}
	if len(rules) != 1 || !rules[0].CacheWritePriceConfigured || rules[0].Revision != 1 || first.Rows[0].ResourceID != rules[0].ID {
		t.Fatalf("stored rules = %#v", rules)
	}
	operation, errOperation := repo.GetBillingModelPriceImportOperation(ctx, first.OperationID)
	if errOperation != nil || operation.OperationID != first.OperationID {
		t.Fatalf("GetBillingModelPriceImportOperation() = %#v, %v", operation, errOperation)
	}
}

func TestBillingModelPriceImportUsesContextBandRowMultiplier(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	catalog := billingImportTestCatalog()
	catalog.Models[0].ContextBands = []BillingModelPriceImportContextBand{{
		MinInputTokens: 272001,
		Cost:           BillingModelPriceImportCost{Input: 4, Output: 16, CacheWriteConfigured: true},
	}}
	input := billingImportTestInput("missing")
	input.Policy.RowMultipliers = map[string]float64{
		"openai::gpt-import::*::272001": 2.5,
	}

	preview, errPreview := repo.CreateBillingModelPriceImportPreview(ctx, input, catalog)
	if errPreview != nil {
		t.Fatalf("CreateBillingModelPriceImportPreview() error = %v", errPreview)
	}
	if len(preview.Rows) != 2 {
		t.Fatalf("preview rows = %#v, want base and context-band rows", preview.Rows)
	}
	if preview.Rows[0].Multiplier != 1 || preview.Rows[1].RowKey != "openai::gpt-import::*::272001" || preview.Rows[1].Multiplier != 2.5 || preview.Rows[1].Final == nil || preview.Rows[1].Final.Input != 10 {
		t.Fatalf("preview rows = %#v, want per-context-band multiplier", preview.Rows)
	}
}

func TestBillingModelPriceImportPreservesContextBandScopeForReviewRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	db, errDB := repo.database()
	if errDB != nil {
		t.Fatalf("database() error = %v", errDB)
	}
	for _, record := range []BillingModelPriceRecord{
		{ID: "disabled_context_rule_1", Provider: "openai", Model: "gpt-import", ServiceTier: BillingServiceTierWildcard, MinInputTokens: 272001, Enabled: false},
		{ID: "disabled_context_rule_2", Provider: "openai", Model: "gpt-import", ServiceTier: BillingServiceTierWildcard, MinInputTokens: 272001, Enabled: false},
	} {
		if errCreate := db.WithContext(ctx).Create(&record).Error; errCreate != nil {
			t.Fatalf("create duplicate disabled context rule: %v", errCreate)
		}
	}
	catalog := billingImportTestCatalog()
	catalog.Models[0].ContextBands = []BillingModelPriceImportContextBand{{
		MinInputTokens: 272001,
		Cost:           BillingModelPriceImportCost{Input: 4, Output: 16, CacheWriteConfigured: true},
	}}

	preview, errPreview := repo.CreateBillingModelPriceImportPreview(ctx, billingImportTestInput("missing"), catalog)
	if errPreview != nil {
		t.Fatalf("CreateBillingModelPriceImportPreview() error = %v", errPreview)
	}
	if len(preview.Rows) != 2 || preview.Rows[0].MinInputTokens != 0 || preview.Rows[1].Status != "conflict" || preview.Rows[1].MinInputTokens != 272001 {
		t.Fatalf("preview rows = %#v, want a context-scoped conflict row", preview.Rows)
	}
}

func TestBillingModelPriceImportMarksAmbiguousPreferredProviderMatchesForReview(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	catalog := BillingModelPriceImportCatalog{
		SourceURL: "https://models.dev/api.json",
		Version:   "fixture-v1",
		FetchedAt: time.Now().UTC(),
		Models: []BillingModelPriceImportCatalogModel{
			{Provider: "openai", Model: "gpt-duplicate", Cost: &BillingModelPriceImportCost{Input: 2, Output: 8}},
			{Provider: "openai", Model: "vendor/gpt-duplicate", Cost: &BillingModelPriceImportCost{Input: 3, Output: 12}},
		},
	}
	input := billingImportTestInput("missing")
	input.Targets[0].Model = "gpt-duplicate"

	preview, errPreview := repo.CreateBillingModelPriceImportPreview(ctx, input, catalog)
	if errPreview != nil {
		t.Fatalf("CreateBillingModelPriceImportPreview() error = %v", errPreview)
	}
	if len(preview.Rows) != 1 || preview.Rows[0].Status != "ambiguous" || preview.Rows[0].Action != "review" || preview.Rows[0].Applicable {
		t.Fatalf("preview rows = %#v, want an ambiguous review row", preview.Rows)
	}
}

func TestBillingModelPriceImportPrunesExpiredRecordsAndAllowsRepeatedSelectionHashes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	db, errDB := repo.database()
	if errDB != nil {
		t.Fatalf("database() error = %v", errDB)
	}
	now := time.Now().UTC()
	oldPreview := BillingModelPriceImportPreviewRecord{ID: "preview_old", Revision: "previewrev_old", Source: BillingModelPriceImportSourceModelsDev, SourceURL: "https://models.dev/api.json", SourceVersion: "old", SourceFetchedAt: now.Add(-48 * time.Hour), Atomic: true, GeneratedAt: now.Add(-48 * time.Hour), ExpiresAt: now.Add(-48 * time.Hour), Payload: JSONB(`{}`), CreatedAt: now.Add(-48 * time.Hour)}
	oldOperation := BillingModelPriceImportOperationRecord{ID: "operation_old", PreviewID: oldPreview.ID, PreviewRevision: oldPreview.Revision, IdempotencyKey: "old-key", RequestHash: "old-request", SelectionHash: "same-selection", Atomic: true, Status: "applied", AppliedAt: now.Add(-31 * 24 * time.Hour), Result: JSONB(`{}`), CreatedAt: now.Add(-31 * 24 * time.Hour)}
	if errCreate := db.WithContext(ctx).Create(&oldPreview).Error; errCreate != nil {
		t.Fatalf("create old preview: %v", errCreate)
	}
	if errCreate := db.WithContext(ctx).Create(&oldOperation).Error; errCreate != nil {
		t.Fatalf("create old operation: %v", errCreate)
	}
	for _, record := range []BillingModelPriceImportOperationRecord{
		{ID: "operation_same_selection_1", PreviewID: "preview_a", PreviewRevision: "revision_a", IdempotencyKey: "same-selection-key-1", RequestHash: "request_a", SelectionHash: "reused-selection", Atomic: true, Status: "applied", AppliedAt: now, Result: JSONB(`{}`), CreatedAt: now},
		{ID: "operation_same_selection_2", PreviewID: "preview_b", PreviewRevision: "revision_b", IdempotencyKey: "same-selection-key-2", RequestHash: "request_b", SelectionHash: "reused-selection", Atomic: true, Status: "applied", AppliedAt: now, Result: JSONB(`{}`), CreatedAt: now},
	} {
		if errCreate := db.WithContext(ctx).Create(&record).Error; errCreate != nil {
			t.Fatalf("create operation with reused selection hash: %v", errCreate)
		}
	}
	if _, errPreview := repo.CreateBillingModelPriceImportPreview(ctx, billingImportTestInput("missing"), billingImportTestCatalog()); errPreview != nil {
		t.Fatalf("CreateBillingModelPriceImportPreview() error = %v", errPreview)
	}
	var previewCount, operationCount int64
	if errCount := db.WithContext(ctx).Model(&BillingModelPriceImportPreviewRecord{}).Where("id = ?", oldPreview.ID).Count(&previewCount).Error; errCount != nil {
		t.Fatalf("count old preview: %v", errCount)
	}
	if errCount := db.WithContext(ctx).Model(&BillingModelPriceImportOperationRecord{}).Where("id = ?", oldOperation.ID).Count(&operationCount).Error; errCount != nil {
		t.Fatalf("count old operation: %v", errCount)
	}
	if previewCount != 0 || operationCount != 0 {
		t.Fatalf("expired records remain: preview=%d operation=%d", previewCount, operationCount)
	}
}

func TestMigrateBillingImportIndexesRemovesLegacySelectionHashUniqueness(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	db, errDB := repo.database()
	if errDB != nil {
		t.Fatalf("database() error = %v", errDB)
	}
	if errDrop := db.Exec("DROP INDEX IF EXISTS idx_billing_model_price_import_operation_selection_hash").Error; errDrop != nil {
		t.Fatalf("drop selection-hash index: %v", errDrop)
	}
	if errCreate := db.Exec("CREATE UNIQUE INDEX idx_billing_model_price_import_operation_selection_hash ON billing_model_price_import_operation (selection_hash)").Error; errCreate != nil {
		t.Fatalf("create legacy unique selection-hash index: %v", errCreate)
	}
	if errMigrate := migrateBillingImportIndexes(db); errMigrate != nil {
		t.Fatalf("migrateBillingImportIndexes() error = %v", errMigrate)
	}
	now := time.Now().UTC()
	for _, record := range []BillingModelPriceImportOperationRecord{
		{ID: "operation_migrated_selection_1", PreviewID: "preview_a", PreviewRevision: "revision_a", IdempotencyKey: "migrated-selection-key-1", RequestHash: "request_a", SelectionHash: "migrated-selection", Atomic: true, Status: "applied", AppliedAt: now, Result: JSONB(`{}`), CreatedAt: now},
		{ID: "operation_migrated_selection_2", PreviewID: "preview_b", PreviewRevision: "revision_b", IdempotencyKey: "migrated-selection-key-2", RequestHash: "request_b", SelectionHash: "migrated-selection", Atomic: true, Status: "applied", AppliedAt: now, Result: JSONB(`{}`), CreatedAt: now},
	} {
		if errCreate := db.WithContext(ctx).Create(&record).Error; errCreate != nil {
			t.Fatalf("create operation after selection-hash migration: %v", errCreate)
		}
	}
}

func TestBillingModelPriceImportConvertsConcurrentCreateToConflict(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	preview, errPreview := repo.CreateBillingModelPriceImportPreview(ctx, billingImportTestInput("missing"), billingImportTestCatalog())
	if errPreview != nil {
		t.Fatalf("CreateBillingModelPriceImportPreview() error = %v", errPreview)
	}
	if _, errCreate := repo.CreateBillingModelPrice(ctx, BillingModelPriceUpdate{Provider: "openai", Model: "gpt-import", Enabled: true}); errCreate != nil {
		t.Fatalf("CreateBillingModelPrice() error = %v", errCreate)
	}
	_, errApply := repo.ApplyBillingModelPriceImport(ctx, BillingModelPriceImportApplyInput{PreviewID: preview.PreviewID, PreviewRevision: preview.PreviewRevision, SelectedKeys: []string{preview.Rows[0].RowKey}, IdempotencyKey: "create-conflict-key"})
	if !errors.Is(errApply, ErrBillingImportRuleConflict) {
		t.Fatalf("ApplyBillingModelPriceImport() error = %v, want rule conflict", errApply)
	}
}

func TestBillingModelPriceImportRejectsNonBaseTargetAndUnsafeContextBand(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	invalidTarget := billingImportTestInput("missing")
	invalidTarget.Targets[0].ServiceTier = "priority"
	if _, errPreview := repo.CreateBillingModelPriceImportPreview(ctx, invalidTarget, billingImportTestCatalog()); errPreview == nil {
		t.Fatal("CreateBillingModelPriceImportPreview() succeeded for non-base target")
	}
	unsafeCatalog := billingImportTestCatalog()
	unsafeCatalog.Models[0].ContextBands = []BillingModelPriceImportContextBand{{MinInputTokens: 272001, Cost: BillingModelPriceImportCost{Input: 4, Output: 16}, MissingPriceFields: []string{"cache_write"}}}
	preview, errPreview := repo.CreateBillingModelPriceImportPreview(ctx, billingImportTestInput("missing"), unsafeCatalog)
	if errPreview != nil {
		t.Fatalf("CreateBillingModelPriceImportPreview() error = %v", errPreview)
	}
	if len(preview.Rows) != 1 || preview.Rows[0].Status != "unsupported" || preview.Rows[0].Applicable || preview.Rows[0].Action != "review" {
		t.Fatalf("unsafe context preview = %#v", preview.Rows)
	}
	duplicateCatalog := billingImportTestCatalog()
	duplicateCatalog.Models[0].ContextBands = []BillingModelPriceImportContextBand{
		{MinInputTokens: 272001, Cost: BillingModelPriceImportCost{Input: 4, Output: 16, CacheWriteConfigured: true}},
		{MinInputTokens: 272001, Cost: BillingModelPriceImportCost{Input: 5, Output: 20, CacheWriteConfigured: true}},
	}
	duplicatePreview, errDuplicatePreview := repo.CreateBillingModelPriceImportPreview(ctx, billingImportTestInput("missing"), duplicateCatalog)
	if errDuplicatePreview != nil {
		t.Fatalf("CreateBillingModelPriceImportPreview() duplicate catalog error = %v", errDuplicatePreview)
	}
	if len(duplicatePreview.Rows) != 1 || duplicatePreview.Rows[0].Status != "invalid" || duplicatePreview.Rows[0].Applicable {
		t.Fatalf("duplicate context preview = %#v", duplicatePreview.Rows)
	}
}

func TestBillingModelPriceImportRejectsChangedRuleRevision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	rule, errCreate := repo.CreateBillingModelPrice(ctx, BillingModelPriceUpdate{Provider: "openai", Model: "gpt-import", Source: BillingPriceSourceSync, Enabled: true})
	if errCreate != nil {
		t.Fatalf("CreateBillingModelPrice() error = %v", errCreate)
	}
	preview, errPreview := repo.CreateBillingModelPriceImportPreview(ctx, billingImportTestInput("sync"), billingImportTestCatalog())
	if errPreview != nil {
		t.Fatalf("CreateBillingModelPriceImportPreview() error = %v", errPreview)
	}
	note := "concurrent operator change"
	if _, errUpdate := repo.UpdateBillingModelPrice(ctx, rule.ID, BillingModelPricePatch{Note: &note}); errUpdate != nil {
		t.Fatalf("UpdateBillingModelPrice() error = %v", errUpdate)
	}
	_, errApply := repo.ApplyBillingModelPriceImport(ctx, BillingModelPriceImportApplyInput{PreviewID: preview.PreviewID, PreviewRevision: preview.PreviewRevision, SelectedKeys: []string{preview.Rows[0].RowKey}, IdempotencyKey: "conflict-key"})
	if !errors.Is(errApply, ErrBillingImportRuleConflict) {
		t.Fatalf("ApplyBillingModelPriceImport() error = %v, want revision conflict", errApply)
	}
}

func TestBillingChargeAmountDistinguishesConfiguredZeroCacheWritePrice(t *testing.T) {
	t.Parallel()

	usage := &UsageRecord{Provider: "openai", ExecutorType: "OpenAICompatExecutor", InputTokens: 1000, CacheCreationTokens: 100}
	configured := billingChargeAmount(usage, BillingPriceSnapshot{InputPricePerMillion: 1000, CacheWritePricePerMillion: 0, CacheWritePriceConfigured: true})
	omitted := billingChargeAmount(usage, BillingPriceSnapshot{InputPricePerMillion: 1000, CacheWritePricePerMillion: 0})
	if configured != 0.9 || omitted != 1 {
		t.Fatalf("configured/omitted cache-write charge = %g/%g, want 0.9/1", configured, omitted)
	}
}

func TestBillingTierDiagnosticsUsesOnlyEligibleRecentRequests(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	db, errDB := repo.database()
	if errDB != nil {
		t.Fatalf("database() error = %v", errDB)
	}
	now := time.Now().UTC()
	for _, record := range []UsageRecord{
		{Timestamp: now, RequestServiceTier: "standard", ResponseServiceTier: "priority", PayloadJSON: JSONB(`{}`), CreatedAt: now},
		{Timestamp: now, RequestServiceTier: "standard", PayloadJSON: JSONB(`{}`), CreatedAt: now},
		{Timestamp: now, PayloadJSON: JSONB(`{}`), CreatedAt: now},
		{Timestamp: now.Add(-31 * 24 * time.Hour), RequestServiceTier: "standard", PayloadJSON: JSONB(`{}`), CreatedAt: now},
	} {
		if errCreate := db.WithContext(ctx).Create(&record).Error; errCreate != nil {
			t.Fatalf("create usage record: %v", errCreate)
		}
	}
	diagnostics, errDiagnostics := repo.GetBillingTierDiagnostics(ctx)
	if errDiagnostics != nil {
		t.Fatalf("GetBillingTierDiagnostics() error = %v", errDiagnostics)
	}
	if !diagnostics.Supported || diagnostics.EligibleRequests != 2 || diagnostics.ResponseTierRequests != 1 || diagnostics.FallbackRequests != 1 || diagnostics.LastResponseTierAt == nil {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestBillingModelPriceImportPrefersFirstPartyProviderForKnownModelFamilies(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo, closeRepo := newBillingTestRepository(t, ctx)
	defer closeRepo()
	catalog := BillingModelPriceImportCatalog{SourceURL: "https://models.dev/api.json", Version: "fixture-v1", FetchedAt: time.Now().UTC(), Models: []BillingModelPriceImportCatalogModel{
		{Provider: "openrouter", Model: "claude-sonnet-4-6", Cost: &BillingModelPriceImportCost{Input: 4, Output: 20}},
		{Provider: "anthropic", Model: "claude-sonnet-4-6", Cost: &BillingModelPriceImportCost{Input: 3, Output: 15}},
	}}
	input := billingImportTestInput("missing")
	input.Targets[0].Provider = "antigravity"
	input.Targets[0].Model = "claude-sonnet-4-6"
	preview, errPreview := repo.CreateBillingModelPriceImportPreview(ctx, input, catalog)
	if errPreview != nil {
		t.Fatalf("CreateBillingModelPriceImportPreview() error = %v", errPreview)
	}
	if len(preview.Rows) != 1 || preview.Rows[0].MatchedProvider != "anthropic" || !preview.Rows[0].Applicable {
		t.Fatalf("preview = %#v", preview.Rows)
	}
}

func billingImportTestInput(overwriteMode string) BillingModelPriceImportPreviewInput {
	return BillingModelPriceImportPreviewInput{Source: BillingModelPriceImportSourceModelsDev, Targets: []BillingModelPriceImportTarget{{Provider: "openai", Model: "gpt-import", Label: "GPT Import"}}, Policy: BillingModelPriceImportPolicy{OverwriteMode: overwriteMode, DefaultMultiplier: 1, IncludeCachePrices: true}}
}

func billingImportTestCatalog() BillingModelPriceImportCatalog {
	return BillingModelPriceImportCatalog{SourceURL: "https://models.dev/api.json", Version: "fixture-v1", FetchedAt: time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC), Models: []BillingModelPriceImportCatalogModel{{Provider: "openai", Model: "gpt-import", Name: "GPT Import", Cost: &BillingModelPriceImportCost{Input: 2, Output: 8, CacheWrite: 0, CacheWriteConfigured: true}}}}
}
