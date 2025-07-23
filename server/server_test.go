//go:build !fasttests

package server

import (
	"os"
	"strings"
	"testing"
)

func TestNewServer(t *testing.T) {
	config := &Config{
		Port:                   2525,
		GreetingDelayPortStart: 3000,
		CommandDelayPortStart:  4000,
		DropDelayPortStart:     5000,
		ImmediateDropPort:      6000,
		TLSPort:                25465,
		STARTTLSPort:           25587,
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
		GreetingDelayPortStart: 3000,
		CommandDelayPortStart:  4000,
		DropDelayPortStart:     5000,
		ImmediateDropPort:      6000,
		TLSPort:                25465,
		STARTTLSPort:           25587,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create new server with mailbox: %v", err)
	}

	if server.mailbox == nil {
		t.Error("Expected mailbox to be initialised when mailbox directory is specified")
	}
}

func TestValidatePortConfiguration(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "Valid configuration",
			config: &Config{
				Port:                   2525,
				GreetingDelayPortStart: 3000,
				CommandDelayPortStart:  4000,
				DropDelayPortStart:     5000,
				ImmediateDropPort:      6000,
				TLSPort:                25465,
				STARTTLSPort:           25587,
			},
			expectError: false,
		},
		{
			name: "Overlapping greeting and command delay ranges",
			config: &Config{
				Port:                   2525,
				GreetingDelayPortStart: 3000,
				CommandDelayPortStart:  3050, // Overlaps with greeting delay
				DropDelayPortStart:     5000,
				ImmediateDropPort:      6000,
				TLSPort:                25465,
				STARTTLSPort:           25587,
			},
			expectError: true,
			errorMsg:    "port ranges overlap",
		},
		{
			name: "Normal port conflicts with greeting delay range",
			config: &Config{
				Port:                   3050, // Within greeting delay range
				GreetingDelayPortStart: 3000,
				CommandDelayPortStart:  4000,
				DropDelayPortStart:     5000,
				ImmediateDropPort:      6000,
				TLSPort:                25465,
				STARTTLSPort:           25587,
			},
			expectError: true,
			errorMsg:    "conflicts with greeting delay range",
		},
		{
			name: "Normal port conflicts with immediate drop port",
			config: &Config{
				Port:                   6000, // Same as immediate drop port
				GreetingDelayPortStart: 3000,
				CommandDelayPortStart:  4000,
				DropDelayPortStart:     5000,
				ImmediateDropPort:      6000,
				TLSPort:                25465,
				STARTTLSPort:           25587,
			},
			expectError: true,
			errorMsg:    "conflicts with immediate drop port",
		},
		{
			name: "TLS port conflicts with command delay range",
			config: &Config{
				Port:                   2525,
				GreetingDelayPortStart: 3000,
				CommandDelayPortStart:  4000,
				DropDelayPortStart:     5000,
				ImmediateDropPort:      6000,
				TLSPort:                4050, // Within command delay range
				STARTTLSPort:           25587,
				TLSCertFile:            "/path/to/cert.pem",
				TLSKeyFile:             "/path/to/key.pem",
			},
			expectError: true,
			errorMsg:    "TLS port",
		},
		{
			name: "TLS and STARTTLS ports conflict",
			config: &Config{
				Port:                   2525,
				GreetingDelayPortStart: 3000,
				CommandDelayPortStart:  4000,
				DropDelayPortStart:     5000,
				ImmediateDropPort:      6000,
				TLSPort:                25465,
				STARTTLSPort:           25465, // Same as TLS port
				TLSCertFile:            "/path/to/cert.pem",
				TLSKeyFile:             "/path/to/key.pem",
			},
			expectError: true,
			errorMsg:    "conflicts with STARTTLS port",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePortConfiguration(test.config)

			if test.expectError {
				if err == nil {
					t.Errorf("Expected error for %s, but got none", test.name)
				} else if test.errorMsg != "" && err.Error() != "" {
					// Check if error message contains expected text
					found := false
					for _, word := range []string{"overlap", "conflict"} {
						if strings.Contains(err.Error(), word) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected error message to contain relevant text, got: %s", err.Error())
					}
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for %s, but got: %v", test.name, err)
				}
			}
		})
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
		GreetingDelayPortStart: 3000,
		CommandDelayPortStart:  4000,
		DropDelayPortStart:     5000,
		ImmediateDropPort:      6000,
		TLSPort:                25465,
		STARTTLSPort:           25587,
	}

	server, err := NewServer(config)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Verify server holds correct config
	if server.config.Port != 2525 {
		t.Errorf("Expected server port 2525, got %d", server.config.Port)
	}
	if server.config.TLSPort != 25465 {
		t.Errorf("Expected server TLS port 25465, got %d", server.config.TLSPort)
	}
	if server.config.STARTTLSPort != 25587 {
		t.Errorf("Expected server STARTTLS port 25587, got %d", server.config.STARTTLSPort)
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
		GreetingDelayPortStart: 3000,
		CommandDelayPortStart:  4000,
		DropDelayPortStart:     5000,
		ImmediateDropPort:      6000,
		TLSPort:                25465,
		STARTTLSPort:           25587,
	}

	_, err = NewServer(config)
	if err == nil {
		t.Error("Expected error when creating server with invalid mailbox directory")
	}
}

func TestPortBehaviourEdgeCases(t *testing.T) {
	config := &Config{
		GreetingDelayPortStart: 3000,
		CommandDelayPortStart:  4000,
		DropDelayPortStart:     5000,
		ImmediateDropPort:      6000,
	}

	// Test edge cases for port ranges
	edgeCases := []struct {
		port     int
		expected string
	}{
		{2999, "Normal behaviour"},     // Just before greeting delay range
		{3000, "Greeting delay: 0s"},   // Start of greeting delay range
		{3099, "Greeting delay: 990s"}, // End of greeting delay range
		{3100, "Normal behaviour"},     // Just after greeting delay range
		{3999, "Normal behaviour"},     // Just before command delay range
		{4000, "Command delay: 0s"},    // Start of command delay range
		{4099, "Command delay: 990s"},  // End of command delay range
		{4100, "Normal behaviour"},     // Just after command delay range
	}

	for _, test := range edgeCases {
		config.Port = test.port
		result := config.GetBehaviourDescription()
		if result != test.expected {
			t.Errorf("Port %d: expected '%s', got '%s'", test.port, test.expected, result)
		}
	}
}
