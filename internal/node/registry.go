package node

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type Node struct {
	IP        string    `json:"ip"`
	Connected time.Time `json:"connected_time"`
}

type Registry struct {
	mu    sync.RWMutex
	nodes map[string]nodeEntry
}

type nodeEntry struct {
	connectedAt time.Time
	count       int
}

func NewRegistry() *Registry {
	return &Registry{
		nodes: make(map[string]nodeEntry),
	}
}

func (r *Registry) Add(ip string, connectedAt time.Time) {
	if r == nil {
		return
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	if connectedAt.IsZero() {
		connectedAt = time.Now()
	}
	r.mu.Lock()
	if r.nodes == nil {
		r.nodes = make(map[string]nodeEntry)
	}
	entry := r.nodes[ip]
	if entry.count <= 0 {
		entry.connectedAt = connectedAt
		entry.count = 1
		r.nodes[ip] = entry
		r.mu.Unlock()
		return
	}
	entry.count++
	r.nodes[ip] = entry
	r.mu.Unlock()
}

func (r *Registry) Remove(ip string) {
	if r == nil {
		return
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return
	}
	r.mu.Lock()
	entry, ok := r.nodes[ip]
	if !ok {
		r.mu.Unlock()
		return
	}
	entry.count--
	if entry.count <= 0 {
		delete(r.nodes, ip)
		r.mu.Unlock()
		return
	}
	r.nodes[ip] = entry
	r.mu.Unlock()
}

func (r *Registry) List() []Node {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	snapshot := make([]Node, 0, len(r.nodes))
	for ip, entry := range r.nodes {
		snapshot = append(snapshot, Node{
			IP:        ip,
			Connected: entry.connectedAt,
		})
	}
	r.mu.RUnlock()

	sort.Slice(snapshot, func(i, j int) bool {
		if snapshot[i].Connected.Equal(snapshot[j].Connected) {
			return snapshot[i].IP < snapshot[j].IP
		}
		return snapshot[i].Connected.Before(snapshot[j].Connected)
	})

	return snapshot
}
