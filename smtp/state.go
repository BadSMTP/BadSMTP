// Package smtp provides SMTP protocol state management functionality.
package smtp

// State represents the current state of an SMTP session.
type State int

// SMTP session states.
const (
	// StateGreeting is the initial state when a client connects.
	StateGreeting State = iota

	// StateHelo is the state after receiving HELO/EHLO command.
	StateHelo

	// StateAuth is the state during authentication.
	StateAuth

	// StateMail is the state after MAIL FROM command.
	StateMail

	// StateRcpt is the state after RCPT TO command.
	StateRcpt

	// StateData is the state during DATA command.
	StateData

	// StateBdat is the state during BDAT chunking mode.
	StateBdat

	// StateQuit is the state after QUIT command.
	StateQuit
)

// String returns a string representation of the State.
func (s State) String() string {
	switch s {
	case StateGreeting:
		return "GREETING"
	case StateHelo:
		return "HELO"
	case StateAuth:
		return "AUTH"
	case StateMail:
		return "MAIL"
	case StateRcpt:
		return "RCPT"
	case StateData:
		return "DATA"
	case StateBdat:
		return "BDAT"
	case StateQuit:
		return "QUIT"
	default:
		return "UNKNOWN"
	}
}

// CanTransitionTo checks whether the state is allowed to transition to the specified next state.
func (s State) CanTransitionTo(next State) bool {
	transitions := map[State]map[State]bool{
		StateGreeting: {StateHelo: true},
		StateHelo:     {StateMail: true, StateAuth: true},
		StateAuth:     {StateMail: true},
		StateMail:     {StateRcpt: true, StateQuit: true},
		StateRcpt:     {StateData: true, StateBdat: true, StateRcpt: true},
		StateData:     {StateMail: true, StateQuit: true},
		StateBdat:     {StateBdat: true, StateMail: true, StateQuit: true},
		StateQuit:     {},
	}

	if m, ok := transitions[s]; ok {
		return m[next]
	}
	return false
}
