package auth

import (
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// Mock connection for testing
type mockAuthConn struct {
	responses []string
	index     int
}

func newMockAuthConn(responses []string) *mockAuthConn {
	return &mockAuthConn{
		responses: responses,
		index:     0,
	}
}

func (m *mockAuthConn) Read(b []byte) (int, error) {
	if m.index >= len(m.responses) {
		return 0, fmt.Errorf("no more responses")
	}
	response := m.responses[m.index] + "\r\n"
	m.index++
	copy(b, response)
	return len(response), nil
}

func (m *mockAuthConn) Write(b []byte) (int, error) {
	return len(b), nil
}

func (m *mockAuthConn) Close() error                     { return nil }
func (m *mockAuthConn) LocalAddr() net.Addr              { return nil }
func (m *mockAuthConn) RemoteAddr() net.Addr             { return nil }
func (m *mockAuthConn) SetDeadline(time.Time) error      { return nil }
func (m *mockAuthConn) SetReadDeadline(time.Time) error  { return nil }
func (m *mockAuthConn) SetWriteDeadline(time.Time) error { return nil }

func TestNewHandler(t *testing.T) {
	tests := []struct {
		mechanism string
		expected  interface{}
	}{
		{"PLAIN", &PlainHandler{}},
		{"LOGIN", &LoginHandler{}},
		{"CRAM-MD5", &CramHandler{}},
		{"CRAM-SHA256", &CramHandler{}},
		{"XOAUTH2", &XOAuth2Handler{}},
		{"INVALID", nil},
	}

	for _, test := range tests {
		t.Run(test.mechanism, func(t *testing.T) {
			handler := NewHandler(test.mechanism)
			if test.expected == nil {
				if handler != nil {
					t.Errorf("Expected nil handler for mechanism %s", test.mechanism)
				}
			} else {
				if handler == nil {
					t.Errorf("Expected non-nil handler for mechanism %s", test.mechanism)
				}
				// Check type
				switch test.expected.(type) {
				case *PlainHandler:
					if _, ok := handler.(*PlainHandler); !ok {
						t.Errorf("Expected PlainHandler for mechanism %s", test.mechanism)
					}
				case *LoginHandler:
					if _, ok := handler.(*LoginHandler); !ok {
						t.Errorf("Expected LoginHandler for mechanism %s", test.mechanism)
					}
				case *CramHandler:
					if _, ok := handler.(*CramHandler); !ok {
						t.Errorf("Expected CramHandler for mechanism %s", test.mechanism)
					}
				case *XOAuth2Handler:
					if _, ok := handler.(*XOAuth2Handler); !ok {
						t.Errorf("Expected XOAuth2Handler for mechanism %s", test.mechanism)
					}
				}
			}
		})
	}
}

func TestPlainHandlerAuthenticate(t *testing.T) {
	handler := &PlainHandler{}

	tests := []struct {
		name     string
		args     []string
		expected string
		hasError bool
	}{
		{
			name:     "Valid PLAIN auth with good user",
			args:     []string{"AUTH", "PLAIN", base64.StdEncoding.EncodeToString([]byte("\x00goodauth@example.com\x00password"))},
			expected: "goodauth@example.com",
			hasError: false,
		},
		{
			name:     "Valid PLAIN auth with bad user",
			args:     []string{"AUTH", "PLAIN", base64.StdEncoding.EncodeToString([]byte("\x00badauth@example.com\x00password"))},
			expected: "badauth@example.com",
			hasError: false,
		},
		{
			name:     "Invalid base64",
			args:     []string{"AUTH", "PLAIN", "invalid-base64"},
			expected: "",
			hasError: true,
		},
		{
			name:     "Missing argument",
			args:     []string{"AUTH", "PLAIN"},
			expected: "",
			hasError: true,
		},
		{
			name:     "Invalid format",
			args:     []string{"AUTH", "PLAIN", base64.StdEncoding.EncodeToString([]byte("invalid-format"))},
			expected: "",
			hasError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := newMockAuthConn([]string{})
			username, err := handler.Authenticate(conn, test.args)

			if test.hasError {
				if err == nil {
					t.Errorf("Expected error for test %s", test.name)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for test %s: %v", test.name, err)
				}
				if username != test.expected {
					t.Errorf("Expected username %s, got %s", test.expected, username)
				}
			}
		})
	}
}

func TestLoginHandlerAuthenticate(t *testing.T) {
	handler := &LoginHandler{}

	tests := []struct {
		name      string
		args      []string
		responses []string
		expected  string
		hasError  bool
	}{
		{
			name: "Valid LOGIN auth with good user",
			args: []string{"AUTH", "LOGIN"},
			responses: []string{
				base64.StdEncoding.EncodeToString([]byte("goodauth@example.com")),
				base64.StdEncoding.EncodeToString([]byte("password")),
			},
			expected: "goodauth@example.com",
			hasError: false,
		},
		{
			name: "Valid LOGIN auth with bad user",
			args: []string{"AUTH", "LOGIN"},
			responses: []string{
				base64.StdEncoding.EncodeToString([]byte("badauth@example.com")),
				base64.StdEncoding.EncodeToString([]byte("password")),
			},
			expected: "badauth@example.com",
			hasError: false,
		},
		{
			name:      "Invalid base64 username",
			args:      []string{"AUTH", "LOGIN"},
			responses: []string{"invalid-base64", "password"},
			expected:  "",
			hasError:  true,
		},
		{
			name:      "LOGIN with username in args",
			args:      []string{"AUTH", "LOGIN", base64.StdEncoding.EncodeToString([]byte("user@example.com"))},
			responses: []string{base64.StdEncoding.EncodeToString([]byte("password"))},
			expected:  "user@example.com",
			hasError:  true, // This should fail because LOGIN mechanism expects interactive flow
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := newMockAuthConn(test.responses)
			username, err := handler.Authenticate(conn, test.args)

			if test.hasError {
				if err == nil {
					t.Errorf("Expected error for test %s", test.name)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for test %s: %v", test.name, err)
				}
				if username != test.expected {
					t.Errorf("Expected username %s, got %s", test.expected, username)
				}
			}
		})
	}
}

func TestCramHandlerAuthenticate(t *testing.T) {
	handler := &CramHandler{}

	tests := []struct {
		name      string
		args      []string
		responses []string
		expected  string
		hasError  bool
	}{
		{
			name:      "Valid CRAM-MD5 auth",
			args:      []string{"AUTH", "CRAM-MD5"},
			responses: []string{"dXNlckBleGFtcGxlLmNvbSBkdW1teWhhc2g="}, // base64 of "user@example.com dummyhash"
			expected:  "user@example.com",
			hasError:  false,
		},
		{
			name:      "Valid CRAM-SHA256 auth",
			args:      []string{"AUTH", "CRAM-SHA256"},
			responses: []string{"dXNlckBleGFtcGxlLmNvbSBkdW1teWhhc2g="}, // base64 of "user@example.com dummyhash"
			expected:  "user@example.com",
			hasError:  false,
		},
		{
			name:      "Invalid base64 response",
			args:      []string{"AUTH", "CRAM-MD5"},
			responses: []string{"invalid-base64"},
			expected:  "",
			hasError:  true,
		},
		{
			name:      "Invalid response format",
			args:      []string{"AUTH", "CRAM-MD5"},
			responses: []string{base64.StdEncoding.EncodeToString([]byte("invalid-format"))},
			expected:  "",
			hasError:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := newMockAuthConn(test.responses)
			username, err := handler.Authenticate(conn, test.args)

			if test.hasError {
				if err == nil {
					t.Errorf("Expected error for test %s", test.name)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for test %s: %v", test.name, err)
				}
				if username != test.expected {
					t.Errorf("Expected username %s, got %s", test.expected, username)
				}
			}
		})
	}
}

func TestXOAuth2HandlerAuthenticate(t *testing.T) {
	handler := &XOAuth2Handler{}

	tests := []struct {
		name      string
		args      []string
		responses []string
		expected  string
		hasError  bool
	}{
		{
			name:      "Valid XOAUTH2 auth",
			args:      []string{"AUTH", "XOAUTH2"},
			responses: []string{base64.StdEncoding.EncodeToString([]byte("user=user@example.com\x01auth=Bearer token123\x01\x01"))},
			expected:  "user@example.com",
			hasError:  false,
		},
		{
			name:      "XOAUTH2 with token in args",
			args:      []string{"AUTH", "XOAUTH2", base64.StdEncoding.EncodeToString([]byte("user=user@example.com\x01auth=Bearer token123\x01\x01"))},
			responses: []string{},
			expected:  "user@example.com",
			hasError:  false,
		},
		{
			name:      "Invalid base64 token",
			args:      []string{"AUTH", "XOAUTH2"},
			responses: []string{"invalid-base64"},
			expected:  "",
			hasError:  true,
		},
		{
			name:      "Invalid token format",
			args:      []string{"AUTH", "XOAUTH2"},
			responses: []string{base64.StdEncoding.EncodeToString([]byte("invalid-format"))},
			expected:  "",
			hasError:  true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := newMockAuthConn(test.responses)
			username, err := handler.Authenticate(conn, test.args)

			if test.hasError {
				if err == nil {
					t.Errorf("Expected error for test %s", test.name)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for test %s: %v", test.name, err)
				}
				if username != test.expected {
					t.Errorf("Expected username %s, got %s", test.expected, username)
				}
			}
		})
	}
}

func TestIsValidAuth(t *testing.T) {
	tests := []struct {
		username string
		expected bool
	}{
		{"goodauth@example.com", true},
		{"user.goodauth@example.com", true},
		{"goodauth.user@example.com", true},
		{"badauth@example.com", false},
		{"user.badauth@example.com", false},
		{"badauth.user@example.com", false},
		{"normal@example.com", true},
		{"", true}, // Empty string should return true in our implementation
	}

	for _, test := range tests {
		t.Run(test.username, func(t *testing.T) {
			result := IsValidAuth(test.username)
			if result != test.expected {
				t.Errorf("IsValidAuth(%s) = %v, expected %v", test.username, result, test.expected)
			}
		})
	}
}

func TestAuthenticationFlow(t *testing.T) {
	// Test complete authentication flow for each mechanism
	mechanisms := []string{"PLAIN", "LOGIN", "CRAM-MD5", "CRAM-SHA256", "XOAUTH2"}

	for _, mechanism := range mechanisms {
		t.Run(mechanism, func(t *testing.T) {
			handler := NewHandler(mechanism)
			if handler == nil {
				t.Fatalf("Failed to create handler for mechanism %s", mechanism)
			}

			var args []string
			var responses []string

			switch mechanism {
			case "PLAIN":
				args = []string{"AUTH", "PLAIN", base64.StdEncoding.EncodeToString([]byte("\x00goodauth@example.com\x00password"))}
				responses = []string{}
			case "LOGIN":
				args = []string{"AUTH", "LOGIN"}
				responses = []string{
					base64.StdEncoding.EncodeToString([]byte("goodauth@example.com")),
					base64.StdEncoding.EncodeToString([]byte("password")),
				}
			case "CRAM-MD5", "CRAM-SHA256":
				args = []string{"AUTH", mechanism}
				responses = []string{base64.StdEncoding.EncodeToString([]byte("goodauth@example.com dummyhash"))}
			case "XOAUTH2":
				args = []string{"AUTH", "XOAUTH2"}
				responses = []string{base64.StdEncoding.EncodeToString([]byte("user=goodauth@example.com\x01auth=Bearer token123\x01\x01"))}
			}

			conn := newMockAuthConn(responses)
			username, err := handler.Authenticate(conn, args)

			if err != nil {
				t.Errorf("Authentication failed for mechanism %s: %v", mechanism, err)
			}

			if !strings.Contains(username, "goodauth") {
				t.Errorf("Expected username to contain 'goodauth' for mechanism %s, got %s", mechanism, username)
			}

			if !IsValidAuth(username) {
				t.Errorf("Expected valid authentication for mechanism %s with username %s", mechanism, username)
			}
		})
	}
}

func TestAuthenticationFailures(t *testing.T) {
	// Test authentication failures for each mechanism
	mechanisms := []string{"PLAIN", "LOGIN", "CRAM-MD5", "CRAM-SHA256", "XOAUTH2"}

	for _, mechanism := range mechanisms {
		t.Run(mechanism, func(t *testing.T) {
			handler := NewHandler(mechanism)
			if handler == nil {
				t.Fatalf("Failed to create handler for mechanism %s", mechanism)
			}

			var args []string
			var responses []string

			switch mechanism {
			case "PLAIN":
				args = []string{"AUTH", "PLAIN", base64.StdEncoding.EncodeToString([]byte("\x00badauth@example.com\x00password"))}
				responses = []string{}
			case "LOGIN":
				args = []string{"AUTH", "LOGIN"}
				responses = []string{
					base64.StdEncoding.EncodeToString([]byte("badauth@example.com")),
					base64.StdEncoding.EncodeToString([]byte("password")),
				}
			case "CRAM-MD5", "CRAM-SHA256":
				args = []string{"AUTH", mechanism}
				responses = []string{base64.StdEncoding.EncodeToString([]byte("badauth@example.com dummyhash"))}
			case "XOAUTH2":
				args = []string{"AUTH", "XOAUTH2"}
				responses = []string{base64.StdEncoding.EncodeToString([]byte("user=badauth@example.com\x01auth=Bearer token123\x01\x01"))}
			}

			conn := newMockAuthConn(responses)
			username, err := handler.Authenticate(conn, args)

			if err != nil {
				t.Errorf("Authentication parsing failed for mechanism %s: %v", mechanism, err)
			}

			if !strings.Contains(username, "badauth") {
				t.Errorf("Expected username to contain 'badauth' for mechanism %s, got %s", mechanism, username)
			}

			if IsValidAuth(username) {
				t.Errorf("Expected invalid authentication for mechanism %s with username %s", mechanism, username)
			}
		})
	}
}

func TestBase64Handling(t *testing.T) {
	// Test various base64 scenarios
	tests := []struct {
		name     string
		input    string
		hasError bool
	}{
		{"Valid base64", base64.StdEncoding.EncodeToString([]byte("test")), false},
		{"Invalid base64", "invalid-base64!", true},
		{"Empty string", "", true},
		{"Whitespace", "   ", true},
		{"Partial base64", "YWJj", false}, // "abc" in base64
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := &PlainHandler{}
			conn := newMockAuthConn([]string{})

			// Try to use the base64 input in a PLAIN auth
			args := []string{"AUTH", "PLAIN", test.input}
			_, err := handler.Authenticate(conn, args)

			if test.hasError {
				if err == nil {
					t.Errorf("Expected error for input %s", test.input)
				}
			} else {
				// For valid base64, we might still get an error due to format, but not due to base64 decoding
				if err != nil && strings.Contains(err.Error(), "illegal base64 data") {
					t.Errorf("Unexpected base64 decoding error for input %s: %v", test.input, err)
				}
			}
		})
	}
}
