package managementhttp

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestCORSMiddlewareMatchesCPAHeaders(t *testing.T) {
	engine := gin.New()
	engine.Use(corsMiddleware())
	engine.GET("/v0/management/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	optionsResp := httptest.NewRecorder()
	optionsReq := httptest.NewRequest(http.MethodOptions, "/v0/management/ping", nil)
	engine.ServeHTTP(optionsResp, optionsReq)

	if optionsResp.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want %d", optionsResp.Code, http.StatusNoContent)
	}
	assertCORSHeaders(t, optionsResp)

	getResp := httptest.NewRecorder()
	getReq := httptest.NewRequest(http.MethodGet, "/v0/management/ping", nil)
	engine.ServeHTTP(getResp, getReq)

	if getResp.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", getResp.Code, http.StatusOK)
	}
	assertCORSHeaders(t, getResp)
}

func TestClusterMTLSMiddlewareRejectsPlainHTTP(t *testing.T) {
	engine := gin.New()
	engine.Use(clusterMTLSMiddleware())
	engine.GET("/v0/cluster/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/cluster/ping", nil)
	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusForbidden)
	}
}

func TestClusterMTLSMiddlewareAllowsVerifiedPeer(t *testing.T) {
	engine := gin.New()
	engine.Use(clusterMTLSMiddleware())
	engine.GET("/v0/cluster/ping", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	cert := &x509.Certificate{}
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v0/cluster/ping", nil)
	req.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{cert},
		VerifiedChains:   [][]*x509.Certificate{{cert}},
	}
	engine.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
}

func assertCORSHeaders(t *testing.T, resp *httptest.ResponseRecorder) {
	t.Helper()

	if got := resp.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := resp.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST, PUT, PATCH, DELETE, OPTIONS" {
		t.Fatalf("Access-Control-Allow-Methods = %q, want CPA methods", got)
	}
	if got := resp.Header().Get("Access-Control-Allow-Headers"); got != "*" {
		t.Fatalf("Access-Control-Allow-Headers = %q, want *", got)
	}
}
