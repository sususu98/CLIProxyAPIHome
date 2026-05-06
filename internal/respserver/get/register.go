package get

import "github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"

func Register(reg *dispatch.Registry) {
	if reg == nil {
		return
	}
	_ = reg.RegisterDirect("GET", "config", handleConfig)
	_ = reg.RegisterDirect("GET", "models", handleModels)
	_ = reg.SetDirectDefault("GET", handleDefault)
}
