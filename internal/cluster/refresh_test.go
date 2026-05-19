package cluster

import (
	"context"
	"crypto/tls"
	"strings"
	"testing"
)

func TestNewRefreshControllerStoresForwardTLSConfig(t *testing.T) {
	t.Parallel()

	tlsConfig := &tls.Config{}
	controller := NewRefreshController(nil, nil, nil, tlsConfig)
	if controller.forwardTLSConfig != tlsConfig {
		t.Fatal("NewRefreshController did not store forward TLS config")
	}
}

func TestForwardRefreshToMasterRequiresTLSConfig(t *testing.T) {
	t.Parallel()

	_, errForward := ForwardRefreshToMaster(context.Background(), &ClusterNodeRecord{IP: "127.0.0.1", Port: 8327}, "auth-id", "secret", nil)
	if errForward == nil {
		t.Fatal("ForwardRefreshToMaster() error = nil, want TLS config error")
	}
	if !strings.Contains(errForward.Error(), "tls config is required") {
		t.Fatalf("ForwardRefreshToMaster() error = %v, want TLS config error", errForward)
	}
}
