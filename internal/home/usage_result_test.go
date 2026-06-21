package home

import (
	"context"
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
)

func newUsageResultTestRuntime(t *testing.T, auth *coreauth.Auth) *Runtime {
	t.Helper()
	manager := coreauth.NewManager(nil, nil, nil)
	if _, errRegister := manager.Register(context.Background(), auth); errRegister != nil {
		t.Fatalf("Register(%s) error = %v", auth.ID, errRegister)
	}
	return &Runtime{coreManager: manager}
}

func TestRecordUsagePayloadUsesModelAsUpstreamKey(t *testing.T) {
	auth := &coreauth.Auth{
		ID:       "usage-model-auth",
		Index:    "usage-model-index",
		Provider: "gemini",
		Status:   coreauth.StatusActive,
	}
	rt := newUsageResultTestRuntime(t, auth)

	rt.RecordUsagePayload(context.Background(), `{
		"auth_index": "usage-model-index",
		"provider": "gemini",
		"model": "upstream-model",
		"alias": "alias-model",
		"failed": true,
		"fail": {
			"status_code": 429,
			"body": "quota exhausted"
		}
	}`)

	got, ok := rt.coreManager.GetByID(auth.ID)
	if !ok || got == nil {
		t.Fatalf("GetByID(%s) missing auth after usage payload", auth.ID)
	}
	state := got.ModelStates["upstream-model"]
	if state == nil {
		t.Fatalf("ModelStates[upstream-model] missing after usage payload: %#v", got.ModelStates)
	}
	if _, exists := got.ModelStates["alias-model"]; exists {
		t.Fatalf("ModelStates contains alias-model, want upstream model only: %#v", got.ModelStates)
	}
	if !state.Unavailable || !state.Quota.Exceeded || state.NextRetryAfter.IsZero() {
		t.Fatalf("ModelStates[upstream-model] = %#v, want 429 cooldown state", state)
	}
}

func TestRecordUsagePayloadFallsBackToAliasWhenModelEmpty(t *testing.T) {
	auth := &coreauth.Auth{
		ID:       "usage-alias-auth",
		Index:    "usage-alias-index",
		Provider: "gemini",
		Status:   coreauth.StatusActive,
	}
	rt := newUsageResultTestRuntime(t, auth)

	rt.RecordUsagePayload(context.Background(), `{
		"auth_index": "usage-alias-index",
		"provider": "gemini",
		"model": "",
		"alias": "alias-model",
		"failed": true,
		"fail": {
			"status_code": 429,
			"body": "quota exhausted"
		}
	}`)

	got, ok := rt.coreManager.GetByID(auth.ID)
	if !ok || got == nil {
		t.Fatalf("GetByID(%s) missing auth after usage payload", auth.ID)
	}
	state := got.ModelStates["alias-model"]
	if state == nil {
		t.Fatalf("ModelStates[alias-model] missing after usage payload: %#v", got.ModelStates)
	}
	if !state.Unavailable || !state.Quota.Exceeded || state.NextRetryAfter.IsZero() {
		t.Fatalf("ModelStates[alias-model] = %#v, want 429 cooldown state", state)
	}
}
