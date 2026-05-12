package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPIHome/internal/auth/antigravity"
	claudeauth "github.com/router-for-me/CLIProxyAPIHome/internal/auth/claude"
	codexauth "github.com/router-for-me/CLIProxyAPIHome/internal/auth/codex"
	kimiauth "github.com/router-for-me/CLIProxyAPIHome/internal/auth/kimi"
	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/runtime/geminicli"
	log "github.com/sirupsen/logrus"
)

// refreshCredential refreshes auth metadata when a refresh token is present.
// It is best-effort: providers that do not support refresh are treated as no-op.
func refreshCredential(ctx context.Context, cfg *config.Config, auth *Auth, rt http.RoundTripper) (*Auth, error) {
	// Resolve credential context before calling upstream OAuth services.
	if auth == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	provider := strings.ToLower(strings.TrimSpace(auth.Provider))
	switch provider {
	case "codex":
		return refreshCodex(ctx, cfg, auth)
	case "claude":
		return refreshClaude(ctx, cfg, auth)
	case "gemini", "gemini-cli":
		return refreshGeminiCLI(ctx, cfg, auth, rt)
	case "kimi":
		return refreshKimi(ctx, cfg, auth)
	case "antigravity":
		return refreshAntigravity(ctx, cfg, auth, rt)
	default:
		return auth, nil
	}
}

// refreshGeminiCLI refreshes a gemini cli.
func refreshGeminiCLI(ctx context.Context, cfg *config.Config, auth *Auth, rt http.RoundTripper) (*Auth, error) {
	// Resolve credential context before calling upstream OAuth services.
	_ = cfg
	if auth == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	meta := auth.Metadata
	shared := geminicli.ResolveSharedCredential(auth.Runtime)
	if shared != nil {
		meta = shared.MetadataSnapshot()
	}
	if meta == nil {
		return auth, nil
	}

	tokenKey := ""
	tokenMap := map[string]any(nil)
	for _, key := range []string{"token", "Token"} {
		raw, ok := meta[key]
		if !ok || raw == nil {
			continue
		}
		switch typed := raw.(type) {
		case map[string]any:
			tokenKey = key
			tokenMap = typed
		case map[string]string:
			converted := make(map[string]any, len(typed))
			for k, v := range typed {
				converted[k] = v
			}
			tokenKey = key
			tokenMap = converted
		}
		if tokenMap != nil {
			break
		}
	}

	if tokenMap == nil {
		tokenMap = meta
	}

	refreshToken, _ := tokenMap["refresh_token"].(string)
	clientID, _ := tokenMap["client_id"].(string)
	clientSecret, _ := tokenMap["client_secret"].(string)
	tokenURI, _ := tokenMap["token_uri"].(string)
	refreshToken = strings.TrimSpace(refreshToken)
	clientID = strings.TrimSpace(clientID)
	clientSecret = strings.TrimSpace(clientSecret)
	tokenURI = strings.TrimSpace(tokenURI)
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}

	if refreshToken == "" || clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("gemini refresh: missing refresh credentials")
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)

	req, errReq := http.NewRequestWithContext(ctx, http.MethodPost, tokenURI, strings.NewReader(form.Encode()))
	if errReq != nil {
		return nil, errReq
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Go-http-client/2.0")

	client := &http.Client{Timeout: 30 * time.Second}
	if rt != nil {
		client.Transport = rt
	}
	resp, errDo := client.Do(req)
	if errDo != nil {
		return nil, errDo
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("gemini refresh: response body close error: %v", errClose)
		}
	}()
	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return nil, errRead
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("gemini refresh: oauth refresh failed: %s", strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if errUnmarshal := json.Unmarshal(body, &tokenResp); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return nil, fmt.Errorf("gemini refresh: no access_token in refresh response")
	}

	tokenMapCopy := make(map[string]any, len(tokenMap)+4)
	for k, v := range tokenMap {
		tokenMapCopy[k] = v
	}
	tokenMapCopy["access_token"] = strings.TrimSpace(tokenResp.AccessToken)
	if strings.TrimSpace(tokenResp.RefreshToken) != "" {
		tokenMapCopy["refresh_token"] = strings.TrimSpace(tokenResp.RefreshToken)
	}
	if tokenResp.ExpiresIn > 0 {
		expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second).UTC()
		if _, ok := tokenMapCopy["expiry_date"]; ok {
			tokenMapCopy["expiry_date"] = expiry.UnixMilli()
		} else if _, ok := tokenMapCopy["expired"]; ok {
			tokenMapCopy["expired"] = expiry.Format(time.RFC3339)
		} else if _, ok := tokenMapCopy["expiry"]; ok {
			tokenMapCopy["expiry"] = expiry.Format(time.RFC3339)
		} else if _, ok := tokenMapCopy["expires_at"]; ok {
			tokenMapCopy["expires_at"] = expiry.Format(time.RFC3339)
		} else if _, ok := tokenMapCopy["expiresAt"]; ok {
			tokenMapCopy["expiresAt"] = expiry.Format(time.RFC3339)
		} else if _, ok := tokenMapCopy["expires"]; ok {
			tokenMapCopy["expires"] = expiry.Format(time.RFC3339)
		}
	}

	now := time.Now().UTC()
	if _, ok := meta["last_refresh"]; ok {
		meta["last_refresh"] = now.Format(time.RFC3339)
	} else if _, ok := meta["lastRefresh"]; ok {
		meta["lastRefresh"] = now.Format(time.RFC3339)
	}

	if tokenKey != "" {
		meta[tokenKey] = tokenMapCopy
	} else {
		meta = tokenMapCopy
	}

	if shared != nil {
		values := map[string]any{}
		if tokenKey != "" {
			values[tokenKey] = tokenMapCopy
		} else {
			values["access_token"] = tokenMapCopy["access_token"]
			if v, ok := tokenMapCopy["refresh_token"]; ok {
				values["refresh_token"] = v
			}
			if v, ok := tokenMapCopy["expiry_date"]; ok {
				values["expiry_date"] = v
			}
			if v, ok := tokenMapCopy["expired"]; ok {
				values["expired"] = v
			}
			if v, ok := tokenMapCopy["expiry"]; ok {
				values["expiry"] = v
			}
		}
		shared.MergeMetadata(values)
	}

	auth.Metadata = meta
	return auth, nil
}

// refreshCodex refreshes a codex.
func refreshCodex(ctx context.Context, cfg *config.Config, auth *Auth) (*Auth, error) {
	// Resolve credential context before calling upstream OAuth services.
	refreshToken := metaStringValue(auth.Metadata, "refresh_token")
	if refreshToken == "" {
		return auth, nil
	}
	svc := codexauth.NewCodexAuthWithProxyURL(cfg, auth.ProxyURL)
	td, err := svc.RefreshTokensWithRetry(ctx, refreshToken, 3)
	if err != nil {
		return nil, err
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["id_token"] = td.IDToken
	auth.Metadata["access_token"] = td.AccessToken
	if td.RefreshToken != "" {
		auth.Metadata["refresh_token"] = td.RefreshToken
	}
	if td.AccountID != "" {
		auth.Metadata["account_id"] = td.AccountID
	}
	auth.Metadata["email"] = td.Email
	auth.Metadata["expired"] = td.Expire
	auth.Metadata["type"] = "codex"
	auth.Metadata["last_refresh"] = time.Now().Format(time.RFC3339)
	return auth, nil
}

// refreshClaude refreshes a claude.
func refreshClaude(ctx context.Context, cfg *config.Config, auth *Auth) (*Auth, error) {
	// Resolve credential context before calling upstream OAuth services.
	refreshToken := metaStringValue(auth.Metadata, "refresh_token")
	if refreshToken == "" {
		return auth, nil
	}
	svc := claudeauth.NewClaudeAuthWithProxyURL(cfg, auth.ProxyURL)
	td, err := svc.RefreshTokensWithRetry(ctx, refreshToken, 3)
	if err != nil {
		return nil, err
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["access_token"] = td.AccessToken
	if td.RefreshToken != "" {
		auth.Metadata["refresh_token"] = td.RefreshToken
	}
	auth.Metadata["email"] = td.Email
	auth.Metadata["expired"] = td.Expire
	auth.Metadata["type"] = "claude"
	auth.Metadata["last_refresh"] = time.Now().Format(time.RFC3339)
	return auth, nil
}

// refreshKimi refreshes a kimi.
func refreshKimi(ctx context.Context, cfg *config.Config, auth *Auth) (*Auth, error) {
	// Resolve credential context before calling upstream OAuth services.
	refreshToken := metaStringValue(auth.Metadata, "refresh_token")
	if strings.TrimSpace(refreshToken) == "" {
		return auth, nil
	}
	client := kimiauth.NewDeviceFlowClientWithDeviceIDAndProxyURL(cfg, resolveKimiDeviceID(auth), auth.ProxyURL)
	td, err := client.RefreshToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["access_token"] = td.AccessToken
	if td.RefreshToken != "" {
		auth.Metadata["refresh_token"] = td.RefreshToken
	}
	if td.ExpiresAt > 0 {
		auth.Metadata["expired"] = time.Unix(td.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}
	auth.Metadata["type"] = "kimi"
	auth.Metadata["last_refresh"] = time.Now().Format(time.RFC3339)
	return auth, nil
}

// resolveKimiDeviceID resolves a kimi device id.
func resolveKimiDeviceID(auth *Auth) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	if v, ok := auth.Metadata["device_id"].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// refreshAntigravity refreshes an antigravity.
func refreshAntigravity(ctx context.Context, cfg *config.Config, auth *Auth, rt http.RoundTripper) (*Auth, error) {
	// Resolve credential context before calling upstream OAuth services.
	_ = cfg
	refreshToken := metaStringValue(auth.Metadata, "refresh_token")
	if refreshToken == "" {
		return auth, nil
	}

	form := url.Values{}
	form.Set("client_id", antigravity.ClientID)
	form.Set("client_secret", antigravity.ClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	req, errReq := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if errReq != nil {
		return nil, errReq
	}
	req.Header.Set("Host", "oauth2.googleapis.com")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Go-http-client/2.0")

	client := &http.Client{Timeout: 30 * time.Second}
	if rt != nil {
		client.Transport = rt
	}
	resp, errDo := client.Do(req)
	if errDo != nil {
		return nil, errDo
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("antigravity refresh: response body close error: %v", errClose)
		}
	}()
	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return nil, errRead
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("antigravity refresh: oauth refresh failed: %s", strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if errUnmarshal := json.Unmarshal(body, &tokenResp); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}
	auth.Metadata["access_token"] = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		auth.Metadata["refresh_token"] = tokenResp.RefreshToken
	}
	now := time.Now()
	auth.Metadata["expires_in"] = tokenResp.ExpiresIn
	auth.Metadata["timestamp"] = now.UnixMilli()
	auth.Metadata["expired"] = now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339)
	auth.Metadata["type"] = "antigravity"
	auth.Metadata["last_refresh"] = now.Format(time.RFC3339)
	return auth, nil
}

// metaStringValue handles a meta string value.
func metaStringValue(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}
