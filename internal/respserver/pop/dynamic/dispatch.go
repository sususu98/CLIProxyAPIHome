package dynamic

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPIHome/internal/access"
	homeerrors "github.com/router-for-me/CLIProxyAPIHome/internal/errors"
	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	typeAuth = "auth"
)

// Register wires package handlers into the provided registry.
func Register(reg *dispatch.Registry) {
	if reg == nil {
		return
	}
	_ = reg.RegisterDynamic("RPOP", typeAuth, handleAuth)
	_ = reg.SetDynamicDefault("RPOP", handleAuth)
}

// handleAuth handles an auth.
func handleAuth(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	// Validate request inputs before mutating persisted state.
	result, userAPIKey, errReply := dispatchRequest(ctx, env, args)
	if errReply != nil {
		return *errReply
	}
	if result == nil {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageNoDispatchResult)))
	}
	if result.Auth == nil {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageNoAuthAvailable)))
	}

	auth := home.SanitizeAuthForDownstream(result.Auth)
	if auth == nil {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageNoAuthAvailable)))
	}

	authJSON, errMarshal := json.Marshal(auth)
	if errMarshal != nil {
		return dispatch.BulkString([]byte(buildErrorJSON(errMarshal.Error())))
	}

	authIndex := strings.TrimSpace(auth.EnsureIndex())
	if authIndex == "" {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageNoAuthAvailable)))
	}
	authJSON, errSetAuthIndex := sjson.SetBytes(authJSON, "auth_index", authIndex)
	if errSetAuthIndex != nil {
		return dispatch.BulkString([]byte(buildErrorJSON(errSetAuthIndex.Error())))
	}

	out := []byte("{}")
	out, _ = sjson.SetBytes(out, "model", strings.TrimSpace(result.Model))
	out, _ = sjson.SetBytes(out, "provider", strings.TrimSpace(result.Provider))
	out, _ = sjson.SetBytes(out, "auth_index", authIndex)
	out, _ = sjson.SetBytes(out, "user_api_key", strings.TrimSpace(userAPIKey))
	out, _ = sjson.SetRawBytes(out, "auth", authJSON)
	return dispatch.BulkString(out)
}

// dispatchRequest handles a dispatch request.
func dispatchRequest(ctx context.Context, env dispatch.Env, args []string) (*home.DispatchResult, string, *dispatch.Reply) {
	// Build the candidate view before applying availability rules.
	if env.Runtime == nil {
		reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageRuntimeNotReady)))
		return nil, "", &reply
	}
	if ctx == nil {
		ctx = context.Background()
	}

	jsonArg, ok := dispatch.ExtractJSONArgument(args, 1)
	if !ok {
		reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageWrongNumberOfArgumentsRPOP)))
		return nil, "", &reply
	}
	jsonArg = strings.TrimSpace(jsonArg)
	if jsonArg == "" || !gjson.Valid(jsonArg) {
		reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageInvalidRequestJSON)))
		return nil, "", &reply
	}

	model := strings.TrimSpace(gjson.Get(jsonArg, "model").String())
	if model == "" {
		reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageMissingModel)))
		return nil, "", &reply
	}
	count := dispatchCount(jsonArg)

	headers := parseHeaders(jsonArg)
	sessionID := strings.TrimSpace(gjson.Get(jsonArg, "session_id").String())
	if sessionID != "" && strings.TrimSpace(headers.Get("X-Session-ID")) == "" {
		headers.Set("X-Session-ID", sessionID)
	}
	authRes, authErr := env.Runtime.Authenticate(ctx, headers)
	if authErr != nil {
		if access.IsAuthErrorCode(authErr, access.AuthErrorCodeNoCredentials) {
			reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageMissingRequiredCredentialHeaders)))
			return nil, "", &reply
		}
		if access.IsAuthErrorCode(authErr, access.AuthErrorCodeInvalidCredential) {
			reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageInvalidAPIKey)))
			return nil, "", &reply
		}
		reply := dispatch.BulkString([]byte(buildErrorJSON(authErr.Error())))
		return nil, "", &reply
	}

	if dispatchRetryExceeded(env.Runtime, count) {
		reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.TypeRequestRetryExceeded + ": " + homeerrors.MessageRequestRetryExceeded)))
		return nil, "", &reply
	}

	result, errDispatch := env.Runtime.Dispatch(ctx, model, headers)
	if errDispatch != nil {
		reply := dispatch.BulkString([]byte(buildErrorJSON(errDispatch.Error())))
		return nil, "", &reply
	}

	userAPIKey := ""
	if authRes != nil {
		userAPIKey = authRes.Principal
	}
	return result, userAPIKey, nil
}

// dispatchCount handles a dispatch count.
func dispatchCount(jsonArg string) int {
	count := int(gjson.Get(jsonArg, "count").Int())
	if count <= 0 {
		return 1
	}
	return count
}

// dispatchRetryExceeded handles a dispatch retry exceeded.
func dispatchRetryExceeded(rt *home.Runtime, count int) bool {
	if count <= 1 || rt == nil {
		return false
	}
	cfg := rt.Config()
	if cfg == nil {
		return false
	}
	requestRetry := cfg.RequestRetry
	if requestRetry < 0 {
		requestRetry = 0
	}
	return count-2 >= requestRetry
}

// parseHeaders parses a headers.
func parseHeaders(jsonArg string) http.Header {
	// Decode the wire frame before dispatching command handling.
	headersObj := gjson.Get(jsonArg, "headers")
	headers := http.Header{}
	if !headersObj.Exists() || !headersObj.IsObject() {
		return headers
	}

	headersObj.ForEach(func(k, v gjson.Result) bool {
		key := strings.TrimSpace(k.String())
		if key == "" {
			return true
		}

		if v.Type == gjson.String {
			headers.Add(key, v.String())
			return true
		}

		if v.IsArray() {
			v.ForEach(func(_, entry gjson.Result) bool {
				if entry.Type == gjson.String {
					headers.Add(key, entry.String())
					return true
				}
				if entry.Type != gjson.Null {
					headers.Add(key, entry.String())
				}
				return true
			})
			return true
		}

		if v.Type != gjson.Null {
			headers.Add(key, v.String())
		}
		return true
	})
	return headers
}

// buildErrorJSON builds an error json.
func buildErrorJSON(message string) string {
	errorType, errorMessage := homeerrors.SplitRedisErrorMessage(message)
	out := "{}"
	out, _ = sjson.Set(out, "error.type", errorType)
	out, _ = sjson.Set(out, "error.message", errorMessage)
	return out
}
