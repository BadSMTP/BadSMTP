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

// TestTLSPostQuantumKeyExchange verifies that the server's TLS listeners
// negotiate a hybrid post-quantum key exchange (MLKEM) by default. Since
// Go 1.24 the default curve preferences include X25519MLKEM768, and since
// Go 1.26 also SecP256r1MLKEM768 and SecP384r1MLKEM1024. BadSMTP does not
// override CurvePreferences, so clients get post-quantum security for free.
func TestTLSPostQuantumKeyExchange(t *testing.T) {
	srvConn, cliConn := net.Pipe()
	defer func() { _ = srvConn.Close(); _ = cliConn.Close() }()

	cfg := &Config{Port: 2525}
	cfg.EnsureDefaults()
	sess := NewSession(srvConn, cfg, nil)

	cert, err := sess.obtainTLSCertificate("localhost")
	if err != nil {
		t.Fatalf("obtainTLSCertificate failed: %v", err)
	}

	clientErrCh := make(chan error, 1)
	clientTLSConf := &tls.Config{
		InsecureSkipVerify: true, // self-signed cert
		ServerName:         "localhost",
	}
	clientTLS := tls.Client(cliConn, clientTLSConf)
	go func() { clientErrCh <- clientTLS.Handshake() }()

	if err := sess.upgradeToTLS(&cert); err != nil {
		t.Fatalf("upgradeToTLS failed: %v", err)
	}

	select {
	case cerr := <-clientErrCh:
		if cerr != nil {
			t.Fatalf("client handshake failed: %v", cerr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for client TLS handshake")
	}

	state := clientTLS.ConnectionState()
	pq := state.CurveID == tls.X25519MLKEM768 ||
		state.CurveID == tls.SecP256r1MLKEM768 ||
		state.CurveID == tls.SecP384r1MLKEM1024 ||
		state.CurveID == tls.MLKEM1024
	if !pq {
		t.Fatalf("expected a post-quantum key exchange, got %s", state.CurveID)
	}
}

// TestLegacyTLSPortAnalysis verifies that AnalysePortBehaviour sets the
// per-port minimum TLS version for the legacy TLS ports and leaves it at the
// secure default everywhere else.
func TestLegacyTLSPortAnalysis(t *testing.T) {
	tests := []struct {
		name string
		port int
		want uint16
	}{
		{name: "legacy implicit TLS 1.0 port", port: 25470, want: tls.VersionTLS10},
		{name: "legacy implicit TLS 1.1 port", port: 25471, want: tls.VersionTLS11},
		{name: "legacy STARTTLS 1.0 port", port: 25590, want: tls.VersionTLS10},
		{name: "legacy STARTTLS 1.1 port", port: 25591, want: tls.VersionTLS11},
		{name: "main implicit TLS port keeps default", port: 25465, want: 0},
		{name: "main STARTTLS port keeps default", port: 25587, want: 0},
		{name: "normal port keeps default", port: 2525, want: 0},
		{name: "port below legacy range keeps default", port: 25469, want: 0},
		{name: "port above legacy range keeps default", port: 25472, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Port: tt.port}
			cfg.EnsureDefaults()
			cfg.AnalysePortBehaviour()
			if cfg.LegacyTLSMinVersion != tt.want {
				t.Fatalf("port %d: LegacyTLSMinVersion = %d, want %d", tt.port, cfg.LegacyTLSMinVersion, tt.want)
			}
		})
	}
}

// TestUpgradeToTLSLegacyPortAccepted verifies that a session on a legacy
// STARTTLS port accepts a TLS 1.0 handshake, while the default STARTTLS port
// rejects it.
func TestUpgradeToTLSLegacyPortAccepted(t *testing.T) {
	for _, tt := range []struct {
		name       string
		port       int
		clientMax  uint16
		expectFail bool
	}{
		{name: "TLS 1.0 client accepted on legacy port 25590", port: 25590, clientMax: tls.VersionTLS10, expectFail: false},
		{name: "TLS 1.1 client accepted on legacy port 25591", port: 25591, clientMax: tls.VersionTLS11, expectFail: false},
		{name: "TLS 1.0 client rejected on default STARTTLS port", port: 25587, clientMax: tls.VersionTLS10, expectFail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srvConn, cliConn := net.Pipe()
			defer func() { _ = srvConn.Close(); _ = cliConn.Close() }()

			cfg := &Config{Port: tt.port}
			cfg.EnsureDefaults()
			cfg.AnalysePortBehaviour()
			sess := NewSession(srvConn, cfg, nil)

			cert, err := sess.obtainTLSCertificate("localhost")
			if err != nil {
				t.Fatalf("obtainTLSCertificate failed: %v", err)
			}

			clientErrCh := make(chan error, 1)
			go func() {
				clientTLS := tls.Client(cliConn, &tls.Config{
					InsecureSkipVerify: true,
					ServerName:         "localhost",
					MinVersion:         tls.VersionTLS10,
					MaxVersion:         tt.clientMax,
				})
				clientErrCh <- clientTLS.Handshake()
			}()

			upgradeErr := sess.upgradeToTLS(&cert)

			select {
			case cerr := <-clientErrCh:
				if tt.expectFail {
					if upgradeErr == nil && cerr == nil {
						t.Fatal("expected handshake to fail, but both sides succeeded")
					}
					return
				}
				if cerr != nil {
					t.Fatalf("client handshake failed: %v", cerr)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timeout waiting for client TLS handshake")
			}
			if upgradeErr != nil {
				t.Fatalf("upgradeToTLS failed: %v", upgradeErr)
			}
		})
	}
}
