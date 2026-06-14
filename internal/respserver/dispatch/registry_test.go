package dispatch

import (
	"context"
	"strings"
	"testing"
)

func TestRegistryRoutesDirectDefaultWithoutDirectHandlers(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	if errSet := reg.SetDirectDefault("DEL", func(ctx context.Context, env Env, args []string) Reply {
		return SimpleString("direct-default")
	}); errSet != nil {
		t.Fatalf("SetDirectDefault() error = %v", errSet)
	}

	reply := reg.Execute(context.Background(), Env{}, []string{"DEL", "a", "b"})
	if reply.Kind != ReplyKindSimpleString || reply.SimpleString != "direct-default" {
		t.Fatalf("Execute() = %#v, want direct default simple string", reply)
	}
}

func TestRegistryDynamicRoutingWinsForRPOPJSON(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	if errSet := reg.SetDirectDefault("RPOP", func(ctx context.Context, env Env, args []string) Reply {
		return SimpleString("direct-default")
	}); errSet != nil {
		t.Fatalf("SetDirectDefault() error = %v", errSet)
	}
	if errRegister := reg.RegisterDynamic("RPOP", "auth", func(ctx context.Context, env Env, args []string) Reply {
		return SimpleString("dynamic")
	}); errRegister != nil {
		t.Fatalf("RegisterDynamic() error = %v", errRegister)
	}

	reply := reg.Execute(context.Background(), Env{}, []string{"RPOP", `{"type":"auth"}`})
	if reply.Kind != ReplyKindSimpleString || reply.SimpleString != "dynamic" {
		t.Fatalf("Execute() = %#v, want dynamic simple string", reply)
	}
}

func TestRegistryUnknownCommand(t *testing.T) {
	t.Parallel()

	reply := NewRegistry().Execute(context.Background(), Env{}, []string{"NOPE"})
	if reply.Kind != ReplyKindRedisError || !strings.Contains(reply.RedisError, "unknown command") {
		t.Fatalf("Execute() = %#v, want unknown command redis error", reply)
	}
}
