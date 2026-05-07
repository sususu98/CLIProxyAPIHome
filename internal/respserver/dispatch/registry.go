package dispatch

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"github.com/tidwall/gjson"
)

type Env struct {
	Runtime *home.Runtime
	Conn    *ConnEnv
}

type ConnEnv struct {
	SubscribeConfigYAML   func() (int64, error)
	UnsubscribeConfigYAML func() (int64, error)
	IsSubscribed          func() bool
}

type ReplyKind int

const (
	ReplyKindSimpleString ReplyKind = iota
	ReplyKindBulkString
	ReplyKindRedisError
	ReplyKindInteger
	ReplyKindArray
)

type Reply struct {
	Kind ReplyKind

	SimpleString string
	BulkString   []byte
	RedisError   string
	Integer      int64
	Array        []Reply
}

func SimpleString(value string) Reply {
	return Reply{
		Kind:         ReplyKindSimpleString,
		SimpleString: value,
	}
}

func BulkString(payload []byte) Reply {
	return Reply{
		Kind:       ReplyKindBulkString,
		BulkString: payload,
	}
}

func Integer(value int64) Reply {
	return Reply{
		Kind:    ReplyKindInteger,
		Integer: value,
	}
}

func Array(elements ...Reply) Reply {
	return Reply{
		Kind:  ReplyKindArray,
		Array: elements,
	}
}

func RedisError(message string) Reply {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "ERR error"
	}
	return Reply{
		Kind:       ReplyKindRedisError,
		RedisError: message,
	}
}

func Err(message string) Reply {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "error"
	}
	return RedisError("ERR " + message)
}

type Handler func(ctx context.Context, env Env, args []string) Reply

type RouteKind int

const (
	RouteKindDirect RouteKind = iota
	RouteKindDynamic
)

type Route struct {
	Kind      RouteKind
	Command   string
	Key       string
	Type      string
	IsDefault bool
}

type Registry struct {
	directHandlers        map[string]map[string]Handler
	directDefaultHandlers map[string]Handler
	dynamicHandlers       map[string]*dynamicHandlers
}

type dynamicHandlers struct {
	byType          map[string]Handler
	defaultHandler  Handler
	extractJSONFunc func(args []string) (string, bool)
}

func NewRegistry() *Registry {
	return &Registry{
		directHandlers:        map[string]map[string]Handler{},
		directDefaultHandlers: map[string]Handler{},
		dynamicHandlers:       map[string]*dynamicHandlers{},
	}
}

func (r *Registry) RegisterDirect(command string, key string, handler Handler) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	command = normalizeCommand(command)
	if command == "" {
		return fmt.Errorf("command is empty")
	}
	key = normalizeKey(key)
	if key == "" {
		return fmt.Errorf("key is empty")
	}
	if handler == nil {
		return fmt.Errorf("handler is nil")
	}

	if r.directHandlers[command] == nil {
		r.directHandlers[command] = map[string]Handler{}
	}
	r.directHandlers[command][key] = handler
	return nil
}

func (r *Registry) SetDirectDefault(command string, handler Handler) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	command = normalizeCommand(command)
	if command == "" {
		return fmt.Errorf("command is empty")
	}
	if handler == nil {
		return fmt.Errorf("handler is nil")
	}

	r.directDefaultHandlers[command] = handler
	return nil
}

func (r *Registry) RegisterDynamic(command string, typeValue string, handler Handler) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	command = normalizeCommand(command)
	if command == "" {
		return fmt.Errorf("command is empty")
	}
	typeValue = normalizeType(typeValue)
	if typeValue == "" {
		return fmt.Errorf("type is empty")
	}
	if handler == nil {
		return fmt.Errorf("handler is nil")
	}

	dyn := r.dynamicHandlers[command]
	if dyn == nil {
		dyn = &dynamicHandlers{
			byType: map[string]Handler{},
			extractJSONFunc: func(args []string) (string, bool) {
				return ExtractJSONArgument(args, 1)
			},
		}
		r.dynamicHandlers[command] = dyn
	}
	dyn.byType[typeValue] = handler
	return nil
}

func (r *Registry) SetDynamicDefault(command string, handler Handler) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	command = normalizeCommand(command)
	if command == "" {
		return fmt.Errorf("command is empty")
	}
	if handler == nil {
		return fmt.Errorf("handler is nil")
	}

	dyn := r.dynamicHandlers[command]
	if dyn == nil {
		dyn = &dynamicHandlers{
			byType: map[string]Handler{},
			extractJSONFunc: func(args []string) (string, bool) {
				return ExtractJSONArgument(args, 1)
			},
		}
		r.dynamicHandlers[command] = dyn
	}
	dyn.defaultHandler = handler
	return nil
}

func (r *Registry) Execute(ctx context.Context, env Env, args []string) Reply {
	if r == nil {
		return Err("registry not ready")
	}
	if len(args) == 0 {
		return Err("empty command")
	}

	command := normalizeCommand(args[0])
	if command == "" {
		return Err("empty command")
	}

	if direct := r.directHandlers[command]; direct != nil {
		if len(args) < 2 {
			if directDefault := r.directDefaultHandlers[command]; directDefault != nil {
				return directDefault(ctx, env, args)
			}
			return Err(fmt.Sprintf("wrong number of arguments for '%s' command", strings.ToLower(command)))
		}
		key := normalizeKey(args[1])
		handler := direct[key]
		if handler == nil {
			if directDefault := r.directDefaultHandlers[command]; directDefault != nil {
				return directDefault(ctx, env, args)
			}
			return Err("unsupported key")
		}
		return handler(ctx, env, args)
	}

	if dyn := r.dynamicHandlers[command]; dyn != nil {
		jsonArg, ok := dyn.extractJSONFunc(args)
		if !ok {
			if dyn.defaultHandler != nil {
				return dyn.defaultHandler(ctx, env, args)
			}
			return Err(fmt.Sprintf("wrong number of arguments for '%s' command", strings.ToLower(command)))
		}

		typeValue := normalizeType(extractTypeFromJSON(jsonArg))
		if typeValue == "" {
			return Err("unsupported type")
		}
		handler := dyn.byType[typeValue]
		if handler != nil {
			return handler(ctx, env, args)
		}
		return Err("unsupported type")
	}

	return RedisError(fmt.Sprintf("ERR unknown command '%s'", strings.ToLower(command)))
}

func (r *Registry) Routes() []Route {
	if r == nil {
		return nil
	}
	var routes []Route
	for cmd, direct := range r.directHandlers {
		for key := range direct {
			routes = append(routes, Route{
				Kind:    RouteKindDirect,
				Command: cmd,
				Key:     key,
			})
		}
		if r.directDefaultHandlers[cmd] != nil {
			routes = append(routes, Route{
				Kind:      RouteKindDirect,
				Command:   cmd,
				IsDefault: true,
			})
		}
	}
	for cmd, dyn := range r.dynamicHandlers {
		if dyn == nil {
			continue
		}
		for typeValue := range dyn.byType {
			routes = append(routes, Route{
				Kind:    RouteKindDynamic,
				Command: cmd,
				Type:    typeValue,
			})
		}
		if dyn.defaultHandler != nil {
			routes = append(routes, Route{
				Kind:      RouteKindDynamic,
				Command:   cmd,
				IsDefault: true,
			})
		}
	}

	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Command != routes[j].Command {
			return routes[i].Command < routes[j].Command
		}
		if routes[i].Kind != routes[j].Kind {
			return routes[i].Kind < routes[j].Kind
		}
		if routes[i].IsDefault != routes[j].IsDefault {
			return routes[i].IsDefault
		}
		if routes[i].Key != routes[j].Key {
			return routes[i].Key < routes[j].Key
		}
		return routes[i].Type < routes[j].Type
	})
	return routes
}

func ExtractJSONArgument(args []string, jsonIndex int) (string, bool) {
	if len(args) == 2 && jsonIndex == 1 {
		return args[1], true
	}
	if len(args) == 3 && jsonIndex == 1 {
		return args[2], true
	}
	return "", false
}

func extractTypeFromJSON(jsonArg string) string {
	jsonArg = strings.TrimSpace(jsonArg)
	if jsonArg == "" || !gjson.Valid(jsonArg) {
		return ""
	}
	return gjson.Get(jsonArg, "type").String()
}

func normalizeCommand(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func normalizeKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
