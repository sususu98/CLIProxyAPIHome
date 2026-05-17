package proxyproto

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

func TestReadOptionalV1UsesSourceAddress(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader("PROXY TCP4 203.0.113.10 198.51.100.2 56324 443\r\n*1\r\n$4\r\nPING\r\n"))
	fallback := &net.TCPAddr{IP: net.ParseIP("172.21.0.1"), Port: 50000}

	addr, errRead := readOptionalV1(reader, fallback)
	if errRead != nil {
		t.Fatalf("readOptionalV1 returned error: %v", errRead)
	}
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("addr type = %T, want *net.TCPAddr", addr)
	}
	if got := tcpAddr.IP.String(); got != "203.0.113.10" {
		t.Fatalf("source IP = %q, want 203.0.113.10", got)
	}
	if got := tcpAddr.Port; got != 56324 {
		t.Fatalf("source port = %d, want 56324", got)
	}
	next, errPeek := reader.Peek(1)
	if errPeek != nil {
		t.Fatalf("Peek returned error: %v", errPeek)
	}
	if next[0] != '*' {
		t.Fatalf("next byte = %q, want '*'", next[0])
	}
}

func TestReadOptionalV1LeavesDirectConnectionUntouched(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader("*1\r\n$4\r\nPING\r\n"))
	fallback := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 50000}

	addr, errRead := readOptionalV1(reader, fallback)
	if errRead != nil {
		t.Fatalf("readOptionalV1 returned error: %v", errRead)
	}
	if addr != fallback {
		t.Fatalf("addr = %v, want fallback", addr)
	}
	next, errPeek := reader.Peek(1)
	if errPeek != nil {
		t.Fatalf("Peek returned error: %v", errPeek)
	}
	if next[0] != '*' {
		t.Fatalf("next byte = %q, want '*'", next[0])
	}
}

func TestReadOptionalV1LeavesHTTPPostUntouched(t *testing.T) {
	t.Parallel()

	reader := bufio.NewReader(strings.NewReader("POST / HTTP/1.1\r\n\r\n"))
	fallback := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 50000}

	addr, errRead := readOptionalV1(reader, fallback)
	if errRead != nil {
		t.Fatalf("readOptionalV1 returned error: %v", errRead)
	}
	if addr != fallback {
		t.Fatalf("addr = %v, want fallback", addr)
	}
	next, errPeek := reader.Peek(4)
	if errPeek != nil {
		t.Fatalf("Peek returned error: %v", errPeek)
	}
	if string(next) != "POST" {
		t.Fatalf("next bytes = %q, want POST", string(next))
	}
}

func TestListenerAcceptDoesNotWaitForFirstByte(t *testing.T) {
	t.Parallel()

	base, errListen := net.Listen("tcp", "127.0.0.1:0")
	if errListen != nil {
		t.Fatalf("Listen returned error: %v", errListen)
	}
	t.Cleanup(func() {
		_ = base.Close()
	})

	wrapped := NewListener(base)
	acceptedCh := make(chan net.Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		accepted, errAccept := wrapped.Accept()
		if errAccept != nil {
			errCh <- errAccept
			return
		}
		acceptedCh <- accepted
	}()

	client, errDial := net.Dial("tcp", base.Addr().String())
	if errDial != nil {
		t.Fatalf("Dial returned error: %v", errDial)
	}
	defer func() {
		_ = client.Close()
	}()

	select {
	case accepted := <-acceptedCh:
		_ = accepted.Close()
	case errAccept := <-errCh:
		t.Fatalf("Accept returned error: %v", errAccept)
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("Accept waited for client data")
	}
}
