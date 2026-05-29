package push

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPIHome/internal/config"
	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	homelogging "github.com/router-for-me/CLIProxyAPIHome/internal/logging"
	"github.com/router-for-me/CLIProxyAPIHome/internal/respserver/dispatch"
)

func TestHandleAppLogWritesCPAFileWhenHomeLoggingToFile(t *testing.T) {
	t.Chdir(t.TempDir())

	rt, errRuntime := home.NewRuntime(&config.Config{LoggingToFile: true})
	if errRuntime != nil {
		t.Fatalf("NewRuntime: %v", errRuntime)
	}

	line := "[2026-05-29 08:00:00] [--------] [debug] debug details"
	payload := `{"line":"` + line + `","level":"debug"}`
	reply := handleAppLog(context.Background(), dispatch.Env{ClientIP: "10.0.0.5", Runtime: rt}, []string{"RPUSH", "app-log", payload})
	if reply.Kind != dispatch.ReplyKindInteger || reply.Integer != 1 {
		t.Fatalf("reply = %+v, want integer 1", reply)
	}

	raw, errRead := os.ReadFile(filepath.Join("logs", "10.0.0.5-main.log"))
	if errRead != nil {
		t.Fatalf("read app log: %v", errRead)
	}
	if strings.TrimSpace(string(raw)) != line {
		t.Fatalf("app log content = %q, want %q", strings.TrimSpace(string(raw)), line)
	}
}

func TestHandleAppLogPrintsCPAConsolePrefixWhenHomeLoggingToConsole(t *testing.T) {
	t.Chdir(t.TempDir())

	oldStdout := os.Stdout
	reader, writer, errPipe := os.Pipe()
	if errPipe != nil {
		t.Fatalf("pipe stdout: %v", errPipe)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = oldStdout
		_ = reader.Close()
	}()

	line := "[2026-05-29 08:00:00] [--------] [debug] debug details"
	payload := `{"line":"` + line + `","level":"debug"}`
	reply := handleAppLog(context.Background(), dispatch.Env{ClientIP: "10.0.0.8"}, []string{"RPUSH", "app-log", payload})
	if reply.Kind != dispatch.ReplyKindInteger || reply.Integer != 1 {
		t.Fatalf("reply = %+v, want integer 1", reply)
	}
	if errClose := writer.Close(); errClose != nil {
		t.Fatalf("close stdout writer: %v", errClose)
	}
	os.Stdout = oldStdout

	raw, errRead := io.ReadAll(reader)
	if errRead != nil {
		t.Fatalf("read stdout: %v", errRead)
	}
	got := strings.TrimSpace(string(raw))
	want := homelogging.FormatLogSourcePrefix("10.0.0.8") + " " + line
	if got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}
