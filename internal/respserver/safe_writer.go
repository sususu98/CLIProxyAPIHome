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

func newSafeWriter(w *bufio.Writer) *safeWriter {
	if w == nil {
		return &safeWriter{}
	}
	return &safeWriter{w: w}
}

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
