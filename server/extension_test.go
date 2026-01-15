package server

import (
	"net/textproto"
	"strings"
	"testing"
	"time"

	"badsmtp/smtp"
)

// testGoBananasExtension implements a custom SMTP extension
// that advertises "GOBANANAS" capability and handles "BANA" command
type testGoBananasExtension struct{}

func (e *testGoBananasExtension) GetCapability() string {
	return "GOBANANAS"
}

func (e *testGoBananasExtension) GetAllowedStates(command string) []smtp.State {
	// Allow BANA command in any state (return nil)
	return nil
}

func (e *testGoBananasExtension) HandleCommand(command string, args []string, session SessionWriter) (bool, error) {
	if command == "BANA" {
		// Respond with custom message
		err := session.WriteResponse("250 OK NA")
		return true, err
	}
	return false, nil
}

func TestCustomSMTPExtension(t *testing.T) {
	// Create session with custom extension
	client, serverConn := connPair()
	cfg := &Config{Port: 2525}
	cfg.EnsureDefaults()
	cfg.SMTPExtensions = []SMTPExtension{&testGoBananasExtension{}}

	sess := NewSession(serverConn, cfg, nil)
	go func() { _ = sess.Handle() }()
	defer func() { client.Close(); serverConn.Close(); time.Sleep(10 * time.Millisecond) }()

	tp := textproto.NewConn(client)
	defer tp.Close()

	// Read greeting
	if _, err := tp.ReadLine(); err != nil {
		t.Fatalf("failed to read greeting: %v", err)
	}

	// Send EHLO and check for GOBANANAS capability
	if err := tp.PrintfLine("EHLO client.example.com"); err != nil {
		t.Fatalf("failed to send EHLO: %v", err)
	}

	foundGobananas := false
	for {
		line, err := tp.ReadLine()
		if err != nil {
			t.Fatalf("error reading EHLO response: %v", err)
		}
		if strings.Contains(line, "GOBANANAS") {
			foundGobananas = true
		}
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	if !foundGobananas {
		t.Fatal("GOBANANAS capability not advertised in EHLO response")
	}

	// Send BANA command
	if err := tp.PrintfLine("BANA"); err != nil {
		t.Fatalf("failed to send BANA: %v", err)
	}

	// Read response
	resp, err := tp.ReadLine()
	if err != nil {
		t.Fatalf("failed to read BANA response: %v", err)
	}

	if resp != "250 OK NA" {
		t.Fatalf("Expected '250 OK NA', got: %s", resp)
	}
}

func TestCustomExtensionWithMetadata(t *testing.T) {
	// Test extension that stores and retrieves metadata
	simpleMetadataExt := &testMetadataExtension{}

	client, serverConn := connPair()
	cfg := &Config{Port: 2525}
	cfg.EnsureDefaults()
	cfg.SMTPExtensions = []SMTPExtension{simpleMetadataExt}

	sess := NewSession(serverConn, cfg, nil)
	go func() { _ = sess.Handle() }()
	defer func() { client.Close(); serverConn.Close(); time.Sleep(10 * time.Millisecond) }()

	tp := textproto.NewConn(client)
	defer tp.Close()

	// Read greeting
	if _, err := tp.ReadLine(); err != nil {
		t.Fatalf("failed to read greeting: %v", err)
	}

	// Send EHLO
	if err := tp.PrintfLine("EHLO client.example.com"); err != nil {
		t.Fatalf("failed to send EHLO: %v", err)
	}

	// Skip EHLO response
	for {
		line, err := tp.ReadLine()
		if err != nil {
			t.Fatalf("error reading EHLO response: %v", err)
		}
		if strings.HasPrefix(line, "250 ") {
			break
		}
	}

	// Set metadata
	if err := tp.PrintfLine("SETMETA testvalue"); err != nil {
		t.Fatalf("failed to send SETMETA: %v", err)
	}

	resp, err := tp.ReadLine()
	if err != nil {
		t.Fatalf("failed to read SETMETA response: %v", err)
	}

	if !strings.HasPrefix(resp, "250") {
		t.Fatalf("SETMETA failed: %s", resp)
	}

	// Get metadata
	if err := tp.PrintfLine("GETMETA"); err != nil {
		t.Fatalf("failed to send GETMETA: %v", err)
	}

	resp, err = tp.ReadLine()
	if err != nil {
		t.Fatalf("failed to read GETMETA response: %v", err)
	}

	if resp != "250 testvalue" {
		t.Fatalf("Expected '250 testvalue', got: %s", resp)
	}
}

// testMetadataExtension is a simple extension for testing metadata
type testMetadataExtension struct{}

func (e *testMetadataExtension) GetCapability() string {
	return "METADATA"
}

func (e *testMetadataExtension) GetAllowedStates(command string) []smtp.State {
	// Allow metadata commands in any state
	return nil
}

func (e *testMetadataExtension) HandleCommand(command string, args []string, session SessionWriter) (bool, error) {
	if command == "SETMETA" {
		if len(args) > 0 {
			session.SetMetadata("custom_key", args[0])
			return true, session.WriteResponse("250 Metadata set")
		}
		return true, session.WriteResponse("501 Syntax error")
	}
	if command == "GETMETA" {
		metadata := session.GetMetadata()
		if val, ok := metadata["custom_key"]; ok {
			return true, session.WriteResponse("250 " + val.(string))
		}
		return true, session.WriteResponse("250 No metadata")
	}
	return false, nil
}
