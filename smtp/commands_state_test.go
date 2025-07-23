package smtp

import "testing"

func TestStateTransitionsIncludeBdat(t *testing.T) {
	if !StateRcpt.CanTransitionTo(StateBdat) {
		t.Fatalf("expected StateRcpt to transition to StateBdat")
	}
	if !StateBdat.CanTransitionTo(StateMail) {
		t.Fatalf("expected StateBdat to transition to StateMail")
	}
}

func TestCommandAllowedInState_BdatAndOthers(t *testing.T) {
	cmd := &Command{Name: CmdBDAT}
	if !cmd.IsAllowedInState(StateRcpt) {
		t.Fatalf("BDAT should be allowed in StateRcpt")
	}
	if !cmd.IsAllowedInState(StateBdat) {
		t.Fatalf("BDAT should be allowed in StateBdat")
	}

	// HELO/EHLO
	helo := &Command{Name: CmdHELO}
	if !helo.IsAllowedInState(StateHelo) && !helo.IsAllowedInState(StateMail) {
		// ensure at least one allowed mapping exists (sanity)
		t.Fatalf("HELO should be allowed in Helo/Mail states")
	}
}
