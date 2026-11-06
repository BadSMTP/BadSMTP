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

func TestParseMinTLSVersion(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    uint16
		wantErr bool
	}{
		{name: "TLS10", value: "TLS10", want: tls.VersionTLS10},
		{name: "TLS11", value: "TLS11", want: tls.VersionTLS11},
		{name: "TLS12", value: "TLS12", want: tls.VersionTLS12},
		{name: "TLS13", value: "TLS13", want: tls.VersionTLS13},
		{name: "case insensitive", value: "tls12", want: tls.VersionTLS12},
		{name: "empty", value: "", wantErr: true},
		{name: "invalid", value: "TLS09", wantErr: true},
		{name: "not a version", value: "SSL30", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMinTLSVersion(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseMinTLSVersion(%q) expected error, got %d", tt.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMinTLSVersion(%q) unexpected error: %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("ParseMinTLSVersion(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestDefaultMinTLSVersionIsTLS12(t *testing.T) {
	cfg := &Config{Port: 2525}
	cfg.EnsureDefaults()
	if cfg.MinTLSVersion != "TLS12" {
		t.Fatalf("expected default min_tls_version TLS12, got %q", cfg.MinTLSVersion)
	}
}

// TestUpgradeToTLSMinVersionEnforced verifies that upgradeToTLS rejects
// handshakes below the configured minimum and accepts those at or above it.
func TestUpgradeToTLSMinVersionEnforced(t *testing.T) {
	for _, tt := range []struct {
		name       string
		minVersion string
		clientMax  uint16
		expectFail bool
	}{
		{name: "TLS10 client rejected by TLS12 default", minVersion: "TLS12", clientMax: tls.VersionTLS10, expectFail: true},
		{name: "TLS11 client rejected by TLS12 default", minVersion: "TLS12", clientMax: tls.VersionTLS11, expectFail: true},
		{name: "TLS12 client accepted by TLS12 default", minVersion: "TLS12", clientMax: tls.VersionTLS12, expectFail: false},
		{name: "TLS10 client accepted when min is TLS10", minVersion: "TLS10", clientMax: tls.VersionTLS10, expectFail: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srvConn, cliConn := net.Pipe()
			defer func() { _ = srvConn.Close(); _ = cliConn.Close() }()

			cfg := &Config{Port: 2525, MinTLSVersion: tt.minVersion}
			cfg.EnsureDefaults()
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
