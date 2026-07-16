package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
)

func (c *Collector) probeClaude(ctx context.Context, auth *coreauth.Auth) (probeResult, *probeError) {
	headers := http.Header{"Content-Type": []string{"application/json"}, "Anthropic-Beta": []string{"oauth-2025-04-20"}}
	body, _, errUsage := c.probeRequest(ctx, auth, http.MethodGet, c.options.ClaudeUsageURL, nil, headers)
	if errUsage != nil {
		return probeResult{}, errUsage
	}
	windows, errParse := parseClaudeUsageWindows(body, c.options.Now().UTC())
	if errParse != nil || len(windows) == 0 {
		return probeResult{}, &probeError{code: "UPSTREAM_RESPONSE_INVALID", message: "Claude quota response did not contain usable windows.", retryable: true}
	}
	result := probeResult{windows: windows, replaceWindows: true}
	profileBody, _, errProfile := c.probeRequest(ctx, auth, http.MethodGet, c.options.ClaudeProfileURL, nil, headers)
	if errProfile != nil {
		result.partial = true
		result.collectionError = probeCollectionError(errProfile, c.options.Now().UTC())
		return result, nil
	}
	if !validClaudeProfile(profileBody) {
		profileError := &probeError{code: "PROFILE_RESPONSE_INVALID", message: "Claude profile response could not be parsed.", retryable: true}
		result.partial = true
		result.collectionError = probeCollectionError(profileError, c.options.Now().UTC())
	}
	return result, nil
}

func (c *Collector) probeAntigravity(ctx context.Context, auth *coreauth.Auth) ([]cluster.QuotaWindow, *probeError) {
	projectID := quotaMetadataString(auth.Metadata, "project_id", "projectId", "project")
	if projectID == "" {
		return nil, &probeError{code: "PROJECT_ID_UNAVAILABLE", message: "Credential project ID is unavailable.", retryable: false}
	}
	body, errMarshal := json.Marshal(map[string]string{"project": projectID})
	if errMarshal != nil {
		return nil, &probeError{code: "PROBE_REQUEST_INVALID", message: "Antigravity quota request could not be created.", retryable: false}
	}
	headers := http.Header{"Content-Type": []string{"application/json"}, "User-Agent": []string{antigravityUserAgent}}
	var lastError *probeError
	for _, targetURL := range c.options.AntigravityURLs {
		payload, _, errRequest := c.probeRequest(ctx, auth, http.MethodPost, targetURL, body, headers)
		if errRequest != nil {
			lastError = errRequest
			if errRequest.code == "UPSTREAM_AUTH_REJECTED" {
				break
			}
			continue
		}
		windows, errParse := parseAntigravityWindows(payload, c.options.Now().UTC())
		if errParse == nil && len(windows) > 0 {
			return windows, nil
		}
		lastError = &probeError{code: "UPSTREAM_RESPONSE_INVALID", message: "Antigravity quota response did not contain usable windows.", retryable: true}
	}
	if lastError == nil {
		lastError = &probeError{code: "PROVIDER_UNAVAILABLE", message: "Antigravity quota collector has no configured endpoint.", retryable: false}
	}
	return nil, lastError
}

func (c *Collector) probeKimi(ctx context.Context, auth *coreauth.Auth) ([]cluster.QuotaWindow, *probeError) {
	payload, _, errRequest := c.probeRequest(ctx, auth, http.MethodGet, c.options.KimiUsageURL, nil, nil)
	if errRequest != nil {
		return nil, errRequest
	}
	windows, errParse := parseKimiUsageWindows(payload, c.options.Now().UTC())
	if errParse != nil || len(windows) == 0 {
		return nil, &probeError{code: "UPSTREAM_RESPONSE_INVALID", message: "Kimi quota response did not contain usable windows.", retryable: true}
	}
	return windows, nil
}

func (c *Collector) probeXAI(ctx context.Context, auth *coreauth.Auth) ([]cluster.QuotaWindow, *probeError) {
	payload, _, errRequest := c.probeRequest(ctx, auth, http.MethodGet, c.options.XAIBillingURL, nil, nil)
	if errRequest != nil {
		return nil, errRequest
	}
	windows, errParse := parseXAIUsageWindows(payload, c.options.Now().UTC())
	if errParse != nil || len(windows) == 0 {
		return nil, &probeError{code: "UPSTREAM_RESPONSE_INVALID", message: "xAI billing response did not contain usable quota.", retryable: true}
	}
	return windows, nil
}

func probeCollectionError(failure *probeError, occurredAt time.Time) *cluster.QuotaCollectionError {
	if failure == nil {
		return nil
	}
	result := &cluster.QuotaCollectionError{Code: failure.code, Message: failure.message, Retryable: failure.retryable, OccurredAt: &occurredAt}
	if failure.statusCode > 0 {
		result.UpstreamStatusCode = &failure.statusCode
	}
	if failure.requestID != "" {
		result.RequestID = &failure.requestID
	}
	return result
}

type claudeUsagePayload struct {
	FiveHour          *claudeUsageWindow `json:"five_hour"`
	SevenDay          *claudeUsageWindow `json:"seven_day"`
	SevenDayOAuthApps *claudeUsageWindow `json:"seven_day_oauth_apps"`
	SevenDayOpus      *claudeUsageWindow `json:"seven_day_opus"`
	SevenDaySonnet    *claudeUsageWindow `json:"seven_day_sonnet"`
	SevenDayCowork    *claudeUsageWindow `json:"seven_day_cowork"`
	IguanaNecktie     *claudeUsageWindow `json:"iguana_necktie"`
	ExtraUsage        *claudeExtraUsage  `json:"extra_usage"`
}

type claudeUsageWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

type claudeExtraUsage struct {
	IsEnabled    bool     `json:"is_enabled"`
	MonthlyLimit *float64 `json:"monthly_limit"`
	UsedCredits  *float64 `json:"used_credits"`
	Utilization  *float64 `json:"utilization"`
}

func parseClaudeUsageWindows(body []byte, observedAt time.Time) ([]cluster.QuotaWindow, error) {
	var payload claudeUsagePayload
	if errDecode := json.Unmarshal(body, &payload); errDecode != nil {
		return nil, fmt.Errorf("decode claude quota response: %w", errDecode)
	}
	windows := make([]cluster.QuotaWindow, 0, 8)
	windows = appendClaudeWindow(windows, "claude-five-hour", nil, "account", nil, payload.FiveHour, "hour", 5, 0, observedAt)
	windows = appendClaudeWindow(windows, "claude-seven-day", nil, "account", nil, payload.SevenDay, "week", 1, 1, observedAt)
	windows = appendClaudeWindow(windows, "claude-seven-day-oauth-apps", quotaStringPtr("OAuth Apps"), "account", nil, payload.SevenDayOAuthApps, "week", 1, 2, observedAt)
	windows = appendClaudeWindow(windows, "claude-seven-day-opus", quotaStringPtr("Opus"), "model", quotaStringPtr("opus"), payload.SevenDayOpus, "week", 1, 3, observedAt)
	windows = appendClaudeWindow(windows, "claude-seven-day-sonnet", quotaStringPtr("Sonnet"), "model", quotaStringPtr("sonnet"), payload.SevenDaySonnet, "week", 1, 4, observedAt)
	windows = appendClaudeWindow(windows, "claude-seven-day-cowork", quotaStringPtr("Cowork"), "account", nil, payload.SevenDayCowork, "week", 1, 5, observedAt)
	windows = appendClaudeWindow(windows, "claude-iguana-necktie", quotaStringPtr("Iguana Necktie"), "account", nil, payload.IguanaNecktie, "unknown", 0, 6, observedAt)
	if extra, ok := claudeExtraUsageWindow(payload.ExtraUsage, observedAt); ok {
		windows = append(windows, extra)
	}
	return windows, nil
}

func appendClaudeWindow(windows []cluster.QuotaWindow, id string, label *string, scope string, scopeID *string, input *claudeUsageWindow, periodUnit string, periodValue float64, priority int, observedAt time.Time) []cluster.QuotaWindow {
	if input == nil || input.Utilization == nil || math.IsNaN(*input.Utilization) || math.IsInf(*input.Utilization, 0) || *input.Utilization < 0 {
		return windows
	}
	usedRatio := normalizedProviderRatio(*input.Utilization)
	remainingRatio := 1 - usedRatio
	used, remaining, limit := usedRatio*100, remainingRatio*100, float64(100)
	window := cluster.QuotaWindow{ID: id, Label: label, Scope: scope, ScopeID: scopeID, Mode: "rolling", Status: quotaProbeStatus(remainingRatio), Unit: "percentage", Used: &used, Remaining: &remaining, Limit: &limit, UsedRatio: &usedRatio, RemainingRatio: &remainingRatio, PeriodUnit: periodUnit, Source: "active_probe", ObservedAt: observedAt, Priority: priority}
	if periodUnit != "unknown" {
		window.PeriodValue = quotaFloatPtr(periodValue)
		window.WindowSeconds = periodSeconds(periodUnit, periodValue)
	}
	window.ResetAt = parseProviderTime(input.ResetsAt)
	return append(windows, window)
}

func claudeExtraUsageWindow(input *claudeExtraUsage, observedAt time.Time) (cluster.QuotaWindow, bool) {
	if input == nil || (!input.IsEnabled && input.MonthlyLimit == nil && input.UsedCredits == nil && input.Utilization == nil) {
		return cluster.QuotaWindow{}, false
	}
	window := cluster.QuotaWindow{ID: "claude-extra-usage", Label: quotaStringPtr("Extra Usage"), Scope: "account", Mode: "fixed", Status: "unknown", Unit: "credits", PeriodUnit: "month", PeriodValue: quotaFloatPtr(1), Source: "active_probe", ObservedAt: observedAt, Priority: 20}
	window.Used = nonNegativeFloat(input.UsedCredits)
	window.Limit = nonNegativeFloat(input.MonthlyLimit)
	if window.Limit != nil && *window.Limit > 0 {
		remaining := math.Max(0, *window.Limit-quotaFloatValue(window.Used))
		window.Remaining = &remaining
		usedRatio := math.Max(0, math.Min(1, quotaFloatValue(window.Used)/(*window.Limit)))
		remainingRatio := 1 - usedRatio
		window.UsedRatio, window.RemainingRatio = &usedRatio, &remainingRatio
		window.Status = quotaProbeStatus(remainingRatio)
	} else if input.Utilization != nil {
		usedRatio := normalizedProviderRatio(*input.Utilization)
		remainingRatio := 1 - usedRatio
		window.UsedRatio, window.RemainingRatio = &usedRatio, &remainingRatio
		window.Status = quotaProbeStatus(remainingRatio)
	}
	return window, true
}

func validClaudeProfile(body []byte) bool {
	var payload struct {
		Account      map[string]any `json:"account"`
		Organization map[string]any `json:"organization"`
	}
	return json.Unmarshal(body, &payload) == nil && (payload.Account != nil || payload.Organization != nil)
}

type antigravityPayload struct {
	Models map[string]struct {
		DisplayName string `json:"displayName"`
		QuotaInfo   *struct {
			RemainingFraction *float64 `json:"remainingFraction"`
			Remaining         *float64 `json:"remaining"`
			ResetTime         string   `json:"resetTime"`
		} `json:"quotaInfo"`
	} `json:"models"`
}

func parseAntigravityWindows(body []byte, observedAt time.Time) ([]cluster.QuotaWindow, error) {
	var payload antigravityPayload
	if errDecode := json.Unmarshal(body, &payload); errDecode != nil {
		return nil, fmt.Errorf("decode antigravity quota response: %w", errDecode)
	}
	keys := make([]string, 0, len(payload.Models))
	for key := range payload.Models {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	windows := make([]cluster.QuotaWindow, 0, len(keys))
	for index, key := range keys {
		model := payload.Models[key]
		if model.QuotaInfo == nil {
			continue
		}
		label := strings.TrimSpace(model.DisplayName)
		if label == "" {
			label = key
		}
		window := cluster.QuotaWindow{ID: "antigravity-model-" + quotaIDSlug(key), Label: &label, Scope: "model", ScopeID: quotaStringPtr(key), Mode: "rolling", Status: "unknown", Unit: "percentage", PeriodUnit: "hour", PeriodValue: quotaFloatPtr(5), WindowSeconds: quotaInt64Ptr(5 * 60 * 60), Source: "active_probe", ObservedAt: observedAt, Priority: index}
		if model.QuotaInfo.RemainingFraction != nil {
			remainingRatio := math.Max(0, math.Min(1, *model.QuotaInfo.RemainingFraction))
			usedRatio := 1 - remainingRatio
			used, remaining, limit := usedRatio*100, remainingRatio*100, float64(100)
			window.Used, window.Remaining, window.Limit = &used, &remaining, &limit
			window.UsedRatio, window.RemainingRatio = &usedRatio, &remainingRatio
			window.Status = quotaProbeStatus(remainingRatio)
		} else if model.QuotaInfo.Remaining != nil {
			window.Unit = "requests"
			window.Remaining = nonNegativeFloat(model.QuotaInfo.Remaining)
		}
		window.ResetAt = parseProviderTime(model.QuotaInfo.ResetTime)
		normalizeWindowValues(&window)
		windows = append(windows, window)
	}
	return windows, nil
}

type kimiUsagePayload struct {
	Usage  *kimiUsageDetail `json:"usage"`
	Limits []kimiLimitItem  `json:"limits"`
}

type kimiUsageDetail struct {
	Used      *float64 `json:"used"`
	Limit     *float64 `json:"limit"`
	Remaining *float64 `json:"remaining"`
	Name      string   `json:"name"`
	Title     string   `json:"title"`
	ResetAt   string   `json:"reset_at"`
	ResetIn   *float64 `json:"resetIn"`
	TTL       *float64 `json:"ttl"`
}

type kimiLimitItem struct {
	Name   string           `json:"name"`
	Title  string           `json:"title"`
	Scope  string           `json:"scope"`
	Detail *kimiUsageDetail `json:"detail"`
	Window *struct {
		Duration int64  `json:"duration"`
		TimeUnit string `json:"timeUnit"`
	} `json:"window"`
	Used      *float64 `json:"used"`
	Limit     *float64 `json:"limit"`
	Remaining *float64 `json:"remaining"`
	Duration  int64    `json:"duration"`
	TimeUnit  string   `json:"timeUnit"`
	ResetAt   string   `json:"resetAt"`
	ResetIn   *float64 `json:"resetIn"`
	TTL       *float64 `json:"ttl"`
}

func parseKimiUsageWindows(body []byte, observedAt time.Time) ([]cluster.QuotaWindow, error) {
	var payload kimiUsagePayload
	if errDecode := json.Unmarshal(body, &payload); errDecode != nil {
		return nil, fmt.Errorf("decode kimi quota response: %w", errDecode)
	}
	windows := make([]cluster.QuotaWindow, 0, len(payload.Limits)+1)
	for index, limit := range payload.Limits {
		window, ok := kimiLimitWindow(limit, index, observedAt)
		if ok {
			windows = append(windows, window)
		}
	}
	if len(windows) == 0 {
		if window, ok := kimiSummaryWindow(payload.Usage, observedAt); ok {
			windows = append(windows, window)
		}
	}
	return windows, nil
}

func kimiLimitWindow(input kimiLimitItem, index int, observedAt time.Time) (cluster.QuotaWindow, bool) {
	used := firstFloat(input.Used, detailFloat(input.Detail, "used"))
	limit := firstFloat(input.Limit, detailFloat(input.Detail, "limit"))
	remaining := firstFloat(input.Remaining, detailFloat(input.Detail, "remaining"))
	if used == nil && limit == nil && remaining == nil {
		return cluster.QuotaWindow{}, false
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = fmt.Sprintf("%d", index+1)
	}
	label := firstNonEmptyString(input.Title, input.Name, "Limit")
	window := cluster.QuotaWindow{ID: "kimi-limit-" + quotaIDSlug(name), Label: &label, Scope: normalizeProviderScope(input.Scope), Mode: "rolling", Status: "unknown", Unit: "requests", Used: nonNegativeFloat(used), Remaining: nonNegativeFloat(remaining), Limit: nonNegativeFloat(limit), PeriodUnit: "unknown", Source: "active_probe", ObservedAt: observedAt, Priority: index}
	duration, unit := input.Duration, input.TimeUnit
	if input.Window != nil {
		duration, unit = input.Window.Duration, input.Window.TimeUnit
	}
	window.PeriodUnit, window.PeriodValue, window.WindowSeconds = structuredPeriod(duration, unit)
	window.ResetAt = firstProviderTime(input.ResetAt, detailString(input.Detail, "reset_at"))
	resetIn := firstFloat(input.ResetIn, detailFloat(input.Detail, "reset_in"))
	if window.ResetAt == nil && resetIn != nil && *resetIn >= 0 {
		resetAt := observedAt.Add(time.Duration(*resetIn) * time.Second)
		window.ResetAt = &resetAt
	}
	normalizeWindowValues(&window)
	return window, true
}

func kimiSummaryWindow(input *kimiUsageDetail, observedAt time.Time) (cluster.QuotaWindow, bool) {
	if input == nil || (input.Used == nil && input.Limit == nil && input.Remaining == nil) {
		return cluster.QuotaWindow{}, false
	}
	label := firstNonEmptyString(input.Title, "Usage")
	window := cluster.QuotaWindow{ID: "kimi-usage", Label: &label, Scope: "account", Mode: "balance", Status: "unknown", Unit: "requests", Used: nonNegativeFloat(input.Used), Remaining: nonNegativeFloat(input.Remaining), Limit: nonNegativeFloat(input.Limit), PeriodUnit: "unknown", Source: "active_probe", ObservedAt: observedAt}
	window.ResetAt = parseProviderTime(input.ResetAt)
	if window.ResetAt == nil && input.ResetIn != nil && *input.ResetIn >= 0 {
		resetAt := observedAt.Add(time.Duration(*input.ResetIn) * time.Second)
		window.ResetAt = &resetAt
	}
	normalizeWindowValues(&window)
	return window, true
}

type xaiBillingPayload struct {
	Config *struct {
		MonthlyLimit struct {
			Val *float64 `json:"val"`
		} `json:"monthlyLimit"`
		Used struct {
			Val *float64 `json:"val"`
		} `json:"used"`
		BillingPeriodEnd string `json:"billingPeriodEnd"`
	} `json:"config"`
}

func parseXAIUsageWindows(body []byte, observedAt time.Time) ([]cluster.QuotaWindow, error) {
	var payload xaiBillingPayload
	if errDecode := json.Unmarshal(body, &payload); errDecode != nil {
		return nil, fmt.Errorf("decode xai billing response: %w", errDecode)
	}
	if payload.Config == nil || payload.Config.MonthlyLimit.Val == nil {
		return nil, nil
	}
	limit := math.Max(0, *payload.Config.MonthlyLimit.Val/100)
	used := 0.0
	if payload.Config.Used.Val != nil {
		used = math.Max(0, *payload.Config.Used.Val/100)
	}
	remaining := math.Max(0, limit-used)
	window := cluster.QuotaWindow{ID: "xai-monthly-spend", Label: quotaStringPtr("Monthly Spend"), Scope: "account", Mode: "fixed", Status: "unknown", Unit: "currency", Currency: quotaStringPtr("USD"), Used: &used, Remaining: &remaining, Limit: &limit, PeriodUnit: "month", PeriodValue: quotaFloatPtr(1), Source: "active_probe", ObservedAt: observedAt}
	window.ResetAt = parseProviderTime(payload.Config.BillingPeriodEnd)
	normalizeWindowValues(&window)
	return []cluster.QuotaWindow{window}, nil
}

func normalizedProviderRatio(value float64) float64 {
	if value > 1 {
		value /= 100
	}
	return math.Max(0, math.Min(1, value))
}

func normalizeWindowValues(window *cluster.QuotaWindow) {
	cluster.NormalizeQuotaWindowValues(window)
}

func structuredPeriod(duration int64, rawUnit string) (string, *float64, *int64) {
	if duration <= 0 {
		return "unknown", nil, nil
	}
	unit := strings.ToLower(strings.TrimSpace(rawUnit))
	value := float64(duration)
	var seconds int64
	switch unit {
	case "minute", "minutes", "m":
		unit, seconds = "minute", duration*60
	case "hour", "hours", "h":
		unit, seconds = "hour", duration*60*60
	case "day", "days", "d":
		unit, seconds = "day", duration*24*60*60
	case "week", "weeks", "w":
		unit, seconds = "week", duration*7*24*60*60
	case "month", "months":
		unit, seconds = "month", 0
	default:
		return "unknown", nil, nil
	}
	if seconds > 0 {
		return unit, &value, &seconds
	}
	return unit, &value, nil
}

func periodSeconds(unit string, value float64) *int64 {
	if value <= 0 || math.Trunc(value) != value {
		return nil
	}
	integer := int64(value)
	switch unit {
	case "minute":
		return quotaInt64Ptr(integer * 60)
	case "hour":
		return quotaInt64Ptr(integer * 60 * 60)
	case "day":
		return quotaInt64Ptr(integer * 24 * 60 * 60)
	case "week":
		return quotaInt64Ptr(integer * 7 * 24 * 60 * 60)
	default:
		return nil
	}
}

func parseProviderTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, errParse := time.Parse(time.RFC3339Nano, value)
	if errParse != nil {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}

func firstProviderTime(values ...string) *time.Time {
	for _, value := range values {
		if parsed := parseProviderTime(value); parsed != nil {
			return parsed
		}
	}
	return nil
}

func normalizeProviderScope(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "project":
		return "project"
	case "model":
		return "model"
	case "organization", "org":
		return "organization"
	case "account", "request", "requests", "user":
		return "account"
	default:
		return "unknown"
	}
}

func nonNegativeFloat(value *float64) *float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil
	}
	normalized := math.Max(0, *value)
	return &normalized
}

func firstFloat(values ...*float64) *float64 {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func detailFloat(detail *kimiUsageDetail, field string) *float64 {
	if detail == nil {
		return nil
	}
	switch field {
	case "used":
		return detail.Used
	case "limit":
		return detail.Limit
	case "remaining":
		return detail.Remaining
	case "reset_in":
		return detail.ResetIn
	default:
		return nil
	}
}

func detailString(detail *kimiUsageDetail, field string) string {
	if detail == nil {
		return ""
	}
	if field == "reset_at" {
		return detail.ResetAt
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func quotaFloatValue(value *float64) float64 {
	if value == nil {
		return 0
	}
	return *value
}

func quotaInt64Ptr(value int64) *int64 { return &value }
