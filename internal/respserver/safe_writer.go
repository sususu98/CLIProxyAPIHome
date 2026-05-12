package respserver

import (
	"bufio"
	"net"
	"sync"

	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
)

type safeWriter struct {
	mu sync.Mutex
	w  *bufio.Writer
}

// newSafeWriter creates a safe writer.
func newSafeWriter(w *bufio.Writer) *safeWriter {
	if w == nil {
		return &safeWriter{}
	}
	return &safeWriter{w: w}
}

// WriteDispatchReply writes a dispatch reply.
func (w *safeWriter) WriteDispatchReply(reply dispatch.Reply) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.w == nil {
		return net.ErrClosed
	}
	if errWrite := writeDispatchReply(w.w, reply); errWrite != nil {
		return errWrite
	}
	return w.w.Flush()
}

// WriteRedisSimpleString writes a redis simple string.
func (w *safeWriter) WriteRedisSimpleString(value string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.w == nil {
		return net.ErrClosed
	}
	if errWrite := writeRedisSimpleString(w.w, value); errWrite != nil {
		return errWrite
	}
	return w.w.Flush()
}

// WriteRedisError writes a redis error.
func (w *safeWriter) WriteRedisError(message string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.w == nil {
		return net.ErrClosed
	}
	if errWrite := writeRedisError(w.w, message); errWrite != nil {
		return errWrite
	}
	return w.w.Flush()
}

// WriteRedisBulkString writes a redis bulk string.
func (w *safeWriter) WriteRedisBulkString(payload []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.w == nil {
		return net.ErrClosed
	}
	if errWrite := writeRedisBulkString(w.w, payload); errWrite != nil {
		return errWrite
	}
	return w.w.Flush()
}
