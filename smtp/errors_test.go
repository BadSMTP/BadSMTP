//nolint:dupl
package smtp

import (
	"testing"
)

func TestGetErrorMessage(t *testing.T) {
	tests := []struct {
		code     int
		expected string
	}{
		{421, "Service not available, closing transmission channel"},
		{450, "Requested mail action not taken: mailbox unavailable"},
		{451, "Requested action aborted: local error in processing"},
		{452, "Requested action not taken: insufficient system storage"},
		{500, "Syntax error, command unrecognized"}, //nolint:misspell // RFC 5321 uses US spelling
		{501, "Syntax error in parameters or arguments"},
		{502, "Command not implemented"},
		{503, "Bad sequence of commands"},
		{504, "Command parameter not implemented"},
		{535, "Authentication failed"},
		{550, "Requested action not taken: mailbox unavailable"},
		{551, "User not local; please try forward path"},
		{552, "Requested mail action aborted: exceeded storage allocation"},
		{553, "Requested action not taken: mailbox name not allowed"},
		{554, "Transaction failed"},
		{571, "Blocked - see https://example.com/blocked"},
		{999, "Unknown error"},
		{0, "Unknown error"},
		{-1, "Unknown error"},
	}

	for _, test := range tests {
		t.Run(string(rune(test.code)), func(t *testing.T) {
			result := GetErrorMessage(test.code)
			if result != test.expected {
				t.Errorf("GetErrorMessage(%d) = %s, expected %s", test.code, result, test.expected)
			}
		})
	}
}

// Tests for verb-prefixed error extraction functions
func TestExtractMailFromError(t *testing.T) {
	tests := []struct {
		name         string
		email        string
		expectedCode int
		expectedExt  string
		shouldBeNil  bool
	}{
		{"Basic mail error", "mail452@example.com", 452, "", false},
		{"Mail with extended code", "mail550_5.7.1@example.com", 550, "5.7.1", false},
		{"Multi-digit extended code", "mail550_5.7.509@example.com", 550, "5.7.509", false},
		{"Multi-digit subject", "mail550_5.75.1@example.com", 550, "5.75.1", false},
		{"All multi-digit", "mail550_12.345.6789@example.com", 550, "12.345.6789", false},
		{"Different domain allowed", "mail421@example.org", 421, "", false},
		{"Case insensitive", "MAIL500@EXAMPLE.COM", 500, "", false},
		{"No mail prefix", "test452@example.com", 0, "", true},
		{"Wrong prefix", "rcpt452@example.com", 0, "", true},
		{"No @ symbol", "mail452", 0, "", true},
		{"Empty string", "", 0, "", true},
		{"Invalid extended format", "mail550_57@example.com", 0, "", true},
		{"Too few error digits", "mail45@example.com", 0, "", true},
		{"Too many error digits", "mail4521@example.com", 0, "", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractMailFromError(test.email)
			if test.shouldBeNil {
				if result != nil {
					t.Errorf("ExtractMailFromError(%s) should return nil but got %+v", test.email, result)
				}
			} else {
				if result == nil {
					t.Errorf("ExtractMailFromError(%s) should not return nil", test.email)
					return
				}
				if result.Code != test.expectedCode {
					t.Errorf("ExtractMailFromError(%s).Code = %d, expected %d", test.email, result.Code, test.expectedCode)
				}
				if result.Enhanced != test.expectedExt {
					t.Errorf("ExtractMailFromError(%s).Enhanced = '%s', expected '%s'", test.email, result.Enhanced, test.expectedExt)
				}
			}
		})
	}
}

func TestExtractRcptToError(t *testing.T) {
	tests := []struct {
		name         string
		email        string
		expectedCode int
		expectedExt  string
		shouldBeNil  bool
	}{
		{"Basic rcpt error", "rcpt452@example.com", 452, "", false},
		{"Rcpt with extended code", "rcpt550_5.7.1@example.com", 550, "5.7.1", false},
		{"Different domain allowed", "rcpt421@example.org", 421, "", false},
		{"Case insensitive", "RCPT500@EXAMPLE.COM", 500, "", false},
		{"No rcpt prefix", "test452@example.com", 0, "", true},
		{"Wrong prefix", "mail452@example.com", 0, "", true},
		{"No @ symbol", "rcpt452", 0, "", true},
		{"Empty string", "", 0, "", true},
		{"Invalid extended format", "rcpt550_57@example.com", 0, "", true},
		{"Too many extended digits", "rcpt550_5712@example.com", 0, "", true},
		{"Too few error digits", "rcpt45@example.com", 0, "", true},
		{"Too many error digits", "rcpt4521@example.com", 0, "", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractRcptToError(test.email)
			if test.shouldBeNil {
				if result != nil {
					t.Errorf("ExtractRcptToError(%s) should return nil but got %+v", test.email, result)
				}
			} else {
				if result == nil {
					t.Errorf("ExtractRcptToError(%s) should not return nil", test.email)
					return
				}
				if result.Code != test.expectedCode {
					t.Errorf("ExtractRcptToError(%s).Code = %d, expected %d", test.email, result.Code, test.expectedCode)
				}
				if result.Enhanced != test.expectedExt {
					t.Errorf("ExtractRcptToError(%s).Enhanced = '%s', expected '%s'", test.email, result.Enhanced, test.expectedExt)
				}
			}
		})
	}
}

func TestExtractDataError(t *testing.T) {
	tests := []struct {
		name         string
		email        string
		expectedCode int
		expectedExt  string
		shouldBeNil  bool
	}{
		{"Basic data error", "data552@example.com", 552, "", false},
		{"Data with extended code", "data550_5.7.1@example.com", 550, "5.7.1", false},
		{"Different domain allowed", "data421@example.org", 421, "", false},
		{"Case insensitive", "DATA500@EXAMPLE.COM", 500, "", false},
		{"No data prefix", "test552@example.com", 0, "", true},
		{"Wrong prefix", "mail552@example.com", 0, "", true},
		{"No @ symbol", "data552", 0, "", true},
		{"Empty string", "", 0, "", true},
		{"Invalid extended format", "data550_57@example.com", 0, "", true},
		{"Too many extended digits", "data550_5712@example.com", 0, "", true},
		{"Too few error digits", "data55@example.com", 0, "", true},
		{"Too many error digits", "data5521@example.com", 0, "", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractDataError(test.email)
			if test.shouldBeNil {
				if result != nil {
					t.Errorf("ExtractDataError(%s) should return nil but got %+v", test.email, result)
				}
			} else {
				if result == nil {
					t.Errorf("ExtractDataError(%s) should not return nil", test.email)
					return
				}
				if result.Code != test.expectedCode {
					t.Errorf("ExtractDataError(%s).Code = %d, expected %d", test.email, result.Code, test.expectedCode)
				}
				if result.Enhanced != test.expectedExt {
					t.Errorf("ExtractDataError(%s).Enhanced = '%s', expected '%s'", test.email, result.Enhanced, test.expectedExt)
				}
			}
		})
	}
}

func TestExtractHeloError(t *testing.T) {
	tests := []struct {
		name         string
		hostname     string
		expectedCode int
		shouldBeNil  bool
	}{
		{"Basic helo error", "helo500.example.com", 500, false},
		{"Basic ehlo error", "ehlo502.example.org", 502, false},
		{"Case insensitive", "HELO421.EXAMPLE.COM", 421, false},
		{"Mixed case ehlo", "Ehlo501.Test.Org", 501, false},
		{"No prefix", "test500.example.com", 0, true},
		{"Wrong prefix", "mail500.example.com", 0, true},
		{"No dot after code", "helo500example.com", 0, true},
		{"Empty string", "", 0, true},
		{"Too few digits", "helo50.example.com", 0, true},
		{"Too many digits", "helo5001.example.com", 0, true},
		{"Letters in code", "helo5a0.example.com", 0, true},
		{"Just hostname", "example.com", 0, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractHeloError(test.hostname)
			if test.shouldBeNil {
				if result != nil {
					t.Errorf("ExtractHeloError(%s) should return nil but got %+v", test.hostname, result)
				}
			} else {
				if result == nil {
					t.Errorf("ExtractHeloError(%s) should not return nil", test.hostname)
					return
				}
				if result.Code != test.expectedCode {
					t.Errorf("ExtractHeloError(%s).Code = %d, expected %d", test.hostname, result.Code, test.expectedCode)
				}
			}
		})
	}
}

func TestExtractRsetError(t *testing.T) {
	tests := []struct {
		name         string
		email        string
		expectedCode int
		expectedExt  string
		shouldBeNil  bool
	}{
		{"Basic rset error", "rset421@example.com", 421, "", false},
		{"Rset with extended code", "rset421_4.5.1@example.com", 421, "4.5.1", false},
		{"Different domain allowed", "rset250@example.org", 250, "", false},
		{"Case insensitive", "RSET500@EXAMPLE.COM", 500, "", false},
		{"No rset prefix", "test421@example.com", 0, "", true},
		{"Wrong prefix", "mail421@example.com", 0, "", true},
		{"No @ symbol", "rset421", 0, "", true},
		{"Empty string", "", 0, "", true},
		{"Too few error digits", "rset42@example.com", 0, "", true},
		{"Too many error digits", "rset4211@example.com", 0, "", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractRsetError(test.email)
			if test.shouldBeNil {
				if result != nil {
					t.Errorf("ExtractRsetError(%s) should return nil but got %+v", test.email, result)
				}
			} else {
				if result == nil {
					t.Errorf("ExtractRsetError(%s) should not return nil", test.email)
					return
				}
				if result.Code != test.expectedCode {
					t.Errorf("ExtractRsetError(%s).Code = %d, expected %d", test.email, result.Code, test.expectedCode)
				}
				if result.Enhanced != test.expectedExt {
					t.Errorf("ExtractRsetError(%s).Enhanced = '%s', expected '%s'", test.email, result.Enhanced, test.expectedExt)
				}
			}
		})
	}
}

func TestExtractQuitError(t *testing.T) {
	tests := []struct {
		name         string
		email        string
		expectedCode int
		expectedExt  string
		shouldBeNil  bool
	}{
		{"Basic quit error", "quit421@example.com", 421, "", false},
		{"Quit with extended code", "quit421_4.2.1@example.com", 421, "4.2.1", false},
		{"Different domain allowed", "quit221@example.org", 221, "", false},
		{"Case insensitive", "QUIT500@EXAMPLE.COM", 500, "", false},
		{"No quit prefix", "test221@example.com", 0, "", true},
		{"Wrong prefix", "mail421@example.com", 0, "", true},
		{"No @ symbol", "quit421", 0, "", true},
		{"Empty string", "", 0, "", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractQuitError(test.email)
			if test.shouldBeNil {
				if result != nil {
					t.Errorf("ExtractQuitError(%s) should return nil but got %+v", test.email, result)
				}
			} else {
				if result == nil {
					t.Errorf("ExtractQuitError(%s) should not return nil", test.email)
					return
				}
				if result.Code != test.expectedCode {
					t.Errorf("ExtractQuitError(%s).Code = %d, expected %d", test.email, result.Code, test.expectedCode)
				}
				if result.Enhanced != test.expectedExt {
					t.Errorf("ExtractQuitError(%s).Enhanced = '%s', expected '%s'", test.email, result.Enhanced, test.expectedExt)
				}
			}
		})
	}
}

func TestExtractStartTLSError(t *testing.T) {
	tests := []struct {
		name         string
		email        string
		expectedCode int
		expectedExt  string
		shouldBeNil  bool
	}{
		{"Basic starttls error", "starttls454@example.com", 454, "", false},
		{"Starttls with extended code", "starttls454_4.7.1@example.com", 454, "4.7.1", false},
		{"Different domain allowed", "starttls500@example.org", 500, "", false},
		{"Case insensitive", "STARTTLS421@EXAMPLE.COM", 421, "", false},
		{"No starttls prefix", "test454@example.com", 0, "", true},
		{"Wrong prefix", "mail454@example.com", 0, "", true},
		{"No @ symbol", "starttls454", 0, "", true},
		{"Empty string", "", 0, "", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractStartTLSError(test.email)
			if test.shouldBeNil {
				if result != nil {
					t.Errorf("ExtractStartTLSError(%s) should return nil but got %+v", test.email, result)
				}
			} else {
				if result == nil {
					t.Errorf("ExtractStartTLSError(%s) should not return nil", test.email)
					return
				}
				if result.Code != test.expectedCode {
					t.Errorf("ExtractStartTLSError(%s).Code = %d, expected %d", test.email, result.Code, test.expectedCode)
				}
				if result.Enhanced != test.expectedExt {
					t.Errorf("ExtractStartTLSError(%s).Enhanced = '%s', expected '%s'", test.email, result.Enhanced, test.expectedExt)
				}
			}
		})
	}
}

func TestExtractNoopError(t *testing.T) {
	tests := []struct {
		name         string
		email        string
		expectedCode int
		expectedExt  string
		shouldBeNil  bool
	}{
		{"Basic noop error", "noop421@example.com", 421, "", false},
		{"Noop with extended code", "noop421_4.5.0@example.com", 421, "4.5.0", false},
		{"Different domain allowed", "noop250@example.org", 250, "", false},
		{"Case insensitive", "NOOP500@EXAMPLE.COM", 500, "", false},
		{"No noop prefix", "test421@example.com", 0, "", true},
		{"Wrong prefix", "mail421@example.com", 0, "", true},
		{"No @ symbol", "noop421", 0, "", true},
		{"Empty string", "", 0, "", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractNoopError(test.email)
			if test.shouldBeNil {
				if result != nil {
					t.Errorf("ExtractNoopError(%s) should return nil but got %+v", test.email, result)
				}
			} else {
				if result == nil {
					t.Errorf("ExtractNoopError(%s) should not return nil", test.email)
					return
				}
				if result.Code != test.expectedCode {
					t.Errorf("ExtractNoopError(%s).Code = %d, expected %d", test.email, result.Code, test.expectedCode)
				}
				if result.Enhanced != test.expectedExt {
					t.Errorf("ExtractNoopError(%s).Enhanced = '%s', expected '%s'", test.email, result.Enhanced, test.expectedExt)
				}
			}
		})
	}
}

func TestExtractAuthError(t *testing.T) {
	tests := []struct {
		name         string
		email        string
		expectedCode int
		expectedExt  string
		shouldBeNil  bool
	}{
		{"Basic auth error", "auth535@example.com", 535, "", false},
		{"Auth with extended code", "auth535_7.3.0@example.com", 535, "7.3.0", false},
		{"Different domain allowed", "auth500@example.org", 500, "", false},
		{"Case insensitive", "AUTH535@EXAMPLE.COM", 535, "", false},
		{"No auth prefix", "test535@example.com", 0, "", true},
		{"Wrong prefix", "mail535@example.com", 0, "", true},
		{"No @ symbol", "auth535", 0, "", true},
		{"Empty string", "", 0, "", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractAuthError(test.email)
			if test.shouldBeNil {
				if result != nil {
					t.Errorf("ExtractAuthError(%s) should return nil but got %+v", test.email, result)
				}
			} else {
				if result == nil {
					t.Errorf("ExtractAuthError(%s) should not return nil", test.email)
					return
				}
				if result.Code != test.expectedCode {
					t.Errorf("ExtractAuthError(%s).Code = %d, expected %d", test.email, result.Code, test.expectedCode)
				}
				if result.Enhanced != test.expectedExt {
					t.Errorf("ExtractAuthError(%s).Enhanced = '%s', expected '%s'", test.email, result.Enhanced, test.expectedExt)
				}
			}
		})
	}
}
