package push

import "github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"

func Register(reg *dispatch.Registry) {
	if reg == nil {
		return
	}
	_ = reg.RegisterDirect("LPUSH", "usage", handleUsage)
	_ = reg.SetDirectDefault("LPUSH", handleUsage)
	_ = reg.RegisterDirect("LPRUSH", "usage", handleUsage)

	_ = reg.RegisterDirect("RPUSH", "request-log", handleRequestLog)
}
