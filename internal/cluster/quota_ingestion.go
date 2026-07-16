package cluster

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"gorm.io/gorm"
)

const (
	codexQuotaHeaderPrefix       = "X-Codex-"
	quotaHeaderValueMaxLength    = 4096
	quotaHeaderSnapshotFreshness = 30 * time.Minute
	quotaLowRemainingRatio       = 0.20
)

func sanitizeUsageQuotaHeaders(payload string) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(gjson.Get(payload, "provider").String()))
	filtered := make(map[string]string)
	collect := func(result gjson.Result) {
		if provider != "codex" || !result.IsObject() {
			return
		}
		for rawKey, rawValue := range result.Map() {
			key := http.CanonicalHeaderKey(strings.TrimSpace(rawKey))
			value := quotaHeaderResultValue(rawValue)
			if isCodexQuotaHeaderKey(key) && value != "" && len(value) <= quotaHeaderValueMaxLength {
				filtered[key] = value
			}
		}
	}
	collect(gjson.Get(payload, "quota_headers"))
	responseHeaders := gjson.Get(payload, "response_headers")
	collect(responseHeaders)
	upstreamRequestID := quotaResponseHeaderRequestID(responseHeaders)

	out, errDelete := sjson.Delete(payload, "quota_headers")
	if errDelete != nil {
		return "", errDelete
	}
	out, errDelete = sjson.Delete(out, "response_headers")
	if errDelete != nil {
		return "", errDelete
	}
	if strings.TrimSpace(gjson.Get(out, "upstream_request_id").String()) == "" && upstreamRequestID != "" {
		out, errDelete = sjson.Set(out, "upstream_request_id", upstreamRequestID)
		if errDelete != nil {
			return "", errDelete
		}
	}
	if len(filtered) == 0 {
		return out, nil
	}
	out, errSet := sjson.Set(out, "quota_headers", filtered)
	if errSet != nil {
		return "", errSet
	}
	return out, nil
}

func quotaHeaderResultValue(value gjson.Result) string {
	if value.IsArray() {
		for _, candidate := range value.Array() {
			if trimmed := strings.TrimSpace(candidate.String()); trimmed != "" {
				return trimmed
			}
		}
		return ""
	}
	return strings.TrimSpace(value.String())
}

func quotaResponseHeaderRequestID(headers gjson.Result) string {
	if !headers.IsObject() {
		return ""
	}
	for rawKey, rawValue := range headers.Map() {
		key := http.CanonicalHeaderKey(strings.TrimSpace(rawKey))
		switch key {
		case "X-Upstream-Request-Id", "X-Request-Id", "Openai-Request-Id":
			value := quotaHeaderResultValue(rawValue)
			if value != "" && len(value) <= 128 {
				return value
			}
		}
	}
	return ""
}

func upsertQuotaFromUsagePayloadTx(ctx context.Context, tx *gorm.DB, payload string, metadata UsageRuntimeMetadata) error {
	input, ok := quotaSnapshotWriteFromUsagePayload(payload, metadata)
	if !ok {
		return nil
	}
	_, errUpsert := upsertQuotaSnapshotDB(ctx, tx, input)
	return errUpsert
}

func quotaSnapshotWriteFromUsagePayload(payload string, metadata UsageRuntimeMetadata) (QuotaSnapshotWrite, bool) {
	provider := strings.ToLower(strings.TrimSpace(gjson.Get(payload, "provider").String()))
	credentialID := strings.TrimSpace(gjson.Get(payload, "auth_index").String())
	if provider != "codex" || credentialID == "" {
		return QuotaSnapshotWrite{}, false
	}
	headerResult := gjson.Get(payload, "quota_headers")
	if !headerResult.IsObject() {
		return QuotaSnapshotWrite{}, false
	}
	headers := make(http.Header)
	for key, value := range headerResult.Map() {
		headers.Set(http.CanonicalHeaderKey(key), value.String())
	}
	observedAt, errTime := time.Parse(time.RFC3339Nano, strings.TrimSpace(gjson.Get(payload, "timestamp").String()))
	if errTime != nil {
		return QuotaSnapshotWrite{}, false
	}
	windows, partial := parseCodexQuotaHeaderWindows(headers, observedAt.UTC())
	if len(windows) == 0 {
		return QuotaSnapshotWrite{}, false
	}
	status := aggregateQuotaWindowStatus(windows)
	collectionStatus := "success"
	if partial {
		collectionStatus = "partial"
	}
	expiresAt := observedAt.UTC().Add(quotaHeaderSnapshotFreshness)
	homeID := strings.TrimSpace(metadata.HomeIP)
	if metadata.HomePort > 0 {
		homeID = fmt.Sprintf("%s:%d", homeID, metadata.HomePort)
	}
	if homeID == ":0" {
		homeID = ""
	}
	runtime := &QuotaRuntime{
		HomeID:       homeID,
		HomeLabel:    homeID,
		CPANodeID:    firstNonEmptyQuotaString(metadata.CPANodeID, metadata.CPAIP),
		CPANodeLabel: firstNonEmptyQuotaString(metadata.CPALabel, metadata.CPANodeID, metadata.CPAIP),
	}
	return QuotaSnapshotWrite{
		CredentialID: credentialID, QuotaStatus: status, CollectionStatus: collectionStatus, Source: "response_header",
		ObservedAt: &observedAt, ExpiresAt: &expiresAt, LastAttemptAt: &observedAt, LastSuccessAt: &observedAt,
		NextProbeAt: &expiresAt, Runtime: runtime, ParserVersion: quotaSnapshotSchemaVersion,
		CollectorVersion: quotaSnapshotSchemaVersion, ClearProbeLease: true, ReplaceWindows: !partial, Windows: windows,
	}, true
}

func parseCodexQuotaHeaderWindows(headers http.Header, observedAt time.Time) ([]QuotaWindow, bool) {
	headers = canonicalQuotaHeaders(headers)
	windows := make([]QuotaWindow, 0, 4)
	partial := false
	baseCount := 0
	var added, present bool
	windows, added, present = appendCodexHeaderWindow(windows, headers, codexQuotaHeaderPrefix+"Primary-", "codex-primary", nil, 0, observedAt)
	if added {
		baseCount++
	} else if present {
		partial = true
	}
	windows, added, present = appendCodexHeaderWindow(windows, headers, codexQuotaHeaderPrefix+"Secondary-", "codex-secondary", nil, 1, observedAt)
	if added {
		baseCount++
	} else if present {
		partial = true
	}
	if baseCount == 1 {
		partial = true
	}
	type group struct{ prefix, id, label string }
	groups := make([]group, 0)
	for key := range headers {
		if !strings.HasPrefix(key, codexQuotaHeaderPrefix) || !strings.HasSuffix(key, "-Limit-Name") {
			continue
		}
		name := firstQuotaHeaderValue(headers, key)
		groupName := strings.TrimSuffix(strings.TrimPrefix(key, codexQuotaHeaderPrefix), "-Limit-Name")
		if name == "" || groupName == "" {
			continue
		}
		groups = append(groups, group{prefix: codexQuotaHeaderPrefix + groupName + "-", id: quotaSlug(groupName), label: name})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].id < groups[j].id })
	for index, item := range groups {
		label := item.label
		groupCount := 0
		windows, added, present = appendCodexHeaderWindow(windows, headers, item.prefix+"Primary-", "codex-"+item.id+"-primary", &label, 10+index*2, observedAt)
		if added {
			groupCount++
		} else if present {
			partial = true
		}
		windows, added, present = appendCodexHeaderWindow(windows, headers, item.prefix+"Secondary-", "codex-"+item.id+"-secondary", &label, 11+index*2, observedAt)
		if added {
			groupCount++
		} else if present {
			partial = true
		}
		if groupCount == 1 {
			partial = true
		}
	}
	return windows, partial
}

func appendCodexHeaderWindow(windows []QuotaWindow, headers http.Header, prefix, id string, label *string, priority int, observedAt time.Time) ([]QuotaWindow, bool, bool) {
	present := codexQuotaHeaderWindowPresent(headers, prefix)
	usedPercent, okUsed := quotaFloatHeader(headers, prefix+"Used-Percent")
	windowMinutes, okWindow := quotaIntHeader(headers, prefix+"Window-Minutes")
	if !okUsed || !okWindow || windowMinutes <= 0 || math.IsNaN(usedPercent) || math.IsInf(usedPercent, 0) {
		return windows, false, present
	}
	usedRatio := math.Max(0, math.Min(1, usedPercent/100))
	remainingRatio := 1 - usedRatio
	used, remaining, limit := usedRatio*100, remainingRatio*100, float64(100)
	windowSeconds := windowMinutes * 60
	resetAt, okReset := codexQuotaResetAt(headers, prefix, observedAt)
	if !okReset {
		return windows, false, present
	}
	periodUnit, periodValue := quotaPeriodFromSeconds(windowSeconds)
	status := quotaStatusFromRemainingRatio(remainingRatio)
	return append(windows, QuotaWindow{
		ID: id, Label: label, Scope: "account", Mode: "rolling", Status: status, Unit: "percentage",
		Used: &used, Remaining: &remaining, Limit: &limit, UsedRatio: &usedRatio, RemainingRatio: &remainingRatio,
		ResetAt: &resetAt, WindowSeconds: &windowSeconds, PeriodUnit: periodUnit, PeriodValue: periodValue,
		Source: "response_header", ObservedAt: observedAt.UTC(), Priority: priority,
	}), true, true
}

func codexQuotaHeaderWindowPresent(headers http.Header, prefix string) bool {
	for _, suffix := range []string{"Used-Percent", "Window-Minutes", "Reset-At", "Reset-After-Seconds"} {
		if firstQuotaHeaderValue(headers, prefix+suffix) != "" {
			return true
		}
	}
	return false
}

func codexQuotaResetAt(headers http.Header, prefix string, observedAt time.Time) (time.Time, bool) {
	if unixValue, ok := quotaIntHeader(headers, prefix+"Reset-At"); ok && unixValue > 0 {
		return time.Unix(unixValue, 0).UTC(), true
	}
	if seconds, ok := quotaIntHeader(headers, prefix+"Reset-After-Seconds"); ok && seconds >= 0 {
		return observedAt.UTC().Add(time.Duration(seconds) * time.Second), true
	}
	return time.Time{}, false
}

func quotaPeriodFromSeconds(seconds int64) (string, *float64) {
	var unit string
	var value float64
	switch {
	case seconds%(7*24*60*60) == 0:
		unit, value = "week", float64(seconds/(7*24*60*60))
	case seconds%(24*60*60) == 0:
		unit, value = "day", float64(seconds/(24*60*60))
	case seconds%(60*60) == 0:
		unit, value = "hour", float64(seconds/(60*60))
	case seconds%60 == 0:
		unit, value = "minute", float64(seconds/60)
	default:
		return "unknown", nil
	}
	return unit, &value
}

func aggregateQuotaWindowStatus(windows []QuotaWindow) string {
	status := "healthy"
	for _, window := range windows {
		if window.Status == "exhausted" {
			return "exhausted"
		}
		if window.Status == "low" {
			status = "low"
		}
	}
	return status
}

func quotaStatusFromRemainingRatio(remaining float64) string {
	if remaining <= 0 {
		return "exhausted"
	}
	if remaining <= quotaLowRemainingRatio {
		return "low"
	}
	return "healthy"
}

func canonicalQuotaHeaders(headers http.Header) http.Header {
	canonical := make(http.Header, len(headers))
	for key, values := range headers {
		canonicalKey := http.CanonicalHeaderKey(strings.TrimSpace(key))
		for _, value := range values {
			if canonicalKey != "" && strings.TrimSpace(value) != "" {
				canonical.Add(canonicalKey, strings.TrimSpace(value))
			}
		}
	}
	return canonical
}

func isCodexQuotaHeaderKey(key string) bool {
	if key == "X-Codex-Plan-Type" {
		return true
	}
	if !strings.HasPrefix(key, codexQuotaHeaderPrefix) {
		return false
	}
	for _, suffix := range []string{"-Limit-Name", "-Used-Percent", "-Window-Minutes", "-Reset-At", "-Reset-After-Seconds"} {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

func firstQuotaHeaderValue(headers http.Header, key string) string {
	for _, value := range headers.Values(key) {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func quotaFloatHeader(headers http.Header, key string) (float64, bool) {
	value := firstQuotaHeaderValue(headers, key)
	parsed, errParse := strconv.ParseFloat(value, 64)
	return parsed, value != "" && errParse == nil && parsed >= 0
}

func quotaIntHeader(headers http.Header, key string) (int64, bool) {
	value := firstQuotaHeaderValue(headers, key)
	parsed, errParse := strconv.ParseInt(value, 10, 64)
	return parsed, value != "" && errParse == nil
}

func quotaSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
		} else if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "-") {
			builder.WriteByte('-')
		}
	}
	return strings.Trim(builder.String(), "-")
}
