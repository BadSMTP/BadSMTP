package server

import (
	"io"
	"strconv"
	"strings"
	"time"

	"badsmtp/logging"
	"badsmtp/smtp"
)

// xclientAdvertisedAttrs is the attribute list advertised after the XCLIENT
// keyword in EHLO responses.
const xclientAdvertisedAttrs = "NAME ADDR PORT PROTO HELO LOGIN"

// handleXclient implements the Postfix XCLIENT extension
// (https://www.postfix.org/XCLIENT_README.html). The client may override
// session attributes (NAME, ADDR, PORT, PROTO, HELO, LOGIN, DESTADDR,
// DESTPORT). Applied overrides re-trigger BadSMTP's behaviour analysis:
//
//   - PORT/DESTPORT re-runs AnalysePortBehaviour for the overridden port,
//     re-triggering greeting/drop delay behaviours,
//   - HELO re-runs the EHLO hostname capability parser (size…, auth…, no…,
//     helo<code> error simulation),
//   - LOGIN feeds the goodauth/badauth authentication patterns,
//   - NAME/ADDR feed logging and message metadata.
//
// On success the server responds 220 and resets the session to the greeting
// protocol stage (like Postfix), retaining TLS state and any attributes set
// by previous XCLIENT commands.
func (s *Session) handleXclient(cmd *smtp.Command) error {
	if s.config.DisableXclient {
		return s.writeResponse("502 Command not implemented")
	}

	attrs, err := smtp.ParseXclientArgs(cmd.Args)
	if err != nil {
		return s.writeResponse(err.Error())
	}

	// Decode all values (xtext, RFC 1891) before applying anything so a
	// bad value does not leave the session half-configured.
	decoded := make(map[string]string, len(attrs))
	for name, raw := range attrs {
		value, derr := smtp.DecodeXtext(raw)
		if derr != nil {
			return s.writeResponse("501 Bad XCLIENT attribute value encoding")
		}
		decoded[name] = value
	}

	// Validate value shapes.
	for name, value := range decoded {
		if verr := validateXclientAttrValue(name, value); verr != nil {
			return s.writeResponse(verr.Error())
		}
	}

	applied := make([]string, 0, len(decoded))
	for name, value := range decoded {
		if smtp.IsXclientUnavailable(value) {
			// Sentinels reset the override back to the real connection value.
			s.resetXclientAttr(name)
			applied = append(applied, name+"=[reset]")
			continue
		}
		s.setXclientAttr(name, value)
		applied = append(applied, name+"="+value)
	}
	s.finishXclient(applied)

	// Re-run behaviour analysis for the overridden attributes. This is the
	// core of XCLIENT integration with BadSMTP's test behaviours: a changed
	// PORT/DESTPORT can re-trigger greeting/drop delays, and a changed HELO
	// re-configures capabilities and error simulation.
	if dropped := s.reapplyPortBehaviour(); dropped {
		// Drop behaviour re-triggered: close without a response, mirroring
		// the drop delay ports.
		return io.EOF
	}

	// Reset to the initial greeting protocol stage (per Postfix), keeping
	// TLS state and previously applied XCLIENT attributes.
	s.resetToGreetingStage()

	// A HELO override re-runs the hostname-driven behaviour: HELO error
	// simulation (helo<code>) and the capability parser (size…, auth…,
	// no… toggles, dlay<N>). If the overridden hostname configures a HELO
	// error, it replaces the 220 success response.
	if helo, ok := decoded[smtp.XclientAttrHelo]; ok {
		if wrote, aerr := s.applyHeloBehaviour(helo); aerr != nil {
			return aerr
		} else if wrote {
			return nil
		}
	}

	return s.writeResponse(strconv.Itoa(smtp.Code220) + " badsmtp.test ESMTP " + ServerGreeting)
}

// setXclientAttr stores a decoded XCLIENT attribute value on the session.
// validateXclientAttrValue validates the shape of a decoded XCLIENT attribute
// value for the given attribute. Unavailable sentinels are always valid.
func validateXclientAttrValue(name, value string) error {
	if smtp.IsXclientUnavailable(value) {
		return nil
	}
	switch name {
	case smtp.XclientAttrPort, smtp.XclientAttrDestPort:
		return smtp.ValidateXclientPort(value)
	case smtp.XclientAttrAddr, smtp.XclientAttrDestAddr:
		return smtp.ValidateXclientAddr(value)
	case smtp.XclientAttrProto:
		return smtp.ValidateXclientProto(value)
	}
	return nil
}

// finishXclient logs the applied XCLIENT attributes.
func (s *Session) finishXclient(applied []string) {
	s.logger.LogBehaviourTriggered("xclient", s.config.Port, len(applied))
	s.logger.Debug("XCLIENT attributes applied",
		logging.F("attributes", strings.Join(applied, " ")))
}

func (s *Session) setXclientAttr(name, value string) {
	switch name {
	case smtp.XclientAttrName:
		s.xclientName = value
	case smtp.XclientAttrAddr:
		s.xclientAddr = value
	case smtp.XclientAttrPort:
		if v, err := strconv.Atoi(value); err == nil {
			s.xclientPort = v
		}
	case smtp.XclientAttrProto:
		s.xclientProto = strings.ToUpper(value)
	case smtp.XclientAttrHelo:
		s.xclientHelo = value
	case smtp.XclientAttrLogin:
		s.xclientLogin = value
	case smtp.XclientAttrDestAddr:
		s.xclientDestAddr = value
	case smtp.XclientAttrDestPort:
		if v, err := strconv.Atoi(value); err == nil {
			s.xclientDestPort = v
		}
	}
}

// resetXclientAttr clears an XCLIENT attribute override so the session falls
// back to the real connection value.
func (s *Session) resetXclientAttr(name string) {
	switch name {
	case smtp.XclientAttrName:
		s.xclientName = ""
	case smtp.XclientAttrAddr:
		s.xclientAddr = ""
	case smtp.XclientAttrPort:
		s.xclientPort = 0
	case smtp.XclientAttrProto:
		s.xclientProto = ""
	case smtp.XclientAttrHelo:
		s.xclientHelo = ""
	case smtp.XclientAttrLogin:
		s.xclientLogin = ""
	case smtp.XclientAttrDestAddr:
		s.xclientDestAddr = ""
	case smtp.XclientAttrDestPort:
		s.xclientDestPort = 0
	}
}

// effectiveXclientPort returns the port used for behaviour analysis after
// XCLIENT overrides: DESTPORT first, then PORT, then the real listener port.
func (s *Session) effectiveXclientPort() int {
	if s.xclientDestPort != 0 {
		return s.xclientDestPort
	}
	if s.xclientPort != 0 {
		return s.xclientPort
	}
	return s.config.Port
}

// reapplyPortBehaviour re-runs AnalysePortBehaviour against the effective
// (XCLIENT-overridden) port and applies the resulting greeting/drop delay
// behaviours to this session. Returns true when the drop behaviour was
// re-triggered and the connection should be dropped.
func (s *Session) reapplyPortBehaviour() bool {
	effectivePort := s.effectiveXclientPort()
	if effectivePort == s.config.Port {
		return false
	}

	// Analyse behaviour for the overridden port on a copy of the config so
	// the listener's own configuration is untouched.
	behaviour := *s.config
	behaviour.Port = effectivePort
	behaviour.GreetingDelay = 0
	behaviour.DropDelay = 0
	behaviour.DropImmediate = false
	behaviour.AnalysePortBehaviour()

	if behaviour.DropImmediate {
		s.logger.LogBehaviourTriggered("immediate_drop", effectivePort, 0)
		return true
	}
	if behaviour.DropDelay > 0 {
		s.logger.LogBehaviourTriggered("drop_delay", effectivePort, behaviour.DropDelay)
		time.Sleep(time.Duration(behaviour.DropDelay) * time.Second)
		return true
	}
	if behaviour.GreetingDelay > 0 {
		s.logger.LogBehaviourTriggered("greeting_delay", effectivePort, behaviour.GreetingDelay)
		time.Sleep(time.Duration(behaviour.GreetingDelay) * time.Second)
	}
	return false
}

// resetToGreetingStage resets the session to the greeting protocol stage as
// if the client had just connected, per the Postfix XCLIENT specification.
// TLS state and previously applied XCLIENT attributes are retained.
func (s *Session) resetToGreetingStage() {
	s.logger.LogStateTransition(s.state.String(), smtp.StateHelo.String(), "XCLIENT")
	s.state = smtp.StateHelo
	s.mailFrom = ""
	s.rcptTo = nil
	s.authenticated = false
	s.heloName = ""

	// Clear transaction-scoped error simulation results.
	s.dataErrorResult = nil
	s.rsetErrorResult = nil
	s.quitErrorResult = nil
	s.startTLSErrorResult = nil
	s.noopErrorResult = nil
	s.authErrorResult = nil

	// Expose overridden values to extensions via session metadata.
	s.metadata["xclient_name"] = s.xclientName
	s.metadata["xclient_addr"] = s.xclientAddr
	s.metadata["xclient_port"] = s.xclientPort
	s.metadata["xclient_proto"] = s.xclientProto
	s.metadata["xclient_helo"] = s.xclientHelo
	s.metadata["xclient_login"] = s.xclientLogin
	s.metadata["xclient_destaddr"] = s.xclientDestAddr
	s.metadata["xclient_destport"] = s.xclientDestPort
}

// applyHeloBehaviour re-runs the hostname-driven behaviour for an XCLIENT
// HELO override: HELO error simulation (helo<code>) and the capability
// parser (size…, auth…, no… toggles, dlay<N>). It returns (wrote, err);
// wrote=true means a response (HELO error / EHLO rejection) was written and
// the caller must not send the 220 success response.
func (s *Session) applyHeloBehaviour(hostname string) (bool, error) {
	s.heloName = hostname

	// HELO error simulation configured by the overridden hostname replaces
	// the 220 success response, exactly as it would for a real HELO/EHLO.
	if errorResult := smtp.ExtractHeloError(hostname); errorResult != nil {
		s.logger.LogErrorSimulation(errorResult.Code, hostname, smtp.CmdXCLIENT)
		return true, s.writeResponse(s.formatErrorResult(errorResult))
	}

	hostname = strings.ToLower(hostname)
	parts := parseCapabilityLabel(hostname)

	// EHLO rejection patterns (reject/noehl) also apply.
	if hasCapability(parts, "reject") || hasCapability(parts, "noehl") {
		return true, s.writeResponse("502 Command not implemented")
	}

	// Clear per-hostname session state derived from the previous HELO so the
	// overridden hostname fully re-configures the session.
	s.advertisedSize = 0
	s.commandDelay = 0

	// Re-apply dlay<N> from the overridden hostname.
	for _, p := range parts {
		if !strings.HasPrefix(p, "dlay") {
			continue
		}
		if v := parseDlayValue(p); v > 0 {
			s.commandDelay = v
			s.logger.LogBehaviourTriggered("command_delay", s.config.Port, v)
		}
	}

	// Re-run the standard capability parser (auth restrictions, SIZE, no…
	// toggles) and the CapabilityParser extension hook. The response lines
	// are discarded: no EHLO response is sent for an XCLIENT HELO override.
	_ = s.buildEhloResponseFromParts(hostname, parts)
	return false, nil
}

// effectiveClientIP returns the client IP honouring XCLIENT ADDR overrides,
// for use in logging and message metadata.
func (s *Session) effectiveClientIP() string {
	if s.xclientAddr != "" {
		return s.xclientAddr
	}
	return s.logger.GetClientIP()
}
