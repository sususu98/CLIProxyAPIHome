package management

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
)

func TestPostBillingModelPriceCreatesRule(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newBillingManagementTestHandler(t)
	defer closeRepo()

	body := []byte(`{"provider":"openai","model":"gpt-4.1-mini","input_price_per_million":2,"output_price_per_million":8,"enabled":true}`)
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
