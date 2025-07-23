package smtp

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode"
)

const maxASCII = 127

var asciiLocalRe = regexp.MustCompile(`^[a-zA-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+$`)
var angleAddrRe = regexp.MustCompile(`<([^>]+)>`)

// ExtractMailboxFromArg extracts a mailbox address from command arguments that may
// include prefixes like FROM: or TO:, angle brackets, or display names. It returns
// the raw mailbox (e.g. user@example.com) or empty string if none found.
func ExtractMailboxFromArg(arg string) string {
	// Remove FROM:/TO: prefixes if present (case-insensitive)
	upper := strings.ToUpper(arg)
	if strings.HasPrefix(upper, "FROM:") {
		arg = arg[5:]
	} else if strings.HasPrefix(upper, "TO:") {
		arg = arg[3:]
	}
	arg = strings.TrimSpace(arg)

	// Prefer net/mail parsing (handles display names and quoted local parts)
	if addr := ParseAddress(arg); addr != "" {
		return addr
	}

	// Fallback: stripped arg
	return strings.Trim(arg, "<>")
}

// NormaliseMailbox returns the mailbox in a canonical form where the local part
// preserves case and the domain is lowercased (common mailserver behaviour).
// If the input is empty, returns empty string.
func NormaliseMailbox(mailbox string) string {
	mailbox = strings.TrimSpace(mailbox)
	// Split on the last '@' to correctly handle quoted local parts that contain '@'
	at := strings.LastIndex(mailbox, "@")
	if at == -1 {
		return ""
	}
	local := mailbox[:at]
	domain := strings.ToLower(mailbox[at+1:])
	return local + "@" + domain
}

// isAllowedLocalRune reports whether r is an allowed character in an unquoted local part.
func isAllowedLocalRune(r rune, allowUTF8Local bool) bool {
	// Common allowed punctuation
	switch r {
	case '.', '!', '#', '$', '%', '&', '\'', '*', '+', '/', '=', '?', '^', '_', '`', '{', '|', '}', '~', '-':
		return true
	}
	if unicode.IsLetter(r) || unicode.IsNumber(r) {
		// If ASCII only is requested, ensure rune is ASCII
		if !allowUTF8Local && r > maxASCII {
			return false
		}
		return true
	}
	return false
}

// checkParsedAddress enforces allowUTF8Local on a parsed mail address string.
func checkParsedAddress(addr string, allowUTF8Local bool) bool {
	at := strings.LastIndex(addr, "@")
	if at == -1 {
		return false
	}
	local := addr[:at]
	if !allowUTF8Local {
		for _, r := range local {
			if r > maxASCII {
				return false
			}
		}
	}
	return true
}

// validateFallbackLocal validates local part when ParseAddress didn't succeed.
func validateFallbackLocal(local string, allowUTF8Local bool) bool {
	// Quoted local part
	if strings.HasPrefix(local, "\"") && strings.HasSuffix(local, "\"") {
		return len(local) >= 2
	}

	if allowUTF8Local {
		for _, r := range local {
			if !isAllowedLocalRune(r, true) {
				return false
			}
		}
		return true
	}

	return asciiLocalRe.MatchString(local)
}

// IsValidMailbox validates a mailbox address. If allowUTF8Local is false, the
// local part is restricted to ASCII characters only; otherwise Unicode letters
// are permitted in the local part. Domain validation is delegated to ValidateDomain.
func IsValidMailbox(mailbox string, allowUTF8Local bool) bool {
	mailbox = strings.TrimSpace(mailbox)
	// Prefer net/mail parsing
	if a, err := mail.ParseAddress(mailbox); err == nil {
		return checkParsedAddress(a.Address, allowUTF8Local)
	}
	if a2, err := mail.ParseAddress("<" + mailbox + ">"); err == nil {
		return checkParsedAddress(a2.Address, allowUTF8Local)
	}

	// Fallback path
	at := strings.LastIndex(mailbox, "@")
	if at == -1 {
		return false
	}
	local := mailbox[:at]
	domain := mailbox[at+1:]

	if local == "" || domain == "" {
		return false
	}
	if len(local) > MaxLocalPartLength || len(domain) > MaxDomainLength {
		return false
	}
	if !ValidateDomain(domain) {
		return false
	}

	return validateFallbackLocal(local, allowUTF8Local)
}

// ParseAddress attempts to extract a single mailbox address from a free-form
// argument. It supports forms like "Display Name <user@example.com>" or
// plain addresses like "user@example.com". It returns the address as-is
// (preserving case) or an empty string if none found.
func ParseAddress(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// Try to parse using net/mail which handles display names and quoted locals
	if a, err := mail.ParseAddress(raw); err == nil {
		return a.Address
	}

	// Fallback: try tokens (space-separated) and attempt to parse each
	for _, tok := range strings.Fields(raw) {
		trim := strings.Trim(tok, `<>,'"`)
		if strings.Contains(trim, "@") {
			if a2, err := mail.ParseAddress(trim); err == nil {
				return a2.Address
			}
			return trim
		}
	}
	return ""
}

// ExtractMailbox finds the first mailbox-like substring in raw and returns it.
// It prefers net/mail parsing and angle-bracket extraction, otherwise heuristically
// locates a substring containing '@' and trims surrounding punctuation.
func ExtractMailbox(raw string) string {
	if addr := ParseAddress(raw); addr != "" {
		return addr
	}

	raw = strings.TrimSpace(raw)
	if m := angleAddrRe.FindStringSubmatch(raw); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}

	// Heuristic fallback: find last '@' and expand until whitespace or common delimiters
	at := strings.LastIndex(raw, "@")
	if at == -1 {
		return ""
	}
	// expand left
	l := at - 1
	for l >= 0 {
		if isDelimiter(rune(raw[l])) {
			l++
			break
		}
		l--
	}
	if l < 0 {
		l = 0
	}
	// expand right
	r := at + 1
	for r < len(raw) {
		if isDelimiter(rune(raw[r])) {
			break
		}
		r++
	}
	candidate := strings.Trim(raw[l:r], `"'<> ,`)
	return candidate
}

// isDelimiter reports whether r is a delimiter that should stop mailbox expansion.
func isDelimiter(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	switch r {
	case '<', '>', '(', ')', ',', ';', ':':
		return true
	}
	return false
}
