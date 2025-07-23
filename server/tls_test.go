package server

import (
	"crypto/tls"
	"net"
	"testing"
	"time"
)

func TestObtainTLSCertificate(t *testing.T) {
	cfg := &Config{Port: 2525}
	cfg.EnsureDefaults()
	sess := NewSession(nil, cfg, nil)
	// Should be able to obtain a certificate for a hostname without error
	if _, err := sess.obtainTLSCertificate("localhost"); err != nil {
		t.Fatalf("obtainTLSCertificate failed: %v", err)
	}
}

func TestUpgradeToTLSNetPipe(t *testing.T) {
	// Create a connected pair of in-memory connections
	srvConn, cliConn := net.Pipe()
	defer func() { _ = srvConn.Close(); _ = cliConn.Close() }()

	cfg := &Config{Port: 2525}
	cfg.EnsureDefaults()
	sess := NewSession(srvConn, cfg, nil)

	// Obtain a certificate
	cert, err := sess.obtainTLSCertificate("localhost")
	if err != nil {
		t.Fatalf("obtainTLSCertificate failed: %v", err)
	}

	clientErrCh := make(chan error, 1)
	// Start client TLS handshake in background
	go func() {
		clientTLSConf := &tls.Config{
			InsecureSkipVerify: true, // self-signed cert
			ServerName:         "localhost",
		}
		clientTLS := tls.Client(cliConn, clientTLSConf)
		clientErrCh <- clientTLS.Handshake()
	}()

	// Run server-side upgrade (blocks until client handshake completes)
	if err := sess.upgradeToTLS(&cert); err != nil {
		t.Fatalf("upgradeToTLS failed: %v", err)
	}

	// Wait for client handshake result with timeout
	select {
	case cerr := <-clientErrCh:
		if cerr != nil {
			t.Fatalf("client handshake failed: %v", cerr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for client TLS handshake")
	}

	// After successful upgrade, session should have tlsState set
	if sess.tlsState == nil {
		t.Fatal("expected session.tlsState to be set after TLS upgrade")
	}
}
