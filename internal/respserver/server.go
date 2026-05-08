package respserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"github.com/router-for-me/CLIProxyAPIHome/internal/node"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
	log "github.com/sirupsen/logrus"
)

type Server struct {
	addr     string
	runtime  *home.Runtime
	registry *dispatch.Registry
	auth     *managementAuthenticator
}

func New(addr string, runtime *home.Runtime) *Server {
	return &Server{
		addr:     strings.TrimSpace(addr),
		runtime:  runtime,
		registry: buildRegistry(),
		auth:     newManagementAuthenticator(runtime),
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("resp server: server is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(s.addr) == "" {
		return fmt.Errorf("resp server: addr is empty")
	}

	listener, errListen := net.Listen("tcp", s.addr)
	if errListen != nil {
		return errListen
	}
	defer func() {
		if errClose := listener.Close(); errClose != nil {
			log.Errorf("resp listener close error: %v", errClose)
		}
	}()

	log.Infof("RESP server listening on %s", s.addr)

	closeCh := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-closeCh:
		}
	}()
	defer close(closeCh)

	for {
		conn, errAccept := listener.Accept()
		if errAccept != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(errAccept, net.ErrClosed) {
				return nil
			}
			log.Errorf("resp accept error: %v", errAccept)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		go s.HandleConn(ctx, conn)
	}
}

func (s *Server) HandleConn(ctx context.Context, conn net.Conn) {
	if s == nil || conn == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	clientIP, localClient := resolveRemoteIP(conn.RemoteAddr())
	authed := false
	reader := bufio.NewReader(conn)
	writer := newSafeWriter(bufio.NewWriter(conn))
	connectedAt := time.Now()
	addedNode := false
	var unsubscribeConfig func()
	defer func() {
		if unsubscribeConfig != nil {
			unsubscribeConfig()
			unsubscribeConfig = nil
		}
		if addedNode {
			node.GlobalRegistry().Remove(clientIP)
			addedNode = false
		}
		if errClose := conn.Close(); errClose != nil {
			log.Errorf("resp connection close error: %v", errClose)
		}
	}()

	for {
		args, errRead := readRESPArray(reader)
		if errRead != nil {
			if !errors.Is(errRead, io.EOF) {
				_ = writer.WriteRedisError("ERR " + errRead.Error())
			}
			return
		}
		if len(args) == 0 {
			_ = writer.WriteRedisError("ERR empty command")
			continue
		}

		cmd := strings.ToUpper(strings.TrimSpace(args[0]))

		if cmd == "PING" {
			switch len(args) {
			case 1:
				if unsubscribeConfig != nil {
					_ = writer.WriteDispatchReply(dispatch.Array(
						dispatch.BulkString([]byte("pong")),
						dispatch.BulkString([]byte{}),
					))
				} else {
					_ = writer.WriteRedisSimpleString("PONG")
				}
			case 2:
				if unsubscribeConfig != nil {
					_ = writer.WriteDispatchReply(dispatch.Array(
						dispatch.BulkString([]byte("pong")),
						dispatch.BulkString([]byte(args[1])),
					))
				} else {
					_ = writer.WriteRedisBulkString([]byte(args[1]))
				}
			default:
				_ = writer.WriteRedisError("ERR wrong number of arguments for 'ping' command")
			}
			continue
		}

		if cmd != "AUTH" && !authed {
			if s.auth != nil {
				_, statusCode, errMsg := s.auth.AuthenticateManagementKey(clientIP, localClient, "")
				if statusCode == http.StatusForbidden && strings.HasPrefix(errMsg, "IP banned due to too many failed attempts") {
					_ = writer.WriteRedisError("ERR " + errMsg)
				} else {
					_ = writer.WriteRedisError("NOAUTH Authentication required.")
				}
			} else {
				_ = writer.WriteRedisError("NOAUTH Authentication required.")
			}
			continue
		}

		if cmd == "AUTH" {
			password, ok := parseAuthPassword(args)
			if !ok {
				if s.auth != nil {
					_, statusCode, errMsg := s.auth.AuthenticateManagementKey(clientIP, localClient, "")
					if statusCode == http.StatusForbidden && strings.HasPrefix(errMsg, "IP banned due to too many failed attempts") {
						_ = writer.WriteRedisError("ERR " + errMsg)
						continue
					}
				}
				_ = writer.WriteRedisError("ERR wrong number of arguments for 'auth' command")
				continue
			}
			if s.auth == nil {
				_ = writer.WriteRedisError("ERR remote management disabled")
				continue
			}
			allowed, _, errMsg := s.auth.AuthenticateManagementKey(clientIP, localClient, password)
			if !allowed {
				_ = writer.WriteRedisError("ERR " + errMsg)
				continue
			}
			authed = true
			_ = writer.WriteRedisSimpleString("OK")
			continue
		}

		reply := dispatch.Err("registry not ready")
		if s.registry != nil {
			reply = s.registry.Execute(ctx, dispatch.Env{
				Runtime: s.runtime,
				Conn: &dispatch.ConnEnv{
					SubscribeConfigYAML: func() (int64, error) {
						if s.runtime == nil {
							return 0, fmt.Errorf("runtime not ready")
						}
						if unsubscribeConfig != nil {
							return 1, nil
						}
						if !addedNode {
							node.GlobalRegistry().Add(clientIP, connectedAt)
							addedNode = true
						}
						unsubscribeConfig = s.runtime.SubscribeConfigYAML(func(payload []byte) error {
							return writer.WriteDispatchReply(dispatch.Array(
								dispatch.BulkString([]byte("message")),
								dispatch.BulkString([]byte("config")),
								dispatch.BulkString(payload),
							))
						})
						return 1, nil
					},
					UnsubscribeConfigYAML: func() (int64, error) {
						if unsubscribeConfig == nil {
							return 0, nil
						}
						unsubscribeConfig()
						unsubscribeConfig = nil
						if addedNode {
							node.GlobalRegistry().Remove(clientIP)
							addedNode = false
						}
						return 0, nil
					},
					IsSubscribed: func() bool {
						return unsubscribeConfig != nil
					},
				},
			}, args)
		}
		if errWrite := writer.WriteDispatchReply(reply); errWrite != nil {
			log.Errorf("resp write reply error: %v", errWrite)
			return
		}
	}
}

func readRESPArray(reader *bufio.Reader) ([]string, error) {
	prefix, errRead := reader.ReadByte()
	if errRead != nil {
		return nil, errRead
	}
	if prefix != '*' {
		return nil, fmt.Errorf("protocol error")
	}
	line, errLine := readRESPLine(reader)
	if errLine != nil {
		return nil, errLine
	}
	count, errAtoi := strconv.Atoi(line)
	if errAtoi != nil || count < 0 {
		return nil, fmt.Errorf("protocol error")
	}
	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		value, errValue := readRESPString(reader)
		if errValue != nil {
			return nil, errValue
		}
		args = append(args, value)
	}
	return args, nil
}

func readRESPString(reader *bufio.Reader) (string, error) {
	prefix, errRead := reader.ReadByte()
	if errRead != nil {
		return "", errRead
	}
	switch prefix {
	case '$':
		return readRESPBulkString(reader)
	case '+', ':':
		return readRESPLine(reader)
	default:
		return "", fmt.Errorf("protocol error")
	}
}

func readRESPBulkString(reader *bufio.Reader) (string, error) {
	line, errLine := readRESPLine(reader)
	if errLine != nil {
		return "", errLine
	}
	length, errAtoi := strconv.Atoi(line)
	if errAtoi != nil {
		return "", fmt.Errorf("protocol error")
	}
	if length < 0 {
		return "", nil
	}
	buf := make([]byte, length+2)
	if _, errRead := io.ReadFull(reader, buf); errRead != nil {
		return "", errRead
	}
	if length+2 < 2 || buf[length] != '\r' || buf[length+1] != '\n' {
		return "", fmt.Errorf("protocol error")
	}
	return string(buf[:length]), nil
}

func readRESPLine(reader *bufio.Reader) (string, error) {
	line, errRead := reader.ReadString('\n')
	if errRead != nil {
		return "", errRead
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, nil
}

func writeRedisSimpleString(writer *bufio.Writer, value string) error {
	if writer == nil {
		return net.ErrClosed
	}
	_, errWrite := writer.WriteString("+" + value + "\r\n")
	return errWrite
}

func writeRedisError(writer *bufio.Writer, message string) error {
	if writer == nil {
		return net.ErrClosed
	}
	_, errWrite := writer.WriteString("-" + message + "\r\n")
	return errWrite
}

func writeRedisNilBulkString(writer *bufio.Writer) error {
	if writer == nil {
		return net.ErrClosed
	}
	_, errWrite := writer.WriteString("$-1\r\n")
	return errWrite
}

func writeRedisBulkString(writer *bufio.Writer, payload []byte) error {
	if writer == nil {
		return net.ErrClosed
	}
	if payload == nil {
		return writeRedisNilBulkString(writer)
	}
	if _, errWrite := writer.WriteString("$" + strconv.Itoa(len(payload)) + "\r\n"); errWrite != nil {
		return errWrite
	}
	if _, errWrite := writer.Write(payload); errWrite != nil {
		return errWrite
	}
	_, errWrite := writer.WriteString("\r\n")
	return errWrite
}

func writeRedisInteger(writer *bufio.Writer, value int64) error {
	if writer == nil {
		return net.ErrClosed
	}
	_, errWrite := writer.WriteString(":" + strconv.FormatInt(value, 10) + "\r\n")
	return errWrite
}
