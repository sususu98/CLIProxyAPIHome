package get

import (
	"context"
	"strings"

	homeerrors "github.com/router-for-me/CLIProxyAPIHome/internal/errors"
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

	payload, errRefresh := env.Runtime.RefreshNow(ctx, authIndex)
	if errRefresh != nil {
		return dispatch.BulkString([]byte(buildErrorJSON(errRefresh.Error())))
	}
	if len(payload) == 0 {
		return dispatch.BulkString([]byte(buildErrorJSON(homeerrors.MessageAuthNotFound)))
	}
	return dispatch.BulkString(payload)
}

func buildErrorJSON(message string) string {
	errorType, errorMessage := homeerrors.SplitRedisErrorMessage(message)
	out := "{}"
	out, _ = sjson.Set(out, "error.type", errorType)
	out, _ = sjson.Set(out, "error.message", errorMessage)
	return out
}
