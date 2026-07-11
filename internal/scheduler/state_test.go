package scheduler

import (
	"errors"
	"testing"
)

func TestValidateTransition_LegalPath(t *testing.T) {
	path := []DeploymentState{
		StateQueued, StateProvisioning, StateWarming, StateReady, StateDraining, StateTerminated,
	}
	for i := 1; i < len(path); i++ {
		if err := ValidateTransition(path[i-1], path[i]); err != nil {
			t.Errorf("ValidateTransition(%s, %s) error = %v, want nil", path[i-1], path[i], err)
		}
	}
}

func TestValidateTransition_ProvisioningCanSkipWarmingDirectlyToReady(t *testing.T) {
	// When there is no intermediate warming observation, a Nomad "running" status
	// maps straight to ready; this must remain legal.
	if err := ValidateTransition(StateProvisioning, StateReady); err != nil {
		t.Errorf("ValidateTransition(provisioning, ready) error = %v, want nil", err)
	}
}

func TestValidateTransition_AnyStateCanFail(t *testing.T) {
	for _, s := range []DeploymentState{StateQueued, StateProvisioning, StateWarming, StateReady, StateDraining} {
		if err := ValidateTransition(s, StateFailed); err != nil {
			t.Errorf("ValidateTransition(%s, failed) error = %v, want nil", s, err)
		}
	}
}

func TestValidateTransition_IllegalSkips(t *testing.T) {
	illegal := [][2]DeploymentState{
		{StateQueued, StateReady},
		{StateQueued, StateDraining},
		{StateQueued, StateTerminated},
		{StateReady, StateProvisioning},
		{StateReady, StateQueued},
		{StateDraining, StateReady},
		{StateDraining, StateQueued},
	}
	for _, pair := range illegal {
		if err := ValidateTransition(pair[0], pair[1]); !errors.Is(err, ErrIllegalTransition) {
			t.Errorf("ValidateTransition(%s, %s) error = %v, want ErrIllegalTransition", pair[0], pair[1], err)
		}
	}
}

func TestValidateTransition_TerminalStatesRejectOutgoingTransitions(t *testing.T) {
	for _, terminal := range []DeploymentState{StateTerminated, StateFailed} {
		for _, target := range []DeploymentState{StateQueued, StateProvisioning, StateReady} {
			if err := ValidateTransition(terminal, target); !errors.Is(err, ErrIllegalTransition) {
				t.Errorf("ValidateTransition(%s, %s) error = %v, want ErrIllegalTransition", terminal, target, err)
			}
		}
	}
}

func TestValidateTransition_SameStateIsAlwaysANoop(t *testing.T) {
	for _, s := range []DeploymentState{StateQueued, StateProvisioning, StateWarming, StateReady, StateDraining, StateTerminated, StateFailed} {
		if err := ValidateTransition(s, s); err != nil {
			t.Errorf("ValidateTransition(%s, %s) error = %v, want nil (same-state no-op)", s, s, err)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	terminal := map[DeploymentState]bool{
		StateQueued:       false,
		StateProvisioning: false,
		StateWarming:      false,
		StateReady:        false,
		StateDraining:     false,
		StateTerminated:   true,
		StateFailed:       true,
	}
	for state, want := range terminal {
		if got := IsTerminal(state); got != want {
			t.Errorf("IsTerminal(%s) = %v, want %v", state, got, want)
		}
	}
}
