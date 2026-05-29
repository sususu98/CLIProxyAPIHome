package push

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	homelogging "github.com/router-for-me/CLIProxyAPIHome/internal/logging"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
)

var appLogMu sync.Mutex

// handleAppLog handles a CPA application log line.
func handleAppLog(ctx context.Context, env dispatch.Env, args []string) dispatch.Reply {
	_ = ctx

	if len(args) != 3 {
		return dispatch.Err("wrong number of arguments for 'rpush' command")
	}

	if !strings.EqualFold(strings.TrimSpace(args[1]), "app-log") {
		return dispatch.Err("unsupported key")
	}

	payload := strings.TrimSpace(args[2])
	if payload == "" || !gjson.Valid(payload) {
		return dispatch.Err("invalid app-log json")
	}

	line := gjson.Get(payload, "line").String()
	if strings.TrimSpace(line) == "" {
		return dispatch.Err("missing line")
	}

	clientIP := strings.TrimSpace(env.ClientIP)
	if clientIP == "" {
		clientIP = "unknown"
	}

	if homeLoggingToFile(env) {
		if errAppend := appendAppLogFile(clientIP, line); errAppend != nil {
			log.Errorf("app log write error: %v", errAppend)
			return dispatch.Err("app log write failed")
		}
		return dispatch.Integer(1)
	}

	writeAppLogConsole(clientIP, line)
	return dispatch.Integer(1)
}

func homeLoggingToFile(env dispatch.Env) bool {
	if env.Runtime == nil {
		return false
	}
	cfg := env.Runtime.Config()
	return cfg != nil && cfg.LoggingToFile
}

func appendAppLogFile(clientIP string, line string) error {
	clientIP = strings.TrimSpace(clientIP)
	if clientIP == "" {
		clientIP = "unknown"
	}
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return nil
	}

	appLogMu.Lock()
	defer appLogMu.Unlock()

	logDir := "logs"
	if errMk := os.MkdirAll(logDir, 0o755); errMk != nil {
		return fmt.Errorf("ensure logs dir: %w", errMk)
	}

	filename := sanitizeForFilename(clientIP) + "-main.log"
	filePath := filepath.Join(logDir, filename)
	f, errOpen := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if errOpen != nil {
		return fmt.Errorf("open app log: %w", errOpen)
	}
	defer func() {
		if errClose := f.Close(); errClose != nil {
			log.Errorf("app log close error: %v", errClose)
		}
	}()

	if _, errWrite := f.WriteString(line + "\n"); errWrite != nil {
		return fmt.Errorf("write app log: %w", errWrite)
	}
	return nil
}

func writeAppLogConsole(clientIP string, line string) {
	clientIP = strings.TrimSpace(clientIP)
	if clientIP == "" {
		clientIP = "unknown"
	}
	line = strings.TrimRight(line, "\r\n")
	if strings.TrimSpace(line) == "" {
		return
	}

	prefix := homelogging.FormatLogSourcePrefix(clientIP) + " "
	for _, part := range strings.Split(line, "\n") {
		part = strings.TrimRight(part, "\r")
		if strings.TrimSpace(part) == "" {
			continue
		}
		_, _ = fmt.Fprintln(os.Stdout, prefix+part)
	}
}
