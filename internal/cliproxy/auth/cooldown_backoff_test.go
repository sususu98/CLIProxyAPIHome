package auth

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func quotaResult(authID, model string) Result {
	return Result{
		AuthID:   authID,
		Provider: "codex",
		Model:    model,
		Success:  false,
		Error: &Error{
			Message:    "quota",
			Retryable:  true,
			HTTPStatus: http.StatusTooManyRequests,
		},
	}
}

func TestMarkResultQuotaBackoffEscalatesOncePerWindow(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	auth := &Auth{
		ID:       "auth-quota-window",
		Provider: "codex",
	}
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("Register returned error: %v", errRegister)
	}

	manager.MarkResult(context.Background(), quotaResult(auth.ID, "gpt-5"))
	first, ok := manager.GetByID(auth.ID)
	if !ok || first == nil || first.ModelStates["gpt-5"] == nil {
		t.Fatalf("expected model state after first failure")
	}
	firstState := first.ModelStates["gpt-5"]
	if firstState.Quota.BackoffLevel != 1 {
		t.Fatalf("expected BackoffLevel 1 after first failure, got %d", firstState.Quota.BackoffLevel)
	}
	if !firstState.Quota.NextRecoverAt.After(time.Now()) {
		t.Fatalf("expected open cooldown window after first failure, got %v", firstState.Quota.NextRecoverAt)
	}

	// A second in-flight failure lands while the first window is still open.
	manager.MarkResult(context.Background(), quotaResult(auth.ID, "gpt-5"))
	second, ok := manager.GetByID(auth.ID)
	if !ok || second == nil || second.ModelStates["gpt-5"] == nil {
		t.Fatalf("expected model state after second failure")
	}
	secondState := second.ModelStates["gpt-5"]
	if secondState.Quota.BackoffLevel != 1 {
		t.Fatalf("expected BackoffLevel to stay 1 for in-window failure, got %d", secondState.Quota.BackoffLevel)
	}
	if !secondState.Quota.NextRecoverAt.Equal(firstState.Quota.NextRecoverAt) {
		t.Fatalf("expected NextRecoverAt to stay %v for in-window failure, got %v", firstState.Quota.NextRecoverAt, secondState.Quota.NextRecoverAt)
	}
	if !secondState.NextRetryAfter.Equal(firstState.NextRetryAfter) {
		t.Fatalf("expected NextRetryAfter to stay %v for in-window failure, got %v", firstState.NextRetryAfter, secondState.NextRetryAfter)
	}
}

func TestMarkResultQuotaBackoffEscalatesAfterWindowExpiry(t *testing.T) {
	expired := time.Now().Add(-time.Second)
	manager := NewManager(nil, nil, nil)
	auth := &Auth{
		ID:       "auth-quota-expired",
		Provider: "codex",
		ModelStates: map[string]*ModelState{
			"gpt-5": {
				Status:         StatusError,
				Unavailable:    true,
				NextRetryAfter: expired,
				Quota:          QuotaState{Exceeded: true, Reason: "quota", NextRecoverAt: expired, BackoffLevel: 3},
			},
		},
	}
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("Register returned error: %v", errRegister)
	}

	manager.MarkResult(context.Background(), quotaResult(auth.ID, "gpt-5"))
	updated, ok := manager.GetByID(auth.ID)
	if !ok || updated == nil || updated.ModelStates["gpt-5"] == nil {
		t.Fatalf("expected model state after failure")
	}
	state := updated.ModelStates["gpt-5"]
	if state.Quota.BackoffLevel != 4 {
		t.Fatalf("expected BackoffLevel 4 after post-window failure, got %d", state.Quota.BackoffLevel)
	}
	if !state.Quota.NextRecoverAt.After(time.Now()) {
		t.Fatalf("expected a fresh cooldown window, got %v", state.Quota.NextRecoverAt)
	}
}

func TestApplyAuthFailureStateQuotaBackoffOncePerWindow(t *testing.T) {
	manager := NewManager(nil, nil, nil)
	now := time.Now()
	quotaErr := &Error{Message: "quota", HTTPStatus: http.StatusTooManyRequests}
	auth := &Auth{ID: "auth-level-quota"}

	applyAuthFailureState(manager, auth, quotaErr, nil, now)
	if auth.Quota.BackoffLevel != 1 {
		t.Fatalf("expected BackoffLevel 1 after first failure, got %d", auth.Quota.BackoffLevel)
	}
	firstRecover := auth.Quota.NextRecoverAt
	if !firstRecover.Equal(now.Add(time.Second)) {
		t.Fatalf("expected first window to close at %v, got %v", now.Add(time.Second), firstRecover)
	}

	// In-window failure keeps the current window and level.
	applyAuthFailureState(manager, auth, quotaErr, nil, now.Add(100*time.Millisecond))
	if auth.Quota.BackoffLevel != 1 {
		t.Fatalf("expected BackoffLevel to stay 1 for in-window failure, got %d", auth.Quota.BackoffLevel)
	}
	if !auth.Quota.NextRecoverAt.Equal(firstRecover) {
		t.Fatalf("expected NextRecoverAt to stay %v for in-window failure, got %v", firstRecover, auth.Quota.NextRecoverAt)
	}

	// A failure after the window expired escalates to the next level.
	applyAuthFailureState(manager, auth, quotaErr, nil, now.Add(2*time.Second))
	if auth.Quota.BackoffLevel != 2 {
		t.Fatalf("expected BackoffLevel 2 after post-window failure, got %d", auth.Quota.BackoffLevel)
	}
	if !auth.Quota.NextRecoverAt.Equal(now.Add(4 * time.Second)) {
		t.Fatalf("expected second window to close at %v, got %v", now.Add(4*time.Second), auth.Quota.NextRecoverAt)
	}

	// A provider supplied retry hint always takes effect, even in-window.
	retryAfter := 10 * time.Second
	applyAuthFailureState(manager, auth, quotaErr, &retryAfter, now.Add(3*time.Second))
	if auth.Quota.BackoffLevel != 2 {
		t.Fatalf("expected BackoffLevel to stay 2 with retry hint, got %d", auth.Quota.BackoffLevel)
	}
	if !auth.Quota.NextRecoverAt.Equal(now.Add(13 * time.Second)) {
		t.Fatalf("expected retry hint window to close at %v, got %v", now.Add(13*time.Second), auth.Quota.NextRecoverAt)
	}
}

func TestNextQuotaCooldownLadder(t *testing.T) {
	cases := []struct {
		prevLevel    int
		wantCooldown time.Duration
		wantLevel    int
	}{
		{-3, time.Second, 1},
		{0, time.Second, 1},
		{1, 2 * time.Second, 2},
		{5, 32 * time.Second, 6},
		{10, 1024 * time.Second, 11},
		{11, quotaBackoffMax, 11},
		{20, quotaBackoffMax, 20},
	}
	for _, tc := range cases {
		cooldown, level := nextQuotaCooldown(tc.prevLevel, false)
		if cooldown != tc.wantCooldown || level != tc.wantLevel {
			t.Fatalf("nextQuotaCooldown(%d) = (%v, %d), want (%v, %d)", tc.prevLevel, cooldown, level, tc.wantCooldown, tc.wantLevel)
		}
	}

	if cooldown, level := nextQuotaCooldown(4, true); cooldown != 0 || level != 4 {
		t.Fatalf("nextQuotaCooldown with cooling disabled = (%v, %d), want (0, 4)", cooldown, level)
	}
}
