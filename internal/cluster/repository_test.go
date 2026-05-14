package cluster

import (
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
)

func TestHydrateAuthListRuntimesInheritsGeminiVirtualProxy(t *testing.T) {
	t.Parallel()

	parent := &coreauth.Auth{
		ID:       "parent-auth",
		Provider: "gemini-cli",
		Prefix:   "team-a",
		ProxyURL: "http://parent-proxy.example:8080",
		Attributes: map[string]string{
			"gemini_virtual_primary": "true",
			"virtual_children":       "project-a,project-b",
		},
		Metadata: map[string]any{
			"type":       "gemini",
			"email":      "user@example.com",
			"project_id": "project-a,project-b",
		},
	}
	child := &coreauth.Auth{
		ID:       "child-auth",
		Provider: "gemini-cli",
		ProxyURL: "http://stale-proxy.example:8080",
		Attributes: map[string]string{
			"gemini_virtual_parent":  parent.ID,
			"gemini_virtual_project": "project-a",
			"runtime_only":           "true",
		},
		Metadata: map[string]any{
			"type":       "gemini",
			"virtual":    true,
			"project_id": "project-a",
		},
	}

	hydrateAuthListRuntimes([]*coreauth.Auth{child, parent})

	if child.ProxyURL != parent.ProxyURL {
		t.Fatalf("child proxy URL = %q, want %q", child.ProxyURL, parent.ProxyURL)
	}
	if child.Prefix != parent.Prefix {
		t.Fatalf("child prefix = %q, want %q", child.Prefix, parent.Prefix)
	}
}
