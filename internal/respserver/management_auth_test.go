package respserver

import (
	"net"
	"testing"
)

// TestNormalizeHostIP verifies test normalize host ip behavior.
func TestNormalizeHostIP(t *testing.T) {
	// Validate request inputs before mutating persisted state.
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "ipv4",
			in:   "192.168.1.10",
			want: "192.168.1.10",
		},
		{
			name: "ipv4 host port",
			in:   "192.168.1.10:8327",
			want: "192.168.1.10",
		},
		{
			name: "ipv6",
			in:   "2001:db8::1",
			want: "2001:db8::1",
		},
		{
			name: "ipv6 host port",
			in:   "[2001:db8::1]:8327",
			want: "2001:db8::1",
		},
		{
			name: "ipv6 zone",
			in:   "fe80::1%lo0",
			want: "fe80::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeHostIP(tt.in); got != tt.want {
				t.Fatalf("normalizeHostIP(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestIsClientHostAllowed verifies test is client host allowed behavior.
func TestIsClientHostAllowed(t *testing.T) {
	// Validate request inputs before mutating persisted state.
	tests := []struct {
		name       string
		clientIP   string
		allowHosts []string
		want       bool
	}{
		{
			name:       "empty allow list allows all clients",
			clientIP:   "203.0.113.10",
			allowHosts: nil,
			want:       true,
		},
		{
			name:       "listed ipv4 client is allowed",
			clientIP:   "203.0.113.10",
			allowHosts: []string{"203.0.113.10"},
			want:       true,
		},
		{
			name:       "unlisted ipv4 client is rejected",
			clientIP:   "203.0.113.11",
			allowHosts: []string{"203.0.113.10"},
			want:       false,
		},
		{
			name:       "ipv4 mapped client matches ipv4 allow host",
			clientIP:   "::ffff:203.0.113.10",
			allowHosts: []string{"203.0.113.10"},
			want:       true,
		},
		{
			name:       "blank client is rejected when allow list is configured",
			clientIP:   "",
			allowHosts: []string{"203.0.113.10"},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isClientHostAllowed(tt.clientIP, tt.allowHosts); got != tt.want {
				t.Fatalf("isClientHostAllowed(%q, %#v) = %t, want %t", tt.clientIP, tt.allowHosts, got, tt.want)
			}
		})
	}
}

// TestResolveRemoteIPNormalizesTCPAddr verifies test resolve remote ip normalizes tcp addr behavior.
func TestResolveRemoteIPNormalizesTCPAddr(t *testing.T) {
	ip, local := resolveRemoteIP(&net.TCPAddr{
		IP:   net.ParseIP("::ffff:127.0.0.1"),
		Port: 8327,
	})
	if ip != "127.0.0.1" {
		t.Fatalf("expected IPv4-mapped address to normalize to 127.0.0.1, got %q", ip)
	}
	if !local {
		t.Fatalf("expected IPv4 localhost to be local")
	}
}
