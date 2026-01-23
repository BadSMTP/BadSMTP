package server

import (
	"crypto/tls"
	"os"
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Test default configuration
	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load default config: %v", err)
	}

	// Check default values
	if config.Port != 2525 {
		t.Errorf("Expected default port 2525, got %d", config.Port)
	}
	if config.GreetingDelayPortStart != DefaultGreetingDelayStart {
		t.Errorf("Expected default greeting delay port start %d, got %d", DefaultGreetingDelayStart, config.GreetingDelayPortStart)
	}
	if config.DropDelayPortStart != DefaultDropDelayStart {
		t.Errorf("Expected default drop delay port start %d, got %d", DefaultDropDelayStart, config.DropDelayPortStart)
	}
	if config.TLSPort != 25465 {
		t.Errorf("Expected default TLS port 25465, got %d", config.TLSPort)
	}
	if config.STARTTLSPort != 25587 {
		t.Errorf("Expected default STARTTLS port 25587, got %d", config.STARTTLSPort)
	}
}

func TestConfigAnalysePortBehaviour(t *testing.T) {
	config := &Config{
		GreetingDelayPortStart: DefaultGreetingDelayStart,
		DropDelayPortStart:     DefaultDropDelayStart,
	}

	tests := []struct {
		port              int
		expectedGreeting  int
		expectedDrop      int
		expectedImmediate bool
	}{
		{DefaultGreetingDelayStart + 0, DelayOptions[0], 0, false},
		{DefaultGreetingDelayStart + 4, DelayOptions[4], 0, false},
		{DefaultDropDelayStart + 0, 0, DelayOptions[0], true},
		{DefaultDropDelayStart + 5, 0, DelayOptions[5], false},
		{2525, 0, 0, false},
	}

	for _, test := range tests {
		config.Port = test.port
		config.GreetingDelay = 0
		config.DropDelay = 0
		config.DropImmediate = false

		config.AnalysePortBehaviour()

		if config.GreetingDelay != test.expectedGreeting {
			t.Errorf("Port %d: expected greeting delay %d, got %d", test.port, test.expectedGreeting, config.GreetingDelay)
		}
		if config.DropDelay != test.expectedDrop {
			t.Errorf("Port %d: expected drop delay %d, got %d", test.port, test.expectedDrop, config.DropDelay)
		}
		if config.DropImmediate != test.expectedImmediate {
			t.Errorf("Port %d: expected immediate drop %v, got %v", test.port, test.expectedImmediate, config.DropImmediate)
		}
	}
}

func TestGetBehaviourDescription(t *testing.T) {
	config := &Config{
		GreetingDelayPortStart: DefaultGreetingDelayStart,
		DropDelayPortStart:     DefaultDropDelayStart,
	}

	tests := []struct {
		port     int
		expected string
	}{
		{DefaultGreetingDelayStart + 4, "Greeting delay: 10s"},
		{2525, "Normal behaviour"},
		{DefaultDropDelayStart + 5, "Drop with delay: 30s"},
		{DefaultDropDelayStart + 0, "Immediate drop"},
		{2525, "Normal behaviour"},
	}

	for _, test := range tests {
		config.Port = test.port
		result := config.GetBehaviourDescription()
		if result != test.expected {
			t.Errorf("Port %d: expected description '%s', got '%s'", test.port, test.expected, result)
		}
	}
}

func TestGetTLSHostname(t *testing.T) {
	config := &Config{}

	// Test default hostname
	hostname := config.GetTLSHostname()
	if hostname != "badsmtp.test" {
		t.Errorf("Expected default hostname 'badsmtp.test', got '%s'", hostname)
	}

	// Test custom hostname
	config.TLSHostname = "custom.test"
	hostname = config.GetTLSHostname()
	if hostname != "custom.test" {
		t.Errorf("Expected custom hostname 'custom.test', got '%s'", hostname)
	}
}

func TestGenerateSelfSignedCert(t *testing.T) {
	config := &Config{}

	// Test certificate generation for hostname
	hostname := "test.example.com"
	cert, err := config.GenerateSelfSignedCert(hostname)
	if err != nil {
		t.Fatalf("Failed to generate self-signed certificate: %v", err)
	}

	// Verify certificate is valid
	if len(cert.Certificate) == 0 {
		t.Error("Generated certificate has no certificate data")
	}
	if cert.PrivateKey == nil {
		t.Error("Generated certificate has no private key")
	}

	// Test certificate generation for IP address
	ipAddr := "127.0.0.1"
	cert, err = config.GenerateSelfSignedCert(ipAddr)
	if err != nil {
		t.Fatalf("Failed to generate self-signed certificate for IP: %v", err)
	}

	if len(cert.Certificate) == 0 {
		t.Error("Generated certificate for IP has no certificate data")
	}
}

func TestHasTLS(t *testing.T) {
	config := &Config{}

	// Test that TLS is always available (returns true)
	if !config.HasTLS() {
		t.Error("Expected HasTLS() to return true (self-signed certificates available)")
	}

	// Test with certificate files
	config.TLSCertFile = "/path/to/cert.pem"
	config.TLSKeyFile = "/path/to/key.pem"
	if !config.HasTLS() {
		t.Error("Expected HasTLS() to return true with certificate files")
	}
}

func TestConfigWithEnvironmentVariables(t *testing.T) {
	// Set environment variables for testing
	envVars := map[string]string{
		"BADSMTP_PORT":                   "2525",
		"BADSMTP_MAILBOXDIR":             "/tmp/test-mailbox",
		"BADSMTP_GREETINGDELAYPORTSTART": "25200",
		"BADSMTP_DROPDELAYPORTSTART":     "25600",
		"BADSMTP_TLSCERTFILE":            "/path/to/cert.pem",
		"BADSMTP_TLSKEYFILE":             "/path/to/key.pem",
		"BADSMTP_TLSPORT":                "8443",
		"BADSMTP_STARTTLSPORT":           "8587",
		"BADSMTP_TLSHOSTNAME":            "test.example.com",
	}

	// Set environment variables and defer cleanup
	originalValues := make(map[string]string)
	for key, value := range envVars {
		originalValues[key] = os.Getenv(key)
		if err := os.Setenv(key, value); err != nil {
			t.Fatalf("Failed to set environment variable %s: %v", key, err)
		}
	}

	// Restore original environment variables after test
	defer func() {
		for key, originalValue := range originalValues {
			if originalValue == "" {
				_ = os.Unsetenv(key)
			} else {
				_ = os.Setenv(key, originalValue)
			}
		}
	}()

	config, err := LoadConfig()
	if err != nil {
		t.Fatalf("Failed to load config with environment variables: %v", err)
	}

	// Verify environment variables were loaded
	if config.Port != 2525 {
		t.Errorf("Expected port 2525 from env, got %d", config.Port)
	}
	if config.MailboxDir != "/tmp/test-mailbox" {
		t.Errorf("Expected mailbox dir '/tmp/test-mailbox' from env, got '%s'", config.MailboxDir)
	}
	if config.GreetingDelayPortStart != 25200 {
		t.Errorf("Expected greeting delay port start 25200 from env, got %d", config.GreetingDelayPortStart)
	}
	if config.TLSCertFile != "/path/to/cert.pem" {
		t.Errorf("Expected TLS cert file '/path/to/cert.pem' from env, got '%s'", config.TLSCertFile)
	}
	if config.TLSHostname != "test.example.com" {
		t.Errorf("Expected TLS hostname 'test.example.com' from env, got '%s'", config.TLSHostname)
	}
}

func TestTLSCertificateValidation(t *testing.T) {
	config := &Config{}

	// Test certificate with various hostnames
	testCases := []string{
		"localhost",
		"test.example.com",
		"127.0.0.1",
		"::1",
		"mail.badsmtp.test",
	}

	for _, hostname := range testCases {
		cert, err := config.GenerateSelfSignedCert(hostname)
		if err != nil {
			t.Errorf("Failed to generate certificate for hostname '%s': %v", hostname, err)
			continue
		}

		// Try to create a TLS configuration with the certificate
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{cert},
			ServerName:   hostname,
			MinVersion:   tls.VersionTLS12,
		}

		if len(tlsConfig.Certificates) == 0 {
			t.Errorf("TLS config has no certificates for hostname '%s'", hostname)
		}
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
				GreetingDelayPortStart: DefaultGreetingDelayStart,
				DropDelayPortStart:     DefaultDropDelayStart,
				TLSPort:                DefaultTLSPort,
				STARTTLSPort:           DefaultSTARTTLSPort,
			},
			expectError: false,
		},
		{
			name: "Overlapping greeting and drop delay ranges",
			config: &Config{
				Port:                   2525,
				GreetingDelayPortStart: DefaultGreetingDelayStart,
				DropDelayPortStart:     DefaultGreetingDelayStart + 4, // Overlaps with greeting delay offsets
				TLSPort:                DefaultTLSPort,
				STARTTLSPort:           DefaultSTARTTLSPort,
			},
			expectError: true,
			errorMsg:    "port ranges overlap",
		},
		{
			name: "TLS port conflicts with greeting delay range",
			config: &Config{
				Port:                   2525,
				GreetingDelayPortStart: DefaultGreetingDelayStart,
				DropDelayPortStart:     DefaultDropDelayStart,
				TLSPort:                DefaultGreetingDelayStart + 2, // Inside greeting delay offsets
				STARTTLSPort:           DefaultSTARTTLSPort,
				TLSCertFile:            "/path/to/cert.pem",
				TLSKeyFile:             "/path/to/key.pem",
			},
			expectError: true,
			errorMsg:    "TLS port",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.config.ValidatePortConfiguration()
			if (err != nil) != test.expectError {
				t.Errorf("Expected error: %v, got: %v", test.expectError, err)
			}
			if err != nil && test.errorMsg != "" {
				if !strings.Contains(err.Error(), test.errorMsg) {
					t.Errorf("Expected error message to contain '%s', got: '%s'", test.errorMsg, err.Error())
				}
			}
		})
	}
}
