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

func Register(reg *dispatch.Registry) {
	if reg == nil {
		return
	}
	_ = reg.RegisterDynamic("RPOP", typeAuth, handleAuth)
	_ = reg.SetDynamicDefault("RPOP", handleAuth)
}

func handleAuth(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	result, errReply := dispatchRequest(ctx, env, args)
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

	out := []byte("{}")
	out, _ = sjson.SetBytes(out, "model", strings.TrimSpace(result.Model))
	out, _ = sjson.SetBytes(out, "provider", strings.TrimSpace(result.Provider))
	out, _ = sjson.SetBytes(out, "auth_index", strings.TrimSpace(auth.ID))
	out, _ = sjson.SetRawBytes(out, "auth", authJSON)
	return dispatch.BulkString(out)
}

func dispatchRequest(ctx context.Context, env dispatch.Env, args []string) (*home.DispatchResult, *dispatch.Reply) {
	if env.Runtime == nil {
		reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageRuntimeNotReady)))
		return nil, &reply
	}
	if ctx == nil {
		ctx = context.Background()
	}

	jsonArg, ok := dispatch.ExtractJSONArgument(args, 1)
	if !ok {
		reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageWrongNumberOfArgumentsRPOP)))
		return nil, &reply
	}
	jsonArg = strings.TrimSpace(jsonArg)
	if jsonArg == "" || !gjson.Valid(jsonArg) {
		reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageInvalidRequestJSON)))
		return nil, &reply
	}

	model := strings.TrimSpace(gjson.Get(jsonArg, "model").String())
	if model == "" {
		reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageMissingModel)))
		return nil, &reply
	}

	headers := parseHeaders(jsonArg)
	sessionID := strings.TrimSpace(gjson.Get(jsonArg, "session_id").String())
	if sessionID != "" && strings.TrimSpace(headers.Get("X-Session-ID")) == "" {
		headers.Set("X-Session-ID", sessionID)
	}
	_, authErr := env.Runtime.Authenticate(ctx, headers)
	if authErr != nil {
		if access.IsAuthErrorCode(authErr, access.AuthErrorCodeNoCredentials) {
			reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageMissingRequiredCredentialHeaders)))
			return nil, &reply
		}
		if access.IsAuthErrorCode(authErr, access.AuthErrorCodeInvalidCredential) {
			reply := dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageInvalidAPIKey)))
			return nil, &reply
		}
		reply := dispatch.BulkString([]byte(buildErrorJSON(authErr.Error())))
		return nil, &reply
	}

	result, errDispatch := env.Runtime.Dispatch(ctx, model, headers)
	if errDispatch != nil {
		reply := dispatch.BulkString([]byte(buildErrorJSON(errDispatch.Error())))
		return nil, &reply
	}
	return result, nil
}

func parseHeaders(jsonArg string) http.Header {
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

func buildErrorJSON(message string) string {
	errorType, errorMessage := homeerrors.SplitRedisErrorMessage(message)
	out := "{}"
	out, _ = sjson.Set(out, "error.type", errorType)
	out, _ = sjson.Set(out, "error.message", errorMessage)
	return out
}
