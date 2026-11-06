package smtp

import (
	"strings"
	"testing"
)

func TestDecodeXtext(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "plain", input: "168.100.189.2", want: "168.100.189.2"},
		{name: "plus escape", input: "1+2B2", want: "1+2"},
		{name: "hex escape", input: "a+41b", want: "aAb"},
		{name: "truncated escape", input: "ab+4", wantErr: true},
		{name: "invalid hex", input: "ab+zz", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeXtext(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseXclientArgs(t *testing.T) {
	attrs, err := ParseXclientArgs([]string{"NAME=spike.porcupine.org", "ADDR=168.100.189.2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attrs["NAME"] != "spike.porcupine.org" {
		t.Errorf("NAME = %q", attrs["NAME"])
	}
	if attrs["ADDR"] != "168.100.189.2" {
		t.Errorf("ADDR = %q", attrs["ADDR"])
	}

	if _, err := ParseXclientArgs([]string{"NAME"}); err == nil {
		t.Error("expected error for attribute without =")
	}
	if _, err := ParseXclientArgs([]string{"BOGUS=1"}); err == nil {
		t.Error("expected error for unsupported attribute")
	}
	if _, err := ParseXclientArgs([]string{"NAME=a", "NAME=b"}); err == nil {
		t.Error("expected error for duplicate attribute")
	}
}

func TestIsXclientUnavailable(t *testing.T) {
	for _, v := range []string{"[UNAVAILABLE]", "[unavailable]", "[TEMPUNAVAIL]", "[tempunavail]"} {
		if !IsXclientUnavailable(v) {
			t.Errorf("%q should be unavailable", v)
		}
	}
	if IsXclientUnavailable("1.2.3.4") {
		t.Error("plain address should not be unavailable")
	}
}

func TestValidateXclientPort(t *testing.T) {
	if err := ValidateXclientPort("12345"); err != nil {
		t.Errorf("valid port rejected: %v", err)
	}
	if err := ValidateXclientPort("0"); err != nil {
		t.Errorf("port 0 rejected: %v", err)
	}
	for _, bad := range []string{"-1", "65536", "abc", ""} {
		if err := ValidateXclientPort(bad); err == nil {
			t.Errorf("invalid port %q accepted", bad)
		}
	}
}

func TestValidateXclientAddr(t *testing.T) {
	if err := ValidateXclientAddr("168.100.189.2"); err != nil {
		t.Errorf("IPv4 rejected: %v", err)
	}
	if err := ValidateXclientAddr("IPV6:2001:db8::1"); err != nil {
		t.Errorf("IPv6 rejected: %v", err)
	}
	if err := ValidateXclientAddr("ipv6:2001:db8::1"); err != nil {
		t.Errorf("lowercase IPV6 prefix rejected: %v", err)
	}
	for _, bad := range []string{"2001:db8::1", "1.2.3", "1.2.3.4.5", "abc"} {
		if err := ValidateXclientAddr(bad); err == nil {
			t.Errorf("invalid addr %q accepted", bad)
		}
	}
}

func TestValidateXclientProto(t *testing.T) {
	if err := ValidateXclientProto("SMTP"); err != nil {
		t.Errorf("SMTP rejected: %v", err)
	}
	if err := ValidateXclientProto("esmtp"); err != nil {
		t.Errorf("esmtp rejected: %v", err)
	}
	if err := ValidateXclientProto("ESMTPX"); err == nil {
		t.Error("ESMTPX should be rejected")
	}
}

func TestXclientCommandStateRules(t *testing.T) {
	// XCLIENT is allowed outside a mail transaction. StateMail is the
	// post-EHLO idle state (before MAIL FROM), so it is allowed.
	for _, st := range []State{StateGreeting, StateHelo, StateAuth, StateMail, StateQuit} {
		cmd := &Command{Name: CmdXCLIENT, Args: []string{"ADDR=1.2.3.4"}}
		if !cmd.IsAllowedInState(st) {
			t.Errorf("XCLIENT should be allowed in %s", st)
		}
	}
	// XCLIENT is rejected in the middle of a mail transaction (after MAIL FROM
	// until DOT/RSET). StateMail (post-EHLO idle) is allowed.
	for _, st := range []State{StateRcpt, StateData, StateBdat} {
		cmd := &Command{Name: CmdXCLIENT, Args: []string{"ADDR=1.2.3.4"}}
		if cmd.IsAllowedInState(st) {
			t.Errorf("XCLIENT should be rejected in %s", st)
		}
	}
}

func TestXclientCommandRecognition(t *testing.T) {
	line := strings.ToUpper("xclient ADDR=1.2.3.4 PORT=12345")
	cmd, err := ParseCommand(line)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !cmd.IsValid() {
		t.Error("XCLIENT should be a valid command")
	}
}
