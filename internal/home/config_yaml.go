package home

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

func (r *Runtime) ConfigPath() string {
	if r == nil {
		return ""
	}
	r.cfgMu.RLock()
	defer r.cfgMu.RUnlock()
	return strings.TrimSpace(r.configPath)
}

func (r *Runtime) ReadConfigYAML() ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("home runtime: runtime is nil")
	}
	path := r.ConfigPath()
	if path == "" {
		return nil, fmt.Errorf("home runtime: config path is empty")
	}
	data, errRead := os.ReadFile(path)
	if errRead != nil {
		return nil, errRead
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("home runtime: config is empty")
	}
	return data, nil
}

func (r *Runtime) SubscribeConfigYAML(subscriber func(payload []byte) error) (unsubscribe func()) {
	if r == nil || subscriber == nil {
		return func() {}
	}

	r.configSubsMu.Lock()
	if r.configSubs == nil {
		r.configSubs = make(map[uint64]func(payload []byte) error)
	}
	r.nextConfigSubID++
	id := r.nextConfigSubID
	r.configSubs[id] = subscriber
	r.configSubsMu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			r.configSubsMu.Lock()
			delete(r.configSubs, id)
			r.configSubsMu.Unlock()
		})
	}
}

func (r *Runtime) PublishConfigYAML(payload []byte) {
	if r == nil || len(payload) == 0 {
		return
	}

	r.configSubsMu.Lock()
	snapshot := make(map[uint64]func(payload []byte) error, len(r.configSubs))
	for id, sub := range r.configSubs {
		snapshot[id] = sub
	}
	r.configSubsMu.Unlock()

	for id, sub := range snapshot {
		if sub == nil {
			continue
		}
		if errSend := sub(payload); errSend != nil {
			r.configSubsMu.Lock()
			delete(r.configSubs, id)
			r.configSubsMu.Unlock()
		}
	}
}
