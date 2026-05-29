package logging

import (
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

func TestLogFormatterAddsCLIProxyAPIHomePrefix(t *testing.T) {
	entry := log.NewEntry(log.StandardLogger())
	entry.Time = time.Date(2026, 5, 29, 8, 0, 0, 0, time.Local)
	entry.Level = log.InfoLevel
	entry.Message = "home runtime ready"

	raw, errFormat := (&LogFormatter{}).Format(entry)
	if errFormat != nil {
		t.Fatalf("Format error: %v", errFormat)
	}
	got := string(raw)
	wantPrefix := FormatLogSourcePrefix("CLIProxyAPIHome") + " [2026-05-29 08:00:00] [--------] [info ] "
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("formatted log = %q, want CLIProxyAPIHome prefix", got)
	}
}

func TestFormatLogSourcePrefixAlignsHomeIPv4AndIPv6(t *testing.T) {
	home := FormatLogSourcePrefix("CLIProxyAPIHome")
	ipv4 := FormatLogSourcePrefix("255.255.255.255")
	ipv6 := FormatLogSourcePrefix("2001:db8:85a3::8a2e:370:7334")

	if len(home) != len(ipv4) {
		t.Fatalf("home prefix len = %d, ipv4 prefix len = %d", len(home), len(ipv4))
	}
	if len(home) != len(ipv6) {
		t.Fatalf("home prefix len = %d, ipv6 prefix len = %d", len(home), len(ipv6))
	}
	if home != "[CLIProxyAPIHome]" {
		t.Fatalf("home prefix = %q, want bracketed CLIProxyAPIHome source", home)
	}
	if ipv6 != "[::8a2e:370:7334]" {
		t.Fatalf("ipv6 prefix = %q, want rightmost IPv4-width address", ipv6)
	}
}
