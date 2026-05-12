package subscribe

import (
	"context"

	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
)

// handleConfig handles a config.
func handleConfig(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	_ = ctx

	if len(args) != 2 {
		return dispatch.Err("wrong number of arguments for 'subscribe' command")
	}

	if env.Conn == nil || env.Conn.SubscribeConfigYAML == nil {
		return dispatch.Err("subscribe not supported")
	}

	count, errSub := env.Conn.SubscribeConfigYAML()
	if errSub != nil {
		return dispatch.Err(errSub.Error())
	}

	return dispatch.Array(
		dispatch.BulkString([]byte("subscribe")),
		dispatch.BulkString([]byte("config")),
		dispatch.Integer(count),
	)
}
