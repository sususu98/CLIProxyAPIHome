package get

import (
	"context"
	"encoding/json"
	"strings"

	homeerrors "github.com/router-for-me/CLIProxyAPIHome/internal/errors"
	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func handleDefault(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	if env.Runtime == nil {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageRuntimeNotReady)))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) != 2 {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageWrongNumberOfArgumentsGet)))
	}

	jsonArg := strings.TrimSpace(args[1])
	if jsonArg == "" || !gjson.Valid(jsonArg) {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageInvalidRequestJSON)))
	}
	typeValue := strings.ToLower(strings.TrimSpace(gjson.Get(jsonArg, "type").String()))
	if typeValue != "refresh" {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageUnsupportedType)))
	}

	authIndex := strings.TrimSpace(gjson.Get(jsonArg, "auth_index").String())
	if authIndex == "" {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageMissingAuthIndex)))
	}

	core := env.Runtime.CoreManager()
	if core == nil {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageRuntimeNotReady)))
	}

	updated, errRefresh := core.RefreshNow(ctx, authIndex)
	if errRefresh != nil {
		return dispatch.BulkString([]byte(buildErrorJSON(errRefresh.Error())))
	}
	if updated == nil {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageAuthNotFound)))
	}

	auth := home.SanitizeAuthForDownstream(updated)
	if auth == nil {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageAuthNotFound)))
	}
	authJSON, errMarshal := json.Marshal(auth)
	if errMarshal != nil {
		return dispatch.BulkString([]byte(buildErrorJSON(errMarshal.Error())))
	}

	authIndexTrimmed := strings.TrimSpace(auth.EnsureIndex())
	if authIndexTrimmed == "" {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageAuthNotFound)))
	}
	authJSON, errSetAuthIndex := sjson.SetBytes(authJSON, "auth_index", authIndexTrimmed)
	if errSetAuthIndex != nil {
		return dispatch.BulkString([]byte(buildErrorJSON(errSetAuthIndex.Error())))
	}

	out := []byte("{}")
	out, _ = sjson.SetBytes(out, "auth_index", authIndexTrimmed)
	out, _ = sjson.SetRawBytes(out, "auth", authJSON)
	return dispatch.BulkString(out)
}

func buildErrorJSON(message string) string {
	errorType, errorMessage := homeerrors.SplitRedisErrorMessage(message)
	out := "{}"
	out, _ = sjson.Set(out, "error.type", errorType)
	out, _ = sjson.Set(out, "error.message", errorMessage)
	return out
}
