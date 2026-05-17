package proxyproto

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
)

const (
	v1Prefix    = "PROXY "
	maxV1Length = 108
)

type listener struct {
	net.Listener
}

type conn struct {
	net.Conn
	reader      *bufio.Reader
	remoteAddr  net.Addr
	parsed      bool
	parseErr    error
	parseHeader sync.Mutex
}

// NewListener wraps a listener and consumes an optional PROXY protocol v1 header.
func NewListener(base net.Listener) net.Listener {
	if base == nil {
		return nil
	}
	return &listener{Listener: base}
}

func (l *listener) Accept() (net.Conn, error) {
	if l == nil || l.Listener == nil {
		return nil, net.ErrClosed
	}
	c, errAccept := l.Listener.Accept()
	if errAccept != nil {
		return nil, errAccept
	}
	return NewConn(c)
}

// NewConn wraps a connection and lazily consumes an optional PROXY protocol v1 header.
func NewConn(c net.Conn) (net.Conn, error) {
	if c == nil {
		return nil, net.ErrClosed
	}
	if _, ok := c.(*conn); ok {
		return c, nil
	}
	return &conn{Conn: c, remoteAddr: c.RemoteAddr()}, nil
}

// NewConnAndRead wraps a connection and immediately consumes an optional PROXY protocol v1 header.
func NewConnAndRead(c net.Conn) (net.Conn, error) {
	wrapped, errWrap := NewConn(c)
	if errWrap != nil {
		return nil, errWrap
	}
	proxyConn, ok := wrapped.(*conn)
	if !ok {
		return wrapped, nil
	}
	if errRead := proxyConn.ensureProxyHeader(); errRead != nil {
		return proxyConn, errRead
	}
	return proxyConn, nil
}

func (c *conn) Read(p []byte) (int, error) {
	if c == nil {
		return 0, net.ErrClosed
	}
	if errRead := c.ensureProxyHeader(); errRead != nil {
		return 0, errRead
	}
	return c.reader.Read(p)
}

func (c *conn) RemoteAddr() net.Addr {
	if c == nil || c.Conn == nil {
		return nil
	}
	c.parseHeader.Lock()
	remoteAddr := c.remoteAddr
	c.parseHeader.Unlock()
	if remoteAddr != nil {
		return remoteAddr
	}
	return c.Conn.RemoteAddr()
}

func (c *conn) ensureProxyHeader() error {
	if c == nil || c.Conn == nil {
		return net.ErrClosed
	}
	c.parseHeader.Lock()
	defer c.parseHeader.Unlock()
	if c.parsed {
		return c.parseErr
	}
	if c.reader == nil {
		c.reader = bufio.NewReader(c.Conn)
	}
	remoteAddr, errRead := readOptionalV1(c.reader, c.Conn.RemoteAddr())
	if errRead == nil && remoteAddr != nil {
		c.remoteAddr = remoteAddr
	}
	c.parsed = true
	c.parseErr = errRead
	return errRead
}

func readOptionalV1(reader *bufio.Reader, fallback net.Addr) (net.Addr, error) {
	if reader == nil {
		return fallback, nil
	}
	first, errPeek := reader.Peek(1)
	if errPeek != nil {
		return fallback, errPeek
	}
	if first[0] != 'P' {
		return fallback, nil
	}
	prefix, errPrefix := reader.Peek(len(v1Prefix))
	if errPrefix != nil {
		return fallback, errPrefix
	}
	if string(prefix) != v1Prefix {
		return fallback, nil
	}
	line, errLine := readLineLimited(reader, maxV1Length)
	if errLine != nil {
		return nil, errLine
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")

	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "PROXY" {
		return nil, fmt.Errorf("invalid proxy protocol header")
	}
	if fields[1] == "UNKNOWN" {
		return fallback, nil
	}
	if fields[1] != "TCP4" && fields[1] != "TCP6" {
		return nil, fmt.Errorf("unsupported proxy protocol family %q", fields[1])
	}
	if len(fields) != 6 {
		return nil, fmt.Errorf("invalid proxy protocol field count")
	}

	ip := net.ParseIP(fields[2])
	if ip == nil {
		return nil, fmt.Errorf("invalid proxy protocol source address")
	}
	if fields[1] == "TCP4" {
		ip = ip.To4()
		if ip == nil {
			return nil, fmt.Errorf("invalid proxy protocol tcp4 source address")
		}
	}
	port, errPort := strconv.Atoi(fields[4])
	if errPort != nil || port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid proxy protocol source port")
	}

	return &net.TCPAddr{IP: ip, Port: port}, nil
}

func readLineLimited(reader *bufio.Reader, limit int) (string, error) {
	var b strings.Builder
	for b.Len() <= limit {
		c, errRead := reader.ReadByte()
		if errRead != nil {
			return "", errRead
		}
		if errWrite := b.WriteByte(c); errWrite != nil {
			return "", errWrite
		}
		if c == '\n' {
			return b.String(), nil
		}
	}
	return "", fmt.Errorf("proxy protocol header too long")
}
