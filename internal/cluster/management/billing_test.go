package management

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
)

type billingImportRoundTripper func(*http.Request) (*http.Response, error)

func (fn billingImportRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestPostBillingModelPriceCreatesRule(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()

	body := []byte(`{"provider":"openai","model":"gpt-4.1-mini","service_tier":"standard","min_input_tokens":272001,"input_price_per_million":2,"output_price_per_million":8,"enabled":true}`)
	resp := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(resp)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/billing/model-prices", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.CreateBillingModelPrice(ctx)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status field = %v, want ok", payload["status"])
	}
	modelPrice, ok := payload["model_price"].(map[string]any)
	if !ok {
		t.Fatalf("model_price = %T, want object", payload["model_price"])
	}
	if modelPrice["provider"] != "openai" {
		t.Fatalf("model_price.provider = %v, want openai", modelPrice["provider"])
	}
	if modelPrice["model"] != "gpt-4.1-mini" {
		t.Fatalf("model_price.model = %v, want gpt-4.1-mini", modelPrice["model"])
	}
	if modelPrice["enabled"] != true {
		t.Fatalf("model_price.enabled = %v, want true", modelPrice["enabled"])
	}
	if modelPrice["input_price_per_million"] != float64(2) {
		t.Fatalf("model_price.input_price_per_million = %v, want 2", modelPrice["input_price_per_million"])
	}
	if modelPrice["service_tier"] != "standard" || modelPrice["min_input_tokens"] != float64(272001) {
		t.Fatalf("model_price tier/band = %v/%v", modelPrice["service_tier"], modelPrice["min_input_tokens"])
	}
}

func TestBillingModelPriceRejectsInvalidServiceTierAsBadRequest(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()

	postResp := httptest.NewRecorder()
	postCtx, _ := gin.CreateTestContext(postResp)
	postCtx.Request = httptest.NewRequest(http.MethodPost, "/billing/model-prices", bytes.NewBufferString(`{"provider":"openai","model":"gpt-test","service_tier":"bad tier"}`))
	postCtx.Request.Header.Set("Content-Type", "application/json")
	handler.CreateBillingModelPrice(postCtx)
	if postResp.Code != http.StatusBadRequest {
		t.Fatalf("POST status = %d body=%s, want 400", postResp.Code, postResp.Body.String())
	}

	price, errCreate := handler.repo.CreateBillingModelPrice(context.Background(), cluster.BillingModelPriceUpdate{Provider: "openai", Model: "gpt-test", Enabled: true})
	if errCreate != nil {
		t.Fatalf("CreateBillingModelPrice() error = %v", errCreate)
	}
	patchResp := httptest.NewRecorder()
	patchCtx, _ := gin.CreateTestContext(patchResp)
	patchCtx.Request = httptest.NewRequest(http.MethodPatch, "/billing/model-prices/"+price.ID, bytes.NewBufferString(`{"service_tier":"bad tier"}`))
	patchCtx.Request.Header.Set("Content-Type", "application/json")
	patchCtx.Params = gin.Params{{Key: "id", Value: price.ID}}
	handler.UpdateBillingModelPrice(patchCtx)
	if patchResp.Code != http.StatusBadRequest {
		t.Fatalf("PATCH status = %d body=%s, want 400", patchResp.Code, patchResp.Body.String())
	}
}

func TestListBillingModelPricesReturnsSchemaVersion(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()
	resp := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(resp)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/billing/model-prices", nil)
	handler.ListBillingModelPrices(ctx)

	var payload map[string]any
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if payload["price_rule_schema_version"] != float64(2) {
		t.Fatalf("price_rule_schema_version = %v, want 2", payload["price_rule_schema_version"])
	}
}

func TestBillingSettingsGetAndPatch(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()

	getResp := httptest.NewRecorder()
	getCtx, _ := gin.CreateTestContext(getResp)
	getCtx.Request = httptest.NewRequest(http.MethodGet, "/billing/settings", nil)
	handler.GetBillingSettings(getCtx)
	if getResp.Code != http.StatusOK || !bytes.Contains(getResp.Body.Bytes(), []byte(`"service_tier_source":"request"`)) {
		t.Fatalf("GET status/body = %d %s", getResp.Code, getResp.Body.String())
	}

	patchResp := httptest.NewRecorder()
	patchCtx, _ := gin.CreateTestContext(patchResp)
	patchCtx.Request = httptest.NewRequest(http.MethodPatch, "/billing/settings", bytes.NewBufferString(`{"service_tier_source":"response"}`))
	patchCtx.Request.Header.Set("Content-Type", "application/json")
	handler.UpdateBillingSettings(patchCtx)
	if patchResp.Code != http.StatusOK || !bytes.Contains(patchResp.Body.Bytes(), []byte(`"service_tier_source":"response"`)) {
		t.Fatalf("PATCH status/body = %d %s", patchResp.Code, patchResp.Body.String())
	}
}

func TestParseBillingModelPriceImportCatalogPreservesZeroPricedContextBand(t *testing.T) {
	t.Parallel()

	catalog, errCatalog := parseBillingModelPriceImportCatalog([]byte(`{
  "openai": {"models": {"gpt-import": {"id": "gpt-import", "cost": {
    "input": 2, "output": 8, "cache_write": 0,
    "context_over_200k": {"input": 3, "output": 12},
    "tiers": [{"input": 4, "output": 16, "cache_write": 0, "tier": {"type": "context", "size": 272000}}]
  }}}}
}`), modelsDevCatalogURL, time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC))
	if errCatalog != nil {
		t.Fatalf("parseBillingModelPriceImportCatalog() error = %v", errCatalog)
	}
	if len(catalog.Models) != 1 || catalog.Models[0].Cost == nil || catalog.Models[0].Cost.CacheWrite != 0 {
		t.Fatalf("catalog models = %#v", catalog.Models)
	}
	if len(catalog.Models[0].ContextBands) != 1 || catalog.Models[0].ContextBands[0].MinInputTokens != 272001 || catalog.Models[0].ContextBands[0].Cost.CacheWrite != 0 || len(catalog.Models[0].ContextBands[0].MissingPriceFields) != 0 {
		t.Fatalf("context bands = %#v", catalog.Models[0].ContextBands)
	}
}

func TestParseBillingModelPriceImportCatalogMarksIncompleteTierUnsafe(t *testing.T) {
	t.Parallel()

	catalog, errCatalog := parseBillingModelPriceImportCatalog([]byte(`{
  "requesty": {"models": {"google/gemini-tier": {"cost": {
    "input": 1.25, "output": 10, "cache_read": 0.125, "cache_write": 2.375,
    "tiers": [{"input": 2.5, "output": 15, "cache_read": 0.25, "tier": {"type": "context", "size": 200000}}]
  }}}}
}`), modelsDevCatalogURL, time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC))
	if errCatalog != nil {
		t.Fatalf("parseBillingModelPriceImportCatalog() error = %v", errCatalog)
	}
	if len(catalog.Models) != 1 || len(catalog.Models[0].ContextBands) != 1 || len(catalog.Models[0].ContextBands[0].MissingPriceFields) != 1 || catalog.Models[0].ContextBands[0].MissingPriceFields[0] != "cache_write" {
		t.Fatalf("catalog context bands = %#v", catalog.Models)
	}
}

func TestParseBillingModelPriceImportCatalogFailsClosedForMalformedPriceAndTier(t *testing.T) {
	t.Parallel()

	catalog, errCatalog := parseBillingModelPriceImportCatalog([]byte(`{
  "openai": {"models": {
    "gpt-malformed-price": {"cost": {"input": "not-a-number", "output": 8}},
    "gpt-malformed-tier": {"cost": {"input": 2, "output": 8, "tiers": [{"input": 4, "output": 16, "tier": {"type": "context", "size": "not-a-number"}}]}}
  }}
}`), modelsDevCatalogURL, time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC))
	if errCatalog != nil {
		t.Fatalf("parseBillingModelPriceImportCatalog() error = %v", errCatalog)
	}
	if len(catalog.Models) != 2 {
		t.Fatalf("catalog models = %#v", catalog.Models)
	}
	if len(catalog.Models[0].InvalidPriceFields) != 1 || catalog.Models[0].InvalidPriceFields[0] != "input" {
		t.Fatalf("malformed price fields = %#v", catalog.Models[0].InvalidPriceFields)
	}
	if len(catalog.Models[1].ContextBandIssues) != 1 || catalog.Models[1].ContextBandIssues[0] != "invalid_context_band_boundary" {
		t.Fatalf("malformed tier issues = %#v", catalog.Models[1].ContextBandIssues)
	}
}

func TestPreviewBillingModelPriceImportRejectsMalformedContextBand(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()
	catalog, errCatalog := parseBillingModelPriceImportCatalog([]byte(`{
  "openai": {"models": {"gpt-malformed-tier": {"cost": {
    "input": 2, "output": 8,
    "tiers": [{"input": 4, "output": 16, "tier": {"type": "context", "size": "invalid"}}]
  }}}}
}`), modelsDevCatalogURL, time.Now().UTC())
	if errCatalog != nil {
		t.Fatalf("parseBillingModelPriceImportCatalog() error = %v", errCatalog)
	}
	preview, errPreview := handler.repo.CreateBillingModelPriceImportPreview(context.Background(), cluster.BillingModelPriceImportPreviewInput{
		Source:  cluster.BillingModelPriceImportSourceModelsDev,
		Targets: []cluster.BillingModelPriceImportTarget{{Provider: "openai", Model: "gpt-malformed-tier"}},
		Policy:  cluster.BillingModelPriceImportPolicy{OverwriteMode: "missing", DefaultMultiplier: 1, IncludeCachePrices: true},
	}, catalog)
	if errPreview != nil {
		t.Fatalf("CreateBillingModelPriceImportPreview() error = %v", errPreview)
	}
	if len(preview.Rows) != 1 || preview.Rows[0].Status != "invalid" || preview.Rows[0].Action != "review" || preview.Rows[0].Applicable || len(preview.Rows[0].Reasons) != 1 || preview.Rows[0].Reasons[0].Code != "invalid_context_bands" {
		t.Fatalf("preview rows = %#v", preview.Rows)
	}
}

func TestPreviewBillingModelPriceImportFetchesModelsDevServerSide(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()
	handler.SetModelsDevHTTPClient(&http.Client{Transport: billingImportRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != modelsDevCatalogURL {
			return nil, fmt.Errorf("unexpected catalog URL %q", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"openai":{"models":{"gpt-import":{"cost":{"input":2,"output":8}}}}}`))}, nil
	})})
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/billing/model-prices/import/preview", strings.NewReader(`{"source":"models.dev","targets":[{"provider":"openai","model":"gpt-import"}],"policy":{"overwrite_mode":"missing","default_multiplier":1}}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.PreviewBillingModelPriceImport(ctx)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", response.Code, response.Body.String())
	}
	var payload map[string]any
	if errDecode := json.Unmarshal(response.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode preview response: %v", errDecode)
	}
	if payload["atomic"] != true || payload["preview_id"] == "" {
		t.Fatalf("preview response = %#v", payload)
	}
	rows, ok := payload["rows"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("preview rows = %#v", payload["rows"])
	}
	row, ok := rows[0].(map[string]any)
	if !ok || row["row_key"] == "" {
		t.Fatalf("preview row = %#v", rows[0])
	}
	applyResponse := httptest.NewRecorder()
	applyContext, _ := gin.CreateTestContext(applyResponse)
	applyContext.Request = httptest.NewRequest(http.MethodPost, "/billing/model-prices/import/apply", strings.NewReader(fmt.Sprintf(`{"preview_id":%q,"preview_revision":%q,"selected_keys":[%q],"idempotency_key":"preview-apply-key"}`, payload["preview_id"], payload["preview_revision"], row["row_key"])))
	applyContext.Request.Header.Set("Content-Type", "application/json")
	applyContext.Request.Header.Set("Idempotency-Key", "preview-apply-key")
	handler.ApplyBillingModelPriceImport(applyContext)
	if applyResponse.Code != http.StatusOK {
		t.Fatalf("apply status = %d body=%s, want 200", applyResponse.Code, applyResponse.Body.String())
	}
	var applyPayload map[string]any
	if errDecode := json.Unmarshal(applyResponse.Body.Bytes(), &applyPayload); errDecode != nil {
		t.Fatalf("decode apply response: %v", errDecode)
	}
	applyRows, ok := applyPayload["rows"].([]any)
	if !ok || len(applyRows) != 1 {
		t.Fatalf("apply rows = %#v", applyPayload["rows"])
	}
	applyRow, ok := applyRows[0].(map[string]any)
	if !ok || applyRow["key"] != row["row_key"] || applyRow["provider"] != "openai" || applyRow["model"] != "gpt-import" || applyRow["action"] != "create" || applyRow["status"] != "created" || applyRow["resource_id"] == "" {
		t.Fatalf("apply row = %#v", applyRows[0])
	}
}

func TestPreviewBillingModelPriceImportValidatesBeforeFetchingCatalog(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()
	fetched := false
	handler.SetModelsDevHTTPClient(&http.Client{Transport: billingImportRoundTripper(func(_ *http.Request) (*http.Response, error) {
		fetched = true
		return nil, fmt.Errorf("catalog should not be fetched")
	})})
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/billing/model-prices/import/preview", strings.NewReader(`{"source":"models.dev","targets":[{"provider":"openai","model":"gpt-import"}],"policy":{"overwrite_mode":"missing","default_multiplier":1,"multiplier_rules":[{"pattern":"gpt","multiplier":2}]}}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.PreviewBillingModelPriceImport(ctx)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body=%s, want 422", response.Code, response.Body.String())
	}
	if fetched {
		t.Fatal("invalid preview fetched the models.dev catalog")
	}
}

func TestPreviewBillingModelPriceImportReturnsInternalErrorForDatabaseFailure(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	closeRepo()
	handler.SetModelsDevHTTPClient(&http.Client{Transport: billingImportRoundTripper(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"openai":{"models":{"gpt-import":{"cost":{"input":2,"output":8}}}}}`))}, nil
	})})
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/billing/model-prices/import/preview", strings.NewReader(`{"source":"models.dev","targets":[{"provider":"openai","model":"gpt-import"}],"policy":{"overwrite_mode":"missing","default_multiplier":1}}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.PreviewBillingModelPriceImport(ctx)

	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "billing_import_preview_failed") {
		t.Fatalf("status = %d body=%s, want 500 billing_import_preview_failed", response.Code, response.Body.String())
	}
}

func TestPatchBillingModelPricePartialUpdatePreservesUnspecifiedFields(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()

	price, errCreate := handler.repo.CreateBillingModelPrice(context.Background(), cluster.BillingModelPriceUpdate{
		Provider:                  "openai",
		Model:                     "gpt-4.1-mini",
		InputPricePerMillion:      2,
		OutputPricePerMillion:     8,
		CacheReadPricePerMillion:  1,
		CacheWritePricePerMillion: 3,
		RequestPrice:              5,
		Source:                    cluster.BillingPriceSourceSync,
		Enabled:                   true,
		Note:                      "keep note",
	})
	if errCreate != nil {
		t.Fatalf("CreateBillingModelPrice() error = %v", errCreate)
	}

	body := []byte(`{"request_price":0,"enabled":false}`)
	resp := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(resp)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/billing/model-prices/"+price.ID, bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: price.ID}}

	handler.UpdateBillingModelPrice(ctx)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status field = %v, want ok", payload["status"])
	}
	modelPrice, ok := payload["model_price"].(map[string]any)
	if !ok {
		t.Fatalf("model_price = %T, want object", payload["model_price"])
	}
	if modelPrice["provider"] != "openai" || modelPrice["model"] != "gpt-4.1-mini" {
		t.Fatalf("model_price provider/model = %v/%v, want openai/gpt-4.1-mini", modelPrice["provider"], modelPrice["model"])
	}
	if modelPrice["input_price_per_million"] != float64(2) ||
		modelPrice["output_price_per_million"] != float64(8) ||
		modelPrice["cache_read_price_per_million"] != float64(1) ||
		modelPrice["cache_write_price_per_million"] != float64(3) {
		t.Fatalf("model_price token prices were not preserved: %#v", modelPrice)
	}
	if modelPrice["request_price"] != float64(0) {
		t.Fatalf("model_price.request_price = %v, want 0", modelPrice["request_price"])
	}
	if modelPrice["enabled"] != false {
		t.Fatalf("model_price.enabled = %v, want false", modelPrice["enabled"])
	}
	if modelPrice["source"] != cluster.BillingPriceSourceSync {
		t.Fatalf("model_price.source = %v, want %s", modelPrice["source"], cluster.BillingPriceSourceSync)
	}
	if modelPrice["note"] != "keep note" {
		t.Fatalf("model_price.note = %v, want keep note", modelPrice["note"])
	}
}

func TestDeleteBillingModelPriceDeletesRule(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()

	price, errCreate := handler.repo.CreateBillingModelPrice(context.Background(), cluster.BillingModelPriceUpdate{
		Provider:     "openai",
		Model:        "gpt-4.1-mini",
		RequestPrice: 1,
		Enabled:      true,
	})
	if errCreate != nil {
		t.Fatalf("CreateBillingModelPrice() error = %v", errCreate)
	}

	resp := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(resp)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/billing/model-prices/"+price.ID, nil)
	ctx.Params = gin.Params{{Key: "id", Value: price.ID}}

	handler.DeleteBillingModelPrice(ctx)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status field = %v, want ok", payload["status"])
	}
}

func TestDeleteBillingModelPriceMissingIDReturnsNotFound(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()

	resp := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(resp)
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/billing/model-prices/missing", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "missing"}}

	handler.DeleteBillingModelPrice(ctx)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s, want 404", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if payload["error"] != "model_price_not_found" {
		t.Fatalf("error = %v, want model_price_not_found", payload["error"])
	}
}

func TestPostBillingBalanceDeductRequiresNote(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()

	body := []byte(`{"user_id":1,"amount":1}`)
	resp := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(resp)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/billing/balance-records/deduct", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.DeductBillingBalance(ctx)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", resp.Code, resp.Body.String())
	}
}

func TestPostBillingBalanceRechargeUpdatesUserCredits(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()

	username := "billing-user"
	credits := 5.0
	user, errCreate := handler.repo.CreateUser(context.Background(), cluster.UserUpdate{Username: &username, Credits: &credits})
	if errCreate != nil {
		t.Fatalf("CreateUser() error = %v", errCreate)
	}

	body := []byte(fmt.Sprintf(`{"user_id":%d,"amount":7,"note":"manual recharge"}`, user.ID))
	resp := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(resp)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/billing/balance-records/recharge", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.RechargeBillingBalance(ctx)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	record, ok := payload["balance_record"].(map[string]any)
	if !ok {
		t.Fatalf("balance_record = %T, want object", payload["balance_record"])
	}
	if record["type"] != cluster.BillingBalanceTypeRecharge {
		t.Fatalf("balance_record.type = %v, want recharge", record["type"])
	}
	if record["balance_before"] != float64(5) {
		t.Fatalf("balance_record.balance_before = %v, want 5", record["balance_before"])
	}
	if record["balance_after"] != float64(12) {
		t.Fatalf("balance_record.balance_after = %v, want 12", record["balance_after"])
	}
	updated, errUser := handler.repo.GetUser(context.Background(), user.ID)
	if errUser != nil {
		t.Fatalf("GetUser() error = %v", errUser)
	}
	if updated.Credits != 12 {
		t.Fatalf("user credits = %v, want 12", updated.Credits)
	}
}

func TestPostBillingBalanceRechargeMissingUserReturnsNotFound(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()

	body := []byte(`{"user_id":999,"amount":1,"note":"manual recharge"}`)
	resp := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(resp)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/billing/balance-records/recharge", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.RechargeBillingBalance(ctx)

	if resp.Code != http.StatusNotFound {
		t.Fatalf("status = %d body=%s, want 404", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if payload["error"] != "user_not_found" {
		t.Fatalf("error = %v, want user_not_found", payload["error"])
	}
}

func TestGetBillingOverviewReturnsTotals(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()
	seedBillingManagementCharge(t, handler)

	resp := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(resp)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/billing/overview", nil)

	handler.GetBillingOverview(ctx)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	overview, ok := payload["overview"].(map[string]any)
	if !ok {
		t.Fatalf("overview = %T, want object", payload["overview"])
	}
	if overview["total_charge_amount"] != float64(2) {
		t.Fatalf("overview.total_charge_amount = %v, want 2", overview["total_charge_amount"])
	}
	if overview["total_recharge_amount"] != float64(50) {
		t.Fatalf("overview.total_recharge_amount = %v, want 50", overview["total_recharge_amount"])
	}
}

func TestListBillingChargesReturnsItems(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()
	seedBillingManagementCharge(t, handler)

	resp := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(resp)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/billing/charges?limit=10", nil)

	handler.ListBillingCharges(ctx)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s, want 200", resp.Code, resp.Body.String())
	}
	var payload map[string]any
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if payload["total"] != float64(1) {
		t.Fatalf("total = %v, want 1", payload["total"])
	}
	items, ok := payload["items"].([]any)
	if !ok {
		t.Fatalf("items = %T, want array", payload["items"])
	}
	if len(items) != 1 {
		t.Fatalf("item count = %d, want 1", len(items))
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item = %T, want object", items[0])
	}
	if item["amount"] != float64(2) {
		t.Fatalf("item.amount = %v, want 2", item["amount"])
	}
}

func TestBillingChargeQueryDateOnlyToNormalizesEndOfDay(t *testing.T) {
	t.Parallel()

	resp := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(resp)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/billing/charges?to=2026-06-10", nil)

	query, ok := billingChargeQueryFromRequest(ctx)
	if !ok {
		t.Fatalf("billingChargeQueryFromRequest() ok = false, status=%d body=%s", resp.Code, resp.Body.String())
	}
	if query.To == nil {
		t.Fatal("query.To is nil")
	}
	want := time.Date(2026, time.June, 10, 23, 59, 59, int(time.Second-time.Nanosecond), time.UTC)
	if !query.To.Equal(want) {
		t.Fatalf("query.To = %s, want %s", query.To.Format(time.RFC3339Nano), want.Format(time.RFC3339Nano))
	}
}

func seedBillingManagementCharge(t *testing.T, handler *Handler) {
	t.Helper()

	ctx := context.Background()
	username := "billing-user"
	credits := 100.0
	user, errCreateUser := handler.repo.CreateUser(ctx, cluster.UserUpdate{Username: &username, Credits: &credits})
	if errCreateUser != nil {
		t.Fatalf("CreateUser() error = %v", errCreateUser)
	}
	key := "client-key"
	if _, errCreateKey := handler.repo.CreateAPIKeyForUser(ctx, user.ID, cluster.APIKeyUserUpdate{APIKey: &key}); errCreateKey != nil {
		t.Fatalf("CreateAPIKeyForUser() error = %v", errCreateKey)
	}
	if _, errCreatePrice := handler.repo.CreateBillingModelPrice(ctx, cluster.BillingModelPriceUpdate{Provider: "openai", Model: "gpt-4.1-mini", RequestPrice: 2, Enabled: true}); errCreatePrice != nil {
		t.Fatalf("CreateBillingModelPrice() error = %v", errCreatePrice)
	}
	if _, errRecharge := handler.repo.ApplyBillingBalanceRecord(ctx, cluster.BillingBalanceUpdate{UserID: user.ID, Type: cluster.BillingBalanceTypeRecharge, Amount: 50, Operator: "admin"}); errRecharge != nil {
		t.Fatalf("ApplyBillingBalanceRecord() error = %v", errRecharge)
	}
	payload := `{"timestamp":"2026-06-10T01:02:03Z","provider":"openai","model":"gpt-4.1-mini","api_key":"client-key","request_id":"req-1","tokens":{"input_tokens":1,"total_tokens":1}}`
	if _, errUsage := handler.repo.AppendUsage(ctx, payload, "192.0.2.10"); errUsage != nil {
		t.Fatalf("AppendUsage() error = %v", errUsage)
	}
}

func newBillingManagementTestHandler(t *testing.T) (*Handler, func()) {
	t.Helper()

	ctx := context.Background()
	db, errOpen := cluster.OpenSQLite(ctx, filepath.Join(t.TempDir(), "home.db"))
	if errOpen != nil {
		t.Fatalf("OpenSQLite() error = %v", errOpen)
	}
	sqlDB, errDB := db.DB()
	if errDB != nil {
		t.Fatalf("db.DB() error = %v", errDB)
	}
	closeRepo := func() {
		if errClose := sqlDB.Close(); errClose != nil {
			t.Errorf("close sqlite db: %v", errClose)
		}
	}
	if errMigrate := cluster.AutoMigrate(db); errMigrate != nil {
		closeRepo()
		t.Fatalf("AutoMigrate() error = %v", errMigrate)
	}
	return NewHandler(cluster.NewRepository(db), nil, "192.0.2.10", 0), closeRepo
}
