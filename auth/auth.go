// Package auth provides authentication functionality for BadSMTP.
package auth

import (
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // CRAM-MD5 is defined in terms of HMAC-MD5 (RFC 2195)
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/textproto"
	"regexp"
	"strings"
	"uuid"
)

var (
	oauthUserRe = regexp.MustCompile(`user=([^,\x01]+)`)
)

// Handler is the interface for authentication handlers.
//
// Prompts are written to w and client responses are read from r. r must be the
// session's own buffered reader so that bytes already pulled past the AUTH line
// (common with pipelining or TLS records) are not lost to a separate reader.
type Handler interface {
	Authenticate(w io.Writer, r *textproto.Reader, parts []string) (string, error)
}

// PlainHandler implements the PLAIN authentication mechanism.
type PlainHandler struct{}

// LoginHandler implements the LOGIN authentication mechanism.
type LoginHandler struct{}

// CramHandler implements the CRAM-MD5 and CRAM-SHA256 authentication mechanisms.
// Both variants share the same handshake; the server extracts the username from
// the client response without verifying the HMAC, so the handler is stateless.
type CramHandler struct{}

// XOAuth2Handler implements the XOAUTH2 authentication mechanism.
type XOAuth2Handler struct{}

// Authenticate handles PLAIN authentication.
func (h *PlainHandler) Authenticate(_ io.Writer, r *textproto.Reader, parts []string) (string, error) {
	var authData string

	// Check if auth data is provided in the command args (AUTH PLAIN <data>)
	if len(parts) >= 3 {
		authData = parts[2]
	} else if len(parts) == 2 {
		// Interactive mode - read the credential line from the session reader
		line, err := r.ReadLine()
		if err != nil {
			return "", fmt.Errorf("failed to read auth data: %w", err)
		}
		authData = strings.TrimSpace(line)
	} else {
		return "", fmt.Errorf("invalid PLAIN command")
	}

	if authData == "" {
		return "", fmt.Errorf("no auth data provided")
	}

	decoded, err := base64.StdEncoding.DecodeString(authData)
	if err != nil {
		return "", fmt.Errorf("invalid base64")
	}

	// PLAIN format: \0username\0password
	authParts := strings.SplitN(string(decoded), "\x00", 3)
	if len(authParts) != 3 {
		return "", fmt.Errorf("invalid PLAIN format")
	}

	return authParts[1], nil
}

// Authenticate handles LOGIN authentication.
func (h *LoginHandler) Authenticate(w io.Writer, r *textproto.Reader, _ []string) (string, error) {
	// Send username prompt
	usernamePrompt := "334 " + base64.StdEncoding.EncodeToString([]byte("Username:"))
	if _, err := w.Write([]byte(usernamePrompt + "\r\n")); err != nil {
		return "", err
	}

	usernameLine, err := r.ReadLine()
	if err != nil {
		return "", fmt.Errorf("failed to read username")
	}
	usernameB64 := strings.TrimSpace(usernameLine)
	username, err := base64.StdEncoding.DecodeString(usernameB64)
	if err != nil {
		return "", fmt.Errorf("invalid username encoding")
	}

	// Send password prompt
	passwordPrompt := "334 " + base64.StdEncoding.EncodeToString([]byte("Password:"))
	if _, err := w.Write([]byte(passwordPrompt + "\r\n")); err != nil {
		return "", err
	}

	passwordLine, err := r.ReadLine()
	if err != nil {
		return "", fmt.Errorf("failed to read password")
	}

	// We don't actually verify the password, just return username
	_ = strings.TrimSpace(passwordLine)
	return string(username), nil
}

// Authenticate handles CRAM-MD5 and CRAM-SHA256 authentication.
func (h *CramHandler) Authenticate(w io.Writer, r *textproto.Reader, _ []string) (string, error) {
	challenge := fmt.Sprintf("<%s@badsmtp.test>", uuid.New())
	challengeB64 := base64.StdEncoding.EncodeToString([]byte(challenge))

	response := "334 " + challengeB64
	if _, err := w.Write([]byte(response + "\r\n")); err != nil {
		return "", err
	}

	responseLine, err := r.ReadLine()
	if err != nil {
		return "", fmt.Errorf("failed to read response")
	}
	responseB64 := strings.TrimSpace(responseLine)
	decoded, err := base64.StdEncoding.DecodeString(responseB64)
	if err != nil {
		return "", fmt.Errorf("invalid response encoding")
	}

	// Parse username from response (format: "username hash")
	responseParts := strings.SplitN(string(decoded), " ", 2)
	if len(responseParts) != 2 {
		return "", fmt.Errorf("invalid response format")
	}

	return responseParts[0], nil
}

// Authenticate handles XOAUTH2 authentication.
func (h *XOAuth2Handler) Authenticate(w io.Writer, r *textproto.Reader, parts []string) (string, error) {
	var authDataB64 string

	// Check if auth data is provided in the command args (AUTH XOAUTH2 <data>)
	if len(parts) >= 3 {
		authDataB64 = parts[2]
	} else if len(parts) == 2 {
		// Interactive mode - send challenge and read from the session reader
		if _, err := w.Write([]byte("334 \r\n")); err != nil {
			return "", err
		}

		line, err := r.ReadLine()
		if err != nil {
			return "", fmt.Errorf("failed to read response")
		}
		authDataB64 = strings.TrimSpace(line)
	} else {
		return "", fmt.Errorf("invalid XOAUTH2 command")
	}

	if authDataB64 == "" {
		return "", fmt.Errorf("no auth data provided")
	}

	decoded, err := base64.StdEncoding.DecodeString(authDataB64)
	if err != nil {
		return "", fmt.Errorf("invalid base64")
	}

	// Extract username from OAuth2 string (simplified)
	authString := string(decoded)
	matches := oauthUserRe.FindStringSubmatch(authString)

	if len(matches) < 2 {
		return "", fmt.Errorf("username not found in OAuth2 string")
	}

	return matches[1], nil
}

const (
	// AuthMechanismPlain represents the PLAIN authentication mechanism.
	AuthMechanismPlain = "PLAIN"

	// AuthMechanismLogin represents the LOGIN authentication mechanism.
	AuthMechanismLogin = "LOGIN"

	// AuthMechanismCramMD5 represents the CRAM-MD5 authentication mechanism.
	AuthMechanismCramMD5 = "CRAM-MD5"

	// AuthMechanismCramSHA256 represents the CRAM-SHA256 authentication mechanism.
	AuthMechanismCramSHA256 = "CRAM-SHA256"

	// AuthMechanismXOAuth2 represents the XOAUTH2 authentication mechanism.
	AuthMechanismXOAuth2 = "XOAUTH2"
)

// NewHandler creates a new authentication handler for the specified mechanism.
func NewHandler(mechanism string) Handler {
	switch strings.ToUpper(mechanism) {
	case AuthMechanismPlain:
		return &PlainHandler{}
	case AuthMechanismLogin:
		return &LoginHandler{}
	case AuthMechanismCramMD5, AuthMechanismCramSHA256:
		return &CramHandler{}
	case AuthMechanismXOAuth2:
		return &XOAuth2Handler{}
	default:
		return nil
	}
}

// IsValidAuth checks if the provided username is valid for authentication.
func IsValidAuth(username string) bool {
	return !strings.Contains(username, "badauth")
}

// cramResponse builds the CRAM client response ("username space hex-digest")
// that a conforming client would send for the given HMAC hash function.
func cramResponse(newHash func() hash.Hash, username, password, challenge string) string {
	h := hmac.New(newHash, []byte(password))
	h.Write([]byte(challenge))
	return username + " " + hex.EncodeToString(h.Sum(nil))
}

// GenerateCramMD5Response generates a CRAM-MD5 (HMAC-MD5) client response.
func GenerateCramMD5Response(username, password, challenge string) string {
	return cramResponse(md5.New, username, password, challenge)
}

// GenerateCramSHA256Response generates a CRAM-SHA256 (HMAC-SHA256) client response.
func GenerateCramSHA256Response(username, password, challenge string) string {
	return cramResponse(sha256.New, username, password, challenge)
}

// RedactAuthArgs returns a copy of args safe for logging by redacting any
// credential/token payloads typically present in AUTH commands.
// Examples:
//   - AUTH PLAIN <base64> -> AUTH PLAIN [redacted]
//   - AUTH XOAUTH2 <base64> -> AUTH XOAUTH2 [redacted]
func RedactAuthArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	out := make([]string, len(args))
	copy(out, args)
	// AUTH mechanisms normally have the credential data in args[1]
	if len(out) > 1 {
		out[1] = "[redacted]"
	}
	return out
}
