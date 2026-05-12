package management

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPIHome/internal/auth/antigravity"
	"github.com/router-for-me/CLIProxyAPIHome/internal/auth/claude"
	"github.com/router-for-me/CLIProxyAPIHome/internal/auth/codex"
	kimiauth "github.com/router-for-me/CLIProxyAPIHome/internal/auth/kimi"
	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/util"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	maxOAuthStateLength = 128

	anthropicAuthURL     = "https://claude.ai/oauth/authorize"
	anthropicRedirectURI = "http://localhost:54545/callback"

	codexAuthURL     = "https://auth.openai.com/oauth/authorize"
	codexRedirectURI = "http://localhost:1455/auth/callback"

	geminiClientID            = "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com"
	geminiClientSecret        = "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"
	geminiDefaultCallbackPort = 8085
	geminiCLIEndpoint         = "https://cloudcode-pa.googleapis.com"
	geminiCLIVersion          = "v1internal"
	geminiCLIUserAgentVersion = "0.34.0"

	antigravityAuthEndpoint     = "https://accounts.google.com/o/oauth2/v2/auth"
	antigravityTokenEndpoint    = "https://oauth2.googleapis.com/token"
	antigravityUserInfoEndpoint = "https://www.googleapis.com/oauth2/v2/userinfo?alt=json"
	antigravityCallbackPort     = 51121
	antigravityAPIEndpoint      = "https://cloudcode-pa.googleapis.com"
	antigravityAPIVersion       = "v1internal"
	antigravityFallbackVersion  = "1.21.9"
	antigravityNodeAPIClientUA  = "google-api-nodejs-client/10.3.0"
	antigravityGoogAPIClientUA  = "gl-node/22.21.1"
)

var (
	errInvalidOAuthState   = errors.New("invalid oauth state")
	errUnsupportedProvider = errors.New("unsupported oauth provider")
	geminiScopes           = []string{"https://www.googleapis.com/auth/cloud-platform", "https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"}
	antigravityScopes      = []string{"https://www.googleapis.com/auth/cloud-platform", "https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile", "https://www.googleapis.com/auth/cclog", "https://www.googleapis.com/auth/experimentsandconfigs"}
)

type pkceCodes struct {
	CodeVerifier  string
	CodeChallenge string
}

type oauthCallbackRequest struct {
	Provider    string `json:"provider"`
	RedirectURL string `json:"redirect_url"`
	Code        string `json:"code"`
	State       string `json:"state"`
	Error       string `json:"error"`
}

type geminiTokenStorage struct {
	Token     map[string]any
	ProjectID string
	Email     string
	Auto      bool
	Checked   bool
}

type gcpProjectList struct {
	Projects []gcpProject `json:"projects"`
}

type gcpProject struct {
	ProjectID string `json:"projectId"`
}

type projectSelectionRequiredError struct{}

func (e *projectSelectionRequiredError) Error() string {
	return "gemini cli: project selection required"
}

func (h *Handler) RequestAnthropicToken(c *gin.Context) {
	pkce, errPKCE := generatePKCECodes()
	if errPKCE != nil {
		log.Errorf("cluster oauth: failed to generate anthropic PKCE codes: %v", errPKCE)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate PKCE codes"})
		return
	}
	state, errState := generateOAuthState("cla")
	if errState != nil {
		log.Errorf("cluster oauth: failed to generate anthropic state: %v", errState)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state parameter"})
		return
	}
	authURL := buildAnthropicAuthURL(state, pkce)
	if errRegister := h.registerOAuthSession(c, "anthropic", state, map[string]any{
		"code_verifier":  pkce.CodeVerifier,
		"code_challenge": pkce.CodeChallenge,
	}); errRegister != nil {
		respondError(c, http.StatusInternalServerError, "oauth_session_failed", errRegister)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "url": authURL, "state": state})
}

func (h *Handler) RequestCodexToken(c *gin.Context) {
	pkce, errPKCE := generatePKCECodes()
	if errPKCE != nil {
		log.Errorf("cluster oauth: failed to generate codex PKCE codes: %v", errPKCE)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate PKCE codes"})
		return
	}
	state, errState := generateOAuthState("cdx")
	if errState != nil {
		log.Errorf("cluster oauth: failed to generate codex state: %v", errState)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state parameter"})
		return
	}
	authURL := buildCodexAuthURL(state, pkce)
	if errRegister := h.registerOAuthSession(c, "codex", state, map[string]any{
		"code_verifier":  pkce.CodeVerifier,
		"code_challenge": pkce.CodeChallenge,
	}); errRegister != nil {
		respondError(c, http.StatusInternalServerError, "oauth_session_failed", errRegister)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "url": authURL, "state": state})
}

func (h *Handler) RequestGeminiCLIToken(c *gin.Context) {
	projectID := strings.TrimSpace(c.Query("project_id"))
	state, errState := generateOAuthState("gem")
	if errState != nil {
		log.Errorf("cluster oauth: failed to generate gemini state: %v", errState)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state parameter"})
		return
	}
	conf := geminiOAuthConfig()
	authURL := conf.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"))
	if errRegister := h.registerOAuthSession(c, "gemini", state, map[string]any{
		"project_id": projectID,
	}); errRegister != nil {
		respondError(c, http.StatusInternalServerError, "oauth_session_failed", errRegister)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "url": authURL, "state": state})
}

func (h *Handler) RequestAntigravityToken(c *gin.Context) {
	state, errState := generateOAuthState("agv")
	if errState != nil {
		log.Errorf("cluster oauth: failed to generate antigravity state: %v", errState)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state parameter"})
		return
	}
	redirectURI := fmt.Sprintf("http://localhost:%d/oauth-callback", antigravityCallbackPort)
	authURL := buildAntigravityAuthURL(state, redirectURI)
	if errRegister := h.registerOAuthSession(c, "antigravity", state, map[string]any{
		"redirect_uri": redirectURI,
	}); errRegister != nil {
		respondError(c, http.StatusInternalServerError, "oauth_session_failed", errRegister)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "url": authURL, "state": state})
}

func (h *Handler) RequestKimiToken(c *gin.Context) {
	cfg := h.oauthConfig()
	ctx, cancel := context.WithTimeout(requestContextOrBackground(c), 30*time.Second)
	defer cancel()

	state, errState := generateOAuthState("kmi")
	if errState != nil {
		log.Errorf("cluster oauth: failed to generate kimi state: %v", errState)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state parameter"})
		return
	}

	kimiAuth := kimiauth.NewKimiAuth(cfg)
	deviceFlow, errDevice := kimiAuth.StartDeviceFlow(ctx)
	if errDevice != nil {
		log.Errorf("cluster oauth: failed to start kimi device flow: %v", errDevice)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate authorization url"})
		return
	}
	authURL := strings.TrimSpace(deviceFlow.VerificationURIComplete)
	if authURL == "" {
		authURL = strings.TrimSpace(deviceFlow.VerificationURI)
	}

	if errRegister := h.registerOAuthSession(c, "kimi", state, map[string]any{
		"device_code": deviceFlow.DeviceCode,
		"user_code":   deviceFlow.UserCode,
	}); errRegister != nil {
		respondError(c, http.StatusInternalServerError, "oauth_session_failed", errRegister)
		return
	}

	go h.waitForKimiAuthorization(state, kimiAuth, deviceFlow)

	c.JSON(http.StatusOK, gin.H{"status": "ok", "url": authURL, "state": state})
}

func (h *Handler) GetAuthStatus(c *gin.Context) {
	state := strings.TrimSpace(c.Query("state"))
	if state == "" {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	if errState := validateOAuthState(state); errState != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "invalid state"})
		return
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()
	session, errSession := h.repo.GetOAuthSession(ctx, state)
	if errSession != nil {
		respondError(c, http.StatusInternalServerError, "oauth_session_failed", errSession)
		return
	}
	if session == nil || strings.EqualFold(session.Status, "complete") {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}
	if strings.EqualFold(session.Status, "error") {
		errMsg := strings.TrimSpace(session.Error)
		if errMsg == "" {
			errMsg = "Authentication failed"
		}
		c.JSON(http.StatusOK, gin.H{"status": "error", "error": errMsg})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "wait"})
}

func (h *Handler) handleOAuthCallback(c *gin.Context) {
	var req oauthCallbackRequest
	if errBind := c.ShouldBindJSON(&req); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "invalid body"})
		return
	}

	provider, errProvider := normalizeOAuthProvider(req.Provider)
	if errProvider != nil || provider == "kimi" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "unsupported provider"})
		return
	}

	state := strings.TrimSpace(req.State)
	code := strings.TrimSpace(req.Code)
	errorMessage := strings.TrimSpace(req.Error)
	if rawRedirect := strings.TrimSpace(req.RedirectURL); rawRedirect != "" {
		parsedURL, errParse := url.Parse(rawRedirect)
		if errParse != nil {
			c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "invalid redirect_url"})
			return
		}
		query := parsedURL.Query()
		if state == "" {
			state = strings.TrimSpace(query.Get("state"))
		}
		if code == "" {
			code = strings.TrimSpace(query.Get("code"))
		}
		if errorMessage == "" {
			errorMessage = strings.TrimSpace(query.Get("error"))
			if errorMessage == "" {
				errorMessage = strings.TrimSpace(query.Get("error_description"))
			}
		}
	}

	if state == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "state is required"})
		return
	}
	if errState := validateOAuthState(state); errState != nil {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "invalid state"})
		return
	}
	if code == "" && errorMessage == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "code or error is required"})
		return
	}

	ctx, cancel := h.requestContext(c)
	defer cancel()
	session, errSession := h.repo.GetOAuthSession(ctx, state)
	if errSession != nil {
		respondError(c, http.StatusInternalServerError, "oauth_session_failed", errSession)
		return
	}
	if session == nil {
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "error": "unknown or expired state"})
		return
	}
	if strings.TrimSpace(session.Status) != "" {
		c.JSON(http.StatusConflict, gin.H{"status": "error", "error": "oauth flow is not pending"})
		return
	}
	if !strings.EqualFold(session.Provider, provider) {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "error": "provider does not match state"})
		return
	}

	if errorMessage != "" {
		_ = h.repo.MergeOAuthSessionData(ctx, state, map[string]any{
			"callback_received_at": time.Now().UTC().Format(time.RFC3339),
			"callback_error":       errorMessage,
		})
		if errSet := h.repo.SetOAuthSessionError(ctx, state, "Authentication failed"); errSet != nil {
			respondError(c, http.StatusInternalServerError, "oauth_session_failed", errSet)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	if errMerge := h.repo.MergeOAuthSessionData(ctx, state, map[string]any{
		"callback_received_at": time.Now().UTC().Format(time.RFC3339),
	}); errMerge != nil {
		respondError(c, http.StatusInternalServerError, "oauth_session_failed", errMerge)
		return
	}

	go h.processOAuthCallback(provider, state, code)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (h *Handler) processOAuthCallback(provider, state, code string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	session, errSession := h.repo.GetOAuthSession(ctx, state)
	if errSession != nil {
		log.Errorf("cluster oauth: load session failed: %v", errSession)
		return
	}
	if session == nil {
		return
	}
	data, errData := cluster.OAuthSessionData(session)
	if errData != nil {
		_ = h.repo.SetOAuthSessionError(ctx, state, "Authentication failed")
		log.Errorf("cluster oauth: decode session data failed: %v", errData)
		return
	}

	var errProcess error
	switch provider {
	case "anthropic":
		errProcess = h.exchangeAnthropicCallback(ctx, state, code, data)
	case "codex":
		errProcess = h.exchangeCodexCallback(ctx, code, data)
	case "gemini":
		errProcess = h.exchangeGeminiCallback(ctx, code, data)
	case "antigravity":
		errProcess = h.exchangeAntigravityCallback(ctx, code, data)
	default:
		errProcess = fmt.Errorf("unsupported provider: %s", provider)
	}
	if errProcess != nil {
		_ = h.repo.SetOAuthSessionError(ctx, state, authStatusMessage(provider, errProcess))
		log.Errorf("cluster oauth: %s callback failed: %v", provider, errProcess)
		return
	}
	if errComplete := h.repo.CompleteOAuthSession(ctx, state); errComplete != nil {
		log.Errorf("cluster oauth: complete session failed: %v", errComplete)
	}
}

func (h *Handler) waitForKimiAuthorization(state string, kimiAuth *kimiauth.KimiAuth, deviceFlow *kimiauth.DeviceCodeResponse) {
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Minute)
	defer cancel()

	authBundle, errWait := kimiAuth.WaitForAuthorization(ctx, deviceFlow)
	if errWait != nil {
		_ = h.repo.SetOAuthSessionError(context.Background(), state, "Authentication failed")
		log.Errorf("cluster oauth: kimi authorization failed: %v", errWait)
		return
	}
	if authBundle == nil || authBundle.TokenData == nil {
		_ = h.repo.SetOAuthSessionError(context.Background(), state, "Authentication failed")
		return
	}

	tokenData := authBundle.TokenData
	metadata := map[string]any{
		"type":          "kimi",
		"access_token":  tokenData.AccessToken,
		"refresh_token": tokenData.RefreshToken,
		"token_type":    tokenData.TokenType,
		"scope":         tokenData.Scope,
		"timestamp":     time.Now().UnixMilli(),
	}
	if tokenData.ExpiresAt > 0 {
		metadata["expired"] = time.Unix(tokenData.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}
	if deviceID := strings.TrimSpace(authBundle.DeviceID); deviceID != "" {
		metadata["device_id"] = deviceID
	}

	storeCtx, cancelStore := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelStore()
	fileName := fmt.Sprintf("kimi-%d.json", time.Now().UnixMilli())
	if errStore := h.storeOAuthMetadataWithContext(storeCtx, metadata, fileName); errStore != nil {
		_ = h.repo.SetOAuthSessionError(context.Background(), state, "Failed to save authentication tokens")
		log.Errorf("cluster oauth: store kimi token failed: %v", errStore)
		return
	}
	if errComplete := h.repo.CompleteOAuthSession(context.Background(), state); errComplete != nil {
		log.Errorf("cluster oauth: complete kimi session failed: %v", errComplete)
	}
}

func (h *Handler) registerOAuthSession(c *gin.Context, provider, state string, data map[string]any) error {
	ctx, cancel := h.requestContext(c)
	defer cancel()
	record, errRecord := cluster.NewOAuthSessionRecord(provider, state, data, time.Now().UTC())
	if errRecord != nil {
		return errRecord
	}
	return h.repo.UpsertOAuthSession(ctx, record)
}

func (h *Handler) exchangeAnthropicCallback(ctx context.Context, state, code string, data map[string]any) error {
	pkce := pkceCodes{
		CodeVerifier:  stringFromAny(data["code_verifier"]),
		CodeChallenge: stringFromAny(data["code_challenge"]),
	}
	if pkce.CodeVerifier == "" {
		return fmt.Errorf("missing PKCE verifier")
	}

	reqBody := map[string]any{
		"code":          strings.Split(strings.TrimSpace(code), "#")[0],
		"state":         state,
		"grant_type":    "authorization_code",
		"client_id":     claude.ClientID,
		"redirect_uri":  anthropicRedirectURI,
		"code_verifier": pkce.CodeVerifier,
	}
	if parts := strings.Split(strings.TrimSpace(code), "#"); len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
		reqBody["state"] = strings.TrimSpace(parts[1])
	}

	rawBody, errMarshal := json.Marshal(reqBody)
	if errMarshal != nil {
		return errMarshal
	}
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodPost, claude.TokenURL, bytes.NewReader(rawBody))
	if errRequest != nil {
		return errRequest
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := claude.NewAnthropicHttpClient(&h.oauthConfig().SDKConfig)
	resp, errDo := client.Do(req)
	if errDo != nil {
		return errDo
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("anthropic oauth: response body close error: %v", errClose)
		}
	}()
	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return errRead
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Account      struct {
			EmailAddress string `json:"email_address"`
		} `json:"account"`
	}
	if errUnmarshal := json.Unmarshal(body, &tokenResp); errUnmarshal != nil {
		return errUnmarshal
	}
	now := time.Now()
	metadata := map[string]any{
		"type":          "claude",
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
		"last_refresh":  now.Format(time.RFC3339),
		"email":         tokenResp.Account.EmailAddress,
		"expired":       now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339),
	}
	return h.storeOAuthMetadataWithContext(ctx, metadata, claudeCredentialFileName(tokenResp.Account.EmailAddress))
}

func (h *Handler) exchangeCodexCallback(ctx context.Context, code string, data map[string]any) error {
	pkce := pkceCodes{
		CodeVerifier:  stringFromAny(data["code_verifier"]),
		CodeChallenge: stringFromAny(data["code_challenge"]),
	}
	if pkce.CodeVerifier == "" {
		return fmt.Errorf("missing PKCE verifier")
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", codex.ClientID)
	form.Set("code", strings.TrimSpace(code))
	form.Set("redirect_uri", codexRedirectURI)
	form.Set("code_verifier", pkce.CodeVerifier)

	req, errRequest := http.NewRequestWithContext(ctx, http.MethodPost, codex.TokenURL, strings.NewReader(form.Encode()))
	if errRequest != nil {
		return errRequest
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, errDo := h.oauthHTTPClient().Do(req)
	if errDo != nil {
		return errDo
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("codex oauth: response body close error: %v", errClose)
		}
	}()
	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return errRead
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if errUnmarshal := json.Unmarshal(body, &tokenResp); errUnmarshal != nil {
		return errUnmarshal
	}

	accountID := ""
	email := ""
	planType := ""
	hashAccountID := ""
	if claims, errParse := codex.ParseJWTToken(tokenResp.IDToken); errParse == nil && claims != nil {
		accountID = claims.GetAccountID()
		email = claims.GetUserEmail()
		planType = strings.TrimSpace(claims.CodexAuthInfo.ChatgptPlanType)
		if accountID != "" {
			digest := sha256.Sum256([]byte(accountID))
			hashAccountID = hex.EncodeToString(digest[:])[:8]
		}
	}

	now := time.Now()
	metadata := map[string]any{
		"type":          "codex",
		"id_token":      tokenResp.IDToken,
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
		"account_id":    accountID,
		"last_refresh":  now.Format(time.RFC3339),
		"email":         email,
		"expired":       now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339),
	}
	return h.storeOAuthMetadataWithContext(ctx, metadata, codexCredentialFileName(email, planType, hashAccountID, true))
}

func (h *Handler) exchangeGeminiCallback(ctx context.Context, code string, data map[string]any) error {
	client := h.oauthHTTPClient()
	oauthCtx := context.WithValue(ctx, oauth2.HTTPClient, client)
	conf := geminiOAuthConfig()

	token, errExchange := conf.Exchange(oauthCtx, strings.TrimSpace(code))
	if errExchange != nil {
		return errExchange
	}

	email, errEmail := fetchGeminiEmail(oauthCtx, conf, token)
	if errEmail != nil {
		return errEmail
	}

	tokenMap := make(map[string]any)
	rawToken, errMarshal := json.Marshal(token)
	if errMarshal != nil {
		return errMarshal
	}
	if errUnmarshal := json.Unmarshal(rawToken, &tokenMap); errUnmarshal != nil {
		return errUnmarshal
	}
	tokenMap["token_uri"] = "https://oauth2.googleapis.com/token"
	tokenMap["client_id"] = geminiClientID
	tokenMap["client_secret"] = geminiClientSecret
	tokenMap["scopes"] = geminiScopes
	tokenMap["universe_domain"] = "googleapis.com"

	storage := &geminiTokenStorage{
		Token:     tokenMap,
		ProjectID: stringFromAny(data["project_id"]),
		Email:     email,
		Auto:      stringFromAny(data["project_id"]) == "",
	}
	geminiClient := conf.Client(oauthCtx, token)
	if errSetup := ensureGeminiProjectAndOnboard(ctx, geminiClient, storage, storage.ProjectID); errSetup != nil {
		return errSetup
	}
	if errVerify := ensureGeminiProjectsEnabled(ctx, geminiClient, splitProjectIDs(storage.ProjectID)); errVerify != nil {
		return fmt.Errorf("failed to verify Cloud AI API status: %w", errVerify)
	}
	storage.Checked = true

	metadata := map[string]any{
		"type":       "gemini",
		"token":      tokenMap,
		"project_id": storage.ProjectID,
		"email":      storage.Email,
		"auto":       storage.Auto,
		"checked":    storage.Checked,
	}
	return h.storeOAuthMetadataWithContext(ctx, metadata, geminiCredentialFileName(storage.Email, storage.ProjectID, true))
}

func (h *Handler) exchangeAntigravityCallback(ctx context.Context, code string, data map[string]any) error {
	redirectURI := stringFromAny(data["redirect_uri"])
	if redirectURI == "" {
		redirectURI = fmt.Sprintf("http://localhost:%d/oauth-callback", antigravityCallbackPort)
	}

	form := url.Values{}
	form.Set("code", strings.TrimSpace(code))
	form.Set("client_id", antigravity.ClientID)
	form.Set("client_secret", antigravity.ClientSecret)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")

	req, errRequest := http.NewRequestWithContext(ctx, http.MethodPost, antigravityTokenEndpoint, strings.NewReader(form.Encode()))
	if errRequest != nil {
		return errRequest
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, errDo := h.oauthHTTPClient().Do(req)
	if errDo != nil {
		return errDo
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("antigravity oauth: response body close error: %v", errClose)
		}
	}()
	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return errRead
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if errUnmarshal := json.Unmarshal(body, &tokenResp); errUnmarshal != nil {
		return errUnmarshal
	}
	if strings.TrimSpace(tokenResp.AccessToken) == "" {
		return fmt.Errorf("token exchange returned empty access token")
	}

	email, errEmail := h.fetchAntigravityEmail(ctx, tokenResp.AccessToken)
	if errEmail != nil {
		return errEmail
	}
	projectID := ""
	if fetchedProjectID, errProject := h.fetchAntigravityProjectID(ctx, tokenResp.AccessToken); errProject != nil {
		log.Warnf("cluster oauth: failed to fetch antigravity project ID: %v", errProject)
	} else {
		projectID = strings.TrimSpace(fetchedProjectID)
	}
	now := time.Now()
	metadata := map[string]any{
		"type":          "antigravity",
		"access_token":  tokenResp.AccessToken,
		"refresh_token": tokenResp.RefreshToken,
		"expires_in":    tokenResp.ExpiresIn,
		"timestamp":     now.UnixMilli(),
		"expired":       now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second).Format(time.RFC3339),
		"email":         email,
	}
	if projectID != "" {
		metadata["project_id"] = projectID
	}
	return h.storeOAuthMetadataWithContext(ctx, metadata, antigravityCredentialFileName(email))
}

func (h *Handler) storeOAuthMetadataWithContext(ctx context.Context, metadata map[string]any, originalFilename string) error {
	raw, errMarshal := json.MarshalIndent(metadata, "", "  ")
	if errMarshal != nil {
		return errMarshal
	}
	raw = append(raw, '\n')
	_, errStore := h.storeOAuthPayloadWithContext(ctx, raw, originalFilename)
	return errStore
}

func buildAnthropicAuthURL(state string, pkce *pkceCodes) string {
	params := url.Values{
		"code":                  {"true"},
		"client_id":             {claude.ClientID},
		"response_type":         {"code"},
		"redirect_uri":          {anthropicRedirectURI},
		"scope":                 {"user:profile user:inference user:sessions:claude_code user:mcp_servers user:file_upload"},
		"code_challenge":        {pkce.CodeChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return anthropicAuthURL + "?" + params.Encode()
}

func buildCodexAuthURL(state string, pkce *pkceCodes) string {
	params := url.Values{
		"client_id":                  {codex.ClientID},
		"response_type":              {"code"},
		"redirect_uri":               {codexRedirectURI},
		"scope":                      {"openid email profile offline_access"},
		"state":                      {state},
		"code_challenge":             {pkce.CodeChallenge},
		"code_challenge_method":      {"S256"},
		"prompt":                     {"login"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
	}
	return codexAuthURL + "?" + params.Encode()
}

func buildAntigravityAuthURL(state, redirectURI string) string {
	params := url.Values{}
	params.Set("access_type", "offline")
	params.Set("client_id", antigravity.ClientID)
	params.Set("prompt", "consent")
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(antigravityScopes, " "))
	params.Set("state", state)
	return antigravityAuthEndpoint + "?" + params.Encode()
}

func geminiOAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     geminiClientID,
		ClientSecret: geminiClientSecret,
		RedirectURL:  fmt.Sprintf("http://localhost:%d/oauth2callback", geminiDefaultCallbackPort),
		Scopes:       geminiScopes,
		Endpoint:     google.Endpoint,
	}
}

func fetchGeminiEmail(ctx context.Context, conf *oauth2.Config, token *oauth2.Token) (string, error) {
	authHTTPClient := conf.Client(ctx, token)
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v1/userinfo?alt=json", nil)
	if errRequest != nil {
		return "", fmt.Errorf("could not get user info: %w", errRequest)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)

	resp, errDo := authHTTPClient.Do(req)
	if errDo != nil {
		return "", errDo
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("gemini oauth: response body close error: %v", errClose)
		}
	}()
	bodyBytes, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return "", errRead
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("get user info request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return strings.TrimSpace(gjson.GetBytes(bodyBytes, "email").String()), nil
}

func ensureGeminiProjectAndOnboard(ctx context.Context, httpClient *http.Client, storage *geminiTokenStorage, requestedProject string) error {
	if storage == nil {
		return fmt.Errorf("gemini storage is nil")
	}
	trimmedRequest := strings.TrimSpace(requestedProject)
	switch {
	case strings.EqualFold(trimmedRequest, "ALL"):
		storage.Auto = false
		projects, errProjects := fetchGCPProjects(ctx, httpClient)
		if errProjects != nil {
			return fmt.Errorf("fetch project list: %w", errProjects)
		}
		activated := make([]string, 0, len(projects))
		seen := make(map[string]struct{}, len(projects))
		for _, project := range projects {
			candidate := strings.TrimSpace(project.ProjectID)
			if candidate == "" {
				continue
			}
			if _, exists := seen[candidate]; exists {
				continue
			}
			if errSetup := performGeminiCLISetup(ctx, httpClient, storage, candidate); errSetup != nil {
				return fmt.Errorf("onboard project %s: %w", candidate, errSetup)
			}
			finalID := strings.TrimSpace(storage.ProjectID)
			if finalID == "" {
				finalID = candidate
			}
			activated = append(activated, finalID)
			seen[candidate] = struct{}{}
		}
		if len(activated) == 0 {
			return fmt.Errorf("no Google Cloud projects available for this account")
		}
		storage.ProjectID = strings.Join(activated, ",")
		return nil
	case strings.EqualFold(trimmedRequest, "GOOGLE_ONE"):
		storage.Auto = false
		return performGeminiCLISetup(ctx, httpClient, storage, "")
	case trimmedRequest == "":
		projects, errProjects := fetchGCPProjects(ctx, httpClient)
		if errProjects != nil {
			return fmt.Errorf("fetch project list: %w", errProjects)
		}
		if len(projects) == 0 {
			return fmt.Errorf("no Google Cloud projects available for this account")
		}
		trimmedRequest = strings.TrimSpace(projects[0].ProjectID)
		if trimmedRequest == "" {
			return fmt.Errorf("resolved project id is empty")
		}
		storage.Auto = true
	default:
		storage.Auto = false
	}

	if errSetup := performGeminiCLISetup(ctx, httpClient, storage, trimmedRequest); errSetup != nil {
		return errSetup
	}
	if strings.TrimSpace(storage.ProjectID) == "" {
		storage.ProjectID = trimmedRequest
	}
	return nil
}

func performGeminiCLISetup(ctx context.Context, httpClient *http.Client, storage *geminiTokenStorage, requestedProject string) error {
	metadata := map[string]string{
		"ideType":    "IDE_UNSPECIFIED",
		"platform":   "PLATFORM_UNSPECIFIED",
		"pluginType": "GEMINI",
	}
	trimmedRequest := strings.TrimSpace(requestedProject)
	explicitProject := trimmedRequest != ""
	loadReqBody := map[string]any{"metadata": metadata}
	if explicitProject {
		loadReqBody["cloudaicompanionProject"] = trimmedRequest
	}

	var loadResp map[string]any
	if errLoad := callGeminiCLI(ctx, httpClient, "loadCodeAssist", loadReqBody, &loadResp); errLoad != nil {
		return fmt.Errorf("load code assist: %w", errLoad)
	}

	tierID := "legacy-tier"
	if tiers, okTiers := loadResp["allowedTiers"].([]any); okTiers {
		for _, rawTier := range tiers {
			tier, okTier := rawTier.(map[string]any)
			if !okTier {
				continue
			}
			if isDefault, okDefault := tier["isDefault"].(bool); okDefault && isDefault {
				if id, okID := tier["id"].(string); okID && strings.TrimSpace(id) != "" {
					tierID = strings.TrimSpace(id)
					break
				}
			}
		}
	}

	projectID := trimmedRequest
	if projectID == "" {
		projectID = geminiProjectIDFromMap(loadResp)
	}
	if projectID == "" {
		autoOnboardReq := map[string]any{
			"tierId":   tierID,
			"metadata": metadata,
		}
		autoCtx, autoCancel := context.WithTimeout(ctx, 30*time.Second)
		defer autoCancel()
		for attempt := 1; ; attempt++ {
			var onboardResp map[string]any
			if errOnboard := callGeminiCLI(autoCtx, httpClient, "onboardUser", autoOnboardReq, &onboardResp); errOnboard != nil {
				return fmt.Errorf("auto-discovery onboardUser: %w", errOnboard)
			}
			if done, okDone := onboardResp["done"].(bool); okDone && done {
				if resp, okResp := onboardResp["response"].(map[string]any); okResp {
					projectID = geminiProjectIDFromMap(resp)
				}
				break
			}
			log.Debugf("Gemini auto-discovery: onboarding in progress, attempt %d", attempt)
			select {
			case <-autoCtx.Done():
				return &projectSelectionRequiredError{}
			case <-time.After(2 * time.Second):
			}
		}
		if projectID == "" {
			return &projectSelectionRequiredError{}
		}
	}

	onboardReqBody := map[string]any{
		"tierId":                  tierID,
		"metadata":                metadata,
		"cloudaicompanionProject": projectID,
	}
	storage.ProjectID = projectID

	for {
		var onboardResp map[string]any
		if errOnboard := callGeminiCLI(ctx, httpClient, "onboardUser", onboardReqBody, &onboardResp); errOnboard != nil {
			return fmt.Errorf("onboard user: %w", errOnboard)
		}
		if done, okDone := onboardResp["done"].(bool); okDone && done {
			responseProjectID := ""
			if resp, okResp := onboardResp["response"].(map[string]any); okResp {
				responseProjectID = geminiProjectIDFromMap(resp)
			}
			if responseProjectID != "" {
				storage.ProjectID = responseProjectID
			}
			if strings.TrimSpace(storage.ProjectID) == "" {
				storage.ProjectID = strings.TrimSpace(projectID)
			}
			if strings.TrimSpace(storage.ProjectID) == "" {
				return fmt.Errorf("onboard user completed without project id")
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func callGeminiCLI(ctx context.Context, httpClient *http.Client, endpoint string, body any, result any) error {
	endpointURL := fmt.Sprintf("%s/%s:%s", geminiCLIEndpoint, geminiCLIVersion, endpoint)
	if strings.HasPrefix(endpoint, "operations/") {
		endpointURL = fmt.Sprintf("%s/%s", geminiCLIEndpoint, endpoint)
	}

	var reader io.Reader
	if body != nil {
		rawBody, errMarshal := json.Marshal(body)
		if errMarshal != nil {
			return fmt.Errorf("marshal request body: %w", errMarshal)
		}
		reader = bytes.NewReader(rawBody)
	}

	req, errRequest := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, reader)
	if errRequest != nil {
		return errRequest
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", geminiCLIUserAgent(""))

	resp, errDo := httpClient.Do(req)
	if errDo != nil {
		return errDo
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("gemini cli: response body close error: %v", errClose)
		}
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	if result == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if errDecode := json.NewDecoder(resp.Body).Decode(result); errDecode != nil {
		return errDecode
	}
	return nil
}

func fetchGCPProjects(ctx context.Context, httpClient *http.Client) ([]gcpProject, error) {
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, "https://cloudresourcemanager.googleapis.com/v1/projects", nil)
	if errRequest != nil {
		return nil, errRequest
	}
	resp, errDo := httpClient.Do(req)
	if errDo != nil {
		return nil, errDo
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("gcp projects: response body close error: %v", errClose)
		}
	}()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("project list request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	var projects gcpProjectList
	if errDecode := json.NewDecoder(resp.Body).Decode(&projects); errDecode != nil {
		return nil, errDecode
	}
	return projects.Projects, nil
}

func ensureGeminiProjectsEnabled(ctx context.Context, httpClient *http.Client, projectIDs []string) error {
	for _, projectID := range projectIDs {
		projectID = strings.TrimSpace(projectID)
		if projectID == "" {
			continue
		}
		isChecked, errCheck := checkCloudAPIIsEnabled(ctx, httpClient, projectID)
		if errCheck != nil {
			return fmt.Errorf("project %s: %w", projectID, errCheck)
		}
		if !isChecked {
			return fmt.Errorf("project %s: Cloud AI API not enabled", projectID)
		}
	}
	return nil
}

func checkCloudAPIIsEnabled(ctx context.Context, httpClient *http.Client, projectID string) (bool, error) {
	serviceUsageURL := "https://serviceusage.googleapis.com"
	requiredServices := []string{
		"cloudaicompanion.googleapis.com",
	}
	for _, service := range requiredServices {
		checkURL := fmt.Sprintf("%s/v1/projects/%s/services/%s", serviceUsageURL, projectID, service)
		req, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, checkURL, nil)
		if errRequest != nil {
			return false, fmt.Errorf("failed to create request: %w", errRequest)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", geminiCLIUserAgent(""))
		resp, errDo := httpClient.Do(req)
		if errDo != nil {
			return false, fmt.Errorf("failed to execute request: %w", errDo)
		}

		if resp.StatusCode == http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			if errClose := resp.Body.Close(); errClose != nil {
				log.Errorf("gemini service usage: response body close error: %v", errClose)
			}
			if gjson.GetBytes(bodyBytes, "state").String() == "ENABLED" {
				continue
			}
		} else if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("gemini service usage: response body close error: %v", errClose)
		}

		enableURL := fmt.Sprintf("%s/v1/projects/%s/services/%s:enable", serviceUsageURL, projectID, service)
		req, errRequest = http.NewRequestWithContext(ctx, http.MethodPost, enableURL, strings.NewReader("{}"))
		if errRequest != nil {
			return false, fmt.Errorf("failed to create request: %w", errRequest)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", geminiCLIUserAgent(""))
		resp, errDo = httpClient.Do(req)
		if errDo != nil {
			return false, fmt.Errorf("failed to execute request: %w", errDo)
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("gemini service enable: response body close error: %v", errClose)
		}
		errMessage := string(bodyBytes)
		errMessageResult := gjson.GetBytes(bodyBytes, "error.message")
		if errMessageResult.Exists() {
			errMessage = errMessageResult.String()
		}
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			continue
		}
		if resp.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(errMessage), "already enabled") {
			continue
		}
		return false, fmt.Errorf("project activation required: %s", errMessage)
	}
	return true, nil
}

func splitProjectIDs(projectIDs string) []string {
	parts := strings.Split(projectIDs, ",")
	result := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		projectID := strings.TrimSpace(part)
		if projectID == "" {
			continue
		}
		if _, exists := seen[projectID]; exists {
			continue
		}
		seen[projectID] = struct{}{}
		result = append(result, projectID)
	}
	return result
}

func geminiProjectIDFromMap(values map[string]any) string {
	if values == nil {
		return ""
	}
	switch projectValue := values["cloudaicompanionProject"].(type) {
	case string:
		return strings.TrimSpace(projectValue)
	case map[string]any:
		return stringFromAny(projectValue["id"])
	default:
		return ""
	}
}

func (h *Handler) fetchAntigravityEmail(ctx context.Context, accessToken string) (string, error) {
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodGet, antigravityUserInfoEndpoint, nil)
	if errRequest != nil {
		return "", errRequest
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	resp, errDo := h.oauthHTTPClient().Do(req)
	if errDo != nil {
		return "", errDo
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("antigravity userinfo: response body close error: %v", errClose)
		}
	}()
	body, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return "", errRead
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("userinfo request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	email := strings.TrimSpace(gjson.GetBytes(body, "email").String())
	if email == "" {
		return "", fmt.Errorf("userinfo response missing email")
	}
	return email, nil
}

func (h *Handler) fetchAntigravityProjectID(ctx context.Context, accessToken string) (string, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return "", fmt.Errorf("antigravity project: missing access token")
	}
	userAgent := antigravityLoadCodeAssistUserAgent()
	loadReqBody := map[string]any{
		"metadata": map[string]string{
			"ide_type":    "ANTIGRAVITY",
			"ide_version": antigravityVersionFromUserAgent(userAgent),
			"ide_name":    "antigravity",
		},
	}
	rawBody, errMarshal := json.Marshal(loadReqBody)
	if errMarshal != nil {
		return "", fmt.Errorf("marshal request body: %w", errMarshal)
	}
	endpointURL := fmt.Sprintf("%s/%s:loadCodeAssist", antigravityAPIEndpoint, antigravityAPIVersion)
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(rawBody))
	if errRequest != nil {
		return "", fmt.Errorf("create request: %w", errRequest)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Goog-Api-Client", antigravityGoogAPIClientUA)

	resp, errDo := h.oauthHTTPClient().Do(req)
	if errDo != nil {
		return "", fmt.Errorf("execute request: %w", errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("antigravity loadCodeAssist: response body close error: %v", errClose)
		}
	}()
	bodyBytes, errRead := io.ReadAll(resp.Body)
	if errRead != nil {
		return "", fmt.Errorf("read response: %w", errRead)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}
	var loadResp map[string]any
	if errDecode := json.Unmarshal(bodyBytes, &loadResp); errDecode != nil {
		return "", fmt.Errorf("decode response: %w", errDecode)
	}
	projectID := geminiProjectIDFromMap(loadResp)
	if projectID != "" {
		return projectID, nil
	}

	tierID := "legacy-tier"
	if tiers, okTiers := loadResp["allowedTiers"].([]any); okTiers {
		for _, rawTier := range tiers {
			tier, okTier := rawTier.(map[string]any)
			if !okTier {
				continue
			}
			if isDefault, okDefault := tier["isDefault"].(bool); okDefault && isDefault {
				if id, okID := tier["id"].(string); okID && strings.TrimSpace(id) != "" {
					tierID = strings.TrimSpace(id)
					break
				}
			}
		}
	}
	return h.onboardAntigravityUser(ctx, accessToken, tierID)
}

func (h *Handler) onboardAntigravityUser(ctx context.Context, accessToken string, tierID string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	userAgent := antigravityLoadCodeAssistUserAgent()
	requestBody := map[string]any{
		"tierId": strings.TrimSpace(tierID),
		"metadata": map[string]string{
			"ide_type":    "ANTIGRAVITY",
			"ide_version": antigravityVersionFromUserAgent(userAgent),
			"ide_name":    "antigravity",
		},
	}
	rawBody, errMarshal := json.Marshal(requestBody)
	if errMarshal != nil {
		return "", fmt.Errorf("marshal request body: %w", errMarshal)
	}

	const maxAttempts = 5
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		reqCtx := ctx
		if reqCtx == nil {
			reqCtx = context.Background()
		}
		reqCtx, cancel := context.WithTimeout(reqCtx, 30*time.Second)
		endpointURL := fmt.Sprintf("%s/%s:onboardUser", antigravityAPIEndpoint, antigravityAPIVersion)
		req, errRequest := http.NewRequestWithContext(reqCtx, http.MethodPost, endpointURL, bytes.NewReader(rawBody))
		if errRequest != nil {
			cancel()
			return "", fmt.Errorf("create request: %w", errRequest)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("X-Goog-Api-Client", antigravityGoogAPIClientUA)

		resp, errDo := h.oauthHTTPClient().Do(req)
		if errDo != nil {
			cancel()
			return "", fmt.Errorf("execute request: %w", errDo)
		}
		bodyBytes, errRead := io.ReadAll(resp.Body)
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("antigravity onboardUser: response body close error: %v", errClose)
		}
		cancel()
		if errRead != nil {
			return "", fmt.Errorf("read response: %w", errRead)
		}
		if resp.StatusCode != http.StatusOK {
			responsePreview := strings.TrimSpace(string(bodyBytes))
			if len(responsePreview) > 200 {
				responsePreview = responsePreview[:200]
			}
			return "", fmt.Errorf("http %d: %s", resp.StatusCode, responsePreview)
		}
		var data map[string]any
		if errDecode := json.Unmarshal(bodyBytes, &data); errDecode != nil {
			return "", fmt.Errorf("decode response: %w", errDecode)
		}
		if done, okDone := data["done"].(bool); okDone && done {
			if responseData, okResp := data["response"].(map[string]any); okResp {
				projectID := geminiProjectIDFromMap(responseData)
				if projectID != "" {
					return projectID, nil
				}
			}
			return "", fmt.Errorf("no project_id in response")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return "", nil
}

func antigravityLoadCodeAssistUserAgent() string {
	return fmt.Sprintf("antigravity/%s darwin/arm64 %s", antigravityFallbackVersion, antigravityNodeAPIClientUA)
}

func antigravityVersionFromUserAgent(userAgent string) string {
	userAgent = strings.TrimSpace(userAgent)
	lower := strings.ToLower(userAgent)
	if !strings.HasPrefix(lower, "antigravity/") {
		return antigravityFallbackVersion
	}
	rest := userAgent[len("antigravity/"):]
	if idx := strings.IndexAny(rest, " \t"); idx >= 0 {
		rest = rest[:idx]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return antigravityFallbackVersion
	}
	return rest
}

func generatePKCECodes() (*pkceCodes, error) {
	verifier, errVerifier := randomURLSafe(96)
	if errVerifier != nil {
		return nil, errVerifier
	}
	hash := sha256.Sum256([]byte(verifier))
	return &pkceCodes{
		CodeVerifier:  verifier,
		CodeChallenge: base64.RawURLEncoding.EncodeToString(hash[:]),
	}, nil
}

func generateOAuthState(prefix string) (string, error) {
	value, errState := randomURLSafe(24)
	if errState != nil {
		return "", errState
	}
	prefix = strings.Trim(prefix, "-_ .")
	if prefix == "" {
		return value, nil
	}
	return prefix + "-" + value, nil
}

func randomURLSafe(size int) (string, error) {
	if size <= 0 {
		size = 32
	}
	raw := make([]byte, size)
	if _, errRead := rand.Read(raw); errRead != nil {
		return "", errRead
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func validateOAuthState(state string) error {
	trimmed := strings.TrimSpace(state)
	if trimmed == "" {
		return fmt.Errorf("%w: empty", errInvalidOAuthState)
	}
	if len(trimmed) > maxOAuthStateLength {
		return fmt.Errorf("%w: too long", errInvalidOAuthState)
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return fmt.Errorf("%w: contains path separator", errInvalidOAuthState)
	}
	if strings.Contains(trimmed, "..") {
		return fmt.Errorf("%w: contains '..'", errInvalidOAuthState)
	}
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return fmt.Errorf("%w: invalid character", errInvalidOAuthState)
		}
	}
	return nil
}

func normalizeOAuthProvider(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "anthropic", "claude":
		return "anthropic", nil
	case "codex", "openai":
		return "codex", nil
	case "gemini", "google", "gemini-cli":
		return "gemini", nil
	case "antigravity", "anti-gravity":
		return "antigravity", nil
	case "kimi":
		return "kimi", nil
	default:
		return "", errUnsupportedProvider
	}
}

func authStatusMessage(provider string, err error) string {
	if err == nil {
		return ""
	}
	switch provider {
	case "anthropic", "codex":
		return "Failed to exchange authorization code for tokens"
	case "gemini", "antigravity":
		return "Failed to exchange token"
	default:
		return "Authentication failed"
	}
}

func (h *Handler) oauthConfig() *config.Config {
	if h != nil && h.runtime != nil {
		if cfg := h.runtime.Config(); cfg != nil {
			return cfg
		}
	}
	return &config.Config{}
}

func (h *Handler) oauthHTTPClient() *http.Client {
	cfg := h.oauthConfig()
	sdkCfg := cfg.SDKConfig
	if strings.TrimSpace(sdkCfg.ProxyURL) == "" {
		sdkCfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	}
	return util.SetProxy(&sdkCfg, &http.Client{Timeout: 60 * time.Second})
}

func requestContextOrBackground(c *gin.Context) context.Context {
	if c != nil && c.Request != nil && c.Request.Context() != nil {
		return c.Request.Context()
	}
	return context.Background()
}

func claudeCredentialFileName(email string) string {
	return fmt.Sprintf("claude-%s.json", strings.TrimSpace(email))
}

func geminiCredentialFileName(email, projectID string, includeProviderPrefix bool) string {
	email = strings.TrimSpace(email)
	project := strings.TrimSpace(projectID)
	if strings.EqualFold(project, "all") || strings.Contains(project, ",") {
		return fmt.Sprintf("gemini-%s-all.json", email)
	}
	prefix := ""
	if includeProviderPrefix {
		prefix = "gemini-"
	}
	return fmt.Sprintf("%s%s-%s.json", prefix, email, project)
}

func codexCredentialFileName(email, planType, hashAccountID string, includeProviderPrefix bool) string {
	email = strings.TrimSpace(email)
	plan := normalizePlanTypeForFilename(planType)
	prefix := ""
	if includeProviderPrefix {
		prefix = "codex"
	}
	if plan == "" {
		return fmt.Sprintf("%s-%s.json", prefix, email)
	}
	if plan == "team" {
		return fmt.Sprintf("%s-%s-%s-%s.json", prefix, hashAccountID, email, plan)
	}
	return fmt.Sprintf("%s-%s-%s.json", prefix, email, plan)
}

func normalizePlanTypeForFilename(planType string) string {
	planType = strings.TrimSpace(planType)
	if planType == "" {
		return ""
	}
	parts := strings.FieldsFunc(planType, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(parts) == 0 {
		return ""
	}
	for i, part := range parts {
		parts[i] = strings.ToLower(strings.TrimSpace(part))
	}
	return strings.Join(parts, "-")
}

func antigravityCredentialFileName(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return "antigravity.json"
	}
	return fmt.Sprintf("antigravity-%s.json", email)
}

func geminiCLIUserAgent(model string) string {
	if strings.TrimSpace(model) == "" {
		model = "unknown"
	}
	return fmt.Sprintf("GeminiCLI/%s/%s (%s; %s; terminal)", geminiCLIUserAgentVersion, model, geminiCLIOS(), geminiCLIArch())
}

func geminiCLIOS() string {
	switch runtime.GOOS {
	case "windows":
		return "win32"
	default:
		return runtime.GOOS
	}
}

func geminiCLIArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	default:
		return runtime.GOARCH
	}
}
