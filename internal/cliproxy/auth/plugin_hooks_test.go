package auth

import (
	"context"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type unhandledSchedulerPlugin struct{}

func (s *unhandledSchedulerPlugin) PickAuth(context.Context, pluginapi.SchedulerPickRequest) (pluginapi.SchedulerPickResponse, bool, error) {
	return pluginapi.SchedulerPickResponse{}, false, nil
}

func TestDispatchPluginUnhandledPreservesBuiltinScheduler(t *testing.T) {
	manager := NewManager(nil, &RoundRobinSelector{}, nil)
	first := geminiAPIKeyAuth("plugin-fallback-a", "first-key", "1")
	second := geminiAPIKeyAuth("plugin-fallback-b", "second-key", "1")
	registerDispatchTestAuth(t, manager, first, "plugin-fallback-model")
	registerDispatchTestAuth(t, manager, second, "plugin-fallback-model")

	decision, errDispatch := manager.Dispatch(context.Background(), []string{"gemini"}, "plugin-fallback-model", Options{})
	if errDispatch != nil {
		t.Fatalf("first Dispatch() error = %v", errDispatch)
	}
	if decision == nil || decision.Auth == nil || decision.Auth.ID != first.ID {
		t.Fatalf("first Dispatch() auth = %#v, want %s", decision, first.ID)
	}

	manager.SetPluginScheduler(&unhandledSchedulerPlugin{})
	decision, errDispatch = manager.Dispatch(context.Background(), []string{"gemini"}, "plugin-fallback-model", Options{})
	if errDispatch != nil {
		t.Fatalf("second Dispatch() error = %v", errDispatch)
	}
	if decision == nil || decision.Auth == nil || decision.Auth.ID != second.ID {
		t.Fatalf("second Dispatch() auth = %#v, want builtin scheduler to continue with %s", decision, second.ID)
	}
}

func TestSchedulerAuthCandidatesAreSanitized(t *testing.T) {
	auth := &Auth{
		ID:       "auth-a",
		Provider: "Gemini",
		Status:   StatusActive,
		Attributes: map[string]string{
			"access_token":  "token-value",
			"api_key":       "api-key-value",
			"cookie":        "cookie-value",
			"priority":      "7",
			"team":          "alpha",
			"authorization": "bearer token",
		},
		Metadata: map[string]any{
			"tenant":        "one",
			"refresh_token": "refresh-token",
		},
	}

	candidates := schedulerAuthCandidates([]*Auth{auth})
	if len(candidates) != 1 {
		t.Fatalf("len(candidates) = %d, want 1", len(candidates))
	}

	candidate := candidates[0]
	if candidate.ID != "auth-a" || candidate.Provider != "gemini" || candidate.Priority != 7 || candidate.Status != string(StatusActive) {
		t.Fatalf("candidate = %#v, want sanitized auth-a", candidate)
	}
	for _, key := range []string{"access_token", "api_key", "cookie", "authorization"} {
		if _, ok := candidate.Attributes[key]; ok {
			t.Fatalf("candidate Attributes contains sensitive key %q", key)
		}
	}
	if candidate.Attributes["priority"] != "7" || candidate.Attributes["team"] != "alpha" {
		t.Fatalf("candidate Attributes = %#v, want priority/team", candidate.Attributes)
	}
	if len(candidate.Metadata) != 0 {
		t.Fatalf("candidate Metadata = %#v, want empty", candidate.Metadata)
	}

	candidate.Attributes["team"] = "mutated"
	if auth.Attributes["team"] != "alpha" {
		t.Fatalf("source attribute team = %q, want alpha", auth.Attributes["team"])
	}
}
