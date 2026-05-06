package push

import (
	"context"
	"strings"

	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
	"github.com/tidwall/gjson"
)

func handleUsage(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	_ = ctx
	_ = env

	if len(args) != 3 {
		return dispatch.Err("wrong number of arguments for 'lprush' command")
	}

	if !strings.EqualFold(strings.TrimSpace(args[1]), "usage") {
		return dispatch.Err("unsupported key")
	}

	payload := strings.TrimSpace(args[2])
	if payload == "" || !gjson.Valid(payload) {
		return dispatch.Err("invalid usage json")
	}

	// Intentionally dropped (no persistence yet).
	return dispatch.SimpleString("OK")
}
