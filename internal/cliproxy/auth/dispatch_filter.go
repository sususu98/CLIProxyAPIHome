package auth

import "strings"

func allowedAuthIDsFromOptions(opts Options) map[string]struct{} {
	if opts.Metadata == nil {
		return nil
	}
	raw, ok := opts.Metadata[AllowedAuthIDsMetadataKey]
	if !ok {
		return nil
	}

	allowed := make(map[string]struct{})
	switch values := raw.(type) {
	case []string:
		for _, value := range values {
			authID := strings.TrimSpace(value)
			if authID != "" {
				allowed[authID] = struct{}{}
			}
		}
	case []any:
		for _, value := range values {
			authID := strings.TrimSpace(toString(value))
			if authID != "" {
				allowed[authID] = struct{}{}
			}
		}
	case map[string]struct{}:
		for value := range values {
			authID := strings.TrimSpace(value)
			if authID != "" {
				allowed[authID] = struct{}{}
			}
		}
	case map[string]bool:
		for value, enabled := range values {
			if !enabled {
				continue
			}
			authID := strings.TrimSpace(value)
			if authID != "" {
				allowed[authID] = struct{}{}
			}
		}
	}
	return allowed
}

func authAllowedByID(authID string, allowed map[string]struct{}) bool {
	if allowed == nil {
		return true
	}
	authID = strings.TrimSpace(authID)
	if authID == "" {
		return false
	}
	_, ok := allowed[authID]
	return ok
}

func schedulerPredicate(tried map[string]struct{}, allowed map[string]struct{}) func(*scheduledAuth) bool {
	return func(entry *scheduledAuth) bool {
		if entry == nil || entry.auth == nil {
			return false
		}
		if len(tried) > 0 {
			if _, ok := tried[entry.auth.ID]; ok {
				return false
			}
		}
		return authAllowedByID(entry.auth.ID, allowed)
	}
}

func toString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}
