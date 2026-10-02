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

// validCommands is the set of recognised SMTP command verbs.
var validCommands = map[string]struct{}{
	CmdHELO: {}, CmdEHLO: {}, CmdAUTH: {}, CmdMAIL: {}, CmdRCPT: {}, CmdDATA: {},
	CmdBDAT: {}, CmdRSET: {}, CmdNOOP: {}, CmdQUIT: {}, CmdSTARTTLS: {}, CmdVRFY: {},
}

// IsValid checks if the command is valid.
func (c *Command) IsValid() bool {
	_, ok := validCommands[c.Name]
	return ok
}

// errSyntax is the canonical "501" reply used by the argument validators.
func errSyntax() error {
	return fmt.Errorf("501 Syntax error in parameters")
}

// requireArg validates that the command has at least one argument.
func requireArg(c *Command) error {
	if len(c.Args) < 1 {
		return errSyntax()
	}
	return nil
}

// argValidators holds the per-command argument validators, built once.
var argValidators = map[string]func(*Command) error{
	CmdHELO: requireArg,
	CmdEHLO: requireArg,
	CmdAUTH: requireArg,
	CmdVRFY: requireArg, // VRFY accepts a single mailbox specification
	CmdMAIL: func(c *Command) error {
		if len(c.Args) < 1 || !strings.HasPrefix(strings.ToUpper(c.Args[0]), "FROM:") {
			return errSyntax()
		}
		return nil
	},
	CmdRCPT: func(c *Command) error {
		if len(c.Args) < 1 || !strings.HasPrefix(strings.ToUpper(c.Args[0]), "TO:") {
			return errSyntax()
		}
		return nil
	},
	CmdBDAT: func(c *Command) error {
		if len(c.Args) < 1 {
			return errSyntax()
		}
		// Optional second argument should be "LAST" if present
		if len(c.Args) > 1 && !strings.EqualFold(c.Args[1], "LAST") {
			return errSyntax()
		}
		return nil
	},
}

// ValidateArgs checks the number of arguments are correct for the given command.
func (c *Command) ValidateArgs() error {
	if v, ok := argValidators[c.Name]; ok {
		return v(c)
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
	localPart, domain, found := strings.CutLast(email, "@")
	if !found || localPart == "" || domain == "" {
		return false
	}

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

// allowedStates maps each command to the set of states in which it is permitted,
// implementing RFC 5321 command sequencing rules. Built once at package scope.
var allowedStates = map[string]map[State]bool{
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

// IsAllowedInState checks if a command is allowed in the specified SMTP state.
func (c *Command) IsAllowedInState(state State) bool {
	if m, ok := allowedStates[c.Name]; ok {
		return m[state]
	}
	return false
}
