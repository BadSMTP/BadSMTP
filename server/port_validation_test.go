package server

import (
	"testing"
)

func TestValidatePortConfigurationRejectsOutOfRangePort(t *testing.T) {
	cfg := &Config{
		Port:                   DefaultGreetingDelayStart + DelayCount + 1, // offset > DelayCount-1
		GreetingDelayPortStart: DefaultGreetingDelayStart,
		DropDelayPortStart:     DefaultDropDelayStart,
		TLSPort:                DefaultTLSPort,
		STARTTLSPort:           DefaultSTARTTLSPort,
	}

	if err := cfg.ValidatePortConfiguration(); err == nil {
		t.Fatalf("Expected error for out-of-range port within greeting base, but got nil")
	}
}

func TestValidatePortConfigurationAcceptsValidConfig(t *testing.T) {
	cfg := &Config{
		Port:                   2525,
		GreetingDelayPortStart: DefaultGreetingDelayStart,
		DropDelayPortStart:     DefaultDropDelayStart,
		TLSPort:                DefaultTLSPort,
		STARTTLSPort:           DefaultSTARTTLSPort,
	}

	if err := cfg.ValidatePortConfiguration(); err != nil {
		t.Fatalf("Expected valid config to pass validation, got error: %v", err)
	}
}
