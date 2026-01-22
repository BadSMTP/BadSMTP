package server

import (
	"bufio"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"
)

// TestParseDlayValue ensures parsing and clamping of dlay values works as expected.
func TestParseDlayValue(t *testing.T) {
	cases := []struct {
		input    string
		expected int
	}{
		{"dlay0", 0},
		{"dlay1", 1},
		{"dlay605", 605},
		{"dlay1000", 605}, // clamped down
		{"dlay-5", 0},
		{"dlayabc", 0},
		{"notdlay123", 0},
	}

	for _, c := range cases {
		if v := parseDlayValue(c.input); v != c.expected {
			t.Fatalf("parseDlayValue(%q) = %d, want %d", c.input, v, c.expected)
		}
	}
}

// TestEHLODlayApplied verifies an EHLO with dlay<N> delays the EHLO response and subsequent commands.
func TestEHLODlayApplied(t *testing.T) {
	client, serverConn := net.Pipe()
	defer client.Close()
	defer serverConn.Close()

	cfg := &Config{Port: 2525}
	sess := NewSession(serverConn, cfg, nil)
	go func() { _ = sess.Handle() }()

	// Wrap client in textproto for easier handling
	tp := textproto.NewConn(client)
	defer tp.Close()

	// Read greeting
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := tp.ReadLine(); err != nil {
		t.Fatalf("failed to read greeting: %v", err)
	}
	_ = client.SetReadDeadline(time.Time{})

	// Send EHLO with small delay (1s) to keep test fast
	start := time.Now()
	if err := tp.PrintfLine("EHLO dlay1.example.com"); err != nil {
		t.Fatalf("failed to send EHLO: %v", err)
	}

	// Expect the EHLO multiline response; first line should be delayed ~1s
	line, err := tp.ReadLine()
	if err != nil {
		t.Fatalf("failed to read EHLO line: %v", err)
	}
	dur := time.Since(start)
	if dur < 900*time.Millisecond {
		t.Fatalf("EHLO response was not delayed sufficiently: %v", dur)
	}
	if !strings.HasPrefix(line, "250-badsmtp.test") {
		t.Fatalf("expected EHLO banner, got: %q", line)
	}

	// Consume remaining EHLO lines
	for {
		l, err := tp.ReadLine()
		if err != nil {
			t.Fatalf("error reading EHLO continuation: %v", err)
		}
		if strings.HasPrefix(l, "250 ") {
			break
		}
	}

	// Now send NOOP and ensure it's also delayed by same amount (~1s)
	startNoop := time.Now()
	if _, err := tp.Cmd("NOOP"); err != nil {
		t.Fatalf("failed to send NOOP: %v", err)
	}
	// Read response line
	noopResp, err := tp.ReadLine()
	if err != nil {
		t.Fatalf("failed to read NOOP response: %v", err)
	}
	noopDur := time.Since(startNoop)
	if noopDur < 900*time.Millisecond {
		t.Fatalf("NOOP response was not delayed sufficiently: %v", noopDur)
	}
	if !strings.HasPrefix(noopResp, "250") {
		t.Fatalf("expected 250 for NOOP, got: %q", noopResp)
	}
}

// TestEHLODlayInvalidIgnored ensures invalid or zero dlay labels are ignored (no delay)
func TestEHLODlayInvalidIgnored(t *testing.T) {
	cases := []string{"dlay0.example.com", "dlayabc.example.com", "example.com"}
	for _, host := range cases {
		client, serverConn := net.Pipe()
		cfg := &Config{Port: 2525}
		sess := NewSession(serverConn, cfg, nil)
		go func() { _ = sess.Handle() }()

		// Wrap client
		r := bufio.NewReader(client)
		// Read greeting
		client.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := r.ReadString('\n'); err != nil {
			t.Fatalf("failed to read greeting: %v", err)
		}
		_ = client.SetReadDeadline(time.Time{})

		start := time.Now()
		if _, err := client.Write([]byte("EHLO " + host + "\r\n")); err != nil {
			t.Fatalf("failed to write EHLO: %v", err)
		}

		// Read first EHLO line
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("failed to read EHLO line: %v", err)
		}
		dur := time.Since(start)
		if dur > 500*time.Millisecond {
			t.Fatalf("unexpected delay for host %s: %v", host, dur)
		}
		if !strings.HasPrefix(line, "250-badsmtp.test") {
			t.Fatalf("expected EHLO banner, got: %q", line)
		}

		// Cleanup
		client.Close()
		serverConn.Close()
	}
}
