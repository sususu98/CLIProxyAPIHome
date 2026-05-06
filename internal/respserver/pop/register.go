package pop

import (
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/pop/dynamic"
)

func Register(reg *dispatch.Registry) {
	if reg == nil {
		return
	}
	dynamic.Register(reg)
}
