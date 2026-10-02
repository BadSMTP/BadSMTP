package server

import (
	"bufio"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"testing/synctest"
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

// TestEHLODlayApplied verifies an EHLO with dlay<N> delays the EHLO response and
// subsequent commands. It runs inside a synctest bubble so the session's sleeps
// use a virtual clock: even a 605-second delay elapses instantly.
func TestEHLODlayApplied(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
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
		if _, err := tp.ReadLine(); err != nil {
			t.Fatalf("failed to read greeting: %v", err)
		}

		// Send EHLO with the maximum clamped delay (605s) — instant under synctest
		start := time.Now()
		if err := tp.PrintfLine("EHLO dlay605.example.com"); err != nil {
			t.Fatalf("failed to send EHLO: %v", err)
		}

		// Expect the EHLO multiline response; first line should be delayed ~605s
		line, err := tp.ReadLine()
		if err != nil {
			t.Fatalf("failed to read EHLO line: %v", err)
		}
		dur := time.Since(start)
		if dur < 600*time.Second {
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

		// Now send NOOP and ensure it's also delayed by the same amount
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
		if noopDur < 600*time.Second {
			t.Fatalf("NOOP response was not delayed sufficiently: %v", noopDur)
		}
		if !strings.HasPrefix(noopResp, "250") {
			t.Fatalf("expected 250 for NOOP, got: %q", noopResp)
		}
	})
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

// TestPortGreetingDelay verifies that ports in the greeting-delay range
// (25200..25209) delay the greeting by the mapped amount. Runs inside a
// synctest bubble so even the 600-second maximum elapses instantly.
func TestPortGreetingDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for offset, want := range DelayOptions {
			if want == 0 {
				continue // offset 0 is the no-delay control port
			}
			port := DefaultGreetingDelayStart + offset

			client, serverConn := net.Pipe()
			cfg := &Config{Port: port}
			cfg.ensureScalarDefaults()
			cfg.AnalysePortBehaviour()
			if cfg.GreetingDelay != want {
				t.Fatalf("port %d: GreetingDelay = %d, want %d", port, cfg.GreetingDelay, want)
			}

			sess := NewSession(serverConn, cfg, nil)
			go func() { _ = sess.Handle() }()

			r := bufio.NewReader(client)
			start := time.Now()
			line, err := r.ReadString('\n')
			if err != nil {
				t.Fatalf("port %d: failed to read greeting: %v", port, err)
			}
			if d := time.Since(start); d < time.Duration(want)*time.Second {
				t.Fatalf("port %d: greeting not delayed: %v", port, d)
			}
			if !strings.HasPrefix(line, "220") {
				t.Fatalf("port %d: expected 220 greeting, got: %q", port, line)
			}

			client.Close()
			serverConn.Close()
		}
	})
}

// TestPortDropDelay verifies that ports in the drop-delay range (25600..25609)
// close the connection without a greeting after the mapped delay. Offset 0 is
// an immediate drop. Runs inside a synctest bubble for instant virtual delays.
func TestPortDropDelay(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for offset, want := range DelayOptions {
			port := DefaultDropDelayStart + offset

			client, serverConn := net.Pipe()
			cfg := &Config{Port: port}
			cfg.ensureScalarDefaults()
			cfg.AnalysePortBehaviour()

			if offset == 0 {
				if !cfg.DropImmediate {
					t.Fatalf("port %d: expected DropImmediate", port)
				}
			} else if cfg.DropDelay != want {
				t.Fatalf("port %d: DropDelay = %d, want %d", port, cfg.DropDelay, want)
			}

			sess := NewSession(serverConn, cfg, nil)
			go func() { _ = sess.Handle() }()

			r := bufio.NewReader(client)
			start := time.Now()
			line, err := r.ReadString('\n')
			if err == nil {
				t.Fatalf("port %d: expected connection drop, got data: %q", port, line)
			}
			// The connection must stay open for the delay period before dropping.
			if d := time.Since(start); d < time.Duration(want)*time.Second {
				t.Fatalf("port %d: dropped too early: %v", port, d)
			}

			client.Close()
			serverConn.Close()
		}
	})
}
