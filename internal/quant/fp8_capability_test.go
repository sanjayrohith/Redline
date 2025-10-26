package quant_test

import (
	"context"
	"os/exec"
	"testing"

	"github.com/sanjayrohith/redline/internal/quant"
)

func TestDetectFP8Support(t *testing.T) {
	cases := []struct {
		computeCapability string
		want              bool
	}{
		{"7.5", false}, // Turing
		{"8.0", false}, // Ampere (A100)
		{"8.6", false}, // Ampere (this repo's own dev GPU)
		{"8.9", true},  // Ada Lovelace
		{"9.0", true},  // Hopper
		{"10.0", true}, // Blackwell
	}

	for _, tc := range cases {
		got, err := quant.DetectFP8Support(tc.computeCapability)
		if err != nil {
			t.Errorf("DetectFP8Support(%q) error = %v", tc.computeCapability, err)
			continue
		}
		if got != tc.want {
			t.Errorf("DetectFP8Support(%q) = %v, want %v", tc.computeCapability, got, tc.want)
		}
	}
}

func TestDetectFP8Support_RejectsUnparsableInput(t *testing.T) {
	if _, err := quant.DetectFP8Support("not-a-number"); err == nil {
		t.Fatal("DetectFP8Support() error = nil, want an error for unparsable input")
	}
}

func TestProbeNodeFP8Support_AgainstRealHardware(t *testing.T) {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		t.Skip("skipping: nvidia-smi not found on PATH")
	}

	got, err := quant.ProbeNodeFP8Support(context.Background())
	if err != nil {
		t.Fatalf("ProbeNodeFP8Support() error = %v", err)
	}

	// No fixed expectation here: this asserts the probe runs and parses
	// cleanly against whatever real GPU this node has, not a specific
	// compute capability that would break if run on different hardware.
	t.Logf("ProbeNodeFP8Support() = %v on this node's actual GPU", got)
}
