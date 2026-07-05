package auth

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/registry"
	"github.com/tidwall/gjson"
)

const (
	quotaBackoffBase = time.Second
	quotaBackoffMax  = 30 * time.Minute
)

var usageRetryDelayPattern = regexp.MustCompile(`(?i)\b(?:resets in|after)\s+([0-9]+(?:\.[0-9]+)?(?:h|m|s)(?:[0-9]+(?:\.[0-9]+)?(?:h|m|s))*)`)

// Result captures an upstream execution result reported by a downstream CPA node.
type Result struct {
	AuthID     string
	AuthIndex  string
	Provider   string
	Model      string
	Success    bool
	Error      *Error
	RetryAfter *time.Duration
}

// MarkResult records a downstream execution result and updates auth cooldown state.
func (m *Manager) MarkResult(ctx context.Context, result Result) {
	// Keep validation before state changes so failures leave existing data intact.
	if m == nil || (strings.TrimSpace(result.AuthID) == "" && strings.TrimSpace(result.AuthIndex) == "") {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	shouldResumeModel := false
	shouldSuspendModel := false
	suspendReason := ""
	clearModelQuota := false
	setModelQuota := false
	var authSnapshot *Auth
	resultModel := canonicalModelKey(result.Model)
	resultAuthID := ""

	m.mu.Lock()
	auth := m.resultAuthLocked(result)
	if auth != nil {
		resultAuthID = auth.ID
		now := time.Now()
		auth.recordRecentRequest(now, result.Success)
		if result.Success {
			auth.Success++
		} else {
			auth.Failed++
		}

		if result.Success {
			if resultModel != "" {
				state := ensureModelState(auth, resultModel)
				resetModelState(state, now)
				updateAggregatedAvailability(auth, now)
				if !hasModelError(auth, now) {
					auth.LastError = nil
					auth.StatusMessage = ""
					auth.Status = StatusActive
				}
				auth.UpdatedAt = now
				shouldResumeModel = true
				clearModelQuota = true
			} else {
				clearAuthStateOnSuccess(auth, now)
			}
		} else {
			if resultModel != "" {
				if !isRequestScopedNotFoundResultError(result.Error) {
					disableCooling := m.quotaCooldownDisabledForAuth(auth)
					state := ensureModelState(auth, resultModel)
					state.Unavailable = true
					state.Status = StatusError
					state.UpdatedAt = now
					if result.Error != nil {
						state.LastError = cloneError(result.Error)
						state.StatusMessage = result.Error.Message
						auth.LastError = cloneError(result.Error)
						auth.StatusMessage = result.Error.Message
					}

					statusCode := statusCodeFromResult(result.Error)
					disableAuth := false
					if isModelSupportResultError(result.Error) {
						next := now.Add(12 * time.Hour)
						state.NextRetryAfter = next
						suspendReason = "model_not_supported"
						shouldSuspendModel = true
					} else {
						switch statusCode {
						case http.StatusUnauthorized:
							disableAuth = true
							suspendReason = "unauthorized"
							shouldSuspendModel = true
						case http.StatusPaymentRequired, http.StatusForbidden:
							if disableCooling {
								state.NextRetryAfter = time.Time{}
							} else {
								next := now.Add(30 * time.Minute)
								state.NextRetryAfter = next
								suspendReason = "payment_required"
								shouldSuspendModel = true
							}
						case http.StatusNotFound:
							if disableCooling {
								state.NextRetryAfter = time.Time{}
							} else {
								next := now.Add(12 * time.Hour)
								state.NextRetryAfter = next
								suspendReason = "not_found"
								shouldSuspendModel = true
							}
						case http.StatusTooManyRequests:
							next, backoffLevel := nextQuotaRecoverAt(now, result.RetryAfter, state.Quota, disableCooling)
							state.NextRetryAfter = next
							state.Quota = QuotaState{
								Exceeded:      true,
								Reason:        "quota",
								NextRecoverAt: next,
								BackoffLevel:  backoffLevel,
							}
							if !disableCooling {
								suspendReason = "quota"
								shouldSuspendModel = true
								setModelQuota = true
							}
						case http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
							if disableCooling {
								state.NextRetryAfter = time.Time{}
							} else {
								state.NextRetryAfter = now.Add(time.Minute)
							}
						default:
							state.NextRetryAfter = time.Time{}
						}
					}

					if disableAuth {
						disableAuthAfterUnauthorized(auth, state, result.Error, now)
					} else {
						auth.Status = StatusError
						auth.UpdatedAt = now
						updateAggregatedAvailability(auth, now)
					}
				}
			} else {
				applyAuthFailureState(m, auth, result.Error, result.RetryAfter, now)
			}
		}

		authSnapshot = auth.Clone()
	}
	m.mu.Unlock()

	if m.scheduler != nil && authSnapshot != nil {
		m.scheduler.upsertAuth(authSnapshot)
	}
	if authSnapshot != nil && authRefreshDisabled(authSnapshot) {
		m.queueRefreshReschedule(authSnapshot.ID)
	}
	m.enqueueResultPersist(ctx, authSnapshot)
	if resultAuthID == "" {
		return
	}
	if clearModelQuota && resultModel != "" {
		registry.GetGlobalRegistry().ClearModelQuotaExceeded(resultAuthID, resultModel)
	}
	if setModelQuota && resultModel != "" {
		registry.GetGlobalRegistry().SetModelQuotaExceeded(resultAuthID, resultModel)
	}
	if shouldResumeModel {
		registry.GetGlobalRegistry().ResumeClientModel(resultAuthID, resultModel)
	} else if shouldSuspendModel {
		registry.GetGlobalRegistry().SuspendClientModel(resultAuthID, resultModel, suspendReason)
	}
}

// resultAuthLocked handles a result auth locked.
func (m *Manager) resultAuthLocked(result Result) *Auth {
	if m == nil {
		return nil
	}
	if id := strings.TrimSpace(result.AuthID); id != "" {
		return m.auths[id]
	}
	if index := strings.TrimSpace(result.AuthIndex); index != "" {
		return m.indexAuth[index]
	}
	return nil
}

// quotaCooldownDisabledForAuth returns a quota cooldown disabled for auth.
func (m *Manager) quotaCooldownDisabledForAuth(auth *Auth) bool {
	if auth != nil {
		if override, ok := auth.DisableCoolingOverride(); ok {
			if override {
				return true
			}
		}
	}
	cfg, _ := m.runtimeConfig.Load().(*config.Config)
	return cfg != nil && cfg.DisableCooling
}

// ensureModelState ensures a model state.
func ensureModelState(auth *Auth, model string) *ModelState {
	if auth == nil || model == "" {
		return nil
	}
	if auth.ModelStates == nil {
		auth.ModelStates = make(map[string]*ModelState)
	}
	if state, ok := auth.ModelStates[model]; ok && state != nil {
		return state
	}
	state := &ModelState{Status: StatusActive}
	auth.ModelStates[model] = state
	return state
}

// resetModelState resets a model state.
func resetModelState(state *ModelState, now time.Time) {
	if state == nil {
		return
	}
	state.Unavailable = false
	state.Status = StatusActive
	state.StatusMessage = ""
	state.NextRetryAfter = time.Time{}
	state.LastError = nil
	state.Quota = QuotaState{}
	state.UpdatedAt = now
}

// updateAggregatedAvailability updates an aggregated availability.
func updateAggregatedAvailability(auth *Auth, now time.Time) {
	// Keep validation before state changes so failures leave existing data intact.
	if auth == nil {
		return
	}
	if len(auth.ModelStates) == 0 {
		clearAggregatedAvailability(auth)
		return
	}
	allUnavailable := true
	earliestRetry := time.Time{}
	quotaExceeded := false
	quotaRecover := time.Time{}
	maxBackoffLevel := 0
	hasState := false
	for _, state := range auth.ModelStates {
		if state == nil {
			continue
		}
		hasState = true
		stateUnavailable := false
		if state.Status == StatusDisabled {
			stateUnavailable = true
		} else if state.Unavailable {
			if state.NextRetryAfter.IsZero() {
				stateUnavailable = false
			} else if state.NextRetryAfter.After(now) {
				stateUnavailable = true
				if earliestRetry.IsZero() || state.NextRetryAfter.Before(earliestRetry) {
					earliestRetry = state.NextRetryAfter
				}
			} else {
				state.Unavailable = false
				state.NextRetryAfter = time.Time{}
			}
		}
		if !stateUnavailable {
			allUnavailable = false
		}
		if state.Quota.Exceeded {
			quotaExceeded = true
			if quotaRecover.IsZero() || (!state.Quota.NextRecoverAt.IsZero() && state.Quota.NextRecoverAt.Before(quotaRecover)) {
				quotaRecover = state.Quota.NextRecoverAt
			}
			if state.Quota.BackoffLevel > maxBackoffLevel {
				maxBackoffLevel = state.Quota.BackoffLevel
			}
		}
	}
	if !hasState {
		clearAggregatedAvailability(auth)
		return
	}
	auth.Unavailable = allUnavailable
	if allUnavailable {
		auth.NextRetryAfter = earliestRetry
	} else {
		auth.NextRetryAfter = time.Time{}
	}
	if quotaExceeded {
		auth.Quota.Exceeded = true
		auth.Quota.Reason = "quota"
		auth.Quota.NextRecoverAt = quotaRecover
		auth.Quota.BackoffLevel = maxBackoffLevel
	} else {
		auth.Quota.Exceeded = false
		auth.Quota.Reason = ""
		auth.Quota.NextRecoverAt = time.Time{}
		auth.Quota.BackoffLevel = 0
	}
}

// clearAggregatedAvailability clears an aggregated availability.
func clearAggregatedAvailability(auth *Auth) {
	if auth == nil {
		return
	}
	auth.Unavailable = false
	auth.NextRetryAfter = time.Time{}
	auth.Quota = QuotaState{}
}

// hasModelError reports whether model error is present.
func hasModelError(auth *Auth, now time.Time) bool {
	if auth == nil || len(auth.ModelStates) == 0 {
		return false
	}
	for _, state := range auth.ModelStates {
		if state == nil {
			continue
		}
		if state.LastError != nil {
			return true
		}
		if state.Status == StatusError {
			if state.Unavailable && (state.NextRetryAfter.IsZero() || state.NextRetryAfter.After(now)) {
				return true
			}
		}
	}
	return false
}

// clearAuthStateOnSuccess clears an auth state on success.
func clearAuthStateOnSuccess(auth *Auth, now time.Time) {
	if auth == nil {
		return
	}
	auth.Unavailable = false
	auth.Status = StatusActive
	auth.StatusMessage = ""
	auth.Quota.Exceeded = false
	auth.Quota.Reason = ""
	auth.Quota.NextRecoverAt = time.Time{}
	auth.Quota.BackoffLevel = 0
	auth.LastError = nil
	auth.NextRetryAfter = time.Time{}
	auth.UpdatedAt = now
}

// cloneError clones an error.
func cloneError(err *Error) *Error {
	if err == nil {
		return nil
	}
	return &Error{
		Code:       err.Code,
		Message:    err.Message,
		Retryable:  err.Retryable,
		HTTPStatus: err.HTTPStatus,
	}
}

// statusCodeFromResult derives status code from result.
func statusCodeFromResult(err *Error) int {
	if err == nil {
		return 0
	}
	return err.StatusCode()
}

// isModelSupportErrorMessage reports whether model support error message.
func isModelSupportErrorMessage(message string) bool {
	// Normalize source data before building the derived payload.
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	patterns := [...]string{
		"model_not_supported",
		"requested model is not supported",
		"requested model is unsupported",
		"requested model is unavailable",
		"model is not supported",
		"model not supported",
		"unsupported model",
		"model unavailable",
		"not available for your plan",
		"not available for your account",
	}
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// isModelSupportResultError reports whether model support result error.
func isModelSupportResultError(err *Error) bool {
	if err == nil {
		return false
	}
	status := statusCodeFromResult(err)
	if status != http.StatusBadRequest && status != http.StatusUnprocessableEntity {
		return false
	}
	return isModelSupportErrorMessage(err.Message)
}

// isRequestScopedNotFoundMessage reports whether request scoped not found message.
func isRequestScopedNotFoundMessage(message string) bool {
	if message == "" {
		return false
	}
	lower := strings.ToLower(message)
	return strings.Contains(lower, "item with id") &&
		strings.Contains(lower, "not found") &&
		strings.Contains(lower, "items are not persisted when `store` is set to false")
}

// isRequestScopedNotFoundResultError reports whether request scoped not found result error.
func isRequestScopedNotFoundResultError(err *Error) bool {
	if err == nil || statusCodeFromResult(err) != http.StatusNotFound {
		return false
	}
	return isRequestScopedNotFoundMessage(err.Message)
}

// applyAuthFailureState applies an auth failure state.
func applyAuthFailureState(m *Manager, auth *Auth, resultErr *Error, retryAfter *time.Duration, now time.Time) {
	// Normalize auth state before updating runtime indexes.
	if auth == nil {
		return
	}
	if isRequestScopedNotFoundResultError(resultErr) {
		return
	}
	disableCooling := m.quotaCooldownDisabledForAuth(auth)
	auth.Unavailable = true
	auth.Status = StatusError
	auth.UpdatedAt = now
	if resultErr != nil {
		auth.LastError = cloneError(resultErr)
		if resultErr.Message != "" {
			auth.StatusMessage = resultErr.Message
		}
	}
	statusCode := statusCodeFromResult(resultErr)
	switch statusCode {
	case http.StatusUnauthorized:
		disableAuthAfterUnauthorized(auth, nil, resultErr, now)
	case http.StatusPaymentRequired, http.StatusForbidden:
		auth.StatusMessage = "payment_required"
		if disableCooling {
			auth.NextRetryAfter = time.Time{}
		} else {
			auth.NextRetryAfter = now.Add(30 * time.Minute)
		}
	case http.StatusNotFound:
		auth.StatusMessage = "not_found"
		if disableCooling {
			auth.NextRetryAfter = time.Time{}
		} else {
			auth.NextRetryAfter = now.Add(12 * time.Hour)
		}
	case http.StatusTooManyRequests:
		auth.StatusMessage = "quota exhausted"
		auth.Quota.Exceeded = true
		auth.Quota.Reason = "quota"
		next, backoffLevel := nextQuotaRecoverAt(now, retryAfter, auth.Quota, disableCooling)
		auth.Quota.BackoffLevel = backoffLevel
		auth.Quota.NextRecoverAt = next
		auth.NextRetryAfter = next
	case http.StatusRequestTimeout, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		auth.StatusMessage = "transient upstream error"
		if disableCooling {
			auth.NextRetryAfter = time.Time{}
		} else {
			auth.NextRetryAfter = now.Add(time.Minute)
		}
	default:
		if auth.StatusMessage == "" {
			auth.StatusMessage = "request failed"
		}
	}
}

// disableAuthAfterUnauthorized removes a credential from request and refresh retries.
func disableAuthAfterUnauthorized(auth *Auth, state *ModelState, resultErr *Error, now time.Time) {
	if auth == nil {
		return
	}
	auth.Disabled = true
	auth.Unavailable = true
	auth.Status = StatusDisabled
	auth.StatusMessage = "unauthorized"
	auth.NextRetryAfter = time.Time{}
	auth.NextRefreshAfter = time.Time{}
	auth.Quota = QuotaState{}
	auth.UpdatedAt = now
	if resultErr != nil {
		auth.LastError = cloneError(resultErr)
	} else if auth.LastError == nil {
		auth.LastError = &Error{Message: http.StatusText(http.StatusUnauthorized), HTTPStatus: http.StatusUnauthorized}
	}
	if state == nil {
		return
	}
	state.Unavailable = true
	state.Status = StatusDisabled
	state.StatusMessage = "unauthorized"
	state.NextRetryAfter = time.Time{}
	state.Quota = QuotaState{}
	state.UpdatedAt = now
	if resultErr != nil {
		state.LastError = cloneError(resultErr)
	} else if state.LastError == nil {
		state.LastError = &Error{Message: http.StatusText(http.StatusUnauthorized), HTTPStatus: http.StatusUnauthorized}
	}
}

// nextQuotaCooldown returns a next quota cooldown.
func nextQuotaCooldown(prevLevel int, disableCooling bool) (time.Duration, int) {
	if prevLevel < 0 {
		prevLevel = 0
	}
	if disableCooling {
		return 0, prevLevel
	}
	cooldown := quotaBackoffBase * time.Duration(1<<prevLevel)
	if cooldown < quotaBackoffBase {
		cooldown = quotaBackoffBase
	}
	if cooldown >= quotaBackoffMax {
		return quotaBackoffMax, prevLevel
	}
	return cooldown, prevLevel + 1
}

func nextQuotaRecoverAt(now time.Time, retryAfter *time.Duration, quota QuotaState, disableCooling bool) (time.Time, int) {
	if disableCooling {
		return time.Time{}, quota.BackoffLevel
	}
	if retryAfter != nil && *retryAfter > 0 {
		return now.Add(*retryAfter), quota.BackoffLevel
	}
	return quotaCooldownAfterFailure(quota, now)
}

// quotaCooldownAfterFailure returns the recovery deadline and backoff level for
// a quota failure observed at now. Failures that land while a previous quota
// window is still open reuse that window instead of escalating, so a burst of
// concurrent failures advances the backoff ladder at most once per window.
func quotaCooldownAfterFailure(quota QuotaState, now time.Time) (time.Time, int) {
	if quota.NextRecoverAt.After(now) {
		return quota.NextRecoverAt, quota.BackoffLevel
	}
	cooldown, nextLevel := nextQuotaCooldown(quota.BackoffLevel, false)
	if cooldown <= 0 {
		return time.Time{}, nextLevel
	}
	return now.Add(cooldown), nextLevel
}

// NewUsageResult creates a new usage result.
func NewUsageResult(authIndex, provider, model string, statusCode int, body string) Result {
	// Keep validation before state changes so failures leave existing data intact.
	authIndex = strings.TrimSpace(authIndex)
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	body = strings.TrimSpace(body)
	if statusCode <= 0 {
		statusCode = http.StatusOK
	}
	if statusCode == http.StatusOK {
		return Result{
			AuthIndex: authIndex,
			Provider:  provider,
			Model:     model,
			Success:   true,
		}
	}
	message := body
	if message == "" {
		message = http.StatusText(statusCode)
	}
	if message == "" {
		message = fmt.Sprintf("request failed with status %d", statusCode)
	}
	return Result{
		AuthIndex:  authIndex,
		Provider:   provider,
		Model:      model,
		Success:    false,
		RetryAfter: parseUsageRetryAfter(body, statusCode),
		Error: &Error{
			Message:    message,
			HTTPStatus: statusCode,
		},
	}
}

func parseUsageRetryAfter(body string, statusCode int) *time.Duration {
	if statusCode != http.StatusTooManyRequests {
		return nil
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}
	if gjson.Valid(body) {
		if retryAfter := parseGoogleRetryDelay(body); retryAfter != nil {
			return retryAfter
		}
		if retryAfter := parseRetryDelayFromMessage(gjson.Get(body, "error.message").String()); retryAfter != nil {
			return retryAfter
		}
	}
	return parseRetryDelayFromMessage(body)
}

func parseGoogleRetryDelay(body string) *time.Duration {
	details := gjson.Get(body, "error.details")
	if !details.Exists() || !details.IsArray() {
		return nil
	}
	for _, detail := range details.Array() {
		if detail.Get("@type").String() != "type.googleapis.com/google.rpc.RetryInfo" {
			continue
		}
		if retryAfter := parseDurationPointer(detail.Get("retryDelay").String()); retryAfter != nil {
			return retryAfter
		}
	}
	for _, detail := range details.Array() {
		if detail.Get("@type").String() != "type.googleapis.com/google.rpc.ErrorInfo" {
			continue
		}
		if retryAfter := parseDurationPointer(detail.Get("metadata.quotaResetDelay").String()); retryAfter != nil {
			return retryAfter
		}
	}
	return nil
}

func parseRetryDelayFromMessage(message string) *time.Duration {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	matches := usageRetryDelayPattern.FindStringSubmatch(message)
	if len(matches) < 2 {
		return nil
	}
	return parseDurationPointer(matches[1])
}

func parseDurationPointer(value string) *time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	duration, errParse := time.ParseDuration(value)
	if errParse != nil || duration <= 0 {
		return nil
	}
	return &duration
}
