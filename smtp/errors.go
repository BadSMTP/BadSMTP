// Package smtp provides error handling functionality for SMTP protocol.
package smtp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	extendedRegex = regexp.MustCompile(`^([a-z]+)(\d{3})_(\d+)\.(\d+)\.(\d+)@`)
	basicRegex    = regexp.MustCompile(`^([a-z]+)(\d{3})@`)
	heloRegex     = regexp.MustCompile(`^(?:helo|ehlo)(\d{3})\.`)
)

//nolint:revive // exported constants are intentionally grouped here
const (
	// extendedCodeMatchGroups is the expected number of regex capture groups for extended error codes
	// (full match, error code, class, subject, detail) = 5 groups
	// Supports multi-digit components like 5.7.509
	extendedCodeMatchGroups = 5

	// Common SMTP codes exported for callers to avoid magic numbers
	Code220 = 220
	Code221 = 221
	Code250 = 250
	Code354 = 354
	Code421 = 421
	Code450 = 450
	Code451 = 451
	Code452 = 452
	Code500 = 500
	Code501 = 501
	Code502 = 502
	Code503 = 503
	Code504 = 504
	Code521 = 521
	Code535 = 535
	Code550 = 550
	Code551 = 551
	Code552 = 552
	Code553 = 553
	Code554 = 554
	Code571 = 571
)

// Package-level mapping of standard SMTP error messages for reverse lookup.
var errorMessages = map[int]string{
	Code421: "Service not available, closing transmission channel",
	Code450: "Requested mail action not taken: mailbox unavailable",
	Code451: "Requested action aborted: local error in processing",
	Code452: "Requested action not taken: insufficient system storage",
	Code500: "Syntax error, command unrecognized", //nolint:misspell // RFC 5321 uses US spelling
	Code501: "Syntax error in parameters or arguments",
	Code502: "Command not implemented",
	Code503: "Bad sequence of commands",
	Code504: "Command parameter not implemented",
	Code521: "Machine does not accept mail",
	Code535: "Authentication failed",
	Code550: "Requested action not taken: mailbox unavailable",
	Code551: "User not local; please try forward path",
	Code552: "Requested mail action aborted: exceeded storage allocation",
	Code553: "Requested action not taken: mailbox name not allowed",
	Code554: "Transaction failed",
	Code571: "Blocked - see https://example.com/blocked",
}

// ErrorResult contains both the main error code and optional RFC2034 enhanced code
type ErrorResult struct {
	Code     int    // Main 3-digit error code (e.g., 550)
	Enhanced string // Optional enhanced code (e.g., "5.7.1")
	Message  string // Full error message
}

// CodeForMessage attempts to find an SMTP code whose standard message is contained in the provided msg.
// Returns (code, true) if found.
func CodeForMessage(msg string) (int, bool) {
	lower := strings.ToLower(msg)
	for code, m := range errorMessages {
		if strings.Contains(lower, strings.ToLower(m)) {
			return code, true
		}
	}
	return 0, false
}

// parsePrefixedError is a helper that handles the common pattern used by many
// Extract* functions: try an enhanced RFC2034 code pattern first, then a basic 3-digit code.
func parsePrefixedError(prefix, email string) *ErrorResult {
	email = strings.ToLower(email)

	// Enhanced form: <prefix><NNN>_<x>.<y>.<z>@...
	if matches := extendedRegex.FindStringSubmatch(email); len(matches) >= extendedCodeMatchGroups {
		if strings.EqualFold(matches[1], prefix) {
			// matches[2] is the 3-digit code per the pattern above
			if code, err := strconv.Atoi(matches[2]); err == nil {
				enhanced := fmt.Sprintf("%s.%s.%s", matches[3], matches[4], matches[5])
				message := fmt.Sprintf("%d %s %s", code, enhanced, GetErrorMessage(code))
				return &ErrorResult{Code: code, Enhanced: enhanced, Message: message}
			}
		}
	}

	// Basic form: <prefix><NNN>@...
	if matches := basicRegex.FindStringSubmatch(email); len(matches) > 1 {
		if strings.EqualFold(matches[1], prefix) {
			if code, err := strconv.Atoi(matches[2]); err == nil {
				return &ErrorResult{Code: code, Message: fmt.Sprintf("%d %s", code, GetErrorMessage(code))}
			}
		}
	}

	return nil
}

// ExtractMailFromError extracts error code from MAIL FROM addresses.
func ExtractMailFromError(email string) *ErrorResult { return parsePrefixedError("mail", email) }

// ExtractRcptToError extracts error code from RCPT TO addresses.
func ExtractRcptToError(email string) *ErrorResult { return parsePrefixedError("rcpt", email) }

// ExtractDataError extracts error code for DATA phase from MAIL FROM addresses.
func ExtractDataError(email string) *ErrorResult { return parsePrefixedError("data", email) }

// ExtractBdatError extracts error code for BDAT command from MAIL FROM addresses.
func ExtractBdatError(email string) *ErrorResult { return parsePrefixedError("bdat", email) }

// ExtractRsetError extracts error code for RSET command from MAIL FROM addresses.
func ExtractRsetError(email string) *ErrorResult { return parsePrefixedError("rset", email) }

// ExtractQuitError extracts error code for QUIT command from MAIL FROM addresses.
func ExtractQuitError(email string) *ErrorResult { return parsePrefixedError("quit", email) }

// ExtractStartTLSError extracts error code for STARTTLS command from MAIL FROM addresses.
func ExtractStartTLSError(email string) *ErrorResult { return parsePrefixedError("starttls", email) }

// ExtractNoopError extracts error code for NOOP command from MAIL FROM addresses.
func ExtractNoopError(email string) *ErrorResult { return parsePrefixedError("noop", email) }

// ExtractAuthError extracts error code for AUTH command from MAIL FROM addresses.
func ExtractAuthError(email string) *ErrorResult { return parsePrefixedError("auth", email) }

// ExtractHeloError extracts error code from HELO/EHLO hostname (different pattern).
func ExtractHeloError(hostname string) *ErrorResult {
	hostname = strings.ToLower(hostname)
	if matches := heloRegex.FindStringSubmatch(hostname); len(matches) > 1 {
		if code, err := strconv.Atoi(matches[1]); err == nil {
			return &ErrorResult{Code: code, Message: fmt.Sprintf("%d %s", code, GetErrorMessage(code))}
		}
	}
	return nil
}

// GetErrorMessage returns a standard SMTP error message for the given error code.
func GetErrorMessage(code int) string {
	if msg, exists := errorMessages[code]; exists {
		return msg
	}
	return "Unknown error"
}
