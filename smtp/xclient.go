package smtp

import (
	"fmt"
	"strconv"
	"strings"
)

// XCLIENT attribute names as defined by the Postfix XCLIENT extension
// (https://www.postfix.org/XCLIENT_README.html). Names are case-insensitive.
const (
	XclientAttrName     = "NAME"
	XclientAttrAddr     = "ADDR"
	XclientAttrPort     = "PORT"
	XclientAttrProto    = "PROTO"
	XclientAttrHelo     = "HELO"
	XclientAttrLogin    = "LOGIN"
	XclientAttrDestAddr = "DESTADDR"
	XclientAttrDestPort = "DESTPORT"
)

// xclientAttributes is the set of XCLIENT attributes supported by the server.
var xclientAttributes = map[string]struct{}{
	XclientAttrName:     {},
	XclientAttrAddr:     {},
	XclientAttrPort:     {},
	XclientAttrProto:    {},
	XclientAttrHelo:     {},
	XclientAttrLogin:    {},
	XclientAttrDestAddr: {},
	XclientAttrDestPort: {},
}

// XCLIENT sentinel values for unavailable information. These are matched
// case-insensitively per the Postfix XCLIENT specification.
const (
	XclientUnavailable     = "[UNAVAILABLE]"
	XclientTempUnavailable = "[TEMPUNAVAIL]"
)

// IsXclientUnavailable reports whether the given raw XCLIENT attribute value is
// one of the "information unavailable" sentinels.
func IsXclientUnavailable(value string) bool {
	return strings.EqualFold(value, XclientUnavailable) || strings.EqualFold(value, XclientTempUnavailable)
}

// ParseXclientArgs parses the arguments of an XCLIENT command into a map of
// attribute name -> raw (still xtext-encoded) value. Attribute names are
// case-insensitive; each attribute may appear at most once.
func ParseXclientArgs(args []string) (map[string]string, error) {
	attrs := make(map[string]string)

	for _, arg := range args {
		name, value, found := strings.Cut(arg, "=")
		if !found {
			return nil, fmt.Errorf("501 Bad XCLIENT attribute syntax")
		}

		upper := strings.ToUpper(strings.TrimSpace(name))
		if _, ok := xclientAttributes[upper]; !ok {
			return nil, fmt.Errorf("501 Unsupported XCLIENT attribute %s", name)
		}
		if _, duplicate := attrs[upper]; duplicate {
			return nil, fmt.Errorf("501 Duplicate XCLIENT attribute %s", name)
		}

		attrs[upper] = value
	}

	return attrs, nil
}

// DecodeXtext decodes an xtext-encoded value as defined in RFC 1891:
// "+" followed by two hexadecimal digits encodes an octet. A malformed
// escape (non-hex digits or truncated) is an error.
func DecodeXtext(s string) (string, error) {
	if !strings.Contains(s, "+") {
		return s, nil
	}

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		if s[i] != '+' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", fmt.Errorf("truncated xtext escape at offset %d", i)
		}
		v, err := strconv.ParseUint(s[i+1:i+3], 16, 8)
		if err != nil {
			return "", fmt.Errorf("invalid xtext escape %q", s[i:i+3])
		}
		b.WriteByte(byte(v))
		i += 2
	}

	return b.String(), nil
}

// ValidateXclientPort validates a decoded XCLIENT PORT/DESTPORT value.
func ValidateXclientPort(portStr string) error {
	v, err := strconv.Atoi(portStr)
	if err != nil || v < 0 || v > 65535 {
		return fmt.Errorf("501 Bad XCLIENT port value %q", portStr)
	}
	return nil
}

// ValidateXclientAddr validates a decoded XCLIENT ADDR/DESTADDR value:
// an IPv4 address, an IPv6 address prefixed with IPV6:, or an unavailable
// sentinel (handled by the caller).
func ValidateXclientAddr(addr string) error {
	if strings.HasPrefix(strings.ToUpper(addr), "IPV6:") {
		return nil
	}
	// IPv4 dotted quad; net.ParseIP would also accept bare IPv6 which is
	// not valid here (IPv6 must be prefixed), so validate manually.
	const ipv4Octets = 4
	parts := strings.Split(addr, ".")
	if len(parts) != ipv4Octets {
		return fmt.Errorf("501 Bad XCLIENT address value %q", addr)
	}
	for _, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || v > 255 {
			return fmt.Errorf("501 Bad XCLIENT address value %q", addr)
		}
	}
	return nil
}

// ValidateXclientProto validates a decoded XCLIENT PROTO value: SMTP or ESMTP.
func ValidateXclientProto(proto string) error {
	p := strings.ToUpper(proto)
	if p != "SMTP" && p != "ESMTP" {
		return fmt.Errorf("501 Bad XCLIENT proto value %q", proto)
	}
	return nil
}
