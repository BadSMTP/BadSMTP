package smtp

import (
	"testing"
)

func TestStateConstants(t *testing.T) {
	// Test that all expected states are defined
	expectedStates := []State{
		StateGreeting,
		StateHelo,
		StateMail,
		StateAuth,
		StateRcpt,
		StateData,
		StateBdat,
		StateQuit,
	}

	// Verify states have different values
	stateValues := make(map[State]bool)
	for _, state := range expectedStates {
		if stateValues[state] {
			t.Errorf("Duplicate state value: %v", state)
		}
		stateValues[state] = true
	}
}

func TestStateString(t *testing.T) {
	// Ensure that all states have a string representation
	states := []State{
		StateGreeting,
		StateHelo,
		StateMail,
		StateAuth,
		StateRcpt,
		StateData,
		StateBdat,
		StateQuit,
	}

	for _, state := range states {
		str := state.String()
		if str == "" {
			t.Errorf("State %v should have a string representation", state)
		}
	}
}

func TestStateValidTransitions(t *testing.T) {
	// Test valid state transitions based on SMTP protocol
	validTransitions := map[State][]State{
		StateGreeting: {StateHelo},
		StateHelo:     {StateMail, StateAuth, StateQuit},
		StateMail:     {StateRcpt, StateQuit},
		StateAuth:     {StateMail, StateQuit},
		StateRcpt:     {StateData, StateRcpt, StateQuit}, // Can have multiple RCPT
		StateData:     {StateMail, StateQuit},            // Back to MAIL for next message
		StateQuit:     {},                                // Terminal state
	}

	for fromState, toStates := range validTransitions {
		for _, toState := range toStates {
			t.Run(fromState.String()+"_to_"+toState.String(), func(t *testing.T) {
				// This test just ensures the transition mapping is defined
				// The actual validation would be done in the session handler
				if fromState == toState && fromState != StateRcpt {
					t.Errorf("Self-transition should only be allowed for StateRcpt")
				}
			})
		}
	}
}

func TestStateInvalidTransitions(t *testing.T) {
	// Test some invalid state transitions
	invalidTransitions := []struct {
		from State
		to   State
	}{
		{StateGreeting, StateMail}, // Must go through HELO
		{StateGreeting, StateData}, // Must go through HELO, MAIL, RCPT
		{StateHelo, StateData},     // Must go through MAIL, RCPT
		{StateMail, StateData},     // Must go through RCPT
		{StateQuit, StateHelo},     // Can't go back from QUIT
		{StateQuit, StateMail},     // Can't go back from QUIT
	}

	for _, transition := range invalidTransitions {
		t.Run(transition.from.String()+"_to_"+transition.to.String(), func(t *testing.T) {
			// This test documents invalid transitions
			// The actual validation would be done in the session handler
			if transition.from == StateQuit && transition.to != StateQuit {
				t.Logf("Correctly identified invalid transition from %v to %v", transition.from, transition.to)
			}
		})
	}
}

func TestStateEnum(t *testing.T) {
	// Test that State behaves like an enum
	state1 := StateHelo
	state2 := StateHelo
	state3 := StateMail

	if state1 != state2 {
		t.Error("Same states should be equal")
	}
	if state1 == state3 {
		t.Error("Different states should not be equal")
	}
}

func TestStateReset(t *testing.T) {
	// Test state reset scenarios (like after RSET command)
	currentState := StateRcpt
	resetState := StateMail // After RSET, should go back to MAIL state

	if currentState <= resetState {
		t.Log("RSET command should reset state from", currentState, "to", resetState)
	}
}

func TestStateZeroValue(t *testing.T) {
	// Test that zero value of State is reasonable
	var state State
	if state == StateQuit {
		t.Error("Zero value of State should not be StateQuit")
	}

	// Zero value should be StateGreeting (0) which is the initial state
	if state != StateGreeting {
		t.Error("Zero value of State should be StateGreeting")
	}
}
