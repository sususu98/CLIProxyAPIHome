package set

import "github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"

func Register(reg *dispatch.Registry) {
	if reg == nil {
		return
	}
	_ = reg.RegisterDirect("SET", "token", handleToken)
}
