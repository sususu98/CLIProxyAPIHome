package respserver

import (
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
	respget "github.com/router-for-me/CLIProxyAPIHome/internal/respserver/get"
	resppop "github.com/router-for-me/CLIProxyAPIHome/internal/respserver/pop"
	resppush "github.com/router-for-me/CLIProxyAPIHome/internal/respserver/push"
	respset "github.com/router-for-me/CLIProxyAPIHome/internal/respserver/set"
	respsubscribe "github.com/router-for-me/CLIProxyAPIHome/internal/respserver/subscribe"
)

func buildRegistry() *dispatch.Registry {
	reg := dispatch.NewRegistry()
	respset.Register(reg)
	respget.Register(reg)
	respsubscribe.Register(reg)
	resppush.Register(reg)
	resppop.Register(reg)
	return reg
}
