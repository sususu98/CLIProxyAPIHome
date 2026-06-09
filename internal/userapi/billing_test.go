package userapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
)

func TestUserBillingChargesUseAuthenticatedUserOnly(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newUserBillingTestHandler(t)
	defer closeRepo()

	firstUser, secondUser := seedUserBillingCharges(t, handler)
	firstToken := createUserBillingBearerToken(t, handler, firstUser.ID)
	secondToken := createUserBillingBearerToken(t, handler, secondUser.ID)

	firstResp := httptest.NewRecorder()
	firstCtx, _ := gin.CreateTestContext(firstResp)
	firstCtx.Request = httptest.NewRequest(http.MethodGet, "/billing/charges?to=2026-06-10", nil)
	firstCtx.Request.Header.Set("Authorization", "Bearer "+firstToken)
	handler.ListCurrentUserBillingCharges(firstCtx)

	if firstResp.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstResp.Code, firstResp.Body.String())
	}
	firstPayload := decodeUserBillingChargeList(t, firstResp)
	assertUserBillingChargeRequests(t, firstPayload.Items, []string{"req-first"}, []string{"req-second"})

	secondResp := httptest.NewRecorder()
	secondCtx, _ := gin.CreateTestContext(secondResp)
	secondCtx.Request = httptest.NewRequest(http.MethodGet, "/billing/charges", nil)
	secondCtx.Request.Header.Set("Authorization", "Bearer "+secondToken)
	handler.ListCurrentUserBillingCharges(secondCtx)

	if secondResp.Code != http.StatusOK {
		t.Fatalf("second status = %d body=%s, want 200", secondResp.Code, secondResp.Body.String())
	}
	secondPayload := decodeUserBillingChargeList(t, secondResp)
	assertUserBillingChargeRequests(t, secondPayload.Items, []string{"req-second"}, []string{"req-first"})
}

func TestUserBillingOverviewUsesAuthenticatedUserOnly(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newUserBillingTestHandler(t)
	defer closeRepo()

	firstUser, secondUser := seedUserBillingCharges(t, handler)
	firstToken := createUserBillingBearerToken(t, handler, firstUser.ID)
	secondToken := createUserBillingBearerToken(t, handler, secondUser.ID)

	firstResp := httptest.NewRecorder()
	firstCtx, _ := gin.CreateTestContext(firstResp)
	firstCtx.Request = httptest.NewRequest(http.MethodGet, "/billing/overview?to=2026-06-10", nil)
	firstCtx.Request.Header.Set("Authorization", "Bearer "+firstToken)
	handler.CurrentUserBillingOverview(firstCtx)

	if firstResp.Code != http.StatusOK {
		t.Fatalf("first status = %d body=%s, want 200", firstResp.Code, firstResp.Body.String())
	}
	firstPayload := decodeUserBillingOverview(t, firstResp)
	if firstPayload.Overview.TodaySpend != 1 || firstPayload.Overview.MonthSpend != 1 {
		t.Fatalf("first spend = today %v month %v, want 1", firstPayload.Overview.TodaySpend, firstPayload.Overview.MonthSpend)
	}
	if firstPayload.Overview.CurrentBalance != 99 {
		t.Fatalf("first current balance = %v, want 99", firstPayload.Overview.CurrentBalance)
	}

	secondResp := httptest.NewRecorder()
	secondCtx, _ := gin.CreateTestContext(secondResp)
	secondCtx.Request = httptest.NewRequest(http.MethodGet, "/billing/overview", nil)
	secondCtx.Request.Header.Set("Authorization", "Bearer "+secondToken)
	handler.CurrentUserBillingOverview(secondCtx)

	if secondResp.Code != http.StatusOK {
		t.Fatalf("second status = %d body=%s, want 200", secondResp.Code, secondResp.Body.String())
	}
	secondPayload := decodeUserBillingOverview(t, secondResp)
	if secondPayload.Overview.TodaySpend != 1 || secondPayload.Overview.MonthSpend != 1 {
		t.Fatalf("second spend = today %v month %v, want 1", secondPayload.Overview.TodaySpend, secondPayload.Overview.MonthSpend)
	}
}

func TestUserBillingRoutesRegistered(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newUserBillingTestHandler(t)
	defer closeRepo()

	router := gin.New()
	Register(router.Group(""), handler)

	for _, path := range []string{"/billing/overview", "/billing/charges"} {
		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(resp, req)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d body=%s, want 401", path, resp.Code, resp.Body.String())
		}
	}
}

func TestUserBillingChargesValidateQuery(t *testing.T) {
	t.Parallel()

	handler, closeRepo := newUserBillingTestHandler(t)
	defer closeRepo()
	user, _ := seedUserBillingCharges(t, handler)
	token := createUserBillingBearerToken(t, handler, user.ID)

	tests := []struct {
		name string
		path string
	}{
		{name: "invalid limit", path: "/billing/charges?limit=0"},
		{name: "non integer limit", path: "/billing/charges?limit=abc"},
		{name: "invalid offset", path: "/billing/charges?offset=-1"},
		{name: "non integer offset", path: "/billing/charges?offset=abc"},
		{name: "invalid from", path: "/billing/charges?from=2026-06-10T00:00:00Z"},
		{name: "invalid to", path: "/billing/charges?to=2026-06-10T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(resp)
			ctx.Request = httptest.NewRequest(http.MethodGet, tt.path, nil)
			ctx.Request.Header.Set("Authorization", "Bearer "+token)

			handler.ListCurrentUserBillingCharges(ctx)

			if resp.Code != http.StatusBadRequest {
				t.Fatalf("status = %d body=%s, want 400", resp.Code, resp.Body.String())
			}
		})
	}
}

type userBillingChargeListPayload struct {
	Items  []userBillingChargePayload `json:"items"`
	Total  int64                      `json:"total"`
	Limit  int                        `json:"limit"`
	Offset int                        `json:"offset"`
}

type userBillingChargePayload struct {
	RequestID     string          `json:"request_id"`
	APIKey        json.RawMessage `json:"api_key"`
	APIKeyMasked  json.RawMessage `json:"api_key_masked"`
	APIKeyLabel   json.RawMessage `json:"api_key_label"`
	PriceSnapshot json.RawMessage `json:"price_snapshot"`
	MatchedRule   json.RawMessage `json:"matched_price_rule"`
	BalanceBefore json.RawMessage `json:"balance_before"`
	Endpoint      json.RawMessage `json:"endpoint"`
	UserID        json.RawMessage `json:"user_id"`
	Amount        float64         `json:"amount"`
	InputTokens   int64           `json:"input_tokens"`
	OutputTokens  int64           `json:"output_tokens"`
	BalanceAfter  float64         `json:"balance_after"`
	CreatedAt     time.Time       `json:"created_at"`
	ID            string          `json:"id"`
	Provider      string          `json:"provider"`
	Model         string          `json:"model"`
	OriginalModel json.RawMessage `json:"original_model"`
	ActualModel   json.RawMessage `json:"actual_model"`
	CacheTokens   json.RawMessage `json:"cache_tokens"`
}

type userBillingOverviewPayload struct {
	Overview struct {
		CurrentBalance float64 `json:"current_balance"`
		TodaySpend     float64 `json:"today_spend"`
		MonthSpend     float64 `json:"month_spend"`
	} `json:"overview"`
}

func decodeUserBillingChargeList(t *testing.T, resp *httptest.ResponseRecorder) userBillingChargeListPayload {
	t.Helper()

	var payload userBillingChargeListPayload
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode charges response: %v", errDecode)
	}
	if payload.Limit != 50 {
		t.Fatalf("limit = %d, want 50", payload.Limit)
	}
	if payload.Offset != 0 {
		t.Fatalf("offset = %d, want 0", payload.Offset)
	}
	return payload
}

func decodeUserBillingOverview(t *testing.T, resp *httptest.ResponseRecorder) userBillingOverviewPayload {
	t.Helper()

	var payload userBillingOverviewPayload
	if errDecode := json.Unmarshal(resp.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode overview response: %v", errDecode)
	}
	return payload
}

func assertUserBillingChargeRequests(t *testing.T, items []userBillingChargePayload, want []string, forbidden []string) {
	t.Helper()

	if len(items) != len(want) {
		t.Fatalf("charge count = %d, want %d: %#v", len(items), len(want), items)
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.RequestID] = true
		assertNoUserBillingAdminFields(t, item)
	}
	for _, requestID := range want {
		if !seen[requestID] {
			t.Fatalf("missing request_id %q in %#v", requestID, items)
		}
	}
	for _, requestID := range forbidden {
		if seen[requestID] {
			t.Fatalf("forbidden request_id %q leaked in %#v", requestID, items)
		}
	}
}

func assertNoUserBillingAdminFields(t *testing.T, item userBillingChargePayload) {
	t.Helper()

	for name, value := range map[string]json.RawMessage{
		"api_key":            item.APIKey,
		"api_key_masked":     item.APIKeyMasked,
		"api_key_label":      item.APIKeyLabel,
		"price_snapshot":     item.PriceSnapshot,
		"matched_price_rule": item.MatchedRule,
		"balance_before":     item.BalanceBefore,
		"endpoint":           item.Endpoint,
		"user_id":            item.UserID,
		"original_model":     item.OriginalModel,
		"actual_model":       item.ActualModel,
		"cache_tokens":       item.CacheTokens,
	} {
		if len(value) != 0 {
			t.Fatalf("admin field %s leaked with value %s", name, string(value))
		}
	}
}

func seedUserBillingCharges(t *testing.T, handler *Handler) (*cluster.UserRecord, *cluster.UserRecord) {
	t.Helper()

	ctx := context.Background()
	firstName := "first-user"
	secondName := "second-user"
	firstCredits := 100.0
	secondCredits := 100.0
	firstUser, errCreateFirst := handler.repo.CreateUser(ctx, cluster.UserUpdate{Username: &firstName, Credits: &firstCredits})
	if errCreateFirst != nil {
		t.Fatalf("CreateUser(first) error = %v", errCreateFirst)
	}
	secondUser, errCreateSecond := handler.repo.CreateUser(ctx, cluster.UserUpdate{Username: &secondName, Credits: &secondCredits})
	if errCreateSecond != nil {
		t.Fatalf("CreateUser(second) error = %v", errCreateSecond)
	}

	firstKey := "first-client-key"
	secondKey := "second-client-key"
	if _, errCreateFirstKey := handler.repo.CreateAPIKeyForUser(ctx, firstUser.ID, cluster.APIKeyUserUpdate{APIKey: &firstKey}); errCreateFirstKey != nil {
		t.Fatalf("CreateAPIKeyForUser(first) error = %v", errCreateFirstKey)
	}
	if _, errCreateSecondKey := handler.repo.CreateAPIKeyForUser(ctx, secondUser.ID, cluster.APIKeyUserUpdate{APIKey: &secondKey}); errCreateSecondKey != nil {
		t.Fatalf("CreateAPIKeyForUser(second) error = %v", errCreateSecondKey)
	}
	if _, errCreatePrice := handler.repo.CreateBillingModelPrice(ctx, cluster.BillingModelPriceUpdate{Provider: "openai", Model: "gpt-4.1-mini", RequestPrice: 1, Enabled: true}); errCreatePrice != nil {
		t.Fatalf("CreateBillingModelPrice() error = %v", errCreatePrice)
	}

	firstPayload := `{"timestamp":"2026-06-10T01:02:03Z","provider":"openai","model":"gpt-4.1-mini","api_key":"first-client-key","request_id":"req-first","tokens":{"input_tokens":1200,"output_tokens":300,"total_tokens":1500}}`
	if _, errAppendFirst := handler.repo.AppendUsage(ctx, firstPayload, "192.0.2.10"); errAppendFirst != nil {
		t.Fatalf("AppendUsage(first) error = %v", errAppendFirst)
	}

	secondPayload := `{"timestamp":"2026-06-10T01:03:03Z","provider":"openai","model":"gpt-4.1-mini","api_key":"second-client-key","request_id":"req-second","tokens":{"input_tokens":2400,"output_tokens":600,"total_tokens":3000}}`
	if _, errAppendSecond := handler.repo.AppendUsage(ctx, secondPayload, "192.0.2.10"); errAppendSecond != nil {
		t.Fatalf("AppendUsage(second) error = %v", errAppendSecond)
	}

	return firstUser, secondUser
}

func createUserBillingBearerToken(t *testing.T, handler *Handler, userID uint) string {
	t.Helper()

	ctx := context.Background()
	if _, _, errKey := handler.repo.ClusterCAKeyPair(ctx); errKey != nil {
		t.Fatalf("ClusterCAKeyPair() error = %v", errKey)
	}
	token, _, errToken := handler.createBearerToken(ctx, userID, time.Hour)
	if errToken != nil {
		t.Fatalf("createBearerToken() error = %v", errToken)
	}
	return token
}

func newUserBillingTestHandler(t *testing.T) (*Handler, func()) {
	t.Helper()

	ctx := context.Background()
	db, errOpenSQLite := cluster.OpenSQLite(ctx, filepath.Join(t.TempDir(), "home.db"))
	if errOpenSQLite != nil {
		t.Fatalf("OpenSQLite() error = %v", errOpenSQLite)
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
	return NewHandler(cluster.NewRepository(db), nil), closeRepo
}
