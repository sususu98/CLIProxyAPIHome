package get

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func handleDefault(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	if env.Runtime == nil {
		return dispatch.BulkString([]byte(buildErrorJSON("runtime not ready")))
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) != 2 {
		return dispatch.BulkString([]byte(buildErrorJSON("wrong number of arguments for 'get' command")))
	}

	jsonArg := strings.TrimSpace(args[1])
	if jsonArg == "" || !gjson.Valid(jsonArg) {
		return dispatch.BulkString([]byte(buildErrorJSON("invalid request json")))
	}
	typeValue := strings.ToLower(strings.TrimSpace(gjson.Get(jsonArg, "type").String()))
	if typeValue != "refresh" {
		return dispatch.BulkString([]byte(buildErrorJSON("unsupported type")))
	}

	authIndex := strings.TrimSpace(gjson.Get(jsonArg, "auth_index").String())
	if authIndex == "" {
		return dispatch.BulkString([]byte(buildErrorJSON("missing auth_index")))
	}

	core := env.Runtime.CoreManager()
	if core == nil {
		return dispatch.BulkString([]byte(buildErrorJSON("runtime not ready")))
	}

	updated, errRefresh := core.RefreshNow(ctx, authIndex)
	if errRefresh != nil {
		return dispatch.BulkString([]byte(buildErrorJSON(errRefresh.Error())))
	}
	if updated == nil {
		return dispatch.BulkString([]byte(buildErrorJSON("auth not found")))
	}

	auth := home.SanitizeAuthForDownstream(updated)
	if auth == nil {
		return dispatch.BulkString([]byte(buildErrorJSON("auth not found")))
	}
	authJSON, errMarshal := json.Marshal(auth)
	if errMarshal != nil {
		return dispatch.BulkString([]byte(buildErrorJSON(errMarshal.Error())))
	}

	out := []byte("{}")
	out, _ = sjson.SetBytes(out, "auth_index", strings.TrimSpace(auth.ID))
	out, _ = sjson.SetRawBytes(out, "auth", authJSON)
	return dispatch.BulkString(out)
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
