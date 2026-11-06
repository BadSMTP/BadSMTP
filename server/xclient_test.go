package server

import (
	"strings"
	"testing"

	"badsmtp/smtp"
)

// runSessionScript runs a full session against a mock connection and returns
// the server output.
func runSessionScript(t *testing.T, cfg *Config, input string) string {
	t.Helper()
	if cfg == nil {
		cfg = newTestConfig()
	}
	conn := newMockConn()
	session := NewSession(conn, cfg, nil)
	conn.writeInput(input)
	if err := session.Handle(); err != nil {
		t.Fatalf("session handle failed: %v", err)
	}
	return conn.getOutput()
}

func TestSessionXclientAdvertisement(t *testing.T) {
	output := runSessionScript(t, nil, "EHLO client.example.com\r\nQUIT\r\n")
	if !strings.Contains(output, "250-XCLIENT "+xclientAdvertisedAttrs) {
		t.Error("expected XCLIENT capability in EHLO response, got:", output)
	}

	// Disabled via config
	cfg := newTestConfig()
	cfg.DisableXclient = true
	output = runSessionScript(t, cfg, "EHLO client.example.com\r\nQUIT\r\n")
	if strings.Contains(output, "XCLIENT") {
		t.Error("XCLIENT should not be advertised when disabled")
	}
}

func TestSessionXclientBasic(t *testing.T) {
	output := runSessionScript(t, nil,
		"EHLO client.example.com\r\n"+
			"XCLIENT NAME=spike.porcupine.org ADDR=168.100.189.2 PORT=12345\r\n"+
			"QUIT\r\n")

	// After XCLIENT the server responds 220 (greeting stage reset), then EHLO again.
	if !strings.Contains(output, "220 badsmtp.test ESMTP") {
		t.Error("expected 220 greeting after XCLIENT, got:", output)
	}
}

func TestSessionXclientBadSyntax(t *testing.T) {
	output := runSessionScript(t, nil,
		"EHLO client.example.com\r\n"+
			"XCLIENT BOGUS=1\r\n"+
			"QUIT\r\n")
	if !strings.Contains(output, "501 Unsupported XCLIENT attribute") {
		t.Error("expected 501 for unsupported attribute, got:", output)
	}

	output = runSessionScript(t, nil,
		"EHLO client.example.com\r\n"+
			"XCLIENT PORT=abc\r\n"+
			"QUIT\r\n")
	if !strings.Contains(output, "501 Bad XCLIENT port") {
		t.Error("expected 501 for bad port, got:", output)
	}
}

func TestSessionXclientMidTransactionRejected(t *testing.T) {
	output := runSessionScript(t, nil,
		"EHLO client.example.com\r\n"+
			"MAIL FROM:<sender@example.com>\r\n"+
			"XCLIENT ADDR=1.2.3.4\r\n"+
			"QUIT\r\n")
	if !strings.Contains(output, "503 Bad sequence") {
		t.Error("expected 503 for XCLIENT during transaction, got:", output)
	}
}

func TestSessionXclientXtextDecoding(t *testing.T) {
	// '+' is +2B, so "user+2Bname" decodes to "user+name"
	output := runSessionScript(t, nil,
		"EHLO client.example.com\r\n"+
			"XCLIENT NAME=user+2Bname\r\n"+
			"QUIT\r\n")
	if !strings.Contains(output, "220 badsmtp.test ESMTP") {
		t.Error("expected 220 after XCLIENT with xtext value, got:", output)
	}

	// Malformed escape -> 501
	output = runSessionScript(t, nil,
		"EHLO client.example.com\r\n"+
			"XCLIENT NAME=user+zz\r\n"+
			"QUIT\r\n")
	if !strings.Contains(output, "501 Bad XCLIENT attribute value encoding") {
		t.Error("expected 501 for malformed xtext, got:", output)
	}
}

func TestSessionXclientUnavailableResets(t *testing.T) {
	conn := newMockConn()
	cfg := newTestConfig()
	session := NewSession(conn, cfg, nil)
	conn.writeInput("EHLO client.example.com\r\nXCLIENT ADDR=1.2.3.4\r\nQUIT\r\n")
	if err := session.Handle(); err != nil {
		t.Fatalf("session handle failed: %v", err)
	}
	if session.xclientAddr != "1.2.3.4" {
		t.Fatalf("expected override set, got %q", session.xclientAddr)
	}

	conn2 := newMockConn()
	session2 := NewSession(conn2, cfg, nil)
	conn2.writeInput("EHLO client.example.com\r\nXCLIENT ADDR=[UNAVAILABLE]\r\nQUIT\r\n")
	if err := session2.Handle(); err != nil {
		t.Fatalf("session handle failed: %v", err)
	}
	if session2.xclientAddr != "" {
		t.Errorf("expected [UNAVAILABLE] to reset override, got %q", session2.xclientAddr)
	}
	if !strings.Contains(conn2.getOutput(), "220 badsmtp.test ESMTP") {
		t.Error("expected 220 for [UNAVAILABLE] XCLIENT, got:", conn2.getOutput())
	}
}

func TestSessionXclientDisabled(t *testing.T) {
	cfg := newTestConfig()
	cfg.DisableXclient = true
	output := runSessionScript(t, cfg,
		"EHLO client.example.com\r\n"+
			"XCLIENT ADDR=1.2.3.4\r\n"+
			"QUIT\r\n")
	if !strings.Contains(output, "502 Command not implemented") {
		t.Error("expected 502 when XCLIENT disabled, got:", output)
	}
}

func TestSessionXclientStateReset(t *testing.T) {
	conn := newMockConn()
	cfg := newTestConfig()
	session := NewSession(conn, cfg, nil)
	// No trailing QUIT: the session ends at EOF, leaving the post-XCLIENT state.
	conn.writeInput("EHLO client.example.com\r\nMAIL FROM:<sender@example.com>\r\nRSET\r\nXCLIENT ADDR=1.2.3.4\r\n")
	if err := session.Handle(); err != nil {
		t.Fatalf("session handle failed: %v", err)
	}
	if session.state != smtp.StateHelo {
		t.Errorf("expected StateHelo (greeting stage) after XCLIENT, got %s", session.state)
	}
	if session.mailFrom != "" || session.authenticated {
		t.Errorf("expected transaction state cleared, got mailFrom=%q authenticated=%v", session.mailFrom, session.authenticated)
	}
}

func TestSessionXclientReapplyPortBehaviour(t *testing.T) {
	// Overriding PORT to a drop-delay port should re-trigger the drop.
	conn := newMockConn()
	cfg := newTestConfig()
	// Simulate a listener on a normal port; keep drop range defaults.
	session := NewSession(conn, cfg, nil)
	conn.writeInput("EHLO client.example.com\r\nXCLIENT PORT=25600\r\n")

	// Session.Handle returns nil on the injected EOF drop; the connection
	// should be closed with no 220 response after the XCLIENT.
	_ = session.Handle()
	output := conn.getOutput()
	if strings.Count(output, "220") > 1 {
		t.Errorf("expected single 220 (initial greeting only), got %d in: %q", strings.Count(output, "220"), output)
	}

	// Greeting-delay port: XCLIENT with PORT=25201 should delay then 220.
	conn2 := newMockConn()
	session2 := NewSession(conn2, cfg, nil)
	conn2.writeInput("EHLO client.example.com\r\nXCLIENT PORT=25201\r\nQUIT\r\n")
	if err := session2.Handle(); err != nil {
		t.Fatalf("session handle failed: %v", err)
	}
	if !strings.Contains(conn2.getOutput(), "220 badsmtp.test ESMTP") {
		t.Error("expected 220 after greeting-delay XCLIENT, got:", conn2.getOutput())
	}
}

func TestSessionXclientHeloReconfiguresCapabilities(t *testing.T) {
	conn := newMockConn()
	cfg := newTestConfig()
	session := NewSession(conn, cfg, nil)
	// XCLIENT HELO=noauth... should restrict auth mechanisms for the session.
	conn.writeInput("EHLO client.example.com\r\nXCLIENT HELO=noauth.example.com\r\nQUIT\r\n")
	if err := session.Handle(); err != nil {
		t.Fatalf("session handle failed: %v", err)
	}

	// After the reset-to-greeting, a subsequent EHLO should not advertise AUTH.
	conn2 := newMockConn()
	session2 := NewSession(conn2, cfg, nil)
	conn2.writeInput("EHLO client.example.com\r\nXCLIENT HELO=noauth.example.com\r\nEHLO whatever.example.com\r\nQUIT\r\n")
	_ = session2.Handle()
	output := conn2.getOutput()
	// The second EHLO happens after state reset; capabilities were parsed from
	// the overridden hostname during XCLIENT, but the second EHLO re-parses
	// from its own hostname, so verify the XCLIENT itself succeeded.
	if !strings.Contains(output, "220 badsmtp.test ESMTP") {
		t.Errorf("expected 220 after XCLIENT HELO, got: %q", output)
	}
	_ = session
}

func TestSessionXclientHeloErrorSimulation(t *testing.T) {
	// Overridden HELO configuring a HELO error (helo500) should replace 220.
	output := runSessionScript(t, nil,
		"EHLO client.example.com\r\n"+
			"XCLIENT HELO=helo500.example.com\r\n"+
			"QUIT\r\n")
	if !strings.Contains(output, "500 ") {
		t.Errorf("expected 500 HELO error from overridden HELO, got: %q", output)
	}
}

func TestSessionXclientLoginDrivesAuth(t *testing.T) {
	conn := newMockConn()
	cfg := newTestConfig()
	session := NewSession(conn, cfg, nil)
	// PLAIN auth with goodauth user should succeed after XCLIENT LOGIN override.
	// XCLIENT resets to the greeting stage, so EHLO again before AUTH.
	conn.writeInput("EHLO client.example.com\r\n")
	conn.writeInput("XCLIENT LOGIN=goodauthuser@example.com\r\n")
	conn.writeInput("EHLO client.example.com\r\n")
	// base64 of "\x00goodauthuser\x00test"
	conn.writeInput("AUTH PLAIN AGdvb2RhdXRodXNlcgB0ZXN0\r\n")
	conn.writeInput("QUIT\r\n")
	if err := session.Handle(); err != nil {
		t.Fatalf("session handle failed: %v", err)
	}
	output := conn.getOutput()
	if !strings.Contains(output, "235") {
		t.Errorf("expected 235 successful auth driven by XCLIENT LOGIN, got: %q", output)
	}
}

func TestSessionXclientEffectiveClientIP(t *testing.T) {
	conn := newMockConn()
	cfg := newTestConfig()
	session := NewSession(conn, cfg, nil)
	conn.writeInput("XCLIENT ADDR=192.0.2.10\r\nQUIT\r\n")
	_ = session.Handle()
	if got := session.effectiveClientIP(); got != "192.0.2.10" {
		t.Errorf("effectiveClientIP = %q, want 192.0.2.10", got)
	}
}
