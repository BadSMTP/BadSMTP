package smtp

import (
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input    string
		expected *Command
		hasError bool
	}{
		{
			input:    "HELO example.com",
			expected: &Command{Name: "HELO", Args: []string{"example.com"}},
			hasError: false,
		},
		{
			input:    "EHLO mail.example.com",
			expected: &Command{Name: "EHLO", Args: []string{"mail.example.com"}},
			hasError: false,
		},
		{
			input:    "MAIL FROM:<user@example.com>",
			expected: &Command{Name: "MAIL", Args: []string{"FROM:<user@example.com>"}},
			hasError: false,
		},
		{
			input:    "RCPT TO:<recipient@example.com>",
			expected: &Command{Name: "RCPT", Args: []string{"TO:<recipient@example.com>"}},
			hasError: false,
		},
		{
			input:    "DATA",
			expected: &Command{Name: "DATA", Args: []string{}},
			hasError: false,
		},
		{
			input:    "QUIT",
			expected: &Command{Name: "QUIT", Args: []string{}},
			hasError: false,
		},
		{
			input:    "AUTH PLAIN dGVzdA==",
			expected: &Command{Name: "AUTH", Args: []string{"PLAIN", "dGVzdA=="}},
			hasError: false,
		},
		{
			input:    "STARTTLS",
			expected: &Command{Name: "STARTTLS", Args: []string{}},
			hasError: false,
		},
		{
			input:    "RSET",
			expected: &Command{Name: "RSET", Args: []string{}},
			hasError: false,
		},
		{
			input:    "NOOP",
			expected: &Command{Name: "NOOP", Args: []string{}},
			hasError: false,
		},
		{
			input:    "",
			expected: nil,
			hasError: true,
		},
		{
			input:    "   ",
			expected: nil,
			hasError: true,
		},
		{
			input:    "MAIL FROM:<user@example.com> SIZE=1024",
			expected: &Command{Name: "MAIL", Args: []string{"FROM:<user@example.com>", "SIZE=1024"}},
			hasError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			cmd, err := ParseCommand(test.input)

			if test.hasError {
				if err == nil {
					t.Errorf("Expected error for input '%s'", test.input)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for input '%s': %v", test.input, err)
				}
				if cmd == nil {
					t.Errorf("Expected command for input '%s', got nil", test.input)
					return
				}
				if cmd.Name != test.expected.Name {
					t.Errorf("Expected command name '%s', got '%s'", test.expected.Name, cmd.Name)
				}
				if len(cmd.Args) != len(test.expected.Args) {
					t.Errorf("Expected %d args, got %d", len(test.expected.Args), len(cmd.Args))
				}
				for i, arg := range cmd.Args {
					if i < len(test.expected.Args) && arg != test.expected.Args[i] {
						t.Errorf("Expected arg %d to be '%s', got '%s'", i, test.expected.Args[i], arg)
					}
				}
			}
		})
	}
}

func TestCommandIsValid(t *testing.T) {
	tests := []struct {
		command  *Command
		expected bool
	}{
		{&Command{Name: "HELO", Args: []string{"example.com"}}, true},
		{&Command{Name: "EHLO", Args: []string{"example.com"}}, true},
		{&Command{Name: "MAIL", Args: []string{"FROM:<user@example.com>"}}, true},
		{&Command{Name: "RCPT", Args: []string{"TO:<user@example.com>"}}, true},
		{&Command{Name: "DATA", Args: []string{}}, true},
		{&Command{Name: "QUIT", Args: []string{}}, true},
		{&Command{Name: "AUTH", Args: []string{"PLAIN"}}, true},
		{&Command{Name: "STARTTLS", Args: []string{}}, true},
		{&Command{Name: "RSET", Args: []string{}}, true},
		{&Command{Name: "NOOP", Args: []string{}}, true},
		{&Command{Name: "INVALID", Args: []string{}}, false},
		{&Command{Name: "", Args: []string{}}, false},
		{&Command{Name: "helo", Args: []string{"example.com"}}, false}, // lowercase
	}

	for _, test := range tests {
		t.Run(test.command.Name, func(t *testing.T) {
			result := test.command.IsValid()
			if result != test.expected {
				t.Errorf("IsValid() for command '%s' = %v, expected %v", test.command.Name, result, test.expected)
			}
		})
	}
}

func TestCommandValidateArgs(t *testing.T) {
	tests := []struct {
		command  *Command
		state    State
		hasError bool
		errorMsg string
	}{
		// HELO/EHLO validation
		{&Command{Name: "HELO", Args: []string{"example.com"}}, StateHelo, false, ""},
		{&Command{Name: "HELO", Args: []string{}}, StateHelo, true, "501 Syntax error in parameters"},
		{&Command{Name: "EHLO", Args: []string{"example.com"}}, StateHelo, false, ""},
		{&Command{Name: "EHLO", Args: []string{}}, StateHelo, true, "501 Syntax error in parameters"},

		// MAIL validation
		{&Command{Name: "MAIL", Args: []string{"FROM:<user@example.com>"}}, StateMail, false, ""},
		{&Command{Name: "MAIL", Args: []string{}}, StateMail, true, "501 Syntax error in parameters"},
		{&Command{Name: "MAIL", Args: []string{"TO:<user@example.com>"}}, StateMail, true, "501 Syntax error in parameters"},

		// RCPT validation
		{&Command{Name: "RCPT", Args: []string{"TO:<user@example.com>"}}, StateRcpt, false, ""},
		{&Command{Name: "RCPT", Args: []string{}}, StateRcpt, true, "501 Syntax error in parameters"},
		{&Command{Name: "RCPT", Args: []string{"FROM:<user@example.com>"}}, StateRcpt, true, "501 Syntax error in parameters"},

		// AUTH validation
		{&Command{Name: "AUTH", Args: []string{"PLAIN"}}, StateMail, false, ""},
		{&Command{Name: "AUTH", Args: []string{}}, StateMail, true, "501 Syntax error in parameters"},

		// Commands that don't require arguments
		{&Command{Name: "DATA", Args: []string{}}, StateData, false, ""},
		{&Command{Name: "QUIT", Args: []string{}}, StateHelo, false, ""},
		{&Command{Name: "RSET", Args: []string{}}, StateMail, false, ""},
		{&Command{Name: "NOOP", Args: []string{}}, StateHelo, false, ""},
		{&Command{Name: "STARTTLS", Args: []string{}}, StateHelo, false, ""},
	}

	for _, test := range tests {
		t.Run(test.command.Name, func(t *testing.T) {
			err := test.command.ValidateArgs()

			if test.hasError {
				if err == nil {
					t.Errorf("Expected error for command '%s' with args %v", test.command.Name, test.command.Args)
				} else if test.errorMsg != "" && err.Error() != test.errorMsg {
					t.Errorf("Expected error message '%s', got '%s'", test.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for command '%s' with args %v: %v", test.command.Name, test.command.Args, err)
				}
			}
		})
	}
}

func TestCommandValidation(t *testing.T) {
	// Test various command format validations
	tests := []struct {
		name     string
		input    string
		hasError bool
	}{
		{"Valid HELO", "HELO example.com", false},
		{"Valid EHLO", "EHLO mail.example.com", false},
		{"Valid MAIL", "MAIL FROM:<user@example.com>", false},
		{"Valid RCPT", "RCPT TO:<user@example.com>", false},
		{"Valid AUTH with mechanism", "AUTH PLAIN", false},
		{"Valid AUTH with data", "AUTH PLAIN dGVzdA==", false},
		{"Command with extra spaces", "HELO   example.com", false},
		{"Command with mixed case", "HeLo example.com", false}, // Should be normalised
		{"Empty command", "", true},
		{"Only spaces", "   ", true},
		{"Command without required args", "HELO", false},      // Parsing succeeds, validation fails
		{"MAIL without FROM", "MAIL user@example.com", false}, // Parsing succeeds, validation fails
		{"RCPT without TO", "RCPT user@example.com", false},   // Parsing succeeds, validation fails
		{"AUTH without mechanism", "AUTH", false},             // Parsing succeeds, validation fails
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd, err := ParseCommand(test.input)

			if test.hasError {
				if err == nil {
					t.Errorf("Expected error for input '%s'", test.input)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for input '%s': %v", test.input, err)
				}
				if cmd == nil {
					t.Errorf("Expected command for input '%s', got nil", test.input)
					return
				}
				if !cmd.IsValid() {
					t.Errorf("Parsed command should be valid for input '%s'", test.input)
				}
			}
		})
	}
}

func TestCommandArgumentParsing(t *testing.T) {
	tests := []struct {
		input        string
		expectedArgs []string
	}{
		{
			"MAIL FROM:<user@example.com>",
			[]string{"FROM:<user@example.com>"},
		},
		{
			"MAIL FROM:<user@example.com> SIZE=1024",
			[]string{"FROM:<user@example.com>", "SIZE=1024"},
		},
		{
			"AUTH PLAIN dGVzdA==",
			[]string{"PLAIN", "dGVzdA=="},
		},
		{
			"EHLO [192.168.1.1]",
			[]string{"[192.168.1.1]"},
		},
		{
			"HELO example.com",
			[]string{"example.com"},
		},
		{
			"DATA",
			[]string{},
		},
		{
			"QUIT",
			[]string{},
		},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			cmd, err := ParseCommand(test.input)
			if err != nil {
				t.Fatalf("Unexpected error parsing command '%s': %v", test.input, err)
			}

			if len(cmd.Args) != len(test.expectedArgs) {
				t.Errorf("Expected %d args, got %d", len(test.expectedArgs), len(cmd.Args))
			}

			for i, expected := range test.expectedArgs {
				if i >= len(cmd.Args) {
					t.Errorf("Missing arg %d: expected '%s'", i, expected)
				} else if cmd.Args[i] != expected {
					t.Errorf("Arg %d: expected '%s', got '%s'", i, expected, cmd.Args[i])
				}
			}
		})
	}
}

func TestCommandEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		hasError bool
	}{
		{"Command with tab separator", "HELO\texample.com", false},
		{"Command with multiple spaces", "HELO     example.com", false},
		{"Command with trailing spaces", "HELO example.com   ", false},
		{"Command with leading spaces", "   HELO example.com", false},
		{"Very long hostname", "HELO " + strings.Repeat("a", 1000) + ".com", false},
		{"Command with special characters", "HELO example-example.net", false},
		{"Command with IP address", "HELO [192.168.1.1]", false},
		{"Command with IPv6 address", "HELO [::1]", false},
		{"Only command name", "QUIT", false},
		{"Command with newline", "HELO example.com\n", false},
		{"Command with carriage return", "HELO example.com\r", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd, err := ParseCommand(test.input)

			if test.hasError {
				if err == nil {
					t.Errorf("Expected error for input '%s'", test.input)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for input '%s': %v", test.input, err)
				}
				if cmd == nil {
					t.Errorf("Expected command for input '%s', got nil", test.input)
				}
			}
		})
	}
}

func TestValidateDomain(t *testing.T) {
	tests := []struct {
		name     string
		domain   string
		expected bool
	}{
		// Valid ASCII domains
		{"Simple domain", "example.com", true},
		{"Subdomain", "mail.example.com", true},
		{"Multiple subdomains", "a.b.c.example.com", true},
		{"Domain with hyphen", "my-site.com", true},
		{"Domain with multiple hyphens", "my-test-site.example.com", true},
		{"Single letter domain", "x.com", true},
		{"Numeric TLD", "example.123", true},
		{"Mixed alphanumeric", "test123.example456.com", true},

		// Valid internationalised domains (UTF-8)
		{"Japanese domain", "例え.jp", true},
		{"Chinese domain", "中国.cn", true},
		{"Arabic domain", "مثال.السعودية", true},
		{"Russian domain", "пример.рф", true},
		{"German umlaut", "münchen.de", true},
		{"French accents", "françois.fr", true},
		{"Mixed Latin/Japanese", "test.例え.jp", true},

		// Invalid domains
		{"Empty string", "", false},
		{"Only dot", ".", false},
		{"Starting with dot", ".example.com", false},
		{"Ending with dot", "example.com.", false},
		{"Double dot", "example..com", false},
		{"Starting with hyphen", "-example.com", false},
		{"Ending with hyphen", "example-.com", false},
		{"Hyphen at label start", "test.-example.com", false},
		{"Hyphen at label end", "test.example-.com", false},
		{"Only at sign", "@", false},
		{"Space in domain", "example .com", false},
		{"Underscore", "example_example.net", false},
		{"Special characters", "example!.com", false},
		{"Too long", strings.Repeat("a", 256), false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ValidateDomain(test.domain)
			if result != test.expected {
				t.Errorf("ValidateDomain(%q) = %v, expected %v", test.domain, result, test.expected)
			}
		})
	}
}

func TestValidateEmailAddress(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		expected bool
	}{
		// Valid ASCII emails
		{"Simple email", "user@example.com", true},
		{"Email with dots", "user.name@example.com", true},
		{"Email with plus", "user+tag@example.com", true},
		{"Email with hyphen", "user-name@example.com", true},
		{"Email with numbers", "user123@example456.com", true},
		{"Email with underscore", "user_name@example.com", true},
		{"Complex local part", "user.name+tag@mail.example.com", true},

		// Valid internationalised emails
		{"Email with Japanese domain", "user@例え.jp", true},
		{"Email with Chinese domain", "test@中国.cn", true},
		{"Email with Arabic domain", "user@مثال.السعودية", true},
		{"Email with Russian domain", "test@пример.рф", true},
		{"Email with umlaut domain", "user@münchen.de", true},

		// Invalid emails
		{"Empty string", "", false},
		{"No at sign", "userexample.com", false},
		{"Multiple at signs", "user@@example.com", false},
		{"No local part", "@example.com", false},
		{"No domain", "user@", false},
		{"Space in local", "user name@example.com", false},
		{"Space in domain", "user@example .com", false},
		{"Invalid domain", "user@-example.com", false},
		{"Local too long", strings.Repeat("a", 65) + "@example.com", false},
		{"Domain too long", "user@" + strings.Repeat("a", 256), false},
		{"Backslash in local", "user\\name@example.com", false},
		{"Parentheses in local", "user(name)@example.com", false},
		{"Brackets in local", "user[name]@example.com", false},
		{"Only at sign", "@", false},
		{"Double at", "user@domain@example.com", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ValidateEmailAddress(test.email)
			if result != test.expected {
				t.Errorf("ValidateEmailAddress(%q) = %v, expected %v", test.email, result, test.expected)
			}
		})
	}
}

func TestExtractEmailAddress(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Note: ExtractEmailAddress preserves the original case of the email address
		{"With angle brackets", "<user@example.com>", "user@example.com"},
		{"With FROM prefix", "FROM:<user@example.com>", "user@example.com"},
		{"With TO prefix", "TO:<user@example.com>", "user@example.com"},
		{"Without brackets", "user@example.com", "user@example.com"},
		{"Mixed case prefix", "From:<user@example.com>", "user@example.com"},
		{"With lowercase prefix", "from:<user@example.com>", "user@example.com"},
		{"Plain address", "test@domain.org", "test@domain.org"},
		{"Complex address", "FROM:<user+tag@mail.example.com>", "user+tag@mail.example.com"},
		{"Mixed case email", "FROM:<User@Example.COM>", "User@Example.COM"},
		{"Uppercase email", "FROM:<USER@EXAMPLE.COM>", "USER@EXAMPLE.COM"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractEmailAddress(test.input)
			if result != test.expected {
				t.Errorf("ExtractEmailAddress(%q) = %q, expected %q", test.input, result, test.expected)
			}
		})
	}
}
