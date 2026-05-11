package home

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
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
	return r.ReadConfigYAMLContext(context.Background())
}

func (r *Runtime) ReadConfigYAMLContext(ctx context.Context) ([]byte, error) {
	if r == nil {
		return nil, fmt.Errorf("home runtime: runtime is nil")
	}
	if r.clusterAdapter != nil && r.clusterAdapter.Enabled() {
		data, errRead := r.clusterAdapter.LoadConfigYAML(ctx)
		if errRead != nil {
			return nil, errRead
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("home runtime: config is empty")
		}
		filtered, errFilter := sanitizeConfigYAMLForDownstream(data)
		if errFilter != nil {
			return nil, errFilter
		}
		return filtered, nil
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
	filtered, errFilter := sanitizeConfigYAMLForDownstream(data)
	if errFilter != nil {
		return nil, errFilter
	}
	return filtered, nil
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

	filtered, errFilter := sanitizeConfigYAMLForDownstream(payload)
	if errFilter != nil || len(filtered) == 0 {
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
		if errSend := sub(filtered); errSend != nil {
			r.configSubsMu.Lock()
			delete(r.configSubs, id)
			r.configSubsMu.Unlock()
		}
	}
}

func sanitizeConfigYAMLForDownstream(payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("home runtime: config is empty")
	}

	var root yaml.Node
	if errUnmarshal := yaml.Unmarshal(payload, &root); errUnmarshal != nil {
		return nil, fmt.Errorf("home runtime: unmarshal config: %w", errUnmarshal)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 || root.Content[0] == nil {
		return nil, fmt.Errorf("home runtime: invalid config yaml document")
	}

	doc := root.Content[0]
	removeConfigKeysForDownstream(doc, []string{
		"remote-management",
		"api-keys",
		"auth-dir",
		"tls",
		"gemini-api-key",
		"codex-api-key",
		"claude-api-key",
		"openai-compatibility",
		"vertex-api-key",
		"oauth-model-alias",
		"oauth-excluded-models",
	})
	stripYAMLComments(&root)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if errEncode := enc.Encode(&root); errEncode != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("home runtime: marshal config: %w", errEncode)
	}
	if errClose := enc.Close(); errClose != nil {
		return nil, fmt.Errorf("home runtime: marshal config: %w", errClose)
	}
	out := bytes.TrimSpace(buf.Bytes())
	if len(out) == 0 {
		return nil, fmt.Errorf("home runtime: config is empty")
	}
	out = append(out, '\n')
	return out, nil
}

func removeConfigKeysForDownstream(node *yaml.Node, keys []string) {
	if node == nil || node.Kind != yaml.MappingNode || len(keys) == 0 {
		return
	}

	keySet := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		keySet[k] = struct{}{}
	}
	if len(keySet) == 0 {
		return
	}

	next := make([]*yaml.Node, 0, len(node.Content))
	for i := 0; i+1 < len(node.Content); i += 2 {
		k := node.Content[i]
		v := node.Content[i+1]
		if k == nil || strings.TrimSpace(k.Value) == "" {
			next = append(next, k, v)
			continue
		}
		if _, ok := keySet[k.Value]; ok {
			continue
		}
		next = append(next, k, v)
	}
	node.Content = next
}

func stripYAMLComments(node *yaml.Node) {
	if node == nil {
		return
	}
	node.HeadComment = ""
	node.LineComment = ""
	node.FootComment = ""

	for _, child := range node.Content {
		stripYAMLComments(child)
	}
	if node.Alias != nil {
		stripYAMLComments(node.Alias)
	}
}
