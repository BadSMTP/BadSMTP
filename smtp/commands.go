package smtp

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Command name constants
const (
	CmdHELO     = "HELO"
	CmdEHLO     = "EHLO"
	CmdAUTH     = "AUTH"
	CmdMAIL     = "MAIL"
	CmdRCPT     = "RCPT"
	CmdDATA     = "DATA"
	CmdBDAT     = "BDAT"
	CmdRSET     = "RSET"
	CmdNOOP     = "NOOP"
	CmdQUIT     = "QUIT"
	CmdSTARTTLS = "STARTTLS"
	CmdVRFY     = "VRFY"
)

// Command represents an SMTP command with its name and arguments.
type Command struct {
	Name string
	Args []string
}

// ParseCommand parses a line of text into an SMTP command.
func ParseCommand(line string) (*Command, error) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	return &Command{
		Name: strings.ToUpper(parts[0]),
		Args: parts[1:],
	}, nil
}

// IsValid checks if the command is valid.
func (c *Command) IsValid() bool {
	validCommands := map[string]bool{
		CmdHELO:     true,
		CmdEHLO:     true,
		CmdAUTH:     true,
		CmdMAIL:     true,
		CmdRCPT:     true,
		CmdDATA:     true,
		CmdBDAT:     true,
		CmdRSET:     true,
		CmdNOOP:     true,
		CmdQUIT:     true,
		CmdSTARTTLS: true,
		CmdVRFY:     true,
	}

	return validCommands[c.Name]
}

// ValidateArgs checks the number of arguments are correct for the given command.
func (c *Command) ValidateArgs() error {
	validators := map[string]func() error{
		CmdHELO: func() error {
			if len(c.Args) < 1 {
				return fmt.Errorf("501 Syntax error in parameters")
			}
			return nil
		},
		CmdEHLO: func() error {
			if len(c.Args) < 1 {
				return fmt.Errorf("501 Syntax error in parameters")
			}
			return nil
		},
		CmdAUTH: func() error {
			if len(c.Args) < 1 {
				return fmt.Errorf("501 Syntax error in parameters")
			}
			return nil
		},
		CmdMAIL: func() error {
			if len(c.Args) < 1 || !strings.HasPrefix(strings.ToUpper(c.Args[0]), "FROM:") {
				return fmt.Errorf("501 Syntax error in parameters")
			}
			return nil
		},
		CmdRCPT: func() error {
			if len(c.Args) < 1 || !strings.HasPrefix(strings.ToUpper(c.Args[0]), "TO:") {
				return fmt.Errorf("501 Syntax error in parameters")
			}
			return nil
		},
		CmdBDAT: func() error {
			if len(c.Args) < 1 {
				return fmt.Errorf("501 Syntax error in parameters")
			}
			// Optional second argument should be "LAST" if present
			if len(c.Args) > 1 && !strings.EqualFold(c.Args[1], "LAST") {
				return fmt.Errorf("501 Syntax error in parameters")
			}
			return nil
		},
		CmdVRFY: func() error {
			// VRFY accepts a single mailbox specification; allow multiple args and join them
			if len(c.Args) < 1 {
				return fmt.Errorf("501 Syntax error in parameters")
			}
			return nil
		},
	}

	if v, ok := validators[c.Name]; ok {
		return v()
	}
	return nil
}

// ExtractEmailAddress extracts an email address from a command argument.
// Preserves the original case of the email address.
func ExtractEmailAddress(arg string) string {
	// Remove FROM: or TO: prefix (case-insensitive) and angle brackets
	addr := arg
	upperArg := strings.ToUpper(arg)

	if strings.HasPrefix(upperArg, "FROM:") {
		addr = addr[5:] // Remove "FROM:" or "from:" etc.
	} else if strings.HasPrefix(upperArg, "TO:") {
		addr = addr[3:] // Remove "TO:" or "to:" etc.
	}

	addr = strings.Trim(addr, "<>")
	return addr
}

const (
	// MaxDomainLength is the RFC 1035 maximum length of a domain name
	MaxDomainLength = 255
	// MaxLocalPartLength is the RFC 5321 maximum length of local part in email address
	MaxLocalPartLength = 64
	// ExtendedCodeCaptureGroups is the expected number of regex capture groups for extended error codes
	ExtendedCodeCaptureGroups = 4
)

var (
	// Compiled domain validation regex with UTF-8 support
	// Supports internationalised domain names (IDN)
	domainRegex     *regexp.Regexp
	domainRegexOnce sync.Once
)

// getDomainRegex returns the compiled domain validation regex, initialising it once
func getDomainRegex() *regexp.Regexp {
	domainRegexOnce.Do(func() {
		// RE2 pattern for validating domains with UTF-8 support
		// Allows Unicode letters (\p{L}), numbers (\p{N}), and marks (\p{M})
		// Each label can be 1-63 characters, with hyphens allowed in the middle
		pattern := `^[\p{L}\p{N}\p{M}]` +
			`(?:[\p{L}\p{N}\p{M}-]{0,61}[\p{L}\p{N}\p{M}])?` +
			`(?:\.[\p{L}\p{N}\p{M}](?:[-\p{L}\p{N}\p{M}]{0,61}[\p{L}\p{N}\p{M}])?)*$`
		domainRegex = regexp.MustCompile(pattern)
	})
	return domainRegex
}

// ValidateDomain validates a domain name with UTF-8/internationalised domain support.
// This supports both ASCII domains (example.com) and internationalised domains (例え.jp).
func ValidateDomain(domain string) bool {
	if domain == "" {
		return false
	}

	// Check overall length (RFC 1035: max 255 octets)
	if len(domain) > MaxDomainLength {
		return false
	}

	re := getDomainRegex()
	return re.MatchString(domain)
}

// ValidateEmailAddress performs email address validation with UTF-8 domain support.
func ValidateEmailAddress(email string) bool {
	// Split email into local and domain parts
	atIndex := strings.LastIndex(email, "@")
	if atIndex == -1 || atIndex == 0 || atIndex == len(email)-1 {
		return false
	}

	localPart := email[:atIndex]
	domain := email[atIndex+1:]

	// Validate local part (simplified - allows common characters)
	// RFC 5321 allows more complex local parts, but this covers common cases.
	// Reuse package-level ASCII regex from smtp/address.go for consistency.
	if !asciiLocalRe.MatchString(localPart) {
		return false
	}

	// Check local part length (RFC 5321: max 64 octets)
	if len(localPart) > MaxLocalPartLength {
		return false
	}

	// Validate domain with UTF-8 support
	return ValidateDomain(domain)
}

// IsAllowedInState checks if a command is allowed in the specified SMTP state.
// This implements RFC 5321 command sequencing rules.
func (c *Command) IsAllowedInState(state State) bool {
	// Map of allowed states for each command
	allowed := map[string]map[State]bool{
		CmdHELO:     {StateHelo: true, StateMail: true},
		CmdEHLO:     {StateHelo: true, StateMail: true},
		CmdAUTH:     {StateMail: true, StateAuth: true},
		CmdMAIL:     {StateMail: true},
		CmdRCPT:     {StateRcpt: true},
		CmdDATA:     {StateRcpt: true},
		CmdBDAT:     {StateRcpt: true, StateBdat: true},
		CmdRSET:     {StateHelo: true, StateMail: true, StateRcpt: true, StateAuth: true},
		CmdNOOP:     {StateHelo: true, StateMail: true, StateRcpt: true, StateData: true, StateBdat: true, StateAuth: true},
		CmdQUIT:     {StateHelo: true, StateMail: true, StateRcpt: true, StateData: true, StateBdat: true, StateAuth: true},
		CmdSTARTTLS: {StateHelo: true, StateMail: true},
		// VRFY may be issued at any time and does not affect session state
		CmdVRFY: {
			StateGreeting: true,
			StateHelo:     true,
			StateAuth:     true,
			StateMail:     true,
			StateRcpt:     true,
			StateData:     true,
			StateBdat:     true,
			StateQuit:     true,
		},
	}

	if m, ok := allowed[c.Name]; ok {
		return m[state]
	}
	return false
}
