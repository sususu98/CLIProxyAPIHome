package dynamic

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
	sdkaccess "github.com/router-for-me/CLIProxyAPIHome/sdk/access"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	typeAccessToken = "access_token"
	typeAuth        = "auth"
)

func Register(reg *dispatch.Registry) {
	if reg == nil {
		return
	}
	_ = reg.RegisterDynamic("RPOP", typeAccessToken, handleAccessToken)
	_ = reg.RegisterDynamic("RPOP", typeAuth, handleAuth)
	_ = reg.SetDynamicDefault("RPOP", handleAccessToken)
}

func handleAccessToken(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	result, errReply := dispatchRequest(ctx, env, args)
	if errReply != nil {
		return *errReply
	}
	if result == nil {
		return dispatch.BulkString([]byte(buildErrorJSON("no dispatch result")))
	}

	out := "{}"
	out, _ = sjson.Set(out, "model", strings.TrimSpace(result.Model))

	if strings.TrimSpace(result.AccessToken) != "" {
		out, _ = sjson.Set(out, "access_token", strings.TrimSpace(result.AccessToken))
		return dispatch.BulkString([]byte(out))
	}

	if strings.TrimSpace(result.APIKey) == "" {
		return dispatch.BulkString([]byte(buildErrorJSON("no credential available")))
	}

	out, _ = sjson.Set(out, "base_url", strings.TrimSpace(result.BaseURL))
	out, _ = sjson.Set(out, "api_key", strings.TrimSpace(result.APIKey))
	return dispatch.BulkString([]byte(out))
}

func handleAuth(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	result, errReply := dispatchRequest(ctx, env, args)
	if errReply != nil {
		return *errReply
	}
	if result == nil {
		return dispatch.BulkString([]byte(buildErrorJSON("no dispatch result")))
	}
	if result.Auth == nil {
		return dispatch.BulkString([]byte(buildErrorJSON("no auth available")))
	}

	auth := home.SanitizeAuthForDownstream(result.Auth)
	if auth == nil {
		return dispatch.BulkString([]byte(buildErrorJSON("no auth available")))
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
		reply := dispatch.BulkString([]byte(buildErrorJSON("runtime not ready")))
		return nil, &reply
	}
	if ctx == nil {
		ctx = context.Background()
	}

	jsonArg, ok := dispatch.ExtractJSONArgument(args, 1)
	if !ok {
		reply := dispatch.BulkString([]byte(buildErrorJSON("wrong number of arguments for 'rpop' command")))
		return nil, &reply
	}
	jsonArg = strings.TrimSpace(jsonArg)
	if jsonArg == "" || !gjson.Valid(jsonArg) {
		reply := dispatch.BulkString([]byte(buildErrorJSON("invalid request json")))
		return nil, &reply
	}

	model := strings.TrimSpace(gjson.Get(jsonArg, "model").String())
	if model == "" {
		reply := dispatch.BulkString([]byte(buildErrorJSON("missing model")))
		return nil, &reply
	}

	headers := parseHeaders(jsonArg)
	_, authErr := env.Runtime.Authenticate(ctx, headers)
	if authErr != nil {
		if sdkaccess.IsAuthErrorCode(authErr, sdkaccess.AuthErrorCodeNoCredentials) {
			reply := dispatch.BulkString([]byte(buildErrorJSON("missing required credential headers")))
			return nil, &reply
		}
		if sdkaccess.IsAuthErrorCode(authErr, sdkaccess.AuthErrorCodeInvalidCredential) {
			reply := dispatch.BulkString([]byte(buildErrorJSON("invalid api key")))
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
	message = strings.TrimSpace(message)
	if message == "" {
		message = "error"
	}
	out := "{}"
	out, _ = sjson.Set(out, "error.message", message)
	return out
}
