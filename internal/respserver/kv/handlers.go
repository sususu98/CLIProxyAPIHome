package kv

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
)

// handleSet handles Redis-compatible SET.
func handleSet(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	if env.Runtime == nil {
		return dispatch.Err("runtime not ready")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) < 3 {
		return wrongArgs("set")
	}

	ttl, mode, errOptions := parseSetOptions(args[3:])
	if errOptions != nil {
		return dispatch.Err(errOptions.Error())
	}
	written, errSet := env.Runtime.KVSet(ctx, args[1], []byte(args[2]), ttl, mode)
	if errSet != nil {
		return dispatch.Err(errSet.Error())
	}
	if !written {
		return dispatch.BulkString(nil)
	}
	return dispatch.SimpleString("OK")
}

// handleSetNX handles Redis-compatible SETNX.
func handleSetNX(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	if env.Runtime == nil {
		return dispatch.Err("runtime not ready")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) != 3 {
		return wrongArgs("setnx")
	}
	written, errSet := env.Runtime.KVSet(ctx, args[1], []byte(args[2]), 0, "nx")
	if errSet != nil {
		return dispatch.Err(errSet.Error())
	}
	if written {
		return dispatch.Integer(1)
	}
	return dispatch.Integer(0)
}

// handleDel handles Redis-compatible DEL.
func handleDel(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	if env.Runtime == nil {
		return dispatch.Err("runtime not ready")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) < 2 {
		return wrongArgs("del")
	}
	deleted, errDel := env.Runtime.KVDel(ctx, args[1:])
	if errDel != nil {
		return dispatch.Err(errDel.Error())
	}
	return dispatch.Integer(deleted)
}

// handleExpire handles Redis-compatible EXPIRE.
func handleExpire(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	if env.Runtime == nil {
		return dispatch.Err("runtime not ready")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) != 3 {
		return wrongArgs("expire")
	}
	seconds, errParse := parsePositiveInt(args[2], "expire seconds")
	if errParse != nil {
		return dispatch.Err(errParse.Error())
	}
	ok, errExpire := env.Runtime.KVExpire(ctx, args[1], time.Duration(seconds)*time.Second)
	if errExpire != nil {
		return dispatch.Err(errExpire.Error())
	}
	if ok {
		return dispatch.Integer(1)
	}
	return dispatch.Integer(0)
}

// handleTTL handles Redis-compatible TTL.
func handleTTL(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	if env.Runtime == nil {
		return dispatch.Err("runtime not ready")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) != 2 {
		return wrongArgs("ttl")
	}
	ttl, errTTL := env.Runtime.KVTTL(ctx, args[1])
	if errTTL != nil {
		return dispatch.Err(errTTL.Error())
	}
	return dispatch.Integer(ttl)
}

// handleIncrBy handles Redis-compatible INCRBY.
func handleIncrBy(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	if env.Runtime == nil {
		return dispatch.Err("runtime not ready")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) != 3 {
		return wrongArgs("incrby")
	}
	delta, errParse := strconv.ParseInt(strings.TrimSpace(args[2]), 10, 64)
	if errParse != nil {
		return dispatch.Err("delta is not an integer")
	}
	value, errIncr := env.Runtime.KVIncrBy(ctx, args[1], delta)
	if errIncr != nil {
		return dispatch.Err(errIncr.Error())
	}
	return dispatch.Integer(value)
}

// handleMGet handles Redis-compatible MGET.
func handleMGet(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	if env.Runtime == nil {
		return dispatch.Err("runtime not ready")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) < 2 {
		return wrongArgs("mget")
	}
	results, errMGet := env.Runtime.KVMGet(ctx, args[1:])
	if errMGet != nil {
		return dispatch.Err(errMGet.Error())
	}
	replies := make([]dispatch.Reply, 0, len(results))
	for _, result := range results {
		if !result.Found {
			replies = append(replies, dispatch.BulkString(nil))
			continue
		}
		replies = append(replies, dispatch.BulkString(result.Value))
	}
	return dispatch.Array(replies...)
}

// handleMSet handles Redis-compatible MSET.
func handleMSet(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	if env.Runtime == nil {
		return dispatch.Err("runtime not ready")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) < 3 || (len(args)-1)%2 != 0 {
		return wrongArgs("mset")
	}
	pairs := make(map[string][]byte, (len(args)-1)/2)
	for i := 1; i < len(args); i += 2 {
		pairs[args[i]] = []byte(args[i+1])
	}
	if errMSet := env.Runtime.KVMSet(ctx, pairs); errMSet != nil {
		return dispatch.Err(errMSet.Error())
	}
	return dispatch.SimpleString("OK")
}

func parseSetOptions(args []string) (time.Duration, string, error) {
	var ttl time.Duration
	ttlSet := false
	mode := ""
	for i := 0; i < len(args); i++ {
		option := strings.ToUpper(strings.TrimSpace(args[i]))
		switch option {
		case "EX", "PX":
			if ttlSet {
				return 0, "", fmt.Errorf("conflicting ttl options")
			}
			if i+1 >= len(args) {
				return 0, "", fmt.Errorf("missing %s value", option)
			}
			value, errParse := parsePositiveInt(args[i+1], strings.ToLower(option)+" value")
			if errParse != nil {
				return 0, "", errParse
			}
			if option == "EX" {
				ttl = time.Duration(value) * time.Second
			} else {
				ttl = time.Duration(value) * time.Millisecond
			}
			ttlSet = true
			i++
		case "NX", "XX":
			normalizedMode := strings.ToLower(option)
			if mode != "" && mode != normalizedMode {
				return 0, "", fmt.Errorf("conflicting set modes")
			}
			if mode == normalizedMode {
				return 0, "", fmt.Errorf("duplicate set mode")
			}
			mode = normalizedMode
		default:
			return 0, "", fmt.Errorf("unsupported set option %q", args[i])
		}
	}
	return ttl, mode, nil
}

func parsePositiveInt(value string, label string) (int64, error) {
	parsed, errParse := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if errParse != nil {
		return 0, fmt.Errorf("%s is not an integer", label)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", label)
	}
	return parsed, nil
}

func wrongArgs(command string) dispatch.Reply {
	return dispatch.Err(fmt.Sprintf("wrong number of arguments for '%s' command", strings.ToLower(command)))
}
