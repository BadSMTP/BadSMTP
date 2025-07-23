package smtp

import (
	"testing"
)

func TestNormaliseMailboxAndValidationASCII(t *testing.T) {
	cases := []struct {
		input  string
		valid  bool
		normal string
	}{
		{"user@example.com", true, "user@example.com"},
		{"User@Example.COM", true, "User@example.com"},
		{"user.name+tag@sub.example.co.uk", true, "user.name+tag@sub.example.co.uk"},
		{"invalid@@example.com", false, ""},
		{"no-at-sign", false, ""},
	}

	for _, c := range cases {
		ok := IsValidMailbox(c.input, false)
		if ok != c.valid {
			t.Fatalf("IsValidMailbox(%q, false) = %v; want %v", c.input, ok, c.valid)
		}
		if c.valid {
			n := NormaliseMailbox(c.input)
			if n != c.normal {
				t.Fatalf("NormaliseMailbox(%q) = %q; want %q", c.input, n, c.normal)
			}
		}
	}
}

func TestIsValidMailboxUTF8Local(t *testing.T) {
	// local part with a Unicode character
	validUTF8 := "usér@例え.jp"
	if IsValidMailbox(validUTF8, false) {
		t.Fatalf("expected IsValidMailbox(%q, false) to be false (no UTF8 allowed)", validUTF8)
	}
	if !IsValidMailbox(validUTF8, true) {
		t.Fatalf("expected IsValidMailbox(%q, true) to be true (UTF8 allowed)", validUTF8)
	}
}

func TestExtractMailboxFromArgVariations(t *testing.T) {
	cases := []struct {
		arg      string
		expected string
	}{
		{"FROM:<user@example.com>", "user@example.com"},
		{"TO:<User@Example.COM>", "User@Example.COM"},
		{"Alice <alice@example.org>", "alice@example.org"},
		{"bob@example.net", "bob@example.net"},
	}

	for _, c := range cases {
		addr := ExtractMailboxFromArg(c.arg)
		if addr != c.expected {
			t.Fatalf("ExtractMailboxFromArg(%q) = %q; want %q", c.arg, addr, c.expected)
		}
	}
}

func TestQuotedLocalPart(t *testing.T) {
	cases := []struct {
		input        string
		expectedNorm string
	}{
		{"\"me@home\"@example.com", "\"me@home\"@example.com"},
		{"\"Name With Space\"@EXAMPLE.COM", "\"Name With Space\"@example.com"},
	}

	for _, c := range cases {
		if !IsValidMailbox(c.input, false) {
			t.Fatalf("IsValidMailbox(%q, false) = false; want true", c.input)
		}
		n := NormaliseMailbox(c.input)
		if n != c.expectedNorm {
			t.Fatalf("NormaliseMailbox(%q) = %q; want %q", c.input, n, c.expectedNorm)
		}
	}
}
