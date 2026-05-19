package cluster

import (
	"crypto/x509"
	"testing"
	"time"
)

func TestCreateServerCertificateSupportsNodeMTLS(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0).UTC()
	caPEM, caKeyPEM, _, errCA := createCACertificate(now)
	if errCA != nil {
		t.Fatalf("createCACertificate() error = %v", errCA)
	}
	caCert, errCACert := parseCertificatePEM(caPEM)
	if errCACert != nil {
		t.Fatalf("parseCertificatePEM(ca) error = %v", errCACert)
	}
	caKey, errCAKey := parseRSAPrivateKeyPEM(caKeyPEM)
	if errCAKey != nil {
		t.Fatalf("parseRSAPrivateKeyPEM(ca) error = %v", errCAKey)
	}
	serverPEM, _, _, errServer := createServerCertificate(now, caCert, caKey, "127.0.0.1")
	if errServer != nil {
		t.Fatalf("createServerCertificate() error = %v", errServer)
	}
	serverCert, errServerCert := parseCertificatePEM(serverPEM)
	if errServerCert != nil {
		t.Fatalf("parseCertificatePEM(server) error = %v", errServerCert)
	}
	if !hasExtKeyUsage(serverCert, x509.ExtKeyUsageServerAuth) {
		t.Fatal("server certificate missing server auth usage")
	}
	if !hasExtKeyUsage(serverCert, x509.ExtKeyUsageClientAuth) {
		t.Fatal("server certificate missing client auth usage")
	}
}

func TestTLSConfigFromCertificateRecordsSetsRootCAs(t *testing.T) {
	t.Parallel()

	now := time.Unix(100, 0).UTC()
	caPEM, caKeyPEM, caSerial, errCA := createCACertificate(now)
	if errCA != nil {
		t.Fatalf("createCACertificate() error = %v", errCA)
	}
	caCert, errCACert := parseCertificatePEM(caPEM)
	if errCACert != nil {
		t.Fatalf("parseCertificatePEM(ca) error = %v", errCACert)
	}
	caKey, errCAKey := parseRSAPrivateKeyPEM(caKeyPEM)
	if errCAKey != nil {
		t.Fatalf("parseRSAPrivateKeyPEM(ca) error = %v", errCAKey)
	}
	serverPEM, serverKeyPEM, serverSerial, errServer := createServerCertificate(now, caCert, caKey, "127.0.0.1")
	if errServer != nil {
		t.Fatalf("createServerCertificate() error = %v", errServer)
	}
	tlsConfig, errTLS := tlsConfigFromCertificateRecords(
		&CertificateRecord{CertificatePEM: string(caPEM), PrivateKeyPEM: string(caKeyPEM), SerialNumber: caSerial, IsCA: true},
		&CertificateRecord{CertificatePEM: string(serverPEM), PrivateKeyPEM: string(serverKeyPEM), SerialNumber: serverSerial, IsServer: true, IP: "127.0.0.1"},
	)
	if errTLS != nil {
		t.Fatalf("tlsConfigFromCertificateRecords() error = %v", errTLS)
	}
	if tlsConfig.RootCAs == nil {
		t.Fatal("RootCAs = nil, want cluster CA pool")
	}
	if tlsConfig.ClientCAs == nil {
		t.Fatal("ClientCAs = nil, want cluster CA pool")
	}
}
