package node

var globalRegistry = NewRegistry()

func GlobalRegistry() *Registry {
	return globalRegistry
}
