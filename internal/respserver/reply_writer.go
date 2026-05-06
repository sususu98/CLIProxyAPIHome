package respserver

import (
	"bufio"
	"fmt"

	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
)

func writeDispatchReply(writer *bufio.Writer, reply dispatch.Reply) error {
	switch reply.Kind {
	case dispatch.ReplyKindSimpleString:
		return writeRedisSimpleString(writer, reply.SimpleString)
	case dispatch.ReplyKindBulkString:
		return writeRedisBulkString(writer, reply.BulkString)
	case dispatch.ReplyKindRedisError:
		return writeRedisError(writer, reply.RedisError)
	default:
		return writeRedisError(writer, fmt.Sprintf("ERR unsupported reply kind: %d", reply.Kind))
	}
}
