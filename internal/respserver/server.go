package respserver

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPIHome/internal/cluster"
	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"github.com/router-for-me/CLIProxyAPIHome/internal/node"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
	log "github.com/sirupsen/logrus"
)

type Server struct {
	addr     string
	runtime  *home.Runtime
	registry *dispatch.Registry
	cluster  *cluster.RESPHandler
}

const (
	clusterSubscriptionUpdateInterval = 30 * time.Second
	subscriptionHeartbeatInterval     = time.Second
)

const (
	respAuthSourceNone = "none"
	respAuthSourceMTLS = "mtls"
)

// New creates a new.
func New(addr string, runtime *home.Runtime) *Server {
	return &Server{
		addr:     strings.TrimSpace(addr),
		runtime:  runtime,
		registry: buildRegistry(),
	}
}

// SetClusterHandler sets a cluster handler.
func (s *Server) SetClusterHandler(handler *cluster.RESPHandler) {
	if s == nil {
		return
	}
	s.cluster = handler
}

func (s *Server) syncClusterClientCount(ctx context.Context) {
	if s == nil || s.cluster == nil {
		return
	}
	if errSync := s.cluster.UpdateClientCount(ctx, node.GlobalRegistry().TotalCount()); errSync != nil {
		log.Warnf("failed to sync cluster client count: %v", errSync)
	}
}

func (s *Server) startSubscriptionUpdates(ctx context.Context, writer *safeWriter) context.CancelFunc {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithCancel(ctx)
	if s == nil || writer == nil {
		return cancel
	}

	go func() {
		heartbeatTicker := time.NewTicker(subscriptionHeartbeatInterval)
		defer heartbeatTicker.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case <-heartbeatTicker.C:
				if errSend := writer.WriteDispatchReply(subscriptionPong([]byte{})); errSend != nil {
					log.Warnf("failed to publish subscription heartbeat: %v", errSend)
					cancel()
					return
				}
			}
		}
	}()

	if s.cluster != nil {
		go func() {
			ticker := time.NewTicker(clusterSubscriptionUpdateInterval)
			defer ticker.Stop()

			for {
				select {
				case <-runCtx.Done():
					return
				case <-ticker.C:
					if errSend := s.writeClusterSubscriptionUpdate(runCtx, writer); errSend != nil {
						log.Warnf("failed to publish cluster update to subscriber: %v", errSend)
						cancel()
						return
					}
				}
			}
		}()
	}

	return cancel
}

func (s *Server) writeClusterSubscriptionUpdate(ctx context.Context, writer *safeWriter) error {
	if s == nil || s.cluster == nil || writer == nil {
		return nil
	}
	s.syncClusterClientCount(ctx)
	payload, errPayload := s.cluster.Handle(ctx, []string{clusterCommand, "NODES"}, "")
	if errPayload != nil {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		log.Warnf("failed to build cluster update for subscriber: %v", errPayload)
		return nil
	}
	if len(payload) == 0 {
		return nil
	}
	return writer.WriteDispatchReply(subscriptionMessage(clusterSubscriptionChannel, payload))
}

func (s *Server) writeNoAuth(writer *safeWriter) {
	if writer == nil {
		return
	}
	_ = writer.WriteRedisError("NOAUTH Authentication required.")
}

func isRESPAuthenticated(source string) bool {
	source = strings.TrimSpace(source)
	return source == respAuthSourceMTLS
}

func isMTLSAuthenticated(conn net.Conn) bool {
	stater, ok := conn.(interface{ ConnectionState() tls.ConnectionState })
	if !ok {
		return false
	}
	state := stater.ConnectionState()
	return len(state.PeerCertificates) > 0 && len(state.VerifiedChains) > 0
}

func subscriptionMessage(channel string, payload []byte) dispatch.Reply {
	return dispatch.Array(
		dispatch.BulkString([]byte("message")),
		dispatch.BulkString([]byte(channel)),
		dispatch.BulkString(payload),
	)
}

func subscriptionPong(payload []byte) dispatch.Reply {
	if payload == nil {
		payload = []byte{}
	}
	return dispatch.Array(
		dispatch.BulkString([]byte("pong")),
		dispatch.BulkString(payload),
	)
}

// HandleConn handles handle conn.
func (s *Server) HandleConn(ctx context.Context, conn net.Conn) {
	// Validate request inputs before mutating persisted state.
	if s == nil || conn == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	clientIP, _ := resolveRemoteIP(conn.RemoteAddr())
	if s.runtime != nil {
		if cfg := s.runtime.Config(); cfg != nil && !isClientHostAllowed(clientIP, cfg.AllowHost) {
			log.Warnf("resp connection rejected from disallowed host %s", clientIP)
			if errClose := conn.Close(); errClose != nil {
				log.Errorf("resp disallowed connection close error: %v", errClose)
			}
			return
		}
	}
	authSource := respAuthSourceNone
	if isMTLSAuthenticated(conn) {
		authSource = respAuthSourceMTLS
	}
	reader := bufio.NewReader(conn)
	writer := newSafeWriter(bufio.NewWriter(conn))
	connectedAt := time.Now()
	addedNode := false
	var unsubscribeConfig func()
	var cancelSubscriptionUpdates context.CancelFunc
	defer func() {
		if cancelSubscriptionUpdates != nil {
			cancelSubscriptionUpdates()
			cancelSubscriptionUpdates = nil
		}
		if unsubscribeConfig != nil {
			unsubscribeConfig()
			unsubscribeConfig = nil
		}
		if addedNode {
			node.GlobalRegistry().Remove(clientIP)
			s.syncClusterClientCount(ctx)
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

		if cmd == "CERTIFICATE" {
			if authSource != respAuthSourceNone {
				_ = writer.WriteRedisError("ERR certificate request is only allowed before authentication")
				continue
			}
			if s.cluster == nil {
				_ = writer.WriteRedisError("ERR cluster disabled")
				continue
			}
			if len(args) != 5 || !strings.EqualFold(strings.TrimSpace(args[1]), "REQUEST") {
				_ = writer.WriteRedisError("ERR wrong number of arguments for 'certificate request' command")
				continue
			}
			payload, errCertificate := s.cluster.RequestClientCertificate(ctx, args[2], args[3], []byte(args[4]))
			if errCertificate != nil {
				_ = writer.WriteRedisError("ERR " + errCertificate.Error())
				continue
			}
			_ = writer.WriteRedisBulkString(payload)
			continue
		}

		if cmd == clusterCommand {
			if !isRESPAuthenticated(authSource) {
				s.writeNoAuth(writer)
				continue
			}
			if s.cluster == nil {
				_ = writer.WriteRedisError("ERR cluster disabled")
				continue
			}
			payload, errCluster := s.cluster.Handle(ctx, args, clientIP)
			if errCluster != nil {
				_ = writer.WriteRedisError("ERR " + errCluster.Error())
				continue
			}
			_ = writer.WriteRedisBulkString(payload)
			continue
		}

		if cmd != "AUTH" && !isRESPAuthenticated(authSource) {
			s.writeNoAuth(writer)
			continue
		}

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

		if cmd == "AUTH" {
			_ = writer.WriteRedisError("ERR RESP AUTH disabled; use mTLS")
			continue
		}

		reply := dispatch.Err("registry not ready")
		if s.registry != nil {
			reply = s.registry.Execute(ctx, dispatch.Env{
				Runtime:  s.runtime,
				ClientIP: clientIP,
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
							s.syncClusterClientCount(ctx)
							addedNode = true
						}
						unsubscribeConfig = s.runtime.SubscribeConfigYAML(func(payload []byte) error {
							return writer.WriteDispatchReply(subscriptionMessage(configSubscriptionChannel, payload))
						})
						cancelSubscriptionUpdates = s.startSubscriptionUpdates(ctx, writer)
						return 1, nil
					},
					UnsubscribeConfigYAML: func() (int64, error) {
						if unsubscribeConfig == nil {
							return 0, nil
						}
						if cancelSubscriptionUpdates != nil {
							cancelSubscriptionUpdates()
							cancelSubscriptionUpdates = nil
						}
						unsubscribeConfig()
						unsubscribeConfig = nil
						if addedNode {
							node.GlobalRegistry().Remove(clientIP)
							s.syncClusterClientCount(ctx)
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

// readRESPArray reads a resp array.
func readRESPArray(reader *bufio.Reader) ([]string, error) {
	// Decode the wire frame before dispatching command handling.
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

// readRESPString reads a resp string.
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

// readRESPBulkString reads a resp bulk string.
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

// readRESPLine reads a resp line.
func readRESPLine(reader *bufio.Reader) (string, error) {
	line, errRead := reader.ReadString('\n')
	if errRead != nil {
		return "", errRead
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, nil
}

// writeRedisSimpleString writes a redis simple string.
func writeRedisSimpleString(writer *bufio.Writer, value string) error {
	if writer == nil {
		return net.ErrClosed
	}
	_, errWrite := writer.WriteString("+" + value + "\r\n")
	return errWrite
}

// writeRedisError writes a redis error.
func writeRedisError(writer *bufio.Writer, message string) error {
	if writer == nil {
		return net.ErrClosed
	}
	_, errWrite := writer.WriteString("-" + message + "\r\n")
	return errWrite
}

// writeRedisNilBulkString writes a redis nil bulk string.
func writeRedisNilBulkString(writer *bufio.Writer) error {
	if writer == nil {
		return net.ErrClosed
	}
	_, errWrite := writer.WriteString("$-1\r\n")
	return errWrite
}

// writeRedisBulkString writes a redis bulk string.
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

// writeRedisInteger writes a redis integer.
func writeRedisInteger(writer *bufio.Writer, value int64) error {
	if writer == nil {
		return net.ErrClosed
	}
	_, errWrite := writer.WriteString(":" + strconv.FormatInt(value, 10) + "\r\n")
	return errWrite
}
