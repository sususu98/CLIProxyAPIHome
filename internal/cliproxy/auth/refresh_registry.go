package auth

import (
	"time"
)

var (
	codexRefreshLead       = 5 * 24 * time.Hour
	claudeRefreshLead      = 4 * time.Hour
	antigravityRefreshLead = 5 * time.Minute
	kimiRefreshLead        = 5 * time.Minute
)

func init() {
	registerRefreshLead("codex", &codexRefreshLead)
	registerRefreshLead("claude", &claudeRefreshLead)
	registerRefreshLead("gemini", nil)
	registerRefreshLead("gemini-cli", nil)
	registerRefreshLead("antigravity", &antigravityRefreshLead)
	registerRefreshLead("kimi", &kimiRefreshLead)
}

func registerRefreshLead(provider string, lead *time.Duration) {
	RegisterRefreshLeadProvider(provider, func() *time.Duration {
		if lead == nil || *lead <= 0 {
			return nil
		}
		return lead
	})
}
