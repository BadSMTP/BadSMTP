//go:build !fasttests

package server

import (
	"os"
	"testing"
)

func TestNewServer(t *testing.T) {
	config := &Config{
		Port:                   2525,
		GreetingDelayPortStart: DefaultGreetingDelayStart,
		DropDelayPortStart:     DefaultDropDelayStart,
		TLSPort:                DefaultTLSPort,
		STARTTLSPort:           DefaultSTARTTLSPort,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create new server: %v", err)
	}

	if server.config != config {
		t.Error("Server config not set correctly")
	}

	if server.mailbox != nil {
		t.Error("Expected mailbox to be nil when no mailbox directory is specified")
	}
}

func TestNewServerWithMailbox(t *testing.T) {
	config := &Config{
		Port:                   2525,
		MailboxDir:             "/tmp/test-mailbox",
		GreetingDelayPortStart: DefaultGreetingDelayStart,
		DropDelayPortStart:     DefaultDropDelayStart,
		TLSPort:                DefaultTLSPort,
		STARTTLSPort:           DefaultSTARTTLSPort,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create new server with mailbox: %v", err)
	}

	if server.mailbox == nil {
		t.Error("Expected mailbox to be initialised when mailbox directory is specified")
	}
}

func TestRangesOverlap(t *testing.T) {
	tests := []struct {
		start1, end1, start2, end2 int
		expected                   bool
		description                string
	}{
		{1000, 1099, 2000, 2099, false, "Non-overlapping ranges"},
		{1000, 1099, 1050, 1149, true, "Overlapping ranges"},
		{1000, 1099, 1100, 1199, false, "Adjacent ranges (no overlap)"},
		{1000, 1099, 999, 1000, true, "Overlap at start"},
		{1000, 1099, 1099, 1199, true, "Overlap at end"},
		{1000, 1099, 1020, 1080, true, "Range2 inside Range1"},
		{1020, 1080, 1000, 1099, true, "Range1 inside Range2"},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			result := rangesOverlap(test.start1, test.end1, test.start2, test.end2)
			if result != test.expected {
				t.Errorf("rangesOverlap(%d, %d, %d, %d) = %v, expected %v",
					test.start1, test.end1, test.start2, test.end2, result, test.expected)
			}
		})
	}
}

func TestServerConfigDefaults(t *testing.T) {
	config := &Config{
		Port:                   2525,
		GreetingDelayPortStart: DefaultGreetingDelayStart,
		DropDelayPortStart:     DefaultDropDelayStart,
		TLSPort:                DefaultTLSPort,
		STARTTLSPort:           DefaultSTARTTLSPort,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Verify server holds correct config
	if server.config.Port != 2525 {
		t.Errorf("Expected server port 2525, got %d", server.config.Port)
	}
	if server.config.TLSPort != DefaultTLSPort {
		t.Errorf("Expected server TLS port %d, got %d", DefaultTLSPort, server.config.TLSPort)
	}
	if server.config.STARTTLSPort != DefaultSTARTTLSPort {
		t.Errorf("Expected server STARTTLS port %d, got %d", DefaultSTARTTLSPort, server.config.STARTTLSPort)
	}
}

func TestServerWithInvalidMailboxDir(t *testing.T) {
	// Create a temp file and use its path; creating a mailbox at a path that is a file should error.
	f, err := os.CreateTemp("", "badsmtp-file-")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	fpath := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(fpath) }()

	config := &Config{
		Port:                   2525,
		MailboxDir:             fpath,
		GreetingDelayPortStart: DefaultGreetingDelayStart,
		DropDelayPortStart:     DefaultDropDelayStart,
		TLSPort:                DefaultTLSPort,
		STARTTLSPort:           DefaultSTARTTLSPort,
	}

	_, err = NewServer(config)
	if err == nil {
		t.Error("Expected error when creating server with invalid mailbox directory")
	}
}

func TestPortBehaviourEdgeCases(t *testing.T) {
	config := &Config{
		GreetingDelayPortStart: DefaultGreetingDelayStart,
		DropDelayPortStart:     DefaultDropDelayStart,
	}

	// Test edge cases for port ranges
	edgeCases := []struct {
		port     int
		expected string
	}{
		{DefaultGreetingDelayStart - 1, "Normal behaviour"},                // Just before greeting delay range
		{DefaultGreetingDelayStart + 0, "Greeting delay: 0s"},              // Start of greeting delay range
		{DefaultGreetingDelayStart + PortRangeEnd, "Greeting delay: 600s"}, // End of greeting delay range
		{DefaultGreetingDelayStart + PortRangeEnd + 1, "Normal behaviour"}, // Just after greeting delay range
		{DefaultDropDelayStart - 1, "Normal behaviour"},                    // Just before drop delay range
		{DefaultDropDelayStart + 0, "Immediate drop"},                      // Start of drop delay range
		{DefaultDropDelayStart + PortRangeEnd, "Drop with delay: 600s"},    // End of drop delay range
		{DefaultDropDelayStart + PortRangeEnd + 1, "Normal behaviour"},     // Just after drop delay range
	}

	for _, test := range edgeCases {
		config.Port = test.port
		result := config.GetBehaviourDescription()
		if result != test.expected {
			t.Errorf("Port %d: expected '%s', got '%s'", test.port, test.expected, result)
		}
	}
}
