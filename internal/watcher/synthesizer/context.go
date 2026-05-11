package synthesizer

import (
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
)

// SynthesisContext provides the context needed for auth synthesis.
type SynthesisContext struct {
	// Config is the current configuration
	Config *config.Config
	// AuthDir is the directory containing auth files
	AuthDir string
	// Now is the current time for timestamps
	Now time.Time
	// IDGenerator generates stable IDs for auth entries
	IDGenerator *StableIDGenerator
	// ClusterMode enables cluster UUID normalization for synthesized auth entries.
	ClusterMode bool
	// UUIDForAuth returns the cluster UUID for an auth entry.
	UUIDForAuth func(auth *coreauth.Auth) string
}

func applyClusterUUID(ctx *SynthesisContext, auth *coreauth.Auth) {
	if ctx == nil || !ctx.ClusterMode || ctx.UUIDForAuth == nil || auth == nil {
		return
	}
	uuidValue := strings.TrimSpace(ctx.UUIDForAuth(auth))
	if uuidValue == "" {
		return
	}
	auth.ID = uuidValue
	auth.Index = uuidValue
	if auth.Attributes == nil {
		auth.Attributes = make(map[string]string)
	}
	auth.Attributes["cluster_uuid"] = uuidValue
	if auth.Metadata != nil {
		auth.Metadata["uuid"] = uuidValue
	}
}
