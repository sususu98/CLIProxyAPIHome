package respserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
	log "github.com/sirupsen/logrus"
)

type Server struct {
	addr     string
	runtime  *home.Runtime
	registry *dispatch.Registry
}

func New(addr string, runtime *home.Runtime) *Server {
	return &Server{
		addr:     strings.TrimSpace(addr),
		runtime:  runtime,
		registry: buildRegistry(),
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

		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	if s == nil || conn == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	defer func() {
		if errClose := conn.Close(); errClose != nil {
			log.Errorf("resp connection close error: %v", errClose)
		}
	}()

	flush := func() bool {
		if errFlush := writer.Flush(); errFlush != nil {
			log.Errorf("resp flush error: %v", errFlush)
			return false
		}
		return true
	}

	for {
		args, errRead := readRESPArray(reader)
		if errRead != nil {
			if !errors.Is(errRead, io.EOF) {
				_ = writeRedisError(writer, "ERR "+errRead.Error())
				_ = writer.Flush()
			}
			return
		}
		if len(args) == 0 {
			_ = writeRedisError(writer, "ERR empty command")
			if !flush() {
				return
			}
			continue
		}

		reply := dispatch.Err("registry not ready")
		if s.registry != nil {
			reply = s.registry.Execute(ctx, dispatch.Env{Runtime: s.runtime}, args)
		}
		if errWrite := writeDispatchReply(writer, reply); errWrite != nil {
			log.Errorf("resp write reply error: %v", errWrite)
			return
		}
		if !flush() {
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
