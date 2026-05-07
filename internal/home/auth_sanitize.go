package home

import (
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPIHome/internal/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPIHome/internal/runtime/geminicli"
)

// SanitizeAuthForDownstream removes refresh-capable credentials from the auth payload
// before it is returned to downstream CPA nodes.
func SanitizeAuthForDownstream(auth *coreauth.Auth) *coreauth.Auth {
	if auth == nil {
		return nil
	}

	out := auth.Clone()

	if shared := geminicli.ResolveSharedCredential(out.Runtime); shared != nil {
		if snapshot := shared.MetadataSnapshot(); len(snapshot) > 0 {
			merged := make(map[string]any, len(snapshot))
			for k, v := range snapshot {
				merged[k] = v
			}
			if out.Metadata != nil {
				for k, v := range out.Metadata {
					merged[k] = v
				}
			}
			out.Metadata = merged
		}
	}

	if out.Attributes != nil {
		delete(out.Attributes, "refresh_token")
		delete(out.Attributes, "service_account")
	}

	if out.Metadata == nil {
		return out
	}

	delete(out.Metadata, "refresh_token")
	delete(out.Metadata, "service_account")

	removeRefreshTokenFromNestedMap(out.Metadata, "token")
	removeRefreshTokenFromNestedMap(out.Metadata, "Token")

	return out
}

func removeRefreshTokenFromNestedMap(meta map[string]any, key string) {
	if meta == nil {
		return
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return
	}

	switch typed := raw.(type) {
	case map[string]any:
		cloned := make(map[string]any, len(typed))
		for k, v := range typed {
			cloned[k] = v
		}
		delete(cloned, "refresh_token")
		delete(cloned, "refreshToken")
		meta[key] = cloned
	case map[string]string:
		cloned := make(map[string]string, len(typed))
		for k, v := range typed {
			cloned[k] = v
		}
		delete(cloned, "refresh_token")
		delete(cloned, "refreshToken")
		meta[key] = cloned
	default:
		if s, okString := raw.(string); okString && strings.TrimSpace(s) == "" {
			return
		}
	}
}
