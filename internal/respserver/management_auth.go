package respserver

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPIHome/internal/home"
	"golang.org/x/crypto/bcrypt"
)

type attemptInfo struct {
	count        int
	blockedUntil time.Time
	lastActivity time.Time // track last activity for cleanup
}

// attemptCleanupInterval controls how often stale IP entries are purged
const attemptCleanupInterval = 1 * time.Hour

// attemptMaxIdleTime controls how long an IP can be idle before cleanup
const attemptMaxIdleTime = 2 * time.Hour

type managementAuthenticator struct {
	runtime *home.Runtime

	attemptsMu     sync.Mutex
	failedAttempts map[string]*attemptInfo // keyed by client IP

	allowRemoteOverride bool
	envSecret           string
}

// newManagementAuthenticator creates a management authenticator.
func newManagementAuthenticator(runtime *home.Runtime) *managementAuthenticator {
	envSecret, _ := os.LookupEnv("MANAGEMENT_PASSWORD")
	envSecret = strings.TrimSpace(envSecret)

	a := &managementAuthenticator{
		runtime:             runtime,
		failedAttempts:      make(map[string]*attemptInfo),
		allowRemoteOverride: envSecret != "",
		envSecret:           envSecret,
	}
	a.startAttemptCleanup()
	return a
}

// startAttemptCleanup launches a background goroutine that periodically
// removes stale IP entries from failedAttempts to prevent memory leaks.
func (a *managementAuthenticator) startAttemptCleanup() {
	go func() {
		ticker := time.NewTicker(attemptCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			a.purgeStaleAttempts()
		}
	}()
}

// purgeStaleAttempts removes IP entries that have been idle beyond attemptMaxIdleTime
// and whose ban (if any) has expired.
func (a *managementAuthenticator) purgeStaleAttempts() {
	now := time.Now()
	a.attemptsMu.Lock()
	defer a.attemptsMu.Unlock()
	for ip, ai := range a.failedAttempts {
		// Skip if still banned
		if !ai.blockedUntil.IsZero() && now.Before(ai.blockedUntil) {
			continue
		}
		// Remove if idle too long
		if now.Sub(ai.lastActivity) > attemptMaxIdleTime {
			delete(a.failedAttempts, ip)
		}
	}
}

// AuthenticateManagementKey verifies the provided management key for the given client.
// It mirrors CLIProxyAPI's management auth behaviour so non-HTTP callers (RESP) can reuse the same logic.
func (a *managementAuthenticator) AuthenticateManagementKey(clientIP string, localClient bool, provided string) (bool, int, string) {
	const maxFailures = 5
	const banDuration = 30 * time.Minute

	if a == nil {
		return false, http.StatusForbidden, "remote management disabled"
	}

	var (
		allowRemote bool
		secretHash  string
		allowHosts  []string
	)
	if a.runtime != nil {
		if cfg := a.runtime.Config(); cfg != nil {
			allowRemote = cfg.RemoteManagement.AllowRemote
			secretHash = cfg.RemoteManagement.SecretKey
			allowHosts = cfg.AllowHost
		}
	}
	if a.allowRemoteOverride {
		allowRemote = true
	}
	envSecret := a.envSecret

	now := time.Now()
	a.attemptsMu.Lock()
	ai := a.failedAttempts[clientIP]
	if ai != nil && !ai.blockedUntil.IsZero() {
		if now.Before(ai.blockedUntil) {
			remaining := ai.blockedUntil.Sub(now).Round(time.Second)
			a.attemptsMu.Unlock()
			return false, http.StatusForbidden, fmt.Sprintf("IP banned due to too many failed attempts. Try again in %s", remaining)
		}
		// Ban expired, reset state
		ai.blockedUntil = time.Time{}
		ai.count = 0
	}
	a.attemptsMu.Unlock()

	if !localClient && !allowRemote {
		return false, http.StatusForbidden, "remote management disabled"
	}

	if !isClientHostAllowed(clientIP, allowHosts) {
		return false, http.StatusForbidden, "client host not allowed"
	}

	fail := func() {
		a.attemptsMu.Lock()
		aip := a.failedAttempts[clientIP]
		if aip == nil {
			aip = &attemptInfo{}
			a.failedAttempts[clientIP] = aip
		}
		aip.count++
		aip.lastActivity = time.Now()
		if aip.count >= maxFailures {
			aip.blockedUntil = time.Now().Add(banDuration)
			aip.count = 0
		}
		a.attemptsMu.Unlock()
	}

	reset := func() {
		a.attemptsMu.Lock()
		if ai := a.failedAttempts[clientIP]; ai != nil {
			ai.count = 0
			ai.blockedUntil = time.Time{}
		}
		a.attemptsMu.Unlock()
	}

	if secretHash == "" && envSecret == "" {
		return false, http.StatusForbidden, "remote management key not set"
	}

	if provided == "" {
		fail()
		return false, http.StatusUnauthorized, "missing management key"
	}

	if envSecret != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(envSecret)) == 1 {
		reset()
		return true, 0, ""
	}

	if secretHash == "" || bcrypt.CompareHashAndPassword([]byte(secretHash), []byte(provided)) != nil {
		fail()
		return false, http.StatusUnauthorized, "invalid management key"
	}

	reset()

	return true, 0, ""
}

// isClientHostAllowed reports whether client host allowed.
func isClientHostAllowed(clientIP string, allowHosts []string) bool {
	if len(allowHosts) == 0 {
		return true
	}
	clientIP = normalizeHostIP(clientIP)
	if clientIP == "" {
		return false
	}
	for _, host := range allowHosts {
		if clientIP == normalizeHostIP(host) {
			return true
		}
	}
	return false
}

// resolveRemoteIP resolves a remote ip.
func resolveRemoteIP(addr net.Addr) (ip string, localClient bool) {
	// Validate request inputs before mutating persisted state.
	if addr == nil {
		return "", false
	}

	var host string
	switch a := addr.(type) {
	case *net.TCPAddr:
		if a != nil && a.IP != nil {
			if ip4 := a.IP.To4(); ip4 != nil {
				host = ip4.String()
			} else {
				host = a.IP.String()
			}
		}
	default:
		host = addr.String()
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.TrimSpace(host)
		if raw, _, ok := strings.Cut(host, "%"); ok {
			host = raw
		}
		if parsed := net.ParseIP(host); parsed != nil {
			if ip4 := parsed.To4(); ip4 != nil {
				host = ip4.String()
			} else {
				host = parsed.String()
			}
		}
	}

	host = strings.TrimSpace(host)
	localClient = host == "127.0.0.1" || host == "::1"
	return host, localClient
}

// normalizeHostIP normalizes a host ip.
func normalizeHostIP(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if raw, _, ok := strings.Cut(host, "%"); ok {
		host = raw
	}
	if parsed := net.ParseIP(host); parsed != nil {
		if ip4 := parsed.To4(); ip4 != nil {
			return ip4.String()
		}
		return parsed.String()
	}
	return strings.TrimSpace(host)
}

// parseAuthPassword parses an auth password.
func parseAuthPassword(args []string) (string, bool) {
	switch len(args) {
	case 2:
		return args[1], true
	case 3:
		// Support AUTH <username> <password> by ignoring username for compatibility.
		return args[2], true
	default:
		return "", false
	}
}
