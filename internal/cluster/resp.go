package cluster

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const clusterRESPTimeout = 30 * time.Second

type RESPHandler struct {
	coordinator *Coordinator
	refresh     *RefreshController
	repo        *Repository
}

// NewRESPHandler creates a new resp handler.
func NewRESPHandler(coordinator *Coordinator, refresh *RefreshController, repo *Repository) *RESPHandler {
	return &RESPHandler{
		coordinator: coordinator,
		refresh:     refresh,
		repo:        repo,
	}
}

// Handle handles handle.
func (h *RESPHandler) Handle(ctx context.Context, args []string, remoteIP string) ([]byte, error) {
	// Validate request inputs before mutating persisted state.
	if h == nil {
		return nil, fmt.Errorf("cluster resp: handler is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("cluster resp: wrong number of arguments")
	}
	if !strings.EqualFold(strings.TrimSpace(args[0]), "CLUSTER") {
		return nil, fmt.Errorf("cluster resp: unsupported command")
	}

	switch strings.ToUpper(strings.TrimSpace(args[1])) {
	case "PING":
		if len(args) != 3 {
			return nil, fmt.Errorf("cluster resp: wrong number of arguments for 'cluster ping'")
		}
		if _, errNode := h.authorizeNode(ctx, remoteIP, args[2]); errNode != nil {
			return nil, errNode
		}
		return json.Marshal(map[string]any{"ok": true})
	case "NODE":
		if len(args) != 3 {
			return nil, fmt.Errorf("cluster resp: wrong number of arguments for 'cluster node'")
		}
		if _, errNode := h.authorizeNode(ctx, remoteIP, args[2]); errNode != nil {
			return nil, errNode
		}
		return h.nodePayload(ctx)
	case "REFRESH":
		if len(args) != 4 {
			return nil, fmt.Errorf("cluster resp: wrong number of arguments for 'cluster refresh'")
		}
		if _, errNode := h.authorizeNode(ctx, remoteIP, args[3]); errNode != nil {
			return nil, errNode
		}
		if h.refresh == nil {
			return nil, fmt.Errorf("cluster resp: refresh controller is nil")
		}
		return h.refresh.RefreshNow(ctx, args[2])
	default:
		return nil, fmt.Errorf("cluster resp: unsupported subcommand")
	}
}

// authorizeNode authorizes a node.
func (h *RESPHandler) authorizeNode(ctx context.Context, remoteIP string, secret string) (*ClusterNodeRecord, error) {
	// Normalize auth state before updating runtime indexes.
	if h == nil || h.repo == nil {
		return nil, fmt.Errorf("cluster resp: repository is nil")
	}
	remoteIP = strings.TrimSpace(remoteIP)
	if remoteIP == "" {
		return nil, fmt.Errorf("cluster resp: remote ip is empty")
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, fmt.Errorf("cluster resp: node secret is required")
	}
	timeout := defaultHeartbeatTimeout
	if h.coordinator != nil && h.coordinator.heartbeatTimeout > 0 {
		timeout = h.coordinator.heartbeatTimeout
	}
	cutoff := time.Now().UTC().Add(-timeout)
	node, errNode := h.repo.LiveClusterNodeByIPAndSecret(ctx, remoteIP, secret, cutoff)
	if errNode != nil {
		return nil, errNode
	}
	if node == nil || node.Port <= 0 {
		return nil, fmt.Errorf("cluster resp: remote node is not authorized")
	}
	return node, nil
}

// nodePayload handles a node payload.
func (h *RESPHandler) nodePayload(ctx context.Context) ([]byte, error) {
	// Validate input data before converting it into runtime state.
	var master *ClusterNodeRecord
	if h.coordinator != nil {
		currentMaster, errMaster := h.coordinator.CurrentMaster(ctx)
		if errMaster != nil {
			return nil, errMaster
		}
		master = currentMaster
	}
	payload := map[string]any{
		"ok":        true,
		"is_master": h.coordinator != nil && h.coordinator.IsMaster(),
	}
	if h.coordinator != nil {
		payload["node"] = map[string]any{
			"ip":   h.coordinator.node.IP,
			"port": h.coordinator.node.Port,
		}
	}
	if master != nil {
		payload["master"] = map[string]any{
			"ip":   master.IP,
			"port": master.Port,
		}
	}
	return json.Marshal(payload)
}

// ForwardRefreshToMaster converts forward refresh to master.
func ForwardRefreshToMaster(ctx context.Context, master *ClusterNodeRecord, authUUID string, secret string) ([]byte, error) {
	// Resolve credential context before calling upstream OAuth services.
	if ctx == nil {
		ctx = context.Background()
	}
	if master == nil {
		return nil, fmt.Errorf("cluster resp: master is nil")
	}
	host := strings.TrimSpace(master.IP)
	if host == "" || master.Port <= 0 {
		return nil, fmt.Errorf("cluster resp: master address is invalid")
	}
	authUUID = strings.TrimSpace(authUUID)
	if authUUID == "" {
		return nil, fmt.Errorf("cluster resp: auth uuid is required")
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, fmt.Errorf("cluster resp: node secret is required")
	}

	dialer := &net.Dialer{Timeout: clusterRESPTimeout}
	conn, errDial := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(master.Port)))
	if errDial != nil {
		return nil, errDial
	}
	defer func() {
		_ = conn.Close()
	}()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(clusterRESPTimeout))
	}

	if _, errWrite := conn.Write(encodeRESPArray("CLUSTER", "REFRESH", authUUID, secret)); errWrite != nil {
		return nil, errWrite
	}
	return readRESPBulk(bufio.NewReader(conn))
}

// encodeRESPArray encodes a resp array.
func encodeRESPArray(args ...string) []byte {
	var buf bytes.Buffer
	buf.WriteString("*")
	buf.WriteString(strconv.Itoa(len(args)))
	buf.WriteString("\r\n")
	for _, arg := range args {
		buf.WriteString("$")
		buf.WriteString(strconv.Itoa(len(arg)))
		buf.WriteString("\r\n")
		buf.WriteString(arg)
		buf.WriteString("\r\n")
	}
	return buf.Bytes()
}

// readRESPBulk reads a resp bulk.
func readRESPBulk(reader *bufio.Reader) ([]byte, error) {
	// Validate input data before converting it into runtime state.
	prefix, errRead := reader.ReadByte()
	if errRead != nil {
		return nil, errRead
	}
	switch prefix {
	case '$':
		line, errLine := reader.ReadString('\n')
		if errLine != nil {
			return nil, errLine
		}
		size, errSize := strconv.Atoi(strings.TrimSpace(line))
		if errSize != nil {
			return nil, errSize
		}
		if size < 0 {
			return nil, fmt.Errorf("cluster resp: nil bulk response")
		}
		payload := make([]byte, size+2)
		if _, errFull := io.ReadFull(reader, payload); errFull != nil {
			return nil, errFull
		}
		return payload[:size], nil
	case '+':
		line, errLine := reader.ReadString('\n')
		if errLine != nil {
			return nil, errLine
		}
		return []byte(strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")), nil
	case '-':
		line, errLine := reader.ReadString('\n')
		if errLine != nil {
			return nil, errLine
		}
		return nil, fmt.Errorf("%s", strings.TrimSpace(line))
	default:
		return nil, fmt.Errorf("cluster resp: unsupported response prefix %q", prefix)
	}
}
